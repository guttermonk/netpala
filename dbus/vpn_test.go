package dbus

import (
	"testing"

	"netpala/common"

	"github.com/godbus/dbus/v5"
)

func parse(t *testing.T, conf string) *common.WireGuardConfig {
	t.Helper()
	cfg, err := common.ParseWireGuardConfig(conf)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	return cfg
}

const fullTunnel = `[Interface]
PrivateKey = qGZk4xFQ3l7pXvBn8hJtYcRmWs2Dv0aNpLuEi9OgXFo=
Address = 10.64.0.2/32, fc00:bbbb:bbbb:bb01::1:5c1/128
DNS = 10.64.0.1
MTU = 1380

[Peer]
PublicKey = /vN1Hq8YpKxTzRcLmWdFgBnJs3Qa7ZuEi0OpXvYtCkM=
AllowedIPs = 0.0.0.0/0, ::0/0
Endpoint = 185.65.135.170:51820
PersistentKeepalive = 25
`

func settingsFor(t *testing.T, conf string) map[string]map[string]dbus.Variant {
	t.Helper()
	s, err := WireGuardSettings(parse(t, conf), "mullvad-se", "mullvad-se", "uuid-1")
	if err != nil {
		t.Fatalf("WireGuardSettings: %v", err)
	}
	return s
}

// NetworkManager creates the WireGuard link itself, so it needs to be told
// what to call it. Without interface-name the profile is rejected at
// activation, not at save time.
func TestConnectionSectionNamesTheInterface(t *testing.T) {
	s := settingsFor(t, fullTunnel)

	conn := s["connection"]
	if got, _ := conn["type"].Value().(string); got != "wireguard" {
		t.Errorf("type = %q, want wireguard", got)
	}
	if got, _ := conn["interface-name"].Value().(string); got != "mullvad-se" {
		t.Errorf("interface-name = %q", got)
	}
	if got, _ := conn["id"].Value().(string); got != "mullvad-se" {
		t.Errorf("id = %q", got)
	}
}

// Importing a file should not decide that every packet this machine sends
// starts going to a provider from the next boot onwards.
func TestImportedProfilesDoNotAutoconnect(t *testing.T) {
	s := settingsFor(t, fullTunnel)
	if got, _ := s["connection"]["autoconnect"].Value().(bool); got {
		t.Error("an imported profile was set to come up on its own")
	}
}

// Flag 0 is "stored in the system connection". An agent-owned secret would
// need netpala running for the tunnel to come up at all.
func TestPrivateKeyIsStoredInTheProfile(t *testing.T) {
	s := settingsFor(t, fullTunnel)

	wg := s["wireguard"]
	if got, _ := wg["private-key"].Value().(string); got != "qGZk4xFQ3l7pXvBn8hJtYcRmWs2Dv0aNpLuEi9OgXFo=" {
		t.Errorf("private-key = %q", got)
	}
	if got, _ := wg["private-key-flags"].Value().(uint32); got != 0 {
		t.Errorf("private-key-flags = %d, want 0 (stored in the profile)", got)
	}
}

func TestPeersAreEncodedForNetworkManager(t *testing.T) {
	s := settingsFor(t, fullTunnel)

	peers, ok := s["wireguard"]["peers"].Value().([]map[string]dbus.Variant)
	if !ok {
		t.Fatalf("peers is %T, want []map[string]dbus.Variant", s["wireguard"]["peers"].Value())
	}
	if len(peers) != 1 {
		t.Fatalf("%d peers, want 1", len(peers))
	}

	p := peers[0]
	if got, _ := p["public-key"].Value().(string); got != "/vN1Hq8YpKxTzRcLmWdFgBnJs3Qa7ZuEi0OpXvYtCkM=" {
		t.Errorf("public-key = %q", got)
	}
	if got, _ := p["endpoint"].Value().(string); got != "185.65.135.170:51820" {
		t.Errorf("endpoint = %q", got)
	}
	if got, _ := p["allowed-ips"].Value().([]string); len(got) != 2 {
		t.Errorf("allowed-ips = %v, want both families", got)
	}
	if got, _ := p["persistent-keepalive"].Value().(uint32); got != 25 {
		t.Errorf("persistent-keepalive = %d", got)
	}
	// Absent rather than empty: NM treats an empty preshared-key as a set one.
	if _, present := p["preshared-key"]; present {
		t.Error("a preshared key was written for a config that has none")
	}
}

func TestPresharedKeyIsWrittenWithItsFlags(t *testing.T) {
	s := settingsFor(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/24
[Peer]
PublicKey = def=
AllowedIPs = 10.0.0.0/24
PresharedKey = psk=
`)
	peers, _ := s["wireguard"]["peers"].Value().([]map[string]dbus.Variant)
	if got, _ := peers[0]["preshared-key"].Value().(string); got != "psk=" {
		t.Errorf("preshared-key = %q", got)
	}
	if got, _ := peers[0]["preshared-key-flags"].Value().(uint32); got != 0 {
		t.Errorf("preshared-key-flags = %d, want 0", got)
	}
}

// The tunnel's own address is an ordinary static address as far as NM is
// concerned. There is no DHCP inside a WireGuard tunnel, so "manual" is the
// only method that can be right.
func TestTunnelAddressesBecomeStaticIPConfig(t *testing.T) {
	s := settingsFor(t, fullTunnel)

	if got, _ := s["ipv4"]["method"].Value().(string); got != "manual" {
		t.Errorf("ipv4.method = %q, want manual", got)
	}
	v4, ok := s["ipv4"]["address-data"].Value().([]map[string]dbus.Variant)
	if !ok || len(v4) != 1 {
		t.Fatalf("ipv4.address-data = %v", s["ipv4"]["address-data"].Value())
	}
	if got, _ := v4[0]["address"].Value().(string); got != "10.64.0.2" {
		t.Errorf("ipv4 address = %q", got)
	}
	if got, _ := v4[0]["prefix"].Value().(uint32); got != 32 {
		t.Errorf("ipv4 prefix = %d", got)
	}

	if got, _ := s["ipv6"]["method"].Value().(string); got != "manual" {
		t.Errorf("ipv6.method = %q, want manual", got)
	}
	v6, ok := s["ipv6"]["address-data"].Value().([]map[string]dbus.Variant)
	if !ok || len(v6) != 1 {
		t.Fatalf("ipv6.address-data = %v", s["ipv6"]["address-data"].Value())
	}
	if got, _ := v6[0]["prefix"].Value().(uint32); got != 128 {
		t.Errorf("ipv6 prefix = %d", got)
	}
}

// NetworkManager rejects an Update that sets nameservers on a disabled
// section, so a v4-only config must not carry v6 DNS -- or any v6 settings.
func TestAFamilyWithNoAddressIsDisabled(t *testing.T) {
	s := settingsFor(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
DNS = 10.0.0.1
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if got, _ := s["ipv6"]["method"].Value().(string); got != "disabled" {
		t.Errorf("ipv6.method = %q, want disabled", got)
	}
	if _, present := s["ipv6"]["dns"]; present {
		t.Error("nameservers set on a disabled ipv6 section; NM rejects that")
	}
	if _, present := s["ipv6"]["address-data"]; present {
		t.Error("addresses set on a disabled ipv6 section")
	}
}

// Each family's nameservers live in its own section and are encoded
// differently -- uint32 for v4, byte arrays for v6.
func TestDNSIsSplitByFamilyAndEncoded(t *testing.T) {
	s := settingsFor(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32, fd00::2/128
DNS = 10.0.0.1, fd00::1
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if _, ok := s["ipv4"]["dns"].Value().([]uint32); !ok {
		t.Errorf("ipv4.dns is %T, want []uint32", s["ipv4"]["dns"].Value())
	}
	if _, ok := s["ipv6"]["dns"].Value().([][]byte); !ok {
		t.Errorf("ipv6.dns is %T, want [][]byte", s["ipv6"]["dns"].Value())
	}
	// Otherwise the underlying network's resolvers are merged in, which for a
	// tunnel whose whole point is its own DNS is a leak.
	for _, section := range []string{"ipv4", "ipv6"} {
		if got, _ := s[section]["ignore-auto-dns"].Value().(bool); !got {
			t.Errorf("%s.ignore-auto-dns = %v, want true", section, got)
		}
	}
}

func TestSearchDomainsOnlyOnConfiguredFamilies(t *testing.T) {
	s := settingsFor(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
DNS = 10.0.0.1, corp.example.com
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if got, _ := s["ipv4"]["dns-search"].Value().([]string); len(got) != 1 {
		t.Errorf("ipv4.dns-search = %v", got)
	}
	if _, present := s["ipv6"]["dns-search"]; present {
		t.Error("search domains set on the disabled ipv6 section")
	}
}

// Absent, not zero: NM reads 0 as "no value" for some of these and as a real
// setting for others, so writing a zero we did not mean is a way to get a
// tunnel that will not come up.
func TestOptionalWireguardKeysAreOmittedWhenUnset(t *testing.T) {
	s := settingsFor(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	for _, key := range []string{"listen-port", "mtu", "fwmark"} {
		if _, present := s["wireguard"][key]; present {
			t.Errorf("%s written for a config that does not set it", key)
		}
	}

	withMTU := settingsFor(t, fullTunnel)
	if got, _ := withMTU["wireguard"]["mtu"].Value().(uint32); got != 1380 {
		t.Errorf("mtu = %d, want 1380", got)
	}
}

func TestSettingsRefusesAnEmptyConfig(t *testing.T) {
	if _, err := WireGuardSettings(nil, "x", "x", "u"); err == nil {
		t.Error("nil config accepted")
	}
	if _, err := WireGuardSettings(&common.WireGuardConfig{}, "x", "x", "u"); err == nil {
		t.Error("a config with no peers and no address accepted")
	}
}

// A profile that is not up has no active connection path, and the bus rejects
// an empty one as a malformed message rather than as an error naming the
// connection -- so the guard has to catch it here, where the message can say
// which VPN it was.
func TestToggleVpnRefusesToDeactivateWithoutAnActivePath(t *testing.T) {
	for _, activePath := range []dbus.ObjectPath{"", "/"} {
		msg := ToggleVpnCmd(nil, "/org/freedesktop/NetworkManager/Settings/8", activePath, false)()

		err, ok := msg.(common.ErrMsg)
		if !ok {
			t.Fatalf("deactivating with activePath %q: got %#v, want an ErrMsg", activePath, msg)
		}
		if err.Err == nil {
			t.Fatalf("deactivating with activePath %q: ErrMsg carries no error", activePath)
		}
	}
}
