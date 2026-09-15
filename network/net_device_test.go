package network

import (
	"netpala/common"
	"testing"
)

// The verifier that undoes a failed MAC change keys off these values, so a
// state mapped to the wrong bucket would either revert a working change or
// leave a broken one in place.
func TestDeviceStateFromNM(t *testing.T) {
	tests := []struct {
		nm   uint32
		want int
		name string
	}{
		{100, common.DeviceStateConnected, "activated"},
		{120, common.DeviceStateFailed, "failed"},
		{20, common.DeviceStateUnavailable, "unavailable"},
		{30, common.DeviceStateDisconnected, "disconnected"},
		{110, common.DeviceStateDisconnected, "deactivating"},
		{10, common.DeviceStateUnmanaged, "unmanaged"},
		{0, common.DeviceStateUnknown, "unknown"},
		{999, common.DeviceStateUnknown, "unrecognised"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeviceStateFromNM(tt.nm); got != tt.want {
				t.Errorf("DeviceStateFromNM(%d) = %d, want %d", tt.nm, got, tt.want)
			}
		})
	}

	// Every intermediate activation state must read as connecting, or the
	// verifier would give up on a device that is still associating.
	for _, nm := range []uint32{40, 50, 60, 70, 80, 90} {
		if got := DeviceStateFromNM(nm); got != common.DeviceStateConnecting {
			t.Errorf("DeviceStateFromNM(%d) = %d, want connecting", nm, got)
		}
	}
}
