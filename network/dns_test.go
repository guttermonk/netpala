package network

import (
	"netpala/common"
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

// The wire encoding is byte-order sensitive; 1.0.0.1 and 149.112.112.112 are
// deliberately non-palindromic so a swapped encoding cannot survive a round trip
// alongside the decoder.
func TestDNSv4VariantRoundTrip(t *testing.T) {
	addrs := []string{"1.1.1.1", "1.0.0.1", "8.8.4.4", "149.112.112.112"}

	variant, err := DNSv4Variant(addrs)
	if err != nil {
		t.Fatalf("DNSv4Variant: %v", err)
	}
	settings := map[string]map[string]dbus.Variant{"ipv4": {"dns": variant}}

	if got := DNSServersFromSettings(settings); !reflect.DeepEqual(got, addrs) {
		t.Errorf("round trip = %v, want %v", got, addrs)
	}
}

// Pin the exact integers NetworkManager stores, so the encoding cannot silently
// drift to the other byte order on a little-endian host.
func TestDNSv4VariantWireValues(t *testing.T) {
	variant, err := DNSv4Variant([]string{"1.0.0.1", "8.8.4.4"})
	if err != nil {
		t.Fatalf("DNSv4Variant: %v", err)
	}
	got, ok := variant.Value().([]uint32)
	if !ok {
		t.Fatalf("variant holds %T, want []uint32", variant.Value())
	}
	// Bytes 01 00 00 01 and 08 08 04 04 read back in native order.
	want := []uint32{0x01000001, 0x04040808}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("encoded = %#v, want %#v", got, want)
	}
}

func TestDNSv4VariantRejectsIPv6(t *testing.T) {
	if _, err := DNSv4Variant([]string{"2606:4700:4700::1111"}); err == nil {
		t.Error("expected error for an IPv6 address in the v4 encoder")
	}
}

func TestDNSv6Variant(t *testing.T) {
	variant, err := DNSv6Variant([]string{"2606:4700:4700::1111"})
	if err != nil {
		t.Fatalf("DNSv6Variant: %v", err)
	}
	got, ok := variant.Value().([][]byte)
	if !ok {
		t.Fatalf("variant holds %T, want [][]byte", variant.Value())
	}
	if len(got) != 1 || len(got[0]) != 16 {
		t.Fatalf("got %d entries of %d bytes, want 1 entry of 16", len(got), len(got[0]))
	}
	if got[0][0] != 0x26 || got[0][1] != 0x06 || got[0][15] != 0x11 {
		t.Errorf("unexpected byte layout: %x", got[0])
	}
}

func TestDNSv6VariantRejectsGarbage(t *testing.T) {
	if _, err := DNSv6Variant([]string{"not-an-ip"}); err == nil {
		t.Error("expected error for a non-IP address")
	}
}

func TestDNSModeFromSettings(t *testing.T) {
	cloudflare, _ := DNSv4Variant([]string{"1.1.1.1", "1.0.0.1"})
	quad9, _ := DNSv4Variant([]string{"9.9.9.9"})

	tests := []struct {
		name     string
		settings map[string]map[string]dbus.Variant
		want     string
	}{
		{"no ipv4 section", map[string]map[string]dbus.Variant{}, common.DNSModeDHCP},
		{"ipv4 without dns key", map[string]map[string]dbus.Variant{"ipv4": {}}, common.DNSModeDHCP},
		{"empty dns list", map[string]map[string]dbus.Variant{
			"ipv4": {"dns": dbus.MakeVariant([]uint32{})},
		}, common.DNSModeDHCP},
		{"cloudflare", map[string]map[string]dbus.Variant{"ipv4": {"dns": cloudflare}}, common.DNSModeCloudflare},
		{"quad9 is custom", map[string]map[string]dbus.Variant{"ipv4": {"dns": quad9}}, common.DNSModeCustom},
		{"wrong variant type", map[string]map[string]dbus.Variant{
			"ipv4": {"dns": dbus.MakeVariant("1.1.1.1")},
		}, common.DNSModeDHCP},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DNSModeFromSettings(tt.settings); got != tt.want {
				t.Errorf("DNSModeFromSettings = %q, want %q", got, tt.want)
			}
		})
	}
}
