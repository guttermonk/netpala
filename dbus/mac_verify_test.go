package dbus

import (
	"netpala/common"
	"strings"
	"testing"
)

func TestMacVerdict(t *testing.T) {
	tests := []struct {
		name    string
		state   int
		attempt int
		want    macVerdictResult
	}{
		{"activated is success", common.DeviceStateConnected, 0, macSucceeded},
		{"failed is failure", common.DeviceStateFailed, 0, macFailed},
		{"unavailable is failure", common.DeviceStateUnavailable, 0, macFailed},

		// Every normal reconnect passes through these, so reverting on them
		// would undo a change that was about to succeed.
		{"connecting keeps waiting", common.DeviceStateConnecting, 0, macKeepWaiting},
		{"disconnected keeps waiting", common.DeviceStateDisconnected, 0, macKeepWaiting},
		{"unknown keeps waiting", common.DeviceStateUnknown, 0, macKeepWaiting},

		// ...but not forever.
		{"disconnected forever is failure", common.DeviceStateDisconnected, macVerifyAttempts - 1, macFailed},
		{"connecting forever is failure", common.DeviceStateConnecting, macVerifyAttempts - 1, macFailed},

		// A device that comes back on the last attempt still counts.
		{"late success still succeeds", common.DeviceStateConnected, macVerifyAttempts - 1, macSucceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := macVerdict(tt.state, tt.attempt); got != tt.want {
				t.Errorf("macVerdict(%d, %d) = %v, want %v", tt.state, tt.attempt, got, tt.want)
			}
		})
	}
}

// The budget has to outlast a slow associate, or a working adapter gets its
// change reverted for being unhurried.
func TestMacVerifyBudgetIsLongEnoughForASlowAssociate(t *testing.T) {
	total := macVerifyInterval * macVerifyAttempts
	if total.Seconds() < 8 {
		t.Errorf("verification gives up after %s; a slow reconnect would be undone", total)
	}
}

func TestMacRevertedText(t *testing.T) {
	msg := common.MacRevertedMsg{
		Iface:     "wlp3s0",
		Attempted: common.MACModeRandom,
		PrevMode:  common.MACModeDefault,
		Restored:  true,
	}

	got := MacRevertedText(msg)
	for _, want := range []string{"wlp3s0", "Random", "Default", "reconnected"} {
		if !strings.Contains(got, want) {
			t.Errorf("message %q is missing %q", got, want)
		}
	}

	msg.Restored = false
	if failed := MacRevertedText(msg); !strings.Contains(failed, "reconnecting failed") {
		t.Errorf("a failed restore must say so, got %q", failed)
	}
}

// The message names modes by their picker labels, not their internal ids, or
// it describes a setting the user cannot find in the UI.
func TestMacRevertedTextUsesPickerLabels(t *testing.T) {
	got := MacRevertedText(common.MacRevertedMsg{
		Iface:     "wlp3s0",
		Attempted: common.MACModeStable,
		PrevMode:  common.MACModePermanent,
	})
	if strings.Contains(got, "stable") || strings.Contains(got, "permanent") {
		t.Errorf("message uses raw mode ids: %q", got)
	}
}
