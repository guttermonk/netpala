package common

import "testing"

// Every label, named rather than spelled out, so a label and its meaning
// cannot drift apart: both decisions in the program -- which popup opens, and
// which key-mgmt is stored -- are made through here.
func TestMechanismForEveryLabel(t *testing.T) {
	for _, tt := range []struct {
		security string
		want     SecurityMechanism
		why      string
	}{
		{LabelOpen, SecurityOpen, "no security at all"},
		{LabelOWE, SecurityOWE, "encrypted with no shared key"},
		{LabelPSK, SecurityPSK, ""},
		{LabelSAE, SecuritySAE, "SAE with nothing else offered"},
		{LabelEAP, SecurityEAP, ""},
		{LabelEAP192, SecurityEAP, "enterprise, and not a password prompt"},
		{LabelWEP, SecurityPSK, "keyed with something, so ask what"},
		{LabelUnknown, SecurityPSK, "keyed with something, so ask what"},

		// Access points offering more than one.
		{LabelSAE + LabelSeparator + LabelPSK, SecurityPSK,
			"transition mode: wpa-psk covers WPA2 and WPA3 personal"},
		{LabelEAP + LabelSeparator + LabelPSK, SecurityEAP,
			"enterprise wins; there is no password to ask for"},
		{LabelEAP192 + LabelSeparator + LabelEAP, SecurityEAP, ""},

		// Nothing produces these, which is the point.
		{"owe", SecurityPSK, "the spelling one producer used before the labels were shared"},
		{"", SecurityPSK, "the fallback is to ask rather than to assume"},
		{"something new", SecurityPSK, "the fallback is to ask rather than to assume"},
		// Both of these answer something other than the fallback, so they
		// cannot pass by failing to recognise anything.
		{"WPA3-SAE", SecuritySAE, "case is not what distinguishes a label"},
		{"wpa2-eap/wpa2-psk", SecurityEAP, "nor is spacing round the separator"},
	} {
		t.Run(tt.security, func(t *testing.T) {
			if got := MechanismFor(tt.security); got != tt.want {
				t.Errorf("MechanismFor(%q) = %s, want %s %s",
					tt.security, got, tt.want, tt.why)
			}
		})
	}
}

// Only the two that have a key worth typing.
func TestOnlyPskAndSaeAskForAPassword(t *testing.T) {
	for _, m := range []SecurityMechanism{SecurityPSK, SecuritySAE} {
		if !m.NeedsPassword() {
			t.Errorf("%s does not ask for a password", m)
		}
	}
	for _, m := range []SecurityMechanism{SecurityOpen, SecurityOWE, SecurityEAP} {
		if m.NeedsPassword() {
			t.Errorf("%s asks for a password", m)
		}
	}
}

// An unset mechanism has to be the one that asks, because that is what an
// unrecognised string falls back to and the two must not disagree.
func TestTheZeroMechanismIsTheConservativeOne(t *testing.T) {
	var unset SecurityMechanism
	if unset != SecurityPSK {
		t.Errorf("the zero value is %s, want psk", unset)
	}
	if !unset.NeedsPassword() {
		t.Error("the zero value does not ask for a password")
	}
}
