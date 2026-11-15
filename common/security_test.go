package common

import "testing"

// The strings come from two places that do not agree with each other: a scan
// lists every mechanism an access point advertises, joined with " / ", and a
// saved profile names the one key-mgmt it holds. Both reach the same two
// decisions -- which popup opens, and which key-mgmt is stored -- so both have
// to classify.
func TestMechanismForEveryStringEitherProducerMakes(t *testing.T) {
	for _, tt := range []struct {
		security string
		want     SecurityMechanism
		why      string
	}{
		// What a scan produces.
		{"open", SecurityOpen, "no security at all"},
		{"wpa-owe", SecurityOWE, "a scan spells OWE this way"},
		{"wpa2-psk", SecurityPSK, ""},
		{"wpa3-sae", SecuritySAE, "SAE with nothing else offered"},
		{"wpa2-eap", SecurityEAP, ""},
		{"wpa3-eap-192", SecurityEAP, "enterprise, and not a password prompt"},

		// What a saved profile produces.
		{"owe", SecurityOWE, "a profile spells OWE this way"},
		{"wep", SecurityPSK, "unrecognised, so the conservative answer"},
		{"encrypted", SecurityPSK, "unrecognised, so the conservative answer"},

		// Access points offering more than one.
		{"wpa3-sae / wpa2-psk", SecurityPSK, "transition mode: wpa-psk covers WPA2 and WPA3 personal"},
		{"wpa2-eap / wpa2-psk", SecurityEAP, "enterprise wins; there is no password to ask for"},
		{"wpa3-eap-192 / wpa2-eap", SecurityEAP, ""},

		// Neither producer makes these, which is the point.
		{"", SecurityPSK, "the fallback is to ask rather than to assume"},
		{"something new", SecurityPSK, "the fallback is to ask rather than to assume"},
		{"WPA2-PSK", SecurityPSK, "case is not what distinguishes them"},
		{"wpa3-sae/wpa2-psk", SecurityPSK, "spacing round the separator is not either"},
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
