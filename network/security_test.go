package network

import (
	"netpala/common"
	"testing"

	"github.com/godbus/dbus/v5"
)

// What NetworkManager stores in key-mgmt, and the label each one earns. Three
// copies of this mapping used to exist and disagree; it is one now, so this is
// the only place the answers have to be right.
func TestLabelForEveryKeyManagement(t *testing.T) {
	for _, tt := range []struct {
		keyMgmt string
		want    string
		why     string
	}{
		{"sae", common.LabelSAE, ""},
		{"owe", common.LabelOWE, "one spelling, the same as a scan result's"},
		{"wpa-psk", common.LabelPSK, ""},
		{"wpa-eap", common.LabelEAP, ""},
		{"wpa-eap-suite-b-192", common.LabelEAP192,
			"tested before wpa-eap, which it contains"},
		{"none", common.LabelWEP, "inside a security section this is WEP"},
		{"ieee8021x", common.LabelUnknown, "dynamic WEP, and not one of the labels"},
		{"", common.LabelUnknown, ""},
		{"something-new", common.LabelUnknown, ""},
		{"WPA-PSK", common.LabelPSK, "NetworkManager's case is not relied on"},
		{"  sae  ", common.LabelSAE, "nor is the absence of whitespace"},
	} {
		t.Run(tt.keyMgmt, func(t *testing.T) {
			if got := labelForKeyMgmt(tt.keyMgmt); got != tt.want {
				t.Errorf("labelForKeyMgmt(%q) = %q, want %q %s",
					tt.keyMgmt, got, tt.want, tt.why)
			}
		})
	}
}

func settingsWith(sec map[string]dbus.Variant) map[string]map[string]dbus.Variant {
	s := map[string]map[string]dbus.Variant{
		"connection":      {"id": dbus.MakeVariant("home")},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("home"))},
	}
	if sec != nil {
		s["802-11-wireless-security"] = sec
	}
	return s
}

// No section and an empty section are different profiles: one is open, the
// other is keyed without saying how.
func TestASectionlessProfileIsOpen(t *testing.T) {
	if got := securityLabel(settingsWith(nil)); got != common.LabelOpen {
		t.Errorf("a profile with no security section = %q, want %q", got, common.LabelOpen)
	}
	if got := securityLabel(settingsWith(map[string]dbus.Variant{})); got != common.LabelUnknown {
		t.Errorf("a profile with an empty security section = %q, want %q", got, common.LabelUnknown)
	}
}

// A key with no key management named is still a key, and a bare psk only means
// one thing. This used to answer "wpa-psk" in one of the three copies, which
// is a label nothing else in the program recognises.
func TestAKeyWithoutKeyManagementIsPsk(t *testing.T) {
	got := securityLabel(settingsWith(map[string]dbus.Variant{
		"psk": dbus.MakeVariant("a good password"),
	}))
	if got != common.LabelPSK {
		t.Errorf("a section holding only a psk = %q, want %q", got, common.LabelPSK)
	}
}

// Reading a profile must not be able to take the program down on startup,
// which an outright type assertion on key-mgmt could.
func TestAMalformedKeyManagementIsNotFatal(t *testing.T) {
	got := securityLabel(settingsWith(map[string]dbus.Variant{
		"key-mgmt": dbus.MakeVariant(uint32(7)),
	}))
	if got != common.LabelUnknown {
		t.Errorf("a key-mgmt that is not a string = %q, want %q", got, common.LabelUnknown)
	}
}

// And the other producer, from the flags an access point advertises rather
// than from a profile. Same labels, so the same network reads the same way in
// either pane.
func TestScanFlagsProduceTheSameLabels(t *testing.T) {
	const (
		psk     = 0x00000100
		eap     = 0x00000200
		sae     = 0x00000400
		owe     = 0x00000800
		oweTM   = 0x00001000
		suiteB  = 0x00002000
		noFlags = 0
	)

	for _, tt := range []struct {
		name     string
		wpa, rsn uint32
		want     string
	}{
		{"nothing advertised", noFlags, noFlags, common.LabelOpen},
		{"psk on wpa", psk, noFlags, common.LabelPSK},
		{"psk on rsn", noFlags, psk, common.LabelPSK},
		{"eap", noFlags, eap, common.LabelEAP},
		{"sae", noFlags, sae, common.LabelSAE},
		{"owe", noFlags, owe, common.LabelOWE},
		{"owe transition mode", noFlags, oweTM, common.LabelOWE},
		{"suite b 192", noFlags, suiteB, common.LabelEAP192},
		{"transition mode", noFlags, sae | psk,
			common.LabelSAE + common.LabelSeparator + common.LabelPSK},
		{"enterprise and personal", noFlags, eap | psk,
			common.LabelEAP + common.LabelSeparator + common.LabelPSK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := getSecurityType(tt.wpa, tt.rsn); got != tt.want {
				t.Errorf("getSecurityType(%#x, %#x) = %q, want %q", tt.wpa, tt.rsn, got, tt.want)
			}
		})
	}
}

// The point of the whole exercise: every label either producer can emit is one
// the rest of the program can act on.
func TestEveryLabelEitherProducerEmitsIsClassifiable(t *testing.T) {
	fromProfiles := []string{
		labelForKeyMgmt("sae"), labelForKeyMgmt("owe"), labelForKeyMgmt("wpa-psk"),
		labelForKeyMgmt("wpa-eap"), labelForKeyMgmt("wpa-eap-suite-b-192"),
		labelForKeyMgmt("none"), labelForKeyMgmt("mystery"),
		securityLabel(settingsWith(nil)),
	}
	fromScans := []string{
		getSecurityType(0, 0), getSecurityType(0, 0x100), getSecurityType(0, 0x200),
		getSecurityType(0, 0x400), getSecurityType(0, 0x800), getSecurityType(0, 0x2000),
		getSecurityType(0, 0x400|0x100),
	}

	for _, label := range append(fromProfiles, fromScans...) {
		mechanism := common.MechanismFor(label)
		// Open is the one answer that must be earned rather than fallen into:
		// it means joining without asking for anything.
		if mechanism == common.SecurityOpen && label != common.LabelOpen {
			t.Errorf("%q classifies as open", label)
		}
	}
}
