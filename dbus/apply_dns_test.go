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

func wirelessSettings() map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("net"))},
	}
}

func assignedMAC(t *testing.T, s map[string]map[string]dbus.Variant) string {
	t.Helper()
	v, ok := s["802-11-wireless"]["assigned-mac-address"]
	if !ok {
		t.Fatal("assigned-mac-address not written")
	}
	got, _ := v.Value().(string)
	return got
}

// assigned-mac-address and cloned-mac-address are two D-Bus spellings of one
// NetworkManager property, not independent fields. Sending both makes the
// legacy byte array win and the keyword is silently discarded - every mode
// read back as unset.
func TestMACWritesOnlyTheStringProperty(t *testing.T) {
	s := wirelessSettings()
	s["802-11-wireless"]["cloned-mac-address"] = dbus.MakeVariant([]byte{2, 17, 34, 51, 68, 85})

	if err := applyMACToSettings(s, common.MACModeStable, ""); err != nil {
		t.Fatal(err)
	}
	if got := assignedMAC(t, s); got != "stable" {
		t.Errorf("assigned-mac-address = %q, want stable", got)
	}
	if _, present := s["802-11-wireless"]["cloned-mac-address"]; present {
		t.Error("legacy cloned-mac-address still sent; it overrides the keyword")
	}
}

func TestMACModesRoundTrip(t *testing.T) {
	for _, tc := range []struct{ mode, explicit, want string }{
		{common.MACModeStable, "", "stable"},
		{common.MACModeRandom, "", "random"},
		{common.MACModePermanent, "", "permanent"},
		{common.MACModePreserve, "", "preserve"},
		{common.MACModeExplicit, "02:11:22:33:44:55", "02:11:22:33:44:55"},
		{common.MACModeDefault, "", ""},
	} {
		name := tc.mode
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			s := wirelessSettings()
			if err := applyMACToSettings(s, tc.mode, tc.explicit); err != nil {
				t.Fatal(err)
			}
			if got := assignedMAC(t, s); got != tc.want {
				t.Errorf("assigned-mac-address = %q, want %q", got, tc.want)
			}
		})
	}
}

// An explicit address must not survive a later switch to a keyword.
func TestExplicitAddressDoesNotLinger(t *testing.T) {
	s := wirelessSettings()
	if err := applyMACToSettings(s, common.MACModeExplicit, "02:11:22:33:44:55"); err != nil {
		t.Fatal(err)
	}
	if err := applyMACToSettings(s, common.MACModeRandom, ""); err != nil {
		t.Fatal(err)
	}
	if got := assignedMAC(t, s); got != "random" {
		t.Errorf("assigned-mac-address = %q, want random", got)
	}
	if _, present := s["802-11-wireless"]["cloned-mac-address"]; present {
		t.Error("explicit address left behind in the legacy property")
	}
}

// The older randomization flag shadows the keyword, so it is cleared.
func TestLegacyRandomizationFlagIsCleared(t *testing.T) {
	s := wirelessSettings()
	s["802-11-wireless"]["mac-address-randomization"] = dbus.MakeVariant(uint32(2))

	if err := applyMACToSettings(s, common.MACModeStable, ""); err != nil {
		t.Fatal(err)
	}
	v := s["802-11-wireless"]["mac-address-randomization"]
	if got, _ := v.Value().(uint32); got != 0 {
		t.Errorf("mac-address-randomization = %d, want 0 (default)", got)
	}
}

func TestMACRejectsBadInput(t *testing.T) {
	for _, tc := range []struct{ name, addr string }{
		{"not an address", "nonsense"},
		{"multicast", "ff:ff:ff:ff:ff:ff"},
		{"odd first octet", "01:22:33:44:55:66"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := wirelessSettings()
			if err := applyMACToSettings(s, common.MACModeExplicit, tc.addr); err == nil {
				t.Errorf("accepted %q", tc.addr)
			}
		})
	}
}

func TestMACRejectsNonWirelessConnection(t *testing.T) {
	s := map[string]map[string]dbus.Variant{"ipv4": {}}
	if err := applyMACToSettings(s, common.MACModeRandom, ""); err == nil {
		t.Error("expected an error for a connection with no wireless section")
	}
}
