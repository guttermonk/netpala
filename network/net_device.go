package network

import (
	"fmt"
	"netpala/common"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	NMDest  = "org.freedesktop.NetworkManager"
	NMPath  = "/org/freedesktop/NetworkManager"
	PropsIF = "org.freedesktop.DBus.Properties"
	DevIF   = "org.freedesktop.NetworkManager.Device"
	// DevStateProp is the device's NM_DEVICE_STATE, read directly when a
	// single device is being watched rather than the whole list.
	DevStateProp = DevIF + ".State"
	// DevIfaceProp is the kernel interface name, used to name the device in
	// messages about it.
	DevIfaceProp  = DevIF + ".Interface"
	WifiIF        = "org.freedesktop.NetworkManager.Device.Wireless"
	AccessPointIF = "org.freedesktop.NetworkManager.AccessPoint"
)

// NMDeviceTypeWifi is NM_DEVICE_TYPE_WIFI.
const NMDeviceTypeWifi = uint32(2)

// IsWifiType reports whether a device's DeviceType property says Wi-Fi.
//
// Every caller walks the list GetDevices returned and then reads properties
// off each path, which is a second round trip -- and the device can be gone by
// the time it happens. Switching the radio off does exactly that, which is why
// a toggle used to be able to take netpala down: a departed device answers
// with no properties, the property is absent, and asserting uint32 on the
// resulting nil panics rather than yielding a value to compare.
//
// Taking a Variant rather than the map suits both shapes of caller: those that
// fetch every property with GetProps, and those that ask for this one alone
// with GetProperty. A zero Variant -- an absent key, or a failed read -- is
// simply not Wi-Fi, which is the answer that makes the loop skip on.
func IsWifiType(v dbus.Variant) bool {
	dType, ok := v.Value().(uint32)
	return ok && dType == NMDeviceTypeWifi
}

func GetDevicesData(c *dbus.Conn) []common.Device {
	var devicesList []common.Device
	nm := c.Object(NMDest, dbus.ObjectPath(NMPath))
	p := GetProps(nm, NMDest)

	settingsObj := c.Object(NMDest, "/org/freedesktop/NetworkManager/Settings")
	var connPaths []dbus.ObjectPath
	_ = settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&connPaths)

	var devs []dbus.ObjectPath
	nm.Call(NMDest+".GetDevices", 0).Store(&devs)

	for _, d := range devs {
		obj := c.Object(NMDest, d)
		dp := GetProps(obj, DevIF)

		// The device can be gone by the time its properties are fetched -- a
		// VPN disconnecting or the radio going down both do it. See IsWifiType.
		if !IsWifiType(dp["DeviceType"]) {
			continue
		}

		deviceState := common.DeviceStateUnknown
		if stateVar, ok := dp["State"]; ok {
			state, _ := stateVar.Value().(uint32)
			deviceState = DeviceStateFromNM(state)
		}

		// Defensive check for Interface name and HW Address
		// Usually safe if DeviceType check passed, but good practice.
		iface := "unknown"
		if v := dp["Interface"].Value(); v != nil {
			iface, _ = v.(string)
		}

		mac := "00:00:00:00:00:00"
		if v := dp["HwAddress"].Value(); v != nil {
			if s, ok := v.(string); ok {
				mac = strings.ToLower(s)
			}
		}

		wp := GetProps(obj, WifiIF)
		mode, _ := wp["Mode"].Value().(uint32)
		ap, _ := wp["ActiveAccessPoint"].Value().(dbus.ObjectPath)

		var isScanning bool
		if scanningVar, ok := wp["Scanning"]; ok {
			isScanning, _ = scanningVar.Value().(bool)
		}

		bssid, frequency, security := "-", 0, "-"
		if ap != "/" && ap != "" { // Check for empty path too
			apObj := c.Object(NMDest, ap)
			if bssidVar, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.HwAddress"); err == nil {
				bssid = bssidVar.Value().(string)
			}
			if freqVar, err := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Frequency"); err == nil {
				frequency = int(freqVar.Value().(uint32))
			}
			ssidVar, _ := apObj.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Ssid")
			activeSSID := ""
			if b, ok := ssidVar.Value().([]byte); ok {
				activeSSID = strings.TrimRight(string(b), "\x00")
			}
			for _, cpath := range connPaths {
				cobj := c.Object(NMDest, cpath)
				var settings map[string]map[string]dbus.Variant
				if cobj.Call("org.freedesktop.NetworkManager.Settings.Connection.GetSettings", 0).Store(&settings) != nil {
					continue
				}
				if wcfg, ok := settings["802-11-wireless"]; ok {
					if ssidV, ok := wcfg["ssid"]; ok {
						if b, ok := ssidV.Value().([]byte); ok && strings.TrimRight(string(b), "\x00") == activeSSID {
							security = securityLabel(settings)
							break
						}
					}
				}
			}
		}
		modeStr := map[uint32]string{1: "ad-hoc", 2: "station", 3: "ap", 4: "mesh"}[mode]
		if modeStr == "" {
			modeStr = fmt.Sprintf("%d", mode)
		}

		// Safe check for global WirelessEnabled properties
		powered := false
		if wEnabled := p["WirelessEnabled"].Value(); wEnabled != nil {
			if whEnabled := p["WirelessHardwareEnabled"].Value(); whEnabled != nil {
				powered = wEnabled.(bool) && whEnabled.(bool)
			}
		}

		devicesList = append(devicesList, common.Device{
			Path:         d,
			Name:         iface,
			Mode:         modeStr,
			Powered:      powered,
			Address:      mac,
			State:        deviceState,
			CurrentBSSID: bssid,
			Scanning:     isScanning,
			Frequency:    frequency,
			Security:     security,
		})
	}
	return devicesList
}

// DeviceStateFromNM collapses an NM_DEVICE_STATE to the values in common.
//
// Every state is mapped, not just the two obvious ones. Sending the unlisted
// ones to a "connecting" default meant a switched-off radio (UNAVAILABLE) and
// a failed connection (FAILED) both read as though they were still trying.
func DeviceStateFromNM(state uint32) int {
	switch state {
	case 100: // ACTIVATED
		return common.DeviceStateConnected
	case 40, 50, 60, 70, 80, 90: // PREPARE, CONFIG, NEED_AUTH, IP_CONFIG, IP_CHECK, SECONDARIES
		return common.DeviceStateConnecting
	case 30, 110: // DISCONNECTED, DEACTIVATING
		return common.DeviceStateDisconnected
	case 120: // FAILED
		return common.DeviceStateFailed
	case 20: // UNAVAILABLE - radio off, rfkill, no carrier
		return common.DeviceStateUnavailable
	case 10: // UNMANAGED
		return common.DeviceStateUnmanaged
	}
	return common.DeviceStateUnknown
}
