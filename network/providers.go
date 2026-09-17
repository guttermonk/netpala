package network

import (
	"netpala/common"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// GetVpnProviders reports the live state of each configured vendor VPN daemon.
//
// Providers whose unit is not installed are omitted rather than shown as
// missing, the same as the Security pane, so listing one in the defaults costs
// nothing on a machine that does not have it.
func GetVpnProviders(conn *dbus.Conn, configured []common.VpnProviderConfig) []common.VpnConnection {
	if len(configured) == 0 || conn == nil {
		return nil
	}
	mgr := conn.Object(SystemdDest, dbus.ObjectPath(SystemdPath))

	var out []common.VpnConnection
	seen := make(map[string]struct{}, len(configured))

	for _, c := range configured {
		if c.Unit == "" || c.Interface == "" {
			continue // not enough to show a truthful row
		}

		// LoadUnit rather than GetUnit: GetUnit fails for a unit that exists on
		// disk but is not currently loaded, which is the normal state for a
		// daemon that has not been started yet.
		var path dbus.ObjectPath
		if err := mgr.Call(SystemdMgrIF+".LoadUnit", 0, c.Unit).Store(&path); err != nil {
			continue
		}
		props := GetProps(conn.Object(SystemdDest, path), SystemdUnitIF)
		if props == nil {
			continue
		}
		if loadState, _ := props["LoadState"].Value().(string); loadState != "loaded" {
			continue // not installed
		}

		unit := c.Unit
		if id, _ := props["Id"].Value().(string); id != "" {
			unit = id
		}
		if _, dup := seen[unit]; dup {
			continue
		}
		seen[unit] = struct{}{}

		name := c.Name
		if name == "" {
			name = strings.TrimSuffix(unit, ".service")
		}

		// The whole point of reading the device rather than the unit: a daemon
		// can be active for hours having connected nothing, and that is its
		// normal idle state rather than a fault.
		connected := InterfaceIsUp(c.Interface)

		out = append(out, common.VpnConnection{
			Kind:       common.VpnKindDaemon,
			Name:       common.SanitizeSSID(name, "[?]"),
			ConnType:   "Daemon",
			Unit:       unit,
			Iface:      c.Interface,
			Connect:    c.Connect,
			Disconnect: c.Disconnect,
			Connected:  connected,
		})
	}
	return out
}

// sysClassNet is where the kernel lists network interfaces. A variable so the
// tests can point it somewhere they control.
var sysClassNet = "/sys/class/net"

// InterfaceIsUp reports whether a network interface exists and is up.
//
// Read from the kernel rather than from NetworkManager on purpose. NM
// enumerates devices it does not manage, but reports them all as state
// "unmanaged" whether the link is up or down -- so for a tunnel some vendor
// daemon created, which is exactly the case here, NM's state answers nothing.
// The interface flags do.
//
// What this does and does not tell you is worth being precise about, because
// it is the whole basis of a daemon row's "connected" marker: it says the
// tunnel device is present and up. For a provider that creates its device on
// connect and removes it on disconnect -- Mullvad works this way -- that is
// exactly right. For one that keeps a device around whenever its daemon runs,
// the row will read connected while the daemon is up, which is why netpala
// ships only providers of the first kind by default.
func InterfaceIsUp(name string) bool {
	if name == "" || strings.ContainsAny(name, "/\x00") {
		return false // never let a config value walk out of sysfs
	}

	raw, err := os.ReadFile(filepath.Join(sysClassNet, name, "flags"))
	if err != nil {
		return false // no such interface
	}

	// The file is the interface flags as hex, "0x1003\n". IFF_UP is bit 0.
	flags, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(
		strings.TrimSpace(string(raw)), "0x")), 16, 64)
	if err != nil {
		return false
	}
	const iffUp = 0x1
	return flags&iffUp != 0
}
