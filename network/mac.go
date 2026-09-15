package network

import (
	"netpala/common"

	"github.com/godbus/dbus/v5"
)

// WirelessSetting is the D-Bus name of the Wi-Fi settings group.
const WirelessSetting = "802-11-wireless"

// AssignedMACKey holds the MAC behaviour: one of NetworkManager's keywords, or
// a literal address. ClonedMACKey is the legacy byte-array mirror, which only
// ever carries an explicit address and is left over from before the keywords
// existed.
const (
	AssignedMACKey = "assigned-mac-address"
	ClonedMACKey   = "cloned-mac-address"
	// MACRandomizationKey is an older companion property (1 = never,
	// 2 = always) that nmcli still sets alongside the keywords.
	MACRandomizationKey = "mac-address-randomization"
)

// assignedMAC reads the profile's stored MAC behaviour.
func assignedMAC(settings map[string]map[string]dbus.Variant) string {
	w := settings[WirelessSetting]
	if w == nil {
		return ""
	}
	if v, ok := w[AssignedMACKey]; ok {
		if s, ok := v.Value().(string); ok {
			return s
		}
	}
	// Fall back to the legacy array for profiles written before NetworkManager
	// gained the string property.
	if v, ok := w[ClonedMACKey]; ok {
		if b, ok := v.Value().([]byte); ok && len(b) == 6 {
			return formatMAC(b)
		}
	}
	return ""
}

func formatMAC(b []byte) string {
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, 17)
	for i, c := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[c>>4], hex[c&0x0f])
	}
	return string(out)
}

// MACModeFromSettings reports which MAC behaviour a profile is configured for.
func MACModeFromSettings(settings map[string]map[string]dbus.Variant) string {
	return common.MatchMACMode(assignedMAC(settings))
}

// MACAddressFromSettings returns the literal address when one is set, and ""
// for the keyword modes.
func MACAddressFromSettings(settings map[string]map[string]dbus.Variant) string {
	value := assignedMAC(settings)
	if common.MatchMACMode(value) == common.MACModeExplicit {
		return value
	}
	return ""
}
