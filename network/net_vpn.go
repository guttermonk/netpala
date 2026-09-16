package network

import (
	"fmt"
	"netpala/common"
	"strings"

	"github.com/godbus/dbus/v5"
)

func GetVpnData(c *dbus.Conn) []common.VpnConnection {
	var vpnList []common.VpnConnection

	nm := c.Object(NMDest, NMPath)
	settingsObj := c.Object(NMDest, "/org/freedesktop/NetworkManager/Settings")

	// 1. Get all active connections and map the saved path to the active path.
	activeConnPathsVariant, err := nm.GetProperty(NMDest + ".ActiveConnections")
	if err != nil {
		fmt.Printf("Error getting active connections: %v\n", err)
		return nil
	}
	activeConnPaths, _ := activeConnPathsVariant.Value().([]dbus.ObjectPath)
	// Map a saved connection path to its active connection path
	activeConnections := make(map[dbus.ObjectPath]dbus.ObjectPath)
	for _, acPath := range activeConnPaths {
		acObj := c.Object(NMDest, acPath)
		connProp, err := acObj.GetProperty("org.freedesktop.NetworkManager.Connection.Active.Connection")
		if err == nil {
			if connPath, ok := connProp.Value().(dbus.ObjectPath); ok {
				activeConnections[connPath] = acPath // Map saved path -> active path
			}
		}
	}

	// 2. Get all saved connection profiles.
	var savedConnPaths []dbus.ObjectPath
	if err := settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&savedConnPaths); err != nil {
		fmt.Printf("Error listing connections: %v\n", err)
		return nil
	}

	// 3. Iterate through saved connections and find the VPNs.
	for _, path := range savedConnPaths {
		connObj := c.Object(NMDest, path)
		var settings map[string]map[string]dbus.Variant
		if err := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings); err != nil {
			continue
		}

		connSettings, ok := settings["connection"]
		if !ok {
			continue
		}

		connTypeVar, ok := connSettings["type"]
		if !ok {
			continue
		}

		connType, _ := connTypeVar.Value().(string)

		if connType == "wireguard" || connType == "vpn" {
			name, _ := connSettings["id"].Value().(string)

			friendlyType := "VPN"
			if connType == "wireguard" {
				friendlyType = "WireGuard"
			} else if vpnSettings, vpnOk := settings["vpn"]; vpnOk {
				if vpnTypeVar, vpnTypeOk := vpnSettings["service-type"]; vpnTypeOk {
					vpnTypeValue, _ := vpnTypeVar.Value().(string)
					parts := strings.Split(vpnTypeValue, ".")
					friendlyType = strings.ToUpper(parts[len(parts)-1])
				}
			}

			// Check if this connection is active and get its active path.
			activePath, isConnected := activeConnections[path]

			vpnList = append(vpnList, common.VpnConnection{
				Path:        path,
				ActivePath:  activePath, // Store the active path
				Name:        common.SanitizeSSID(name, "[?]"),
				ConnType:    friendlyType,
				Connected:   isConnected,
				AutoConnect: autoconnectFrom(connSettings),
				Endpoint:    vpnEndpoint(connType, settings),
			})
		}
	}

	return vpnList
}

// autoconnectFrom reads connection.autoconnect.
//
// NetworkManager omits a property that sits at its default, and the default
// here is true. So an absent key means the profile will come up on its own --
// reporting the zero value instead would tell the user the opposite of what
// the profile does.
func autoconnectFrom(connSettings map[string]dbus.Variant) bool {
	v, ok := connSettings["autoconnect"]
	if !ok {
		return true
	}
	b, ok := v.Value().(bool)
	if !ok {
		return true
	}
	return b
}

// vpnEndpoint reports the far end of the tunnel, or "" when the profile does
// not name one.
//
// The two connection types keep it in unrelated places: a wireguard profile
// carries it per peer, while a VPN plugin buries it in an opaque string map
// whose key is the plugin's own business. Neither is guaranteed to be there --
// a wireguard peer with no endpoint is a valid listen-only config -- so an
// empty result is normal rather than an error.
func vpnEndpoint(connType string, settings map[string]map[string]dbus.Variant) string {
	if connType == "wireguard" {
		return wireguardEndpoint(settings["wireguard"])
	}
	return pluginEndpoint(settings["vpn"])
}

// wireguardEndpoint returns the first peer's endpoint, noting how many other
// peers there are.
//
// Showing only the first would be a lie for a mesh config, where every peer is
// equally the far end; the count keeps the row honest without giving a
// single-peer commercial profile a suffix it does not need.
func wireguardEndpoint(wg map[string]dbus.Variant) string {
	peersVar, ok := wg["peers"]
	if !ok {
		return ""
	}
	peers, ok := peersVar.Value().([]map[string]dbus.Variant)
	if !ok || len(peers) == 0 {
		return ""
	}

	endpoint, _ := peers[0]["endpoint"].Value().(string)
	if endpoint == "" {
		return ""
	}
	if len(peers) > 1 {
		return fmt.Sprintf("%s +%d", endpoint, len(peers)-1)
	}
	return endpoint
}

// pluginEndpoint digs the server out of a VPN plugin's data map.
//
// The key is chosen by the plugin rather than by NetworkManager, so there is
// nothing to look it up in - openvpn says "remote", openconnect and l2tp say
// "gateway", vpnc says "IPSec gateway". The list is ordered by how common the
// plugin is, and an unrecognised plugin simply shows no endpoint rather than
// guessing at whichever value happens to look like a hostname.
func pluginEndpoint(vpn map[string]dbus.Variant) string {
	dataVar, ok := vpn["data"]
	if !ok {
		return ""
	}
	data, ok := dataVar.Value().(map[string]string)
	if !ok {
		return ""
	}

	for _, key := range []string{"remote", "gateway", "IPSec gateway", "host", "remote-addr", "right"} {
		if v := strings.TrimSpace(data[key]); v != "" {
			return v
		}
	}
	return ""
}
