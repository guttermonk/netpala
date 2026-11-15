package dbus

import (
	"fmt"
	"strings"

	"netpala/common"
	"netpala/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// Editing a saved profile's credentials.
//
// What a profile holds and what the forms show are the same fields, so these
// go through the same writers the add path uses -- the CA certificate's
// encoding and the password's ownership flags are stated once, in actions.go,
// and a profile written by an edit is the same shape as one written by a join.
//
// Two rules are particular to editing, and both are what the forms say they
// are. An empty password means the stored secret is left alone, because the
// form cannot show a secret it was not allowed to read and a blank field must
// not be mistaken for an instruction. And a live profile is re-activated after
// the write, as the DNS and MAC editors do: NetworkManager keeps the running
// connection on the settings it was activated with, so without that the new
// key does nothing until the next connect.

// LoadEapProfileCmd reads a saved EAP profile so the form can open on it.
//
// The secret is asked for separately and its absence is not an error:
// GetSecrets only answers a caller polkit allows, and a form that opens with
// an empty password field is already saying the right thing.
func LoadEapProfileCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath, ssid string) tea.Cmd {
	return func() tea.Msg {
		connObj := conn.Object(network.NMDest, connectionPath)

		var settings map[string]map[string]dbus.Variant
		call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to read profile: %w", call.Err)}
		}
		if err := call.Store(&settings); err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to parse profile: %w", err)}
		}

		profile := eapProfileFromSettings(settings)
		profile.Path = connectionPath
		profile.SSID = ssid

		var secrets map[string]map[string]dbus.Variant
		secretsCall := connObj.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.GetSecrets", 0, "802-1x")
		if secretsCall.Err == nil && secretsCall.Store(&secrets) == nil {
			if pw, ok := secrets["802-1x"]["password"]; ok {
				profile.Password, _ = pw.Value().(string)
			}
		}

		return profile
	}
}

// eapProfileFromSettings reads the fields the form shows out of a profile.
//
// The two selectors are upper case because that is how the form lists them,
// and the certificate loses the scheme and the terminator it is stored with so
// that what comes back out of the field is what went in.
func eapProfileFromSettings(settings map[string]map[string]dbus.Variant) common.EapProfileMsg {
	var profile common.EapProfileMsg

	sec, ok := settings["802-1x"]
	if !ok {
		return profile
	}

	if v, ok := sec["eap"]; ok {
		if methods, ok := v.Value().([]string); ok && len(methods) > 0 {
			profile.EapMethod = strings.ToUpper(methods[0])
		}
	}
	if v, ok := sec["phase2-auth"]; ok {
		if s, ok := v.Value().(string); ok {
			profile.Phase2Auth = strings.ToUpper(s)
		}
	}
	if v, ok := sec["identity"]; ok {
		profile.Identity, _ = v.Value().(string)
	}
	if v, ok := sec["ca-cert"]; ok {
		if b, ok := v.Value().([]byte); ok {
			profile.CaCert = strings.TrimRight(
				strings.TrimPrefix(string(b), "file://"), "\x00")
		}
	}

	return profile
}

// UpdateSecretCmd replaces the pre-shared key of a saved PSK or SAE profile.
//
// An empty password writes nothing at all. The alternative -- storing the
// empty string -- would leave a profile that can never associate, which is a
// strange thing for a popup saying "leave blank to keep the current one" to do.
func UpdateSecretCmd(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	devicePath dbus.ObjectPath,
	password string,
	reactivate bool,
) tea.Cmd {
	return func() tea.Msg {
		if password == "" {
			return nil
		}

		connObj := conn.Object(network.NMDest, connectionPath)

		var settings map[string]map[string]dbus.Variant
		call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to get connection settings: %w", call.Err)}
		}
		if err := call.Store(&settings); err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to parse connection settings: %w", err)}
		}

		cleanSettingsForUpdate(settings)
		if err := applySecretToSettings(settings, password); err != nil {
			return common.ErrMsg{Err: err}
		}

		if call := connObj.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		); call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update the password: %w", call.Err)}
		}

		return afterEdit(conn, connectionPath, devicePath, reactivate)
	}
}

// applySecretToSettings writes the key into whichever section the profile
// keeps it in.
//
// The property is "psk" for SAE as well as for WPA2-PSK, so the key management
// is left exactly as it was: this edit changes the key, not what the network
// is. A profile with no security section has no key to change, and saying so
// is better than adding one and guessing which mechanism it should name.
func applySecretToSettings(settings map[string]map[string]dbus.Variant, password string) error {
	sec, ok := settings["802-11-wireless-security"]
	if !ok {
		return fmt.Errorf("this network has no password to change")
	}
	sec["psk"] = dbus.MakeVariant(password)
	// Owned by the system, like the key written when the network was joined.
	// A profile whose flags say an agent holds the secret would otherwise go
	// on saying so, and the key written here would never be used.
	sec["psk-flags"] = dbus.MakeVariant(uint32(0))
	return nil
}

// UpdateEapCmd writes edited 802-1x credentials back to a saved profile.
func UpdateEapCmd(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	devicePath dbus.ObjectPath,
	config map[string]string,
	reactivate bool,
) tea.Cmd {
	return func() tea.Msg {
		connObj := conn.Object(network.NMDest, connectionPath)

		var settings map[string]map[string]dbus.Variant
		call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to get connection settings: %w", call.Err)}
		}
		if err := call.Store(&settings); err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to parse connection settings: %w", err)}
		}

		cleanSettingsForUpdate(settings)
		applyEapEditToSettings(settings, config)

		if call := connObj.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings,
		); call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update the EAP settings: %w", call.Err)}
		}

		return afterEdit(conn, connectionPath, devicePath, reactivate)
	}
}

// applyEapEditToSettings writes the form's 802-1x fields over a profile's.
//
// The method and identity are written when given and left alone when not, so a
// form that failed to read one of them cannot blank it. The password is the
// same rule, stated once in the popup: empty means the stored secret stays.
func applyEapEditToSettings(settings map[string]map[string]dbus.Variant, config map[string]string) {
	sec := eap1xSection(settings)

	if method := config["eap"]; method != "" {
		sec["eap"] = dbus.MakeVariant([]string{strings.ToLower(method)})
	}
	if identity := config["identity"]; identity != "" {
		sec["identity"] = dbus.MakeVariant(identity)
	}

	// The inner method and the certificate are cleared when the form clears
	// them, which is the whole point of being able to edit them.
	applyEapToSettings(settings, config)

	if password := config["password"]; password != "" {
		applyEapPasswordToSettings(settings, password)
	}
}

// afterEdit is what every credential write ends with: the live connection put
// back on its new settings, and the pane repainted from NetworkManager.
func afterEdit(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	devicePath dbus.ObjectPath,
	reactivate bool,
) tea.Msg {
	var cmds []tea.Cmd
	if reactivate && devicePath != "" && devicePath != "/" {
		cmds = append(cmds, ConnectToNetworkCmd(conn, connectionPath, devicePath))
	}
	cmds = append(cmds, func() tea.Msg {
		return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
	})
	// BatchMsg, not Batch: this is returned as a Msg, and the runtime only
	// dispatches BatchMsg. Returning a Cmd here silently drops it.
	return tea.BatchMsg(cmds)
}
