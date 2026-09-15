package network

import (
	"netpala/common"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	SystemdDest   = "org.freedesktop.systemd1"
	SystemdPath   = "/org/freedesktop/systemd1"
	SystemdMgrIF  = "org.freedesktop.systemd1.Manager"
	SystemdUnitIF = "org.freedesktop.systemd1.Unit"
)

// GetSecurityServices reports the live state of each configured unit.
//
// Units that are not installed are omitted rather than shown as "missing", so
// the Security pane stays hidden entirely until there is something real to
// toggle. State is read from systemd every refresh instead of being cached,
// because a toggle made with systemctl must not leave the UI lying.
func GetSecurityServices(conn *dbus.Conn, configured []common.SecurityServiceConfig) []common.SecurityService {
	if len(configured) == 0 || conn == nil {
		return nil
	}
	mgr := conn.Object(SystemdDest, dbus.ObjectPath(SystemdPath))

	var out []common.SecurityService
	byUnit := make(map[string]struct{}, len(configured))
	for _, c := range configured {
		if c.Unit == "" {
			continue
		}

		// LoadUnit rather than GetUnit: GetUnit fails for a unit that exists
		// on disk but is not currently loaded, which is the normal state for
		// a dormant toggle.
		var path dbus.ObjectPath
		if err := mgr.Call(SystemdMgrIF+".LoadUnit", 0, c.Unit).Store(&path); err != nil {
			continue
		}

		props := GetProps(conn.Object(SystemdDest, path), SystemdUnitIF)
		if props == nil {
			continue
		}

		// "not-found" means it is not installed; "masked" means it cannot be
		// started. Neither is worth a row.
		if loadState, _ := props["LoadState"].Value().(string); loadState != "loaded" {
			continue
		}

		// Resolve to the canonical name. systemd unit renames leave the old
		// name behind as an alias, so a config naming either one loads the
		// same unit -- but the two strings are not interchangeable elsewhere:
		// a polkit rule matches the name it is handed, and a NixOS override of
		// the alias defines a second, unrelated unit instead of changing the
		// real one. Using Id everywhere keeps netpala on the name systemd
		// itself uses.
		unitName := c.Unit
		if id, _ := props["Id"].Value().(string); id != "" {
			unitName = id
		}
		if _, seen := byUnit[unitName]; seen {
			continue // an alias of something already listed
		}
		byUnit[unitName] = struct{}{}

		activeState, _ := props["ActiveState"].Value().(string)
		subState, _ := props["SubState"].Value().(string)

		name := c.Name
		if name == "" {
			name = strings.TrimSuffix(unitName, ".service")
		}

		out = append(out, common.SecurityService{
			Name:        name,
			Unit:        unitName,
			StateFile:   c.StateFile,
			ProvidesDNS: c.ProvidesDNS,
			Active:      activeState == "active",
			State:       activeState,
			SubState:    subState,
		})
	}
	return out
}

// AppliedResolvers reports the nameservers NetworkManager has actually applied
// for the active connections.
//
// Comparing these against /etc/resolv.conf is how netpala tells "NM is driving
// DNS" apart from "something outside NM overrode it". The profile alone cannot
// answer that: a profile on DHCP looks identical whether the router's servers
// are in use or a distro-level setting replaced them.
func AppliedResolvers(conn *dbus.Conn) []string {
	if conn == nil {
		return nil
	}
	nm := conn.Object(NMDest, dbus.ObjectPath(NMPath))

	var devs []dbus.ObjectPath
	if nm.Call(NMDest+".GetDevices", 0).Store(&devs) != nil {
		return nil
	}

	var out []string
	for _, d := range devs {
		obj := conn.Object(NMDest, d)

		ip4Var, err := obj.GetProperty(DevIF + ".Ip4Config")
		if err != nil {
			continue
		}
		ip4, ok := ip4Var.Value().(dbus.ObjectPath)
		if !ok || ip4 == "/" || ip4 == "" {
			continue
		}

		// NameserverData is the modern form; it carries the address as a
		// string so there is no byte-order guesswork.
		nsVar, err := conn.Object(NMDest, ip4).
			GetProperty("org.freedesktop.NetworkManager.IP4Config.NameserverData")
		if err != nil {
			continue
		}
		entries, ok := nsVar.Value().([]map[string]dbus.Variant)
		if !ok {
			continue
		}
		for _, e := range entries {
			if addr, ok := e["address"]; ok {
				if s, ok := addr.Value().(string); ok && s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return out
}
