package network

import (
	"strings"

	"netpala/common"

	"github.com/godbus/dbus/v5"
)

// securityLabel is the Security column's word for a saved profile, worked out
// from the key management the profile holds.
//
// There were three copies of this, one for each pane that shows a security:
// Known Networks, the Device pane and -- from the access point's flags rather
// than a profile -- New Networks. They disagreed. The same OWE network read
// "owe" in two of them and "wpa-owe" in the third, which is the spelling the
// select handler went on, and one copy answered "wpa-psk" where the others
// said "wpa2-psk", a fourth spelling that nothing matched at all. The labels
// are common's now, and this is the only place a profile is read for one.
func securityLabel(settings map[string]map[string]dbus.Variant) string {
	// A profile with no security section is an open network. Absent and
	// present-but-empty are different: the second says the profile is keyed
	// without saying how.
	return securityLabelFromSection(settings["802-11-wireless-security"])
}

func securityLabelFromSection(sec map[string]dbus.Variant) string {
	if sec == nil {
		return common.LabelOpen
	}
	if km, ok := sec["key-mgmt"]; ok {
		// Not asserted outright: a key-mgmt that is not a string is a
		// malformed profile, not a reason to take the program down on startup.
		kmStr, _ := km.Value().(string)
		return labelForKeyMgmt(kmStr)
	}
	// No key management named, but a key stored, so it is keyed with the one
	// thing a bare psk can mean.
	if _, ok := sec["psk"]; ok {
		return common.LabelPSK
	}
	return common.LabelUnknown
}

// labelForKeyMgmt maps NetworkManager's key-mgmt to a label. Contains rather
// than equality because the property has carried suffixes over the years, and
// the more specific value has to be tested before the one it contains:
// "wpa-eap-suite-b-192" contains "wpa-eap".
func labelForKeyMgmt(keyMgmt string) string {
	km := strings.ToLower(strings.TrimSpace(keyMgmt))
	switch {
	case strings.Contains(km, "sae"):
		return common.LabelSAE
	case strings.Contains(km, "owe"):
		return common.LabelOWE
	case strings.Contains(km, "wpa-eap-suite-b-192"):
		return common.LabelEAP192
	case strings.Contains(km, "wpa-psk"):
		return common.LabelPSK
	case strings.Contains(km, "wpa-eap"):
		return common.LabelEAP
	case strings.Contains(km, "none"):
		// "none" inside a security section is WEP, not an open network.
		return common.LabelWEP
	}
	return common.LabelUnknown
}
