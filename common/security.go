package common

import (
	"slices"
	"strings"
)

// SecurityMechanism is what a network's security says about joining it, as
// opposed to what it says on screen.
//
// The two are not the same string, and treating them as one is what sent OWE
// networks to the password prompt. A scan result's Security is built for the
// Security column: it lists every key management the access point advertises,
// joined with " / ", so a router in WPA2/WPA3 transition mode reads
// "wpa3-sae / wpa2-psk". Saved profiles arrive with a vocabulary of their own,
// derived from the one key-mgmt the profile holds, which spells OWE "owe"
// where a scan spells it "wpa-owe". Matching either of those exactly, as both
// call sites used to, works for the handful of strings that happen to be
// written the same way round and silently mishandles the rest.
type SecurityMechanism int

// PSK is the zero value deliberately: it is the conservative answer for a
// string this does not recognise. Asking for a key that turns out not to be
// needed is a worse prompt; assuming none is needed is a connection that
// cannot succeed and a profile saved wrong.
const (
	SecurityPSK SecurityMechanism = iota
	SecuritySAE
	SecurityEAP
	SecurityOWE
	SecurityOpen
)

func (m SecurityMechanism) String() string {
	switch m {
	case SecuritySAE:
		return "sae"
	case SecurityEAP:
		return "eap"
	case SecurityOWE:
		return "owe"
	case SecurityOpen:
		return "open"
	}
	return "psk"
}

// NeedsPassword reports whether joining the network means asking for one.
// OWE derives its own keys and an open network has none.
func (m SecurityMechanism) NeedsPassword() bool {
	return m == SecurityPSK || m == SecuritySAE
}

// MechanismFor classifies a Security string from either producer.
//
// The order of the cases is the answer to an access point offering more than
// one mechanism:
//
//   - Enterprise wins outright. There is no password to ask for, and
//     nm-settings-dbus(5) has "wpa-eap" covering WPA2 and WPA3 enterprise
//     both, so one form serves either.
//   - PSK is preferred over SAE, which is what a transition-mode access point
//     offers together. The same page has "wpa-psk" as "WPA2 + WPA3 personal"
//     and "sae" as "WPA3 personal only", so PSK is the one that still
//     negotiates WPA3 where both ends have it, without ruling out a client
//     that has not. SAE is for an access point offering nothing else.
//   - OWE and open have no key either way.
func MechanismFor(security string) SecurityMechanism {
	tokens := strings.Split(strings.ToLower(strings.TrimSpace(security)), "/")
	for i := range tokens {
		tokens[i] = strings.TrimSpace(tokens[i])
	}
	has := func(names ...string) bool {
		for _, name := range names {
			if slices.Contains(tokens, name) {
				return true
			}
		}
		return false
	}

	switch {
	case has("wpa2-eap", "wpa3-eap-192"):
		return SecurityEAP
	case has("wpa2-psk"):
		return SecurityPSK
	case has("wpa3-sae"):
		return SecuritySAE
	case has("owe", "wpa-owe"):
		return SecurityOWE
	case has("open"):
		return SecurityOpen
	}
	// Unrecognised, which includes a saved profile's "wep" and "encrypted".
	return SecurityPSK
}
