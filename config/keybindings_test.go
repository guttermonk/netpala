package config

import (
	"strings"
	"testing"
)

// The consent texts exist to tell the user what leaves the machine, so that is
// what they have to contain. A prompt that only says "are you sure?" is worse
// than none: it trains people to accept without reading.
func TestSecurityConfirmTextsNameWhatIsShared(t *testing.T) {
	want := map[string][]string{
		"i2pd.service": {
			"Shared:",
			"IP address",  // what peers see
			"relaying",    // bandwidth spent on others' traffic
			"not an exit", // and the misconception to head off
		},
		"tor-transparent.service": {
			"Shared:",
			"ISP",  // who learns you use Tor
			"exit", // what sites see instead of you
			"UDP",  // and what stops working
		},
	}

	seen := map[string]bool{}
	for _, svc := range DefaultSecurity().Services {
		needles, ok := want[svc.Unit]
		if !ok {
			continue
		}
		seen[svc.Unit] = true

		if svc.Confirm == "" {
			t.Errorf("%s has no consent text", svc.Unit)
			continue
		}
		for _, needle := range needles {
			if !strings.Contains(svc.Confirm, needle) {
				t.Errorf("%s consent text does not mention %q:\n%s", svc.Unit, needle, svc.Confirm)
			}
		}
	}
	for unit := range want {
		if !seen[unit] {
			t.Errorf("%s is not in the default service list", unit)
		}
	}
}

// DNSCrypt is low-stakes and already guarded by the DNS picker, so prompting
// for it would be noise.
func TestDNSCryptHasNoConfirmText(t *testing.T) {
	for _, svc := range DefaultSecurity().Services {
		if svc.ProvidesDNS && svc.Confirm != "" {
			t.Errorf("%s asks for confirmation; the DNS picker already guards it", svc.Unit)
		}
	}
}
