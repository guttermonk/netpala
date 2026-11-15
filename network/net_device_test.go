package network

import (
	"netpala/common"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Toggling the radio takes the device off the bus between the call that lists
// it and the call that reads its type, and the answer that comes back for a
// device that is no longer there carries no value to compare. Asserting
// uint32 on it panics -- which, from a command goroutine, closes netpala
// outright. Every one of these shapes has to come back false, not explode.
func TestIsWifiTypeSurvivesAVanishedDevice(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]dbus.Variant
		want  bool
	}{
		{"no properties at all", nil, false},
		{"properties but none of them", map[string]dbus.Variant{}, false},
		{"type absent", map[string]dbus.Variant{"Interface": dbus.MakeVariant("wlan0")}, false},
		{"type not a uint32", map[string]dbus.Variant{"DeviceType": dbus.MakeVariant("2")}, false},
		{"ethernet", map[string]dbus.Variant{"DeviceType": dbus.MakeVariant(uint32(1))}, false},
		{"wifi", map[string]dbus.Variant{"DeviceType": dbus.MakeVariant(NMDeviceTypeWifi)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsWifiType(tt.props["DeviceType"]); got != tt.want {
				t.Errorf("IsWifiType(%v) = %v, want %v", tt.props, got, tt.want)
			}
		})
	}
}

// The callers that ask for the one property rather than all of them hand over
// whatever GetProperty returned, and on failure that is the zero Variant.
func TestIsWifiTypeOnAFailedPropertyRead(t *testing.T) {
	if IsWifiType(dbus.Variant{}) {
		t.Error("a failed property read must not read as a wifi device")
	}
}

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
