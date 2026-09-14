package dbus

import (
	"netpala/common"
	"testing"

	"github.com/godbus/dbus/v5"
)

func ipv4Servers(t *testing.T, settings map[string]map[string]dbus.Variant) []uint32 {
	t.Helper()
	v, ok := settings["ipv4"]["dns"]
	if !ok {
		t.Fatal(`ipv4 "dns" key absent; NetworkManager keeps the stored value ` +
			`for a property the update does not mention, so the key must be ` +
			`present and empty rather than deleted`)
	}
	servers, ok := v.Value().([]uint32)
	if !ok {
		t.Fatalf("ipv4 dns holds %T, want []uint32", v.Value())
	}
	return servers
}

func boolProp(t *testing.T, settings map[string]map[string]dbus.Variant, section, key string) bool {
	t.Helper()
	v, ok := settings[section][key]
	if !ok {
		t.Fatalf("%s.%s missing", section, key)
	}
	b, _ := v.Value().(bool)
	return b
}

// Switching back to DHCP has to clear the servers, not merely stop preferring
// them. Deleting the key left the previous provider's addresses in the profile
// while ignore-auto-dns went back to false, so the network carried on
// resolving through them and the UI kept showing the old provider.
func TestDHCPClearsServersRatherThanDeletingTheKey(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("auto")},
	}

	cf := common.DNSProviderByID(common.DNSModeCloudflare)
	if err := applyDNSToSettings(settings, cf, cf.V4, cf.V6); err != nil {
		t.Fatal(err)
	}
	if got := ipv4Servers(t, settings); len(got) != 2 {
		t.Fatalf("precondition: expected Cloudflare's two servers, got %v", got)
	}

	dhcp := common.DNSProviderByID(common.DNSModeDHCP)
	if err := applyDNSToSettings(settings, dhcp, nil, nil); err != nil {
		t.Fatal(err)
	}

	if got := ipv4Servers(t, settings); len(got) != 0 {
		t.Errorf("ipv4 dns = %v after switching to DHCP, want empty", got)
	}
	if boolProp(t, settings, "ipv4", "ignore-auto-dns") {
		t.Error("ipv4 ignore-auto-dns still set; the router's servers stay suppressed")
	}
	if boolProp(t, settings, "ipv6", "ignore-auto-dns") {
		t.Error("ipv6 ignore-auto-dns still set")
	}
}

// Every provider must round-trip back to DHCP, not just the one that happened
// to get tested.
func TestEveryProviderRoundTripsToDHCP(t *testing.T) {
	dhcp := common.DNSProviderByID(common.DNSModeDHCP)

	for _, id := range []string{
		common.DNSModeCloudflare,
		common.DNSModeGoogle,
		common.DNSModeDNSCrypt,
	} {
		t.Run(id, func(t *testing.T) {
			settings := map[string]map[string]dbus.Variant{
				"ipv4": {"method": dbus.MakeVariant("auto")},
				"ipv6": {"method": dbus.MakeVariant("auto")},
			}
			p := common.DNSProviderByID(id)
			if err := applyDNSToSettings(settings, p, p.V4, p.V6); err != nil {
				t.Fatal(err)
			}
			if len(ipv4Servers(t, settings)) == 0 {
				t.Fatalf("precondition: %s set no servers", id)
			}

			if err := applyDNSToSettings(settings, dhcp, nil, nil); err != nil {
				t.Fatal(err)
			}
			if got := ipv4Servers(t, settings); len(got) != 0 {
				t.Errorf("%s -> DHCP left %v behind", id, got)
			}
		})
	}
}

// A custom list with no IPv6 entries must clear any IPv6 servers a previous
// provider left, rather than mixing the two.
func TestV4OnlyProviderClearsStaleIPv6(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("auto")},
	}

	cf := common.DNSProviderByID(common.DNSModeCloudflare) // has IPv6 servers
	if err := applyDNSToSettings(settings, cf, cf.V4, cf.V6); err != nil {
		t.Fatal(err)
	}
	if v6, _ := settings["ipv6"]["dns"].Value().([][]byte); len(v6) == 0 {
		t.Fatal("precondition: Cloudflare should have set IPv6 servers")
	}

	custom := common.DNSProviderByID(common.DNSModeCustom)
	if err := applyDNSToSettings(settings, custom, []string{"9.9.9.9"}, nil); err != nil {
		t.Fatal(err)
	}

	v6, ok := settings["ipv6"]["dns"].Value().([][]byte)
	if !ok || len(v6) != 0 {
		t.Errorf("ipv6 dns = %v, want empty; stale servers would bypass the chosen provider", v6)
	}
	if !boolProp(t, settings, "ipv6", "ignore-auto-dns") {
		t.Error("ipv6 ignore-auto-dns must stay set, or the router's v6 resolvers leak back in")
	}
}

func TestMissingIPSectionsAreCreated(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{}
	p := common.DNSProviderByID(common.DNSModeGoogle)
	if err := applyDNSToSettings(settings, p, p.V4, p.V6); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"ipv4", "ipv6"} {
		if settings[section] == nil {
			t.Errorf("%s section not created", section)
		}
	}
}

func TestProviderWithNoServersIsRejected(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("auto")},
	}
	custom := common.DNSProviderByID(common.DNSModeCustom)
	if err := applyDNSToSettings(settings, custom, nil, nil); err == nil {
		t.Error("expected an error rather than silently writing nothing")
	}
}

// A disabled IPv6 stack must not have servers written to it; NetworkManager
// rejects the update outright.
func TestDisabledStackIsLeftAlone(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("disabled")},
	}
	cf := common.DNSProviderByID(common.DNSModeCloudflare)
	if err := applyDNSToSettings(settings, cf, cf.V4, cf.V6); err != nil {
		t.Fatal(err)
	}
	if v6, _ := settings["ipv6"]["dns"].Value().([][]byte); len(v6) != 0 {
		t.Errorf("wrote %v to a disabled IPv6 stack", v6)
	}
}
