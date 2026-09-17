package dbus

import (
	"context"
	"fmt"
	"time"

	"netpala/common"
	"netpala/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// ToggleVpnProviderCmd connects or disconnects a vendor VPN daemon.
//
// Two steps, because the daemon and the tunnel are separate things. Starting
// mullvad-daemon connects nothing; it sits idle until told to connect. So
// connecting means "make sure the daemon is running, then ask it to connect",
// and the unit start is skipped when it is already up.
//
// Disconnecting only asks the daemon to disconnect. Stopping the unit as well
// would be netpala deciding to shut down a service the user may want running
// for other reasons -- and if they do want it stopped, the Security pane
// toggles units and this one does not.
func ToggleVpnProviderCmd(conn *dbus.Conn, provider common.VpnConnection, connect bool) tea.Cmd {
	return func() tea.Msg {
		argv := provider.Connect
		verb := "connect"
		if !connect {
			argv, verb = provider.Disconnect, "disconnect"
		}
		if len(argv) == 0 {
			return common.ErrMsg{Err: fmt.Errorf(
				"no %s command configured for %s", verb, provider.Name)}
		}

		if connect {
			if err := ensureUnitRunning(conn, provider.Unit); err != nil {
				return common.ErrMsg{Err: err}
			}
		}

		if err := common.RunCommand(context.Background(), argv); err != nil {
			return common.ErrMsg{Err: err}
		}

		// The command returns as soon as the daemon accepts the request; the
		// interface appears a moment later. Reading state back immediately
		// would show the old answer and leave the row wrong until the next
		// tick.
		return tea.BatchMsg{
			tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg {
				return common.RefreshVpnProvidersMsg{}
			}),
		}
	}
}

// ensureUnitRunning starts a unit that is not already active.
//
// Waiting for the job to finish rather than firing and forgetting: the connect
// command runs next and would fail against a daemon whose socket is not up
// yet, with an error about the CLI rather than about the daemon.
func ensureUnitRunning(conn *dbus.Conn, unit string) error {
	if unit == "" {
		return nil
	}

	mgr := conn.Object(network.SystemdDest, dbus.ObjectPath(network.SystemdPath))

	var path dbus.ObjectPath
	if err := mgr.Call(network.SystemdMgrIF+".LoadUnit", 0, unit).Store(&path); err != nil {
		return fmt.Errorf("cannot find %s: %w", unit, err)
	}
	props := network.GetProps(conn.Object(network.SystemdDest, path), network.SystemdUnitIF)
	if state, _ := props["ActiveState"].Value().(string); state == "active" {
		return nil
	}

	var job dbus.ObjectPath
	if err := mgr.Call(network.SystemdMgrIF+".StartUnit", 0, unit, "replace").Store(&job); err != nil {
		return describeUnitError(err, "start", unit)
	}

	// StartUnit queues the job. Poll briefly rather than sleeping a fixed
	// amount, so a daemon that starts quickly is not waited on needlessly and
	// a slow one still gets a chance.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		props := network.GetProps(conn.Object(network.SystemdDest, path), network.SystemdUnitIF)
		if state, _ := props["ActiveState"].Value().(string); state == "active" {
			return nil
		} else if state == "failed" {
			return fmt.Errorf("%s failed to start; see systemctl status %s", unit, unit)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become active in time", unit)
}
