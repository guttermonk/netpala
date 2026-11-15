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
		// carries WirelessEnabled; the two device interfaces carry the device
		// state and the scanning flag.
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
		// A scan starting and finishing is two of these, and the only thing
		// either one changes is a flag the signal is already carrying. Reading
		// it out of the body is both cheaper than a device and VPN re-read for
		// something no connection depends on, and the only way to get it right:
		// the round trip regularly lands after NetworkManager has finished
		// scanning, so both signals re-read Scanning as false and it never
		// appears to have been true at all.
		if iface == network.WifiIF {
			if scanning, ok := scanningFlag(s); ok {
				return []tea.Cmd{func() tea.Msg {
					return common.ScanningStateMsg{DevicePath: s.Path, Scanning: scanning}
				}}
			}
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

// scanningFlag is the Scanning property out of a PropertiesChanged body, and
// whether this signal carried it at all. Body[1] is the changed-properties
// dictionary; a signal about some other property leaves it absent.
func scanningFlag(s *dbus.Signal) (bool, bool) {
	if len(s.Body) < 2 {
		return false, false
	}
	changed, ok := s.Body[1].(map[string]dbus.Variant)
	if !ok {
		return false, false
	}
	v, ok := changed["Scanning"]
	if !ok {
		return false, false
	}
	scanning, ok := v.Value().(bool)
	return scanning, ok
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

// GetScanResults reads the access points NetworkManager already knows about.
//
// It only reads. It used to ask every wireless device for a fresh scan first,
// which made it feed itself: the debounce that calls this runs off
// AccessPointAdded/Removed, so each read asked for a scan, the scan moved the
// access point list, and the churn came back round as more signals. Asking is
// RequestScan's job, and the two callers that want a scan rather than the
// current picture call it themselves.
func GetScanResults(conn *dbus.Conn) tea.Cmd {
	return func() tea.Msg {
		return common.ScannedNetworksUpdateMsg(network.GetScannedNetworks(conn))
	}
}

func RefreshAllData(conn *dbus.Conn) tea.Cmd {
	return tea.Batch(
		// The periodic refresh is the one place a scan is still worth asking
		// for without being told to: a connected device scans rarely on its
		// own, so New Networks would otherwise sit still. Timer-driven, so it
		// cannot feed itself the way the signal path did.
		RequestScan(conn),
		GetScanResults(conn),
		func() tea.Msg { return common.DeviceUpdateMsg(network.GetDevicesData(conn)) },
		func() tea.Msg { return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(conn)) },
		func() tea.Msg { return common.VpnUpdateMsg(network.GetVpnData(conn)) },
	)
}
