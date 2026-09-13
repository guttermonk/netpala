package common

import (
	"reflect"
	"testing"
)

func TestMatchDNSProvider(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		want    string
	}{
		{"no servers is dhcp", nil, DNSModeDHCP},
		{"empty slice is dhcp", []string{}, DNSModeDHCP},
		{"cloudflare", []string{"1.1.1.1", "1.0.0.1"}, DNSModeCloudflare},
		{"cloudflare reordered", []string{"1.0.0.1", "1.1.1.1"}, DNSModeCloudflare},
		{"google", []string{"8.8.8.8", "8.8.4.4"}, DNSModeGoogle},
		{"quad9 is custom", []string{"9.9.9.9", "149.112.112.112"}, DNSModeCustom},
		{"partial cloudflare is custom", []string{"1.1.1.1"}, DNSModeCustom},
		{"cloudflare plus extra is custom", []string{"1.1.1.1", "1.0.0.1", "9.9.9.9"}, DNSModeCustom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchDNSProvider(tt.servers); got != tt.want {
				t.Errorf("MatchDNSProvider(%v) = %q, want %q", tt.servers, got, tt.want)
			}
		})
	}
}

func TestParseDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantV4  []string
		wantV6  []string
		wantErr bool
	}{
		{"comma separated", "1.1.1.1,1.0.0.1", []string{"1.1.1.1", "1.0.0.1"}, nil, false},
		{"comma space separated", "9.9.9.9, 149.112.112.112", []string{"9.9.9.9", "149.112.112.112"}, nil, false},
		{"space separated", "8.8.8.8 8.8.4.4", []string{"8.8.8.8", "8.8.4.4"}, nil, false},
		{"mixed families", "1.1.1.1, 2606:4700:4700::1111", []string{"1.1.1.1"}, []string{"2606:4700:4700::1111"}, false},
		{"ipv6 only", "2001:4860:4860::8888", nil, []string{"2001:4860:4860::8888"}, false},
		{"empty", "", nil, nil, true},
		{"whitespace only", "   ", nil, nil, true},
		{"not an ip", "not-an-ip", nil, nil, true},
		{"octet out of range", "1.1.1.256", nil, nil, true},
		{"hostname rejected", "dns.google", nil, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4, v6, err := ParseDNSServers(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDNSServers(%q) = %v/%v, want error", tt.input, v4, v6)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDNSServers(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(v4, tt.wantV4) {
				t.Errorf("v4 = %v, want %v", v4, tt.wantV4)
			}
			if !reflect.DeepEqual(v6, tt.wantV6) {
				t.Errorf("v6 = %v, want %v", v6, tt.wantV6)
			}
		})
	}
}

func TestDNSProviderByIDFallsBackToDHCP(t *testing.T) {
	if got := DNSProviderByID("nonsense").ID; got != DNSModeDHCP {
		t.Errorf("DNSProviderByID(nonsense) = %q, want %q", got, DNSModeDHCP)
	}
	if got := DNSModeLabel(""); got != "DHCP" {
		t.Errorf("DNSModeLabel(\"\") = %q, want DHCP", got)
	}
}

// A profile netpala has never touched has an empty DNSMode, which must still
// render as DHCP in the table rather than blank.
func TestDNSModeLabelForEveryProvider(t *testing.T) {
	for _, p := range DNSProviders {
		if got := DNSModeLabel(p.ID); got != p.Label {
			t.Errorf("DNSModeLabel(%q) = %q, want %q", p.ID, got, p.Label)
		}
	}
}

func TestMatchDNSProviderLoopbackIsDNSCrypt(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		want    string
	}{
		{"default listener", []string{"127.0.0.1"}, DNSModeDNSCrypt},
		{"alternate loopback", []string{"127.0.0.53"}, DNSModeDNSCrypt},
		{"ipv6 loopback", []string{"::1"}, DNSModeDNSCrypt},
		{"several loopbacks", []string{"127.0.0.1", "::1"}, DNSModeDNSCrypt},
		{"mixed loopback and public is custom", []string{"127.0.0.1", "1.1.1.1"}, DNSModeCustom},
		{"lan pihole is not loopback", []string{"192.168.1.2"}, DNSModeCustom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchDNSProvider(tt.servers); got != tt.want {
				t.Errorf("MatchDNSProvider(%v) = %q, want %q", tt.servers, got, tt.want)
			}
		})
	}
}

func TestDNSProvidersFor(t *testing.T) {
	find := func(list []DNSProvider) DNSProvider {
		for _, p := range list {
			if p.ID == DNSModeDNSCrypt {
				return p
			}
		}
		t.Fatal("no dnscrypt provider in list")
		return DNSProvider{}
	}

	if got := find(DNSProvidersFor(nil)).V4; !reflect.DeepEqual(got, DefaultDNSCryptAddresses) {
		t.Errorf("nil config gave %v, want the default", got)
	}

	custom := find(DNSProvidersFor([]string{"127.0.0.53", "::1"}))
	if !reflect.DeepEqual(custom.V4, []string{"127.0.0.53"}) {
		t.Errorf("V4 = %v, want [127.0.0.53]", custom.V4)
	}
	if !reflect.DeepEqual(custom.V6, []string{"::1"}) {
		t.Errorf("V6 = %v, want [::1]", custom.V6)
	}
	if custom.Desc != "127.0.0.53, ::1" {
		t.Errorf("Desc = %q, want the configured addresses", custom.Desc)
	}

	if got := find(DNSProvidersFor([]string{"garbage"})).V4; !reflect.DeepEqual(got, DefaultDNSCryptAddresses) {
		t.Errorf("unparseable config gave %v, want the default", got)
	}

	// The shared package-level list must not be mutated by a call.
	if !reflect.DeepEqual(find(DNSProviders).V4, DefaultDNSCryptAddresses) {
		t.Error("DNSProvidersFor mutated the package-level provider table")
	}
}

func TestProbeResolverDead(t *testing.T) {
	// 127.0.0.1:53 has nothing on it in CI; either way an invalid address must
	// fail fast rather than hang.
	if err := ProbeResolver("not-an-ip"); err == nil {
		t.Error("expected an error for an invalid address")
	}
	if err := ProbeResolvers(nil); err == nil {
		t.Error("expected an error for an empty address list")
	}
}
