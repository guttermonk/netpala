package dbus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netpala/common"
	"netpala/network"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// ToggleSecurityServiceCmd starts or stops a systemd unit and records the
// choice so a boot-time restore unit can replay it.
//
// StartUnit only queues a job, so the new state is not visible immediately;
// the refresh is deferred rather than read back straight away.
func ToggleSecurityServiceCmd(conn *dbus.Conn, svc common.SecurityService, configured []common.SecurityServiceConfig) tea.Cmd {
	return func() tea.Msg {
		mgr := conn.Object(network.SystemdDest, dbus.ObjectPath(network.SystemdPath))

		method, verb, past := network.SystemdMgrIF+".StartUnit", "start", "started"
		if svc.Active {
			method, verb, past = network.SystemdMgrIF+".StopUnit", "stop", "stopped"
		}

		var job dbus.ObjectPath
		if err := mgr.Call(method, 0, svc.Unit, "replace").Store(&job); err != nil {
			return common.ErrMsg{Err: describeUnitError(err, verb, svc.Unit)}
		}

		// Persisting is best-effort: the unit did change either way, the
		// choice just would not survive a reboot. Report it, don't fail.
		var cmds []tea.Cmd
		if svc.StateFile != "" {
			value := "on"
			if svc.Active {
				value = "off"
			}
			if err := writeStateFile(svc.StateFile, value); err != nil {
				cmds = append(cmds, func() tea.Msg {
					return common.ErrMsg{Err: fmt.Errorf(
						"%s %s but the choice was not recorded: %w", svc.Unit, past, err)}
				})
			}
		}

		// Give systemd a moment to run the job before reading state back.
		cmds = append(cmds, tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg {
			return common.SecurityUpdateMsg(network.GetSecurityServices(conn, configured))
		}))
		return tea.Batch(cmds...)
	}
}

// describeUnitError turns polkit's refusal into something actionable, since
// "interactive authentication required" tells the user nothing about the fix.
func describeUnitError(err error, verb, unit string) error {
	if strings.Contains(strings.ToLower(err.Error()), "interactive authentication") {
		return fmt.Errorf(
			"not permitted to %s %s - add a polkit rule allowing org.freedesktop.systemd1.manage-units for this unit",
			verb, unit)
	}
	return fmt.Errorf("failed to %s %s: %w", verb, unit, err)
}

// writeStateFile replaces the file atomically. A torn read by the boot-time
// restore unit would decide whether traffic goes through Tor, so a partial
// write is not acceptable.
func writeStateFile(path, value string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".netpala-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(value + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
