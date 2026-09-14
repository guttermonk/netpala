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

		activeState, _ := props["ActiveState"].Value().(string)
		subState, _ := props["SubState"].Value().(string)

		name := c.Name
		if name == "" {
			name = strings.TrimSuffix(c.Unit, ".service")
		}

		out = append(out, common.SecurityService{
			Name:        name,
			Unit:        c.Unit,
			StateFile:   c.StateFile,
			ProvidesDNS: c.ProvidesDNS,
			Active:      activeState == "active",
			State:       activeState,
			SubState:    subState,
		})
	}
	return out
}
