package common

import "testing"

func TestServiceStateLabel(t *testing.T) {
	tests := []struct {
		name   string
		active string
		sub    string
		want   string
	}{
		// The case that prompted this: a oneshot that loaded the nftables
		// ruleset and returned. "exited" reads like a crash; it is success.
		{"oneshot that finished its job", "active", "exited", "active"},
		{"resident daemon", "active", "running", "running"},
		{"stopped", "inactive", "dead", "inactive"},
		{"crashed", "failed", "failed", "failed"},
		{"mid-start", "activating", "start", "starting"},
		{"mid-stop", "deactivating", "stop", "stopping"},
		{"unrecognised falls back to substate", "reloading", "reload", "reload"},
		{"no substate falls back to state", "weird", "", "weird"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ServiceStateLabel(SecurityService{State: tt.active, SubState: tt.sub})
			if got != tt.want {
				t.Errorf("State=%q SubState=%q -> %q, want %q", tt.active, tt.sub, got, tt.want)
			}
		})
	}
}

// A healthy unit must never be labelled with a word that reads as a failure.
func TestActiveUnitNeverLooksBroken(t *testing.T) {
	for _, sub := range []string{"running", "exited", "waiting", "mounted", ""} {
		got := ServiceStateLabel(SecurityService{State: "active", SubState: sub})
		if got != "active" && got != "running" {
			t.Errorf("active/%s labelled %q; an active unit must read as healthy", sub, got)
		}
	}
}
