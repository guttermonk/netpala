package dbus

import (
	"netpala/common"
	"netpala/network"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// WaitForDBusSignal waits for one signal worth acting on and returns the
// refreshes it calls for, with a fresh copy of itself at the head of the batch.
//
// Re-arming here rather than in the handlers is what keeps the reader count at
// one. It used to be the handlers' job, and the batch a device signal produces
// contains both a DeviceUpdateMsg and a KnownNetworksUpdateMsg -- each of which
// returned a listener, so one signal armed two readers and every device state
// change left an extra one parked on the channel for good. A connection emits a
// run of StateChanged, so the count grew with every connect, and a burst then
// fanned out into that many concurrent full refreshes, all querying
// NetworkManager while NetworkManager was busy bringing the link up.
//
// Being first in the batch also closes the window the old arrangement left:
// the batch runs its commands concurrently, so the next reader is waiting while
// the refreshes are still in flight, rather than only once one of them has
// landed back in Update.
//
// Signals netpala does not care about are skipped in the loop below instead of
// going round through the runtime, so the uninteresting majority -- every
// PropertiesChanged on the bus reaches this channel, most of them systemd's --
// costs nothing but the comparison.
func WaitForDBusSignal(conn *dbus.Conn, sig chan *dbus.Signal) tea.Cmd {
	return func() tea.Msg {
		for {
			s := <-sig // Block for next signal

			if refresh := signalRefresh(conn, s); refresh != nil {
				return append(tea.BatchMsg{WaitForDBusSignal(conn, sig)}, refresh...)
			}
		}
	}
}

// signalRefresh is the work a signal calls for, or nil if it is not one of ours.
func signalRefresh(conn *dbus.Conn, s *dbus.Signal) []tea.Cmd {
	devices := func() tea.Msg { return common.DeviceUpdateMsg(network.GetDevicesData(conn)) }
	known := func() tea.Msg { return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn)) }
	// VPN status can depend on device state, so it is re-read alongside it.
	vpn := func() tea.Msg { return common.VpnUpdateMsg(network.GetVpnData(conn)) }

	switch s.Name {
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		// Body[0] is the interface whose properties changed. The manager
		// carries WirelessEnabled; the two device interfaces carry the state
		// and scanning flags the Device pane shows.
		if len(s.Body) == 0 {
			return nil
		}
		iface, ok := s.Body[0].(string)
		if !ok {
			return nil
		}
		if iface != network.NMDest && iface != network.DevIF && iface != network.WifiIF {
			return nil
		}
		return []tea.Cmd{devices, vpn}

	case "org.freedesktop.NetworkManager.DeviceAdded",
		"org.freedesktop.NetworkManager.DeviceRemoved",
		"org.freedesktop.NetworkManager.Device.StateChanged":
		// Device state changes definitely affect connectivity.
		return []tea.Cmd{devices, known, vpn}

	case "org.freedesktop.NetworkManager.Settings.NewConnection",
		"org.freedesktop.NetworkManager.Settings.ConnectionRemoved":
		// Adding or removing a profile affects Known Networks and the VPN list.
		return []tea.Cmd{known, vpn}

	case "org.freedesktop.NetworkManager.Device.Wireless.AccessPointAdded",
		"org.freedesktop.NetworkManager.Device.Wireless.AccessPointRemoved":
		// Scan results *might* have changed. Trigger the debounce.
		return []tea.Cmd{func() tea.Msg { return common.ScannedNetworksUpdateMsg(nil) }}
	}
	return nil
}

// Command to periodically trigger a full data refresh.
func RefreshTicker() tea.Cmd {
	return tea.Tick(15*time.Second, func(t time.Time) tea.Msg {
		return common.PeriodicRefreshMsg{}
	})
}

func RequestScan(conn *dbus.Conn) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
		var devPaths []dbus.ObjectPath
		// Perform the D-Bus call, return error if it fails
		if err := nm.Call(network.NMDest+".GetDevices", 0).Store(&devPaths); err != nil {
			return common.ErrMsg{Err: err}
		}

		// Trigger scan on WiFi devices
		scanRequested := false
		for _, devPath := range devPaths {
			devObj := conn.Object(network.NMDest, devPath)
			devProps := network.GetProps(devObj, network.DevIF)
			if network.IsWifiType(devProps["DeviceType"]) {
				_ = devObj.Call(network.WifiIF+".RequestScan", 0, map[string]dbus.Variant{})
				scanRequested = true // Mark that we asked at least one device
			}
		}

		if !scanRequested {
			// Optional: Inform user if no WiFi device was found to scan
			// return common.ErrMsg{Err: fmt.Errorf("no wifi device found to scan")}
		}

		// Success: Scan requested, return nil. The signal listener will handle updates.
		return nil
	}
}

func GetScanResults(conn *dbus.Conn) tea.Cmd {
	return func() tea.Msg {
		nm := conn.Object(network.NMDest, dbus.ObjectPath(network.NMPath))
		var devPaths []dbus.ObjectPath
		if err := nm.Call(network.NMDest+".GetDevices", 0).Store(&devPaths); err != nil {
			return common.ErrMsg{Err: err}
		}

		for _, devPath := range devPaths {
			devObj := conn.Object(network.NMDest, devPath)
			devProps := network.GetProps(devObj, network.DevIF)
			// The scan that follows a radio toggle is exactly when a device
			// goes missing mid-loop, and this is the read that used to panic
			// on it. See network.IsWifiType.
			if network.IsWifiType(devProps["DeviceType"]) {
				_ = devObj.Call(network.WifiIF+".RequestScan", 0, map[string]dbus.Variant{})
			}
		}

		return common.ScannedNetworksUpdateMsg(network.GetScannedNetworks(conn))
	}
}

func RefreshAllData(conn *dbus.Conn) tea.Cmd {
	return tea.Batch(
		GetScanResults(conn),
		func() tea.Msg { return common.DeviceUpdateMsg(network.GetDevicesData(conn)) },
		func() tea.Msg { return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn)) },
		func() tea.Msg { return common.VpnUpdateMsg(network.GetVpnData(conn)) },
	)
}
