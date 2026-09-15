package config

import (
	"strings"
	"testing"
)

// The contrib NixOS module derives each state file from the unit name as
// /var/lib/netpala/<unit without .service>. netpala writes the choice and the
// module's boot unit reads it, so a disagreement is silent: the file is
// written and never read, and the service reverts at the next reboot.
func TestStateFilePathsMatchTheModuleConvention(t *testing.T) {
	for _, s := range DefaultSecurity().Services {
		if s.StateFile == "" {
			t.Errorf("%s has no state_file, so its on/off choice cannot survive a reboot", s.Unit)
			continue
		}
		want := "/var/lib/netpala/" + strings.TrimSuffix(s.Unit, ".service")
		if s.StateFile != want {
			t.Errorf("%s state_file = %q, but the module derives %q",
				s.Unit, s.StateFile, want)
		}
	}
}
