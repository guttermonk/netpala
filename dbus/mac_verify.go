package dbus

import (
	"fmt"
	"netpala/common"
	"netpala/network"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// Wi-Fi association normally completes in a couple of seconds; a driver that
// rejects the address fails well inside that. The budget is generous enough
// that a slow-but-working reconnect is not mistaken for a failure.
const (
	macVerifyInterval = 700 * time.Millisecond
	macVerifyAttempts = 14 // a little under ten seconds
)

// VerifyMacCmd watches the activation started after a MAC change and undoes it
// if the device does not come back.
//
// Polling rather than subscribing is deliberate. A missed StateChanged signal
// here would leave the user with no network and no explanation - exactly the
// failure this exists to prevent - whereas a poll that runs one extra time
// costs nothing.
func VerifyMacCmd(
	conn *dbus.Conn,
	connectionPath, devicePath dbus.ObjectPath,
	attempted, prevMode, prevExplicit string,
	attempt int,
) tea.Cmd {
	return tea.Tick(macVerifyInterval, func(time.Time) tea.Msg {
		switch macVerdict(readDeviceState(conn, devicePath), attempt) {
		case macSucceeded:
			// Came back on the new address: nothing to undo.
			return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn))
		case macFailed:
			return revertMAC(conn, connectionPath, devicePath, attempted, prevMode, prevExplicit)
		}
		return tea.BatchMsg([]tea.Cmd{
			VerifyMacCmd(conn, connectionPath, devicePath, attempted, prevMode, prevExplicit, attempt+1),
		})
	})
}

type macVerdictResult int

const (
	macKeepWaiting macVerdictResult = iota
	macSucceeded
	macFailed
)

// macVerdict decides what an observed device state means for a MAC change.
//
// DISCONNECTED deliberately does not count as failure on its own: the device
// passes through it on every normal reconnect, so treating it as failure would
// revert a change that was about to work. A device that never reaches
// ACTIVATED is caught by the attempt budget instead.
func macVerdict(state, attempt int) macVerdictResult {
	switch state {
	case common.DeviceStateConnected:
		return macSucceeded
	case common.DeviceStateFailed, common.DeviceStateUnavailable:
		return macFailed
	}
	if attempt+1 >= macVerifyAttempts {
		return macFailed
	}
	return macKeepWaiting
}

// revertMAC restores the previous MAC behaviour and reconnects, so a driver
// that refuses the change costs a few seconds rather than the connection.
func revertMAC(
	conn *dbus.Conn,
	connectionPath, devicePath dbus.ObjectPath,
	attempted, prevMode, prevExplicit string,
) tea.Msg {
	result := common.MacRevertedMsg{
		Iface:     interfaceName(conn, devicePath),
		Attempted: attempted,
		PrevMode:  prevMode,
	}

	connObj := conn.Object(network.NMDest, connectionPath)

	var settings map[string]map[string]dbus.Variant
	call := connObj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0)
	if call.Err == nil && call.Store(&settings) == nil {
		cleanSettingsForUpdate(settings)
		if applyMACToSettings(settings, prevMode, prevExplicit) == nil {
			update := connObj.Call(
				"org.freedesktop.NetworkManager.Settings.Connection.Update", 0, settings)
			if update.Err == nil {
				nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
				activate := nm.Call("org.freedesktop.NetworkManager.ActivateConnection", 0,
					connectionPath, devicePath, dbus.ObjectPath("/"))
				result.Restored = activate.Err == nil
			}
		}
	}

	return tea.BatchMsg([]tea.Cmd{
		func() tea.Msg { return result },
		func() tea.Msg { return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn)) },
	})
}

func readDeviceState(conn *dbus.Conn, devicePath dbus.ObjectPath) int {
	v, err := conn.Object(network.NMDest, devicePath).GetProperty(network.DevStateProp)
	if err != nil {
		return common.DeviceStateUnknown
	}
	raw, _ := v.Value().(uint32)
	return network.DeviceStateFromNM(raw)
}

func interfaceName(conn *dbus.Conn, devicePath dbus.ObjectPath) string {
	v, err := conn.Object(network.NMDest, devicePath).GetProperty(network.DevIfaceProp)
	if err != nil {
		return "the device"
	}
	if s, _ := v.Value().(string); s != "" {
		return s
	}
	return "the device"
}

// MacRevertedText is the message shown when a MAC change had to be undone.
//
// Kept here rather than inline so the wording is testable: the whole point of
// this path is that the user is told what happened, and a silent or misleading
// message would be the same bug in a different place.
func MacRevertedText(msg common.MacRevertedMsg) string {
	attempted := common.MACModeLabel(msg.Attempted)
	restored := common.MACModeLabel(msg.PrevMode)

	if msg.Restored {
		return fmt.Sprintf(
			"%s would not take a %s MAC address - reverted to %s and reconnected",
			msg.Iface, attempted, restored)
	}
	return fmt.Sprintf(
		"%s would not take a %s MAC address - reverted to %s, but reconnecting failed",
		msg.Iface, attempted, restored)
}
