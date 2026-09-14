package network

import (
	"fmt"
	"netpala/common"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	NMDest        = "org.freedesktop.NetworkManager"
	NMPath        = "/org/freedesktop/NetworkManager"
	PropsIF       = "org.freedesktop.DBus.Properties"
	DevIF         = "org.freedesktop.NetworkManager.Device"
	WifiIF        = "org.freedesktop.NetworkManager.Device.Wireless"
	AccessPointIF = "org.freedesktop.NetworkManager.AccessPoint"
)

func GetDevicesData(c *dbus.Conn) []common.Device {
	var devicesList []common.Device
	nm := c.Object(NMDest, dbus.ObjectPath(NMPath))
	p := GetProps(nm, NMDest)

	settingsObj := c.Object(NMDest, "/org/freedesktop/NetworkManager/Settings")
	var connPaths []dbus.ObjectPath
	_ = settingsObj.Call("org.freedesktop.NetworkManager.Settings.ListConnections", 0).Store(&connPaths)

	inferSecurity := func(sec map[string]dbus.Variant) string {
		if sec == nil {
			return "open"
		}
		if v, ok := sec["key-mgmt"]; ok {
			km := strings.ToLower(v.Value().(string))
			switch {
			case strings.Contains(km, "sae"):
				return "wpa3-sae"
			case strings.Contains(km, "owe"):
				return "owe"
			case strings.Contains(km, "wpa-psk"):
				return "wpa2-psk"
			case strings.Contains(km, "wpa-eap"):
				return "wpa2-eap"
			case strings.Contains(km, "none"):
				// "none" key-mgmt inside a security block typically means WEP.
				return "wep"
			}
		}
		// Fallback if key-mgmt is missing but a security section exists
		if _, ok := sec["psk"]; ok {
			return "wpa-psk"
		}
		return "encrypted"
	}

	var devs []dbus.ObjectPath
	nm.Call(NMDest+".GetDevices", 0).Store(&devs)

	for _, d := range devs {
		obj := c.Object(NMDest, d)
		dp := GetProps(obj, DevIF)

		// --- FIX START: Safe DeviceType Check ---
		// When a VPN disconnects, the device might exist in the list 'devs',
		// but by the time we fetch props, it's gone or empty.
		rawType := dp["DeviceType"].Value()
		if rawType == nil {
			continue
		}

		// Safely check type assertion
		dType, ok := rawType.(uint32)
		if !ok || dType != 2 { // 2 == Wifi
			continue
		}
		// --- FIX END ---

		// Map every NM_DEVICE_STATE, not just the two obvious ones. Sending
		// unlisted states to a "connecting" default meant a switched-off radio
		// (UNAVAILABLE) and a failed connection (FAILED) both read as though
		// they were still trying to connect.
		deviceState := common.DeviceStateUnknown
		if stateVar, ok := dp["State"]; ok {
			state, _ := stateVar.Value().(uint32)
			switch state {
			case 100: // ACTIVATED
				deviceState = common.DeviceStateConnected
			case 40, 50, 60, 70, 80, 90: // PREPARE, CONFIG, NEED_AUTH, IP_CONFIG, IP_CHECK, SECONDARIES
				deviceState = common.DeviceStateConnecting
			case 30, 110: // DISCONNECTED, DEACTIVATING
				deviceState = common.DeviceStateDisconnected
			case 120: // FAILED
				deviceState = common.DeviceStateFailed
			case 20: // UNAVAILABLE - radio off, rfkill, no carrier
				deviceState = common.DeviceStateUnavailable
			case 10: // UNMANAGED
				deviceState = common.DeviceStateUnmanaged
			case 0: // UNKNOWN
				deviceState = common.DeviceStateUnknown
			}
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
							security = inferSecurity(settings["802-11-wireless-security"])
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
