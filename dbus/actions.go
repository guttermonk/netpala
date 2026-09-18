package dbus

import (
	"fmt"
	"strings"
	"time" // Added time import

	"netpala/common"
	"netpala/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
	"github.com/google/uuid"
)

// connectToNetworkCmd tells NetworkManager to activate a connection on a specific device.
func ConnectToNetworkCmd(conn *dbus.Conn, connectionPath, devicePath dbus.ObjectPath) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))

		// The D-Bus method call to activate the connection.
		call := nm.Call(
			"org.freedesktop.NetworkManager.ActivateConnection",
			0,
			connectionPath,
			devicePath,
			dbus.ObjectPath("/"),
		)

		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to activate connection: %w", call.Err)}
		}
		// Success is handled by signal listener
		return nil
	}
}

// AddAndConnectToNetworkCmd adds a standard network and attempts connection.
func AddAndConnectToNetworkCmd(conn *dbus.Conn, net common.ScannedNetwork, password string, devicePath dbus.ObjectPath) tea.Cmd {
	return func() tea.Msg {
		// 1. Generate UUID
		newUUID, err := uuid.NewRandom()
		if err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to generate uuid: %w", err)}
		}

		// 2. Build settings map
		settings := map[string]map[string]dbus.Variant{
			"connection": {
				"id":          dbus.MakeVariant(net.SSID),
				"uuid":        dbus.MakeVariant(newUUID.String()),
				"type":        dbus.MakeVariant("802-11-wireless"),
				"autoconnect": dbus.MakeVariant(true),
			},
			"802-11-wireless": {
				"ssid":     dbus.MakeVariant([]byte(net.SSID)),
				"mode":     dbus.MakeVariant("infrastructure"),
				"security": dbus.MakeVariant("802-11-wireless-security"),
			},
			"ipv4": {"method": dbus.MakeVariant("auto")},
			"ipv6": {"method": dbus.MakeVariant("auto")},
		}
		securitySettings := make(map[string]dbus.Variant)
		switch net.Security {
		case "wpa3-sae":
			securitySettings["key-mgmt"] = dbus.MakeVariant("sae")
			securitySettings["psk"] = dbus.MakeVariant(password)
		case "wpa2-psk":
			securitySettings["key-mgmt"] = dbus.MakeVariant("wpa-psk")
			securitySettings["psk"] = dbus.MakeVariant(password)
		default:
			if net.Security != "open" {
				securitySettings["key-mgmt"] = dbus.MakeVariant("wpa-psk")
				securitySettings["psk"] = dbus.MakeVariant(password)
			}
		}
		if len(securitySettings) > 0 {
			settings["802-11-wireless-security"] = securitySettings
		}

		// 3. Add the connection via D-Bus
		settingsObj := conn.Object(network.NMDest, "/org/freedesktop/NetworkManager/Settings")
		call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.AddConnection", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to add connection: %w", call.Err)}
		}

		// 4. Get the path (optional)
		var newConnectionPath dbus.ObjectPath
		err = call.Store(&newConnectionPath) // Store error

		// 5. Create the delayed refresh command
		refreshCmd := tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
			return common.RefreshKnownNetworksMsg{}
		})

		// 6. Create the optimistic update message
		optimisticMsg := common.OptimisticAddMsg{
			SSID:     net.SSID,
			Security: net.Security, // Use the security string from scanned network
		}

		// 7. Batch commands based on success
		var batchCmds []tea.Cmd
		batchCmds = append(batchCmds, func() tea.Msg { return optimisticMsg }) // Send optimistic update first
		batchCmds = append(batchCmds, refreshCmd)                              // Schedule real refresh

		if err == nil {
			// If we got the path, attempt connection
			batchCmds = append(batchCmds, ConnectToNetworkCmd(conn, newConnectionPath, devicePath))
		} else {
			// If we didn't get the path, report the error but still refresh
			batchCmds = append(batchCmds, func() tea.Msg {
				return common.ErrMsg{Err: fmt.Errorf("added connection but failed to read path: %w", err)}
			})
		}
		// BatchMsg, not Batch: this is returned as a Msg, and the runtime only
		// dispatches BatchMsg. Returning a Cmd here silently drops it.
		return tea.BatchMsg(batchCmds)
	}
}

// AddAndConnectEAPCmd adds a WPA-EAP network and attempts connection.
func AddAndConnectEAPCmd(conn *dbus.Conn, config map[string]string, devicePath dbus.ObjectPath) tea.Cmd {
	return func() tea.Msg {
		// 1. Validate required fields
		ssid, ok := config["ssid"]
		if !ok || ssid == "" {
			return common.ErrMsg{Err: fmt.Errorf("EAP config is missing SSID")}
		}
		eapMethod, ok := config["eap"]
		if !ok || eapMethod == "" {
			return common.ErrMsg{Err: fmt.Errorf("EAP config is missing EAP method")}
		}
		identity, ok := config["identity"]
		if !ok || identity == "" {
			return common.ErrMsg{Err: fmt.Errorf("EAP config is missing identity")}
		}

		// 2. Generate UUID
		newUUID, err := uuid.NewRandom()
		if err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to generate UUID for EAP: %w", err)}
		}

		// 3. Build EAP settings
		eapSettings := map[string]dbus.Variant{
			"eap":      dbus.MakeVariant([]string{strings.ToLower(eapMethod)}),
			"identity": dbus.MakeVariant(identity),
			"password": dbus.MakeVariant(config["password"]),
		}
		if phase2, ok := config["phase2-auth"]; ok && phase2 != "" && phase2 != "NONE" {
			eapSettings["phase2-auth"] = dbus.MakeVariant(strings.ToLower(phase2))
		}
		if certPath, ok := config["ca_cert"]; ok && certPath != "" {
			eapSettings["ca-cert"] = dbus.MakeVariant("file://" + certPath)
		}

		// 4. Build complete settings map
		settings := map[string]map[string]dbus.Variant{
			"connection": {
				"id":          dbus.MakeVariant(ssid),
				"uuid":        dbus.MakeVariant(newUUID.String()),
				"type":        dbus.MakeVariant("802-11-wireless"),
				"autoconnect": dbus.MakeVariant(true),
			},
			"802-11-wireless": {
				"ssid":     dbus.MakeVariant([]byte(ssid)),
				"mode":     dbus.MakeVariant("infrastructure"),
				"security": dbus.MakeVariant("802-11-wireless-security"),
			},
			"802-11-wireless-security": {
				"key-mgmt": dbus.MakeVariant("wpa-eap"),
			},
			"802-1x": eapSettings,
			"ipv4":   {"method": dbus.MakeVariant("auto")},
			"ipv6":   {"method": dbus.MakeVariant("auto")},
		}

		// 5. Add the connection via D-Bus
		settingsObj := conn.Object(network.NMDest, "/org/freedesktop/NetworkManager/Settings")
		call := settingsObj.Call("org.freedesktop.NetworkManager.Settings.AddConnection", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to add EAP connection: %w", call.Err)}
		}

		// 6. Get the path (optional)
		var newConnectionPath dbus.ObjectPath
		err = call.Store(&newConnectionPath) // Store error

		// 7. Create the delayed refresh command
		refreshCmd := tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
			return common.RefreshKnownNetworksMsg{}
		})

		// 8. Create the optimistic update message
		optimisticMsg := common.OptimisticAddMsg{
			SSID:     ssid,
			Security: "wpa2-eap", // Generally correct assumption for EAP
		}

		// 9. Batch commands based on success
		var batchCmds []tea.Cmd
		batchCmds = append(batchCmds, func() tea.Msg { return optimisticMsg }) // Send optimistic update first
		batchCmds = append(batchCmds, refreshCmd)                              // Schedule real refresh

		if err == nil {
			// If we got the path, attempt connection
			batchCmds = append(batchCmds, ConnectToNetworkCmd(conn, newConnectionPath, devicePath))
		} else {
			// If we didn't get the path, report the error but still refresh
			batchCmds = append(batchCmds, func() tea.Msg {
				return common.ErrMsg{Err: fmt.Errorf("added EAP connection but failed to read path: %w", err)}
			})
		}
		// BatchMsg, not Batch: this is returned as a Msg, and the runtime only
		// dispatches BatchMsg. Returning a Cmd here silently drops it.
		return tea.BatchMsg(batchCmds)
	}
}

// ToggleVpnCmd activates or deactivates a VPN connection.
//
// connect is the state being asked for, not the state the profile is in, which
// matches ToggleVpnProviderCmd -- the two are called side by side off the same
// key, and an inverted flag here deactivates a profile the user is trying to
// bring up.
func ToggleVpnCmd(conn *dbus.Conn, vpnPath dbus.ObjectPath, activePath dbus.ObjectPath, connect bool) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
		var call *dbus.Call
		action := "activate" // For error message
		reported := vpnPath  // The path the error message should name

		if !connect {
			action = "deactivate"
			reported = activePath
			// "" is an unset path and "/" is NetworkManager's empty one; the
			// bus rejects both, the first as a malformed message rather than
			// as an error naming the connection.
			if activePath == "" || activePath == "/" {
				return common.ErrMsg{Err: fmt.Errorf("cannot deactivate VPN: no active connection path found")}
			}
			// Both halves go through the manager: the active connection object
			// carries state and properties but exports no methods of its own,
			// so there is nothing to call on activePath itself.
			call = nm.Call(
				"org.freedesktop.NetworkManager.DeactivateConnection",
				0,
				activePath, // Active connection path, not the saved profile
			)
		} else {
			// Activate using the *saved* connection path
			call = nm.Call(
				"org.freedesktop.NetworkManager.ActivateConnection",
				0,
				vpnPath,              // Saved connection path
				dbus.ObjectPath("/"), // device path is not needed for VPN
				dbus.ObjectPath("/"), // specific object path
			)
		}

		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to %s vpn connection '%s': %w", action, reported, call.Err)}
		}
		// Success handled by signal listener
		return nil
	}
}

// ToggleWifiCmd sets the master Wi-Fi radio state.
func ToggleWifiCmd(conn *dbus.Conn, enable bool) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
		call := nm.Call(
			"org.freedesktop.DBus.Properties.Set",
			0,
			network.NMDest,
			"WirelessEnabled",
			dbus.MakeVariant(enable),
		)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to set WirelessEnabled property: %w", call.Err)}
		}
		// Success handled by signal listener
		return nil
	}
}

// DeleteConnectionCmd tells NetworkManager to delete a saved connection profile.
func DeleteConnectionCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath) tea.Cmd {
	return func() tea.Msg {
		connObj := conn.Object(network.NMDest, connectionPath)
		call := connObj.Call(
			"org.freedesktop.NetworkManager.Settings.Connection.Delete",
			0,
		)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to delete connection %s: %w", connectionPath, call.Err)}
		}
		// Success handled by signal listener
		return nil
	}
}

// cleanSettingsForUpdate removes the deprecated address/route fields, which
// GetSettings returns in a legacy format that Update rejects. The modern
// "address-data"/"route-data" equivalents round-trip fine and are kept, so a
// profile using a static IP survives an update untouched.
func cleanSettingsForUpdate(settings map[string]map[string]dbus.Variant) {
	for _, section := range []string{"ipv4", "ipv6"} {
		if settings[section] == nil {
			continue
		}
		for _, field := range []string{"addresses", "routes"} {
			delete(settings[section], field)
		}
	}
}

// SetMacCmd rewrites a saved connection's MAC behaviour.
//
// Like the DNS switcher this re-activates the live connection, but here it is
// not optional: the address is chosen when the interface associates, so
// without a reconnect the change would not take effect until next time.
func SetMacCmd(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	devicePath dbus.ObjectPath,
	mode, explicit string,
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

		// Captured before the write overwrites it, so a driver that refuses
		// the new address can be put back where it was.
		prevMode := network.MACModeFromSettings(settings)
		prevExplicit := network.MACAddressFromSettings(settings)

		if err := applyMACToSettings(settings, mode, explicit); err != nil {
			return common.ErrMsg{Err: err}
		}

		call = connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update MAC settings: %w", call.Err)}
		}

		var cmds []tea.Cmd
		if reactivate && devicePath != "" && devicePath != "/" {
			cmds = append(cmds, ConnectToNetworkCmd(conn, connectionPath, devicePath))

			// Only worth watching when the address actually changes. Going
			// back to the mode the profile already had cannot fail this way,
			// and reverting it would be a no-op.
			if mode != prevMode || explicit != prevExplicit {
				cmds = append(cmds, VerifyMacCmd(conn, connectionPath, devicePath,
					mode, prevMode, prevExplicit, 0))
			}
		}
		cmds = append(cmds, func() tea.Msg {
			return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
		})
		// BatchMsg, not Batch: this is returned as a Msg, and the runtime only
		// dispatches BatchMsg. Returning a Cmd here silently drops it.
		return tea.BatchMsg(cmds)
	}
}

// applyMACToSettings writes the chosen MAC behaviour into a connection's
// settings.
//
// The keywords live in the string property "assigned-mac-address". Two legacy
// properties shadow it and are cleared here rather than left behind: the
// "cloned-mac-address" byte array, which NetworkManager still populates when
// an explicit address is set, and "mac-address-randomization", which nmcli
// sets alongside the keywords. Leaving either in place means a profile that
// once had an explicit address keeps carrying it.
//
// Clearing is done by writing an empty value, not by deleting the key:
// NetworkManager keeps the stored value for a property an update does not
// mention, which is what made the DNS switcher's "DHCP" option do nothing.
func applyMACToSettings(settings map[string]map[string]dbus.Variant, mode, explicit string) error {
	if settings[network.WirelessSetting] == nil {
		return fmt.Errorf("not a wireless connection")
	}
	w := settings[network.WirelessSetting]

	value := mode
	if mode == common.MACModeExplicit {
		addr, err := common.ParseMAC(explicit)
		if err != nil {
			return err
		}
		value = addr
	}

	// The two names are alternative D-Bus spellings of one NetworkManager
	// property, not independent fields. Sending both makes the legacy byte
	// array win, which silently discards the keyword - so it is removed from
	// the map and only the string form is written.
	delete(w, network.ClonedMACKey)
	w[network.AssignedMACKey] = dbus.MakeVariant(value)

	// The older randomization flag shadows the keyword when set, so clear it
	// back to "default" rather than leaving a contradictory pair behind.
	w[network.MACRandomizationKey] = dbus.MakeVariant(uint32(0))
	return nil
}

// applyDNSToSettings writes the chosen provider into a connection's settings.
//
// Kept separate from the D-Bus round trip so these rules can be tested
// directly. The bug that made "DHCP" appear to do nothing lived here, and was
// invisible to every test that only checked which commands were returned.
func applyDNSToSettings(settings map[string]map[string]dbus.Variant, provider common.DNSProvider, v4, v6 []string) error {
	// A profile without an IP section defaults to auto.
	for _, section := range []string{"ipv4", "ipv6"} {
		if settings[section] == nil {
			settings[section] = map[string]dbus.Variant{
				"method": dbus.MakeVariant("auto"),
			}
		}
	}

	// DHCP means "no explicit servers": an empty list, plus ignore-auto-dns
	// turned back off so the router's servers are used again.
	if provider.ID == common.DNSModeDHCP {
		settings["ipv4"]["dns"] = emptyV4DNS()
		settings["ipv6"]["dns"] = emptyV6DNS()
		for _, section := range []string{"ipv4", "ipv6"} {
			settings[section]["ignore-auto-dns"] = dbus.MakeVariant(false)
		}
		return nil
	}

	if len(v4) == 0 && len(v6) == 0 {
		return fmt.Errorf("no DNS servers to apply for %s", provider.Label)
	}

	if len(v4) > 0 && ipMethodAcceptsDNS(settings["ipv4"]) {
		variant, err := network.DNSv4Variant(v4)
		if err != nil {
			return err
		}
		settings["ipv4"]["dns"] = variant
		settings["ipv4"]["ignore-auto-dns"] = dbus.MakeVariant(true)
	}

	if len(v6) > 0 && ipMethodAcceptsDNS(settings["ipv6"]) {
		variant, err := network.DNSv6Variant(v6)
		if err != nil {
			return err
		}
		settings["ipv6"]["dns"] = variant
	} else {
		// Drop stale servers from a previous selection rather than mixing
		// them with the new provider.
		settings["ipv6"]["dns"] = emptyV6DNS()
	}

	// Suppress the router's IPv6 resolvers either way. A v4-only provider
	// (DNSCrypt on 127.0.0.1, a v4-only custom list) would otherwise leave
	// DHCPv6/RA servers in resolv.conf, and queries landing on those bypass
	// the chosen provider entirely.
	settings["ipv6"]["ignore-auto-dns"] = dbus.MakeVariant(true)
	return nil
}

// emptyV4DNS and emptyV6DNS clear a section's nameservers.
//
// Removing the key from the settings map is not enough: NetworkManager keeps
// the stored value for a property the update does not mention, so deleting
// "dns" left the old servers in place. Switching a network back to DHCP looked
// like it did nothing, because ignore-auto-dns went back to false while the
// explicit servers survived. The property has to be set to an empty list of
// the right D-Bus type.
func emptyV4DNS() dbus.Variant { return dbus.MakeVariant([]uint32{}) }
func emptyV6DNS() dbus.Variant { return dbus.MakeVariant([][]byte{}) }

// ipMethodAcceptsDNS reports whether an ipv4/ipv6 section's method allows
// nameservers. NetworkManager rejects an Update that sets "dns" on a disabled
// or link-local stack, which is common for ipv6 on locked-down profiles.
func ipMethodAcceptsDNS(sec map[string]dbus.Variant) bool {
	if sec == nil {
		return false
	}
	v, ok := sec["method"]
	if !ok {
		return true // absent method defaults to "auto"
	}
	switch m, _ := v.Value().(string); m {
	case "disabled", "ignore", "link-local":
		return false
	}
	return true
}

// SetDnsCmd rewrites a saved connection's nameservers to the given provider.
// DHCP clears the explicit servers and re-enables the ones the router supplies;
// every other provider pins its own and sets ignore-auto-dns. When the profile
// is the live connection it is re-activated so the change takes effect now
// rather than at the next reconnect.
func SetDnsCmd(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	devicePath dbus.ObjectPath,
	provider common.DNSProvider,
	v4, v6 []string,
	reactivate bool,
) tea.Cmd {
	return func() tea.Msg {
		if err := writeDNS(conn, connectionPath, provider, v4, v6); err != nil {
			return common.ErrMsg{Err: err}
		}

		// Re-activate so resolv.conf is rewritten immediately.
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
}

// SetVpnDnsCmd is the same write against a VPN profile.
//
// Two things differ, and both would be wrong if SetDnsCmd were reused. A VPN
// is not re-activated on a device -- it has to be taken down through its own
// active connection and brought back up, which is what actually rewrites
// resolv.conf. And the refresh afterwards has to repaint the VPN pane;
// returning KnownNetworksUpdateMsg would re-read every wireless profile and
// leave the row the user is looking at showing the old value.
func SetVpnDnsCmd(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	activePath dbus.ObjectPath,
	provider common.DNSProvider,
	v4, v6 []string,
	reactivate bool,
) tea.Cmd {
	return func() tea.Msg {
		if err := writeDNS(conn, connectionPath, provider, v4, v6); err != nil {
			return common.ErrMsg{Err: err}
		}

		refresh := func() tea.Msg { return common.VpnUpdateMsg(network.GetVpnData(conn)) }

		if !reactivate || activePath == "" || activePath == "/" {
			return tea.BatchMsg([]tea.Cmd{refresh})
		}

		// Down then up, sequenced: NetworkManager reads the profile when the
		// tunnel comes up, so a running tunnel keeps the old resolvers until it
		// is cycled. Batching the two would race, and bringing it up before it
		// is fully down leaves the old activation in place.
		return tea.BatchMsg([]tea.Cmd{
			tea.Sequence(
				ToggleVpnCmd(conn, connectionPath, activePath, false), // deactivate
				ToggleVpnCmd(conn, connectionPath, activePath, true),  // activate
				refresh,
			),
		})
	}
}

// writeDNS rewrites one saved connection's nameservers.
func writeDNS(
	conn *dbus.Conn,
	connectionPath dbus.ObjectPath,
	provider common.DNSProvider,
	v4, v6 []string,
) error {
	connObj := conn.Object(network.NMDest, connectionPath)

	var settings map[string]map[string]dbus.Variant
	call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to get connection settings: %w", call.Err)
	}
	if err := call.Store(&settings); err != nil {
		return fmt.Errorf("failed to parse connection settings: %w", err)
	}

	// Remove fields with incompatible types
	cleanSettingsForUpdate(settings)

	if err := applyDNSToSettings(settings, provider, v4, v6); err != nil {
		return err
	}

	if call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings); call.Err != nil {
		return fmt.Errorf("failed to update DNS settings: %w", call.Err)
	}
	return nil
}

// ToggleAutoConnectCmd toggles the autoconnect setting for a saved connection.
func ToggleAutoConnectCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath, currentValue bool) tea.Cmd {
	return func() tea.Msg {
		if err := setAutoconnect(conn, connectionPath, !currentValue); err != nil {
			return common.ErrMsg{Err: err}
		}
		return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
	}
}

// ToggleVpnAutoConnectCmd is the same write against a VPN profile, refreshing
// the VPN pane instead of the network list.
//
// Split rather than parameterised because the refresh is the whole difference:
// returning KnownNetworksUpdateMsg here would re-read every wireless profile,
// and leave the pane the user is looking at showing the old value until the
// 15-second tick caught up.
//
// Worth knowing before relying on it: for a `wireguard` profile autoconnect
// behaves like any other device connection, but for a `vpn` plugin profile
// NetworkManager's willingness to bring it up on its own has varied by version,
// and the dependable way to chain one to a network is connection.secondaries on
// that network's profile. netpala writes the setting and reports it back
// faithfully either way.
func ToggleVpnAutoConnectCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath, currentValue bool) tea.Cmd {
	return func() tea.Msg {
		if err := setAutoconnect(conn, connectionPath, !currentValue); err != nil {
			return common.ErrMsg{Err: err}
		}
		return common.VpnUpdateMsg(network.GetVpnData(conn))
	}
}

// setAutoconnect writes connection.autoconnect on a saved profile.
func setAutoconnect(conn *dbus.Conn, connectionPath dbus.ObjectPath, value bool) error {
	connObj := conn.Object(network.NMDest, connectionPath)

	var settings map[string]map[string]dbus.Variant
	call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
	if call.Err != nil {
		return fmt.Errorf("failed to get connection settings: %w", call.Err)
	}
	if err := call.Store(&settings); err != nil {
		return fmt.Errorf("failed to parse connection settings: %w", err)
	}

	// Remove fields with incompatible types
	cleanSettingsForUpdate(settings)

	if settings["connection"] == nil {
		settings["connection"] = make(map[string]dbus.Variant)
	}
	settings["connection"]["autoconnect"] = dbus.MakeVariant(value)

	if call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings); call.Err != nil {
		return fmt.Errorf("failed to update connection: %w", call.Err)
	}
	return nil
}

// ToggleHiddenCmd toggles the hidden setting for a saved wireless connection.
func ToggleHiddenCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath, currentValue bool) tea.Cmd {
	return func() tea.Msg {
		connObj := conn.Object(network.NMDest, connectionPath)

		// 1. Get current settings
		var settings map[string]map[string]dbus.Variant
		call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to get connection settings: %w", call.Err)}
		}
		if err := call.Store(&settings); err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to parse connection settings: %w", err)}
		}

		// 2. Remove fields with incompatible types
		cleanSettingsForUpdate(settings)

		// 3. Ensure 802-11-wireless section exists
		if settings["802-11-wireless"] == nil {
			return common.ErrMsg{Err: fmt.Errorf("connection is not a wireless connection")}
		}

		// 4. Toggle hidden
		settings["802-11-wireless"]["hidden"] = dbus.MakeVariant(!currentValue)

		// 5. Update the connection
		call = connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update connection: %w", call.Err)}
		}

		// 6. Return refresh
		return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
	}
}
