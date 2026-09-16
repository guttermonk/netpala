package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Mullvad config, which is the shape most people will import.
const mullvadConf = `[Interface]
PrivateKey = qGZk4xFQ3l7pXvBn8hJtYcRmWs2Dv0aNpLuEi9OgXFo=
Address = 10.64.0.2/32,fc00:bbbb:bbbb:bb01::1:5c1/128
DNS = 10.64.0.1

[Peer]
PublicKey = /vN1Hq8YpKxTzRcLmWdFgBnJs3Qa7ZuEi0OpXvYtCkM=
AllowedIPs = 0.0.0.0/0,::0/0
Endpoint = 185.65.135.170:51820
`

func TestParseMullvadConfig(t *testing.T) {
	cfg, err := ParseWireGuardConfig(mullvadConf)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}

	if cfg.PrivateKey != "qGZk4xFQ3l7pXvBn8hJtYcRmWs2Dv0aNpLuEi9OgXFo=" {
		t.Errorf("PrivateKey = %q", cfg.PrivateKey)
	}
	if len(cfg.Addresses) != 2 {
		t.Errorf("Addresses = %v, want both families", cfg.Addresses)
	}
	if len(cfg.DNS) != 1 || cfg.DNS[0] != "10.64.0.1" {
		t.Errorf("DNS = %v", cfg.DNS)
	}
	if len(cfg.Peers) != 1 {
		t.Fatalf("Peers = %d, want 1", len(cfg.Peers))
	}
	if cfg.Peers[0].Endpoint != "185.65.135.170:51820" {
		t.Errorf("Endpoint = %q", cfg.Peers[0].Endpoint)
	}
	if !cfg.RoutesAllTraffic() {
		t.Error("a 0.0.0.0/0 config did not report that it routes everything")
	}
}

// wg-quick matches keys without regard to case, so a config written in any of
// the conventional spellings has to import.
func TestParseIsCaseInsensitive(t *testing.T) {
	cfg, err := ParseWireGuardConfig(`[interface]
privatekey = abc=
address = 10.0.0.2/32
[PEER]
PUBLICKEY = def=
ALLOWEDIPS = 10.0.0.0/24
`)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}
	if cfg.PrivateKey != "abc=" || cfg.Peers[0].PublicKey != "def=" {
		t.Errorf("keys not matched case-insensitively: %+v", cfg)
	}
}

func TestParseComments(t *testing.T) {
	cfg, err := ParseWireGuardConfig(`# a leading comment
[Interface]
PrivateKey = abc=   # trailing
Address = 10.0.0.2/32
; semicolon comment
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}
	if cfg.PrivateKey != "abc=" {
		t.Errorf("PrivateKey = %q, want the comment stripped", cfg.PrivateKey)
	}
}

// A PostUp can be doing something load-bearing. Importing a config that
// silently loses it would produce a tunnel that looks right and is not, so the
// directives NM cannot express have to come back to the caller.
func TestUnsupportedDirectivesAreReported(t *testing.T) {
	cfg, err := ParseWireGuardConfig(`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
PostUp = iptables -I OUTPUT ! -o %i -m mark ! --mark $(wg show %i fwmark) -j REJECT
Table = off
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}
	if len(cfg.Unsupported) != 2 {
		t.Fatalf("Unsupported = %v, want PostUp and Table", cfg.Unsupported)
	}
	joined := strings.Join(cfg.Unsupported, " ")
	for _, want := range []string{"postup", "table"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q not reported as unsupported: %v", want, cfg.Unsupported)
		}
	}
}

// wg-quick allows search domains in DNS=, told apart from nameservers by
// whether they parse as an address.
func TestDNSSplitsAddressesFromSearchDomains(t *testing.T) {
	cfg, err := ParseWireGuardConfig(`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
DNS = 10.0.0.1, corp.example.com, fd00::1
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}
	if len(cfg.DNS) != 2 {
		t.Errorf("DNS = %v, want the two addresses", cfg.DNS)
	}
	if len(cfg.SearchDomains) != 1 || cfg.SearchDomains[0] != "corp.example.com" {
		t.Errorf("SearchDomains = %v", cfg.SearchDomains)
	}
}

func TestParseMultiplePeers(t *testing.T) {
	cfg, err := ParseWireGuardConfig(`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/24
[Peer]
PublicKey = one=
AllowedIPs = 10.0.0.1/32
PersistentKeepalive = 25
[Peer]
PublicKey = two=
AllowedIPs = 10.0.0.3/32
PresharedKey = psk=
`)
	if err != nil {
		t.Fatalf("ParseWireGuardConfig: %v", err)
	}
	if len(cfg.Peers) != 2 {
		t.Fatalf("Peers = %d, want 2", len(cfg.Peers))
	}
	if cfg.Peers[0].PersistentKeepalive != 25 {
		t.Errorf("keepalive = %d", cfg.Peers[0].PersistentKeepalive)
	}
	if cfg.Peers[1].PresharedKey != "psk=" {
		t.Errorf("preshared key = %q", cfg.Peers[1].PresharedKey)
	}
	if cfg.RoutesAllTraffic() {
		t.Error("a split-tunnel config claimed to route everything")
	}
}

// Refusing a config outright is better than importing half of one: an
// incomplete profile fails later, from inside NetworkManager, where the
// message has nothing to do with the file.
func TestIncompleteConfigsAreRefused(t *testing.T) {
	for name, tc := range map[string]struct{ conf, wantErr string }{
		"no private key": {`[Interface]
Address = 10.0.0.2/32
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`, "PrivateKey"},
		"no address": {`[Interface]
PrivateKey = abc=
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`, "Address"},
		"no peer": {`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
`, "Peer"},
		"peer without public key": {`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
[Peer]
AllowedIPs = 0.0.0.0/0
`, "PublicKey"},
		"peer without allowed ips": {`[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
[Peer]
PublicKey = def=
`, "AllowedIPs"},
	} {
		_, err := ParseWireGuardConfig(tc.conf)
		if err == nil {
			t.Errorf("%s: accepted an incomplete config", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", name, err, tc.wantErr)
		}
	}
}

func TestMalformedValuesAreRefused(t *testing.T) {
	for name, conf := range map[string]string{
		"bad address": `[Interface]
PrivateKey = abc=
Address = not-an-ip/32
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`,
		"prefix too long": `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/33
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
`,
		"endpoint without port": `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/32
[Peer]
PublicKey = def=
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.com
`,
		"line without a key": `[Interface]
PrivateKey = abc=
nonsense
`,
		"key before any section": `PrivateKey = abc=
[Interface]
Address = 10.0.0.2/32
`,
	} {
		if _, err := ParseWireGuardConfig(conf); err == nil {
			t.Errorf("%s: accepted a malformed config", name)
		}
	}
}

func TestParseAddress(t *testing.T) {
	for name, tc := range map[string]struct {
		in     string
		ip     string
		prefix uint32
		isV6   bool
	}{
		"v4 with prefix": {"10.64.0.2/32", "10.64.0.2", 32, false},
		"v4 subnet":      {"10.0.0.2/24", "10.0.0.2", 24, false},
		// wg-quick reads a bare address as a host address.
		"v4 bare":        {"10.64.0.2", "10.64.0.2", 32, false},
		"v6 with prefix": {"fc00::1/128", "fc00::1", 128, true},
		"v6 bare":        {"fc00::1", "fc00::1", 128, true},
		"default route":  {"0.0.0.0/0", "0.0.0.0", 0, false},
	} {
		ip, prefix, isV6, err := ParseWireGuardAddress(tc.in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if ip != tc.ip || prefix != tc.prefix || isV6 != tc.isV6 {
			t.Errorf("%s: got (%q, %d, %v), want (%q, %d, %v)",
				name, ip, prefix, isV6, tc.ip, tc.prefix, tc.isV6)
		}
	}
}

// The kernel rejects an interface name over 15 bytes or containing a slash, so
// a file name that would produce one has to be cut down before NetworkManager
// is asked to create the link.
func TestInterfaceNameFitsTheKernelsRules(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"plain":        {"mullvad-se.conf", "mullvad-se"},
		"no extension": {"wg0", "wg0"},
		"too long":     {"a-very-long-provider-name.conf", "a-very-long-pro"},
		"slashes":      {"we/ird.conf", "we-ird"},
		"leading dots": {".hidden.conf", "hidden"},
		"spaces":       {"my tunnel.conf", "my-tunnel"},
	} {
		got, err := WireGuardInterfaceName(tc.in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: WireGuardInterfaceName(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
		if len(got) > 15 {
			t.Errorf("%s: %q is %d bytes, over the kernel's limit", name, got, len(got))
		}
	}

	if _, err := WireGuardInterfaceName("....conf"); err == nil {
		t.Error("a file name with nothing usable in it was accepted")
	}
}

func TestLoadWireGuardConfigNamesTheProfileAfterTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mullvad-se.conf")
	if err := os.WriteFile(path, []byte(mullvadConf), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, id, ifname, err := LoadWireGuardConfig(path)
	if err != nil {
		t.Fatalf("LoadWireGuardConfig: %v", err)
	}
	if id != "mullvad-se" {
		t.Errorf("id = %q, want the file's base name", id)
	}
	if ifname != "mullvad-se" {
		t.Errorf("ifname = %q", ifname)
	}
	if cfg == nil || len(cfg.Peers) != 1 {
		t.Error("config not parsed")
	}
}

// The error has to name the file, since a mistyped path is the most likely
// reason to land here.
func TestLoadWireGuardConfigReportsAMissingFile(t *testing.T) {
	_, _, _, err := LoadWireGuardConfig(filepath.Join(t.TempDir(), "nope.conf"))
	if err == nil {
		t.Fatal("a missing file was accepted")
	}
	if !strings.Contains(err.Error(), "nope.conf") {
		t.Errorf("error %q does not name the file", err)
	}
}

func TestLoadWireGuardConfigRejectsADirectory(t *testing.T) {
	_, _, _, err := LoadWireGuardConfig(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Errorf("error = %v, want it to say the path is a directory", err)
	}
}
