package network

import (
	"netpala/common"
	"strings"

	"github.com/godbus/dbus/v5"
)

func GetProps(o dbus.BusObject, iface string) map[string]dbus.Variant {
	var m map[string]dbus.Variant
	o.Call(PropsIF+".GetAll", 0, iface).Store(&m)
	return m
}

func GetScannedNetworks(c *dbus.Conn) []common.ScannedNetwork {
	nm := c.Object(NMDest, dbus.ObjectPath(NMPath))
	var devPaths []dbus.ObjectPath
	if err := nm.Call(NMDest+".GetDevices", 0).Store(&devPaths); err != nil {
		return nil
	}

	var allNetworks []common.ScannedNetwork
	for _, devPath := range devPaths {
		devObj := c.Object(NMDest, devPath)
		devProps := GetProps(devObj, DevIF)

		// Powering the radio off takes the device away between GetDevices and
		// this read, which is why the toggle used to crash. See IsWifiType.
		if !IsWifiType(devProps["DeviceType"]) {
			continue
		}

		var apPaths []dbus.ObjectPath
		// The error is dropped rather than printed: this runs under the alt
		// screen, where anything written to stdout lands in the middle of a
		// rendered frame. A device losing its access points is the expected
		// outcome of switching the radio off, not something to report.
		_ = devObj.Call(WifiIF+".GetAllAccessPoints", 0).Store(&apPaths)

		for _, apPath := range apPaths {
			apObj := c.Object(NMDest, apPath)
			apProps := GetProps(apObj, AccessPointIF)

			var ssid string
			if ssidVal, ok := apProps["Ssid"]; ok {
				if ssidBytes, ok := ssidVal.Value().([]byte); ok {
					ssid = strings.TrimRight(string(ssidBytes), "\x00")
				}
			}
			if ssid == "" {
				continue
			}

			bssid := ""
			if s, ok := apProps["HwAddress"].Value().(string); ok {
				bssid = s
			}
			var signal int
			if b, ok := apProps["Strength"].Value().(byte); ok {
				signal = int(b)
			}

			ap := c.Object(NMDest, apPath)
			// Flags as well as the two key-management sets: WEP announces
			// itself only by encrypting without any of them.
			apFlagsVar, _ := ap.GetProperty("org.freedesktop.NetworkManager.AccessPoint.Flags")
			wpaFlagsVar, _ := ap.GetProperty("org.freedesktop.NetworkManager.AccessPoint.WpaFlags")
			rsnFlagsVar, _ := ap.GetProperty("org.freedesktop.NetworkManager.AccessPoint.RsnFlags")
			var apFlags, wpaFlags, rsnFlags uint32
			if val, ok := apFlagsVar.Value().(uint32); ok {
				apFlags = val
			}
			if val, ok := wpaFlagsVar.Value().(uint32); ok {
				wpaFlags = val
			}
			if val, ok := rsnFlagsVar.Value().(uint32); ok {
				rsnFlags = val
			}

			allNetworks = append(allNetworks, common.ScannedNetwork{
				Path:     apPath,
				SSID:     common.SanitizeSSID(ssid, "[?]"),
				BSSID:    bssid,
				Security: getSecurityType(apFlags, wpaFlags, rsnFlags),
				Signal:   signal,
			})
		}
	}
	return removeDuplicates(allNetworks)
}

func getSecurityType(apFlags, wpaFlags, rsnFlags uint32) string {
	const (
		// Set when the access point encrypts, whichever way it does it. On its
		// own, with no key management advertised in either set, that is WEP:
		// WEP predates WPA and has nothing to announce in WpaFlags or
		// RsnFlags, so a WEP network read as having no security at all and sat
		// in the list looking like one that could be joined by pressing enter.
		NM_802_11_AP_FLAGS_PRIVACY = 0x00000001

		NM_802_11_AP_SEC_KEY_MGMT_PSK             = 0x00000100
		NM_802_11_AP_SEC_KEY_MGMT_802_1X          = 0x00000200
		NM_802_11_AP_SEC_KEY_MGMT_SAE             = 0x00000400
		NM_802_11_AP_SEC_KEY_MGMT_OWE             = 0x00000800
		NM_802_11_AP_SEC_KEY_MGMT_OWE_TM          = 0x00001000
		NM_802_11_AP_SEC_KEY_MGMT_EAP_SUITE_B_192 = 0x00002000
	)

	// The labels are common's, so that a network reads the same way whether it
	// is being discovered here or has already been saved. An access point can
	// advertise several of these at once, and all of them are listed: which one
	// is used to join is common.MechanismFor's decision, not the column's.
	var security []string
	if rsnFlags&NM_802_11_AP_SEC_KEY_MGMT_SAE != 0 {
		security = append(security, common.LabelSAE)
	}
	if rsnFlags&NM_802_11_AP_SEC_KEY_MGMT_EAP_SUITE_B_192 != 0 {
		security = append(security, common.LabelEAP192)
	}
	if (rsnFlags&NM_802_11_AP_SEC_KEY_MGMT_802_1X != 0) || (wpaFlags&NM_802_11_AP_SEC_KEY_MGMT_802_1X != 0) {
		security = append(security, common.LabelEAP)
	}
	if (rsnFlags&NM_802_11_AP_SEC_KEY_MGMT_PSK != 0) || (wpaFlags&NM_802_11_AP_SEC_KEY_MGMT_PSK != 0) {
		security = append(security, common.LabelPSK)
	}
	if rsnFlags&(NM_802_11_AP_SEC_KEY_MGMT_OWE|NM_802_11_AP_SEC_KEY_MGMT_OWE_TM) != 0 {
		security = append(security, common.LabelOWE)
	}
	if len(security) == 0 {
		if apFlags&NM_802_11_AP_FLAGS_PRIVACY != 0 {
			return common.LabelWEP
		}
		return common.LabelOpen
	}
	return strings.Join(security, common.LabelSeparator)
}

// removeDuplicates keeps the strongest access point for each SSID, which is
// also the one worth activating on, and returns them in the display order.
//
// Sorting here rather than leaving it to the caller is what makes the order a
// function of the scan. It was a map iteration followed by a sort on signal
// alone, so networks reporting the same strength -- which the weak half of a
// scan mostly does -- came back in a different order every time, and the list
// reshuffled under the cursor several times a minute with nothing having
// changed. The signal is the only thing that should move a row.
func removeDuplicates(networks []common.ScannedNetwork) []common.ScannedNetwork {
	networkMap := make(map[string]common.ScannedNetwork)
	for _, network := range networks {
		existing, exists := networkMap[network.SSID]
		if !exists || network.Signal > existing.Signal {
			networkMap[network.SSID] = network
		}
	}

	var result []common.ScannedNetwork
	for _, network := range networkMap {
		result = append(result, network)
	}
	common.SortDevicesBySignal(result)
	return result
}
