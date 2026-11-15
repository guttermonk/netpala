package common

import (
	"slices"
	"strings"
)

// The vocabulary of the Security column, and the only one.
//
// There are two places a network's security is worked out -- from the key
// management an access point advertises, and from the one a saved profile
// holds -- and they used to name the results separately. The spellings drifted
// where you would expect them to: the same OWE network read "wpa-owe" as a
// scan result and "owe" once saved, so the Security column said two different
// things about it depending on which pane it was in, and the handler matching
// one of those strings exactly did the wrong thing with the other.
//
// Naming them here rather than at each producer is what keeps that from
// happening again. MechanismFor below is the other half: a scan lists every
// mechanism an access point offers, so these also arrive joined together.
const (
	LabelOpen   = "open"
	LabelOWE    = "wpa-owe"
	LabelPSK    = "wpa2-psk"
	LabelSAE    = "wpa3-sae"
	LabelEAP    = "wpa2-eap"
	LabelEAP192 = "wpa3-eap-192"
	// WEP is a saved profile keyed with "none", and an access point that
	// encrypts while advertising no key management at all -- there being no
	// WPA or RSN flags to advertise it in.
	LabelWEP = "wep"
	// Keyed with something else again, which only a saved profile can be:
	// an access point has flags, and they either say a mechanism or they do
	// not.
	LabelUnknown = "encrypted"
	// LabelSeparator joins the mechanisms of an access point offering several.
	LabelSeparator = " / "
)

// SecurityMechanism is what a network's security says about joining it, as
// opposed to what it says on screen. The labels above are written for the
// column; this is what the two decisions -- which popup opens, which key-mgmt
// is stored -- are actually made on.
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
	// WEP is not a mechanism netpala can join a network with: the profile it
	// needs is nothing like the others -- no key management, the key in
	// wep-key0, and a key type that cannot be told from the key itself. It is
	// named so that a WEP network can be refused with a reason, which is
	// better than being joined with a profile that cannot work. An already
	// saved WEP profile still connects: NetworkManager holds the key, and
	// activating it does not come through here.
	SecurityWEP
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
	case SecurityWEP:
		return "wep"
	}
	return "psk"
}

// NeedsPassword reports whether joining the network means asking for one.
// OWE derives its own keys, an open network has none, and WEP is refused
// before it gets as far as a prompt.
func (m SecurityMechanism) NeedsPassword() bool {
	return m == SecurityPSK || m == SecuritySAE
}

// MechanismFor classifies a Security label, or several of them joined.
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
	case has(LabelEAP, LabelEAP192):
		return SecurityEAP
	case has(LabelPSK):
		return SecurityPSK
	case has(LabelSAE):
		return SecuritySAE
	case has(LabelOWE):
		return SecurityOWE
	case has(LabelOpen):
		return SecurityOpen
	case has(LabelWEP):
		return SecurityWEP
	}
	// Unrecognised, which includes LabelUnknown: keyed with something, and
	// asking which key is the only way to find out.
	return SecurityPSK
}
