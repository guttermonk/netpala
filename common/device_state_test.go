package common

import "testing"

func TestDeviceStateLabel(t *testing.T) {
	tests := []struct {
		state int
		want  string
	}{
		{DeviceStateConnected, "connected"},
		{DeviceStateConnecting, "connecting"},
		{DeviceStateDisconnected, "disconnected"},
		{DeviceStateFailed, "failed"},
		{DeviceStateUnavailable, "unavailable"},
		{DeviceStateUnmanaged, "unmanaged"},
		{DeviceStateUnknown, "unknown"},
		{99, "unknown"}, // anything unmapped must not masquerade as a real state
	}

	for _, tt := range tests {
		if got := DeviceStateLabel(tt.state); got != tt.want {
			t.Errorf("DeviceStateLabel(%d) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// The bug this replaced: a switched-off radio reports UNAVAILABLE, which fell
// through to a "connecting" default. Nothing that means "not connected" may
// ever render as an in-progress state again.
func TestNonConnectedStatesNeverReadAsConnecting(t *testing.T) {
	for _, state := range []int{
		DeviceStateDisconnected,
		DeviceStateFailed,
		DeviceStateUnavailable,
		DeviceStateUnmanaged,
		DeviceStateUnknown,
	} {
		if got := DeviceStateLabel(state); got == "connecting" || got == "connected" {
			t.Errorf("state %d labelled %q, which implies a live connection", state, got)
		}
	}
}

// Only DeviceStateConnected is positive, so "is the device up?" stays a
// single comparison for any caller that needs it.
func TestOnlyConnectedIsPositive(t *testing.T) {
	for _, state := range []int{
		DeviceStateConnecting,
		DeviceStateDisconnected,
		DeviceStateFailed,
		DeviceStateUnavailable,
		DeviceStateUnmanaged,
		DeviceStateUnknown,
	} {
		if state >= DeviceStateConnected {
			t.Errorf("state %d (%q) sorts at or above DeviceStateConnected",
				state, DeviceStateLabel(state))
		}
	}
}
