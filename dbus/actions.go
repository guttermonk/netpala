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
func ToggleVpnCmd(conn *dbus.Conn, vpnPath dbus.ObjectPath, activePath dbus.ObjectPath, active bool) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
		var call *dbus.Call
		action := "activate" // For error message

		if active {
			// Deactivate using the *active* connection path
			action = "deactivate"
			if activePath == "/" { // Sanity check
				return common.ErrMsg{Err: fmt.Errorf("cannot deactivate VPN: no active connection path found")}
			}
			activeConnObj := conn.Object(network.NMDest, activePath)
			// Note: Deactivate is on the Active connection interface, not the main NM interface
			call = activeConnObj.Call("org.freedesktop.NetworkManager.Connection.Active.Deactivate", 0)
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
			return common.ErrMsg{Err: fmt.Errorf("failed to %s vpn connection '%s': %w", action, vpnPath, call.Err)}
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

		// 3. Ensure the IP sections exist; a profile without one defaults to auto.
		for _, section := range []string{"ipv4", "ipv6"} {
			if settings[section] == nil {
				settings[section] = map[string]dbus.Variant{
					"method": dbus.MakeVariant("auto"),
				}
			}
		}

		// 4. Apply the provider. DHCP means "no explicit servers", which is the
		//    absence of the key plus ignore-auto-dns turned back off.
		if provider.ID == common.DNSModeDHCP {
			for _, section := range []string{"ipv4", "ipv6"} {
				delete(settings[section], "dns")
				settings[section]["ignore-auto-dns"] = dbus.MakeVariant(false)
			}
		} else {
			if len(v4) == 0 && len(v6) == 0 {
				return common.ErrMsg{Err: fmt.Errorf("no DNS servers to apply for %s", provider.Label)}
			}

			if len(v4) > 0 && ipMethodAcceptsDNS(settings["ipv4"]) {
				variant, err := network.DNSv4Variant(v4)
				if err != nil {
					return common.ErrMsg{Err: err}
				}
				settings["ipv4"]["dns"] = variant
				settings["ipv4"]["ignore-auto-dns"] = dbus.MakeVariant(true)
			}

			if len(v6) > 0 && ipMethodAcceptsDNS(settings["ipv6"]) {
				variant, err := network.DNSv6Variant(v6)
				if err != nil {
					return common.ErrMsg{Err: err}
				}
				settings["ipv6"]["dns"] = variant
			} else {
				// Drop stale servers from a previous selection rather than
				// mixing them with the new provider.
				delete(settings["ipv6"], "dns")
			}

			// Suppress the router's IPv6 resolvers either way. A v4-only
			// provider (DNSCrypt on 127.0.0.1, a v4-only custom list) would
			// otherwise leave DHCPv6/RA servers in resolv.conf, and queries
			// landing on those bypass the chosen provider entirely.
			settings["ipv6"]["ignore-auto-dns"] = dbus.MakeVariant(true)
		}

		// 5. Update the connection
		call = connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update DNS settings: %w", call.Err)}
		}

		// 6. Re-activate so resolv.conf is rewritten immediately.
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

// ToggleAutoConnectCmd toggles the autoconnect setting for a saved connection.
func ToggleAutoConnectCmd(conn *dbus.Conn, connectionPath dbus.ObjectPath, currentValue bool) tea.Cmd {
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

		// 3. Ensure connection section exists
		if settings["connection"] == nil {
			settings["connection"] = make(map[string]dbus.Variant)
		}

		// 4. Toggle autoconnect
		settings["connection"]["autoconnect"] = dbus.MakeVariant(!currentValue)

		// 5. Update the connection
		call = connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings)
		if call.Err != nil {
			return common.ErrMsg{Err: fmt.Errorf("failed to update connection: %w", call.Err)}
		}

		// 6. Return refresh
		return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
	}
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
