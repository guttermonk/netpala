package common

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Parsing for the WireGuard configuration files that wg-quick reads, and that
// every commercial provider hands out.
//
// The format is INI-ish but not INI: keys are case-insensitive, values are
// comma- or space-separated lists, and several of the directives are wg-quick's
// own rather than WireGuard's. Only the WireGuard ones have a NetworkManager
// equivalent, so the wg-quick extras are collected in Unsupported rather than
// dropped -- a PostUp that installs a killswitch or points resolv.conf
// somewhere is load-bearing, and importing a config that silently loses it
// would produce a tunnel that looks right and is not.

// WireGuardPeer is one [Peer] section.
type WireGuardPeer struct {
	PublicKey           string
	PresharedKey        string
	Endpoint            string
	AllowedIPs          []string
	PersistentKeepalive uint32
}

// WireGuardConfig is a parsed wg-quick configuration file.
type WireGuardConfig struct {
	// From [Interface].
	PrivateKey    string
	Addresses     []string // CIDR, as written
	DNS           []string // IP addresses only
	SearchDomains []string // non-IP entries of DNS=, which wg-quick allows
	MTU           uint32
	ListenPort    uint32
	FwMark        uint32

	Peers []WireGuardPeer

	// Unsupported names the wg-quick directives found in the file that
	// NetworkManager cannot express. Each entry is "Key = value".
	Unsupported []string
}

// wgQuickOnly are the directives wg-quick implements itself by shelling out,
// which is exactly why NetworkManager has nothing to map them to.
var wgQuickOnly = map[string]bool{
	"preup":      true,
	"postup":     true,
	"predown":    true,
	"postdown":   true,
	"table":      true,
	"saveconfig": true,
}

// ParseWireGuardConfig reads a wg-quick configuration.
//
// It is deliberately strict about the WireGuard directives and lenient about
// everything else: a malformed key is worth refusing the import over, while an
// unknown one is more likely to be a newer wg-quick feature than a mistake.
func ParseWireGuardConfig(text string) (*WireGuardConfig, error) {
	cfg := &WireGuardConfig{}

	section := ""
	seenInterface := false

	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(stripComment(raw))
		if line == "" {
			continue
		}
		lineNo := n + 1

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			switch strings.ToLower(strings.TrimSpace(line[1 : len(line)-1])) {
			case "interface":
				if seenInterface {
					return nil, fmt.Errorf("line %d: a second [Interface] section; a config describes one tunnel", lineNo)
				}
				seenInterface = true
				section = "interface"
			case "peer":
				section = "peer"
				cfg.Peers = append(cfg.Peers, WireGuardPeer{})
			default:
				return nil, fmt.Errorf("line %d: unknown section %s", lineNo, line)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: %q is not a key = value pair", lineNo, line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch section {
		case "interface":
			if err := cfg.setInterfaceKey(key, value, lineNo); err != nil {
				return nil, err
			}
		case "peer":
			if err := cfg.setPeerKey(key, value, lineNo); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("line %d: %q appears before any section header", lineNo, key)
		}
	}

	return cfg, cfg.validate()
}

// stripComment removes a trailing comment. WireGuard uses "#"; ";" is accepted
// too, since it is the other INI convention and costs nothing to allow.
func stripComment(line string) string {
	if i := strings.IndexAny(line, "#;"); i >= 0 {
		return line[:i]
	}
	return line
}

func (c *WireGuardConfig) setInterfaceKey(key, value string, lineNo int) error {
	if wgQuickOnly[key] {
		c.Unsupported = append(c.Unsupported, fmt.Sprintf("%s = %s", key, value))
		return nil
	}

	switch key {
	case "privatekey":
		c.PrivateKey = value
	case "address":
		for _, a := range splitList(value) {
			if _, _, _, err := ParseWireGuardAddress(a); err != nil {
				return fmt.Errorf("line %d: Address: %w", lineNo, err)
			}
			c.Addresses = append(c.Addresses, a)
		}
	case "dns":
		// wg-quick allows search domains here alongside nameservers, telling
		// them apart by whether the entry parses as an IP.
		for _, d := range splitList(value) {
			if net.ParseIP(d) != nil {
				c.DNS = append(c.DNS, d)
			} else {
				c.SearchDomains = append(c.SearchDomains, d)
			}
		}
	case "mtu":
		n, err := parseUint32(value)
		if err != nil {
			return fmt.Errorf("line %d: MTU: %w", lineNo, err)
		}
		c.MTU = n
	case "listenport":
		n, err := parseUint32(value)
		if err != nil {
			return fmt.Errorf("line %d: ListenPort: %w", lineNo, err)
		}
		if n > 65535 {
			return fmt.Errorf("line %d: ListenPort %d is not a port number", lineNo, n)
		}
		c.ListenPort = n
	case "fwmark":
		n, err := parseFwMark(value)
		if err != nil {
			return fmt.Errorf("line %d: FwMark: %w", lineNo, err)
		}
		c.FwMark = n
	}
	// Anything else is left alone: more likely a newer wg-quick key than a
	// mistake, and refusing the whole import over it would be unhelpful.
	return nil
}

func (c *WireGuardConfig) setPeerKey(key, value string, lineNo int) error {
	peer := &c.Peers[len(c.Peers)-1]

	switch key {
	case "publickey":
		peer.PublicKey = value
	case "presharedkey":
		peer.PresharedKey = value
	case "endpoint":
		if _, _, err := net.SplitHostPort(value); err != nil {
			return fmt.Errorf("line %d: Endpoint %q is not host:port", lineNo, value)
		}
		peer.Endpoint = value
	case "allowedips":
		for _, a := range splitList(value) {
			if _, _, _, err := ParseWireGuardAddress(a); err != nil {
				return fmt.Errorf("line %d: AllowedIPs: %w", lineNo, err)
			}
			peer.AllowedIPs = append(peer.AllowedIPs, a)
		}
	case "persistentkeepalive":
		if strings.EqualFold(value, "off") {
			return nil
		}
		n, err := parseUint32(value)
		if err != nil {
			return fmt.Errorf("line %d: PersistentKeepalive: %w", lineNo, err)
		}
		peer.PersistentKeepalive = n
	}
	return nil
}

func (c *WireGuardConfig) validate() error {
	if c.PrivateKey == "" {
		return fmt.Errorf("no PrivateKey in [Interface]; this is not a complete WireGuard config")
	}
	if len(c.Addresses) == 0 {
		return fmt.Errorf("no Address in [Interface]; the tunnel has no address to use")
	}
	if len(c.Peers) == 0 {
		return fmt.Errorf("no [Peer] section; there is nothing to connect to")
	}
	for i, p := range c.Peers {
		if p.PublicKey == "" {
			return fmt.Errorf("[Peer] %d has no PublicKey", i+1)
		}
		if len(p.AllowedIPs) == 0 {
			return fmt.Errorf("[Peer] %d has no AllowedIPs; no traffic would use the tunnel", i+1)
		}
	}
	return nil
}

// RoutesAllTraffic reports whether the config sends everything through the
// tunnel, which is what a commercial provider's config does and what a
// split-tunnel or mesh config does not.
func (c *WireGuardConfig) RoutesAllTraffic() bool {
	for _, p := range c.Peers {
		for _, a := range p.AllowedIPs {
			if a == "0.0.0.0/0" || a == "::/0" || a == "::0/0" {
				return true
			}
		}
	}
	return false
}

// ParseWireGuardAddress splits a CIDR address into its parts. A bare address
// with no prefix is a host address, which is how wg-quick reads it.
func ParseWireGuardAddress(s string) (ip string, prefix uint32, isV6 bool, err error) {
	addr, mask, found := strings.Cut(s, "/")

	parsed := net.ParseIP(strings.TrimSpace(addr))
	if parsed == nil {
		return "", 0, false, fmt.Errorf("%q is not a valid IP address", s)
	}
	isV6 = parsed.To4() == nil

	hostBits := uint32(32)
	if isV6 {
		hostBits = 128
	}

	if !found {
		return addr, hostBits, isV6, nil
	}

	n, err := strconv.ParseUint(strings.TrimSpace(mask), 10, 32)
	if err != nil {
		return "", 0, false, fmt.Errorf("%q has an unreadable prefix length", s)
	}
	if uint32(n) > hostBits {
		return "", 0, false, fmt.Errorf("%q has a prefix length above /%d", s, hostBits)
	}
	return addr, uint32(n), isV6, nil
}

// splitList breaks a comma- or whitespace-separated value into its entries.
func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
}

func parseUint32(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return uint32(n), nil
}

// parseFwMark accepts the hexadecimal form wg-quick writes as well as decimal.
func parseFwMark(s string) (uint32, error) {
	if strings.EqualFold(s, "off") {
		return 0, nil
	}
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "0x") {
		n, err := strconv.ParseUint(lower[2:], 16, 32)
		if err != nil {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		return uint32(n), nil
	}
	return parseUint32(s)
}

// ExpandPath resolves a leading "~" against the user's home directory.
//
// The shell does this before a program ever sees a path, so a path typed into
// a text input inside a TUI is the one place it has to be done by hand -- and
// "~/wg0.conf" is what anyone will type.
func ExpandPath(path string) string {
	path = strings.TrimSpace(path)
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// LoadWireGuardConfig reads and parses a config file, returning the name the
// profile should take alongside it.
//
// The name comes from the file, the way wg-quick derives the interface from it:
// mullvad-se.conf is the mullvad-se tunnel. That is already what the user calls
// the thing.
func LoadWireGuardConfig(path string) (cfg *WireGuardConfig, id, ifname string, err error) {
	path = ExpandPath(path)
	if path == "" {
		return nil, "", "", fmt.Errorf("no file given")
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf("%s does not exist", path)
		}
		return nil, "", "", fmt.Errorf("cannot read %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, "", "", fmt.Errorf("%s is a directory, not a config file", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("cannot read %s: %w", path, err)
	}

	cfg, err = ParseWireGuardConfig(string(data))
	if err != nil {
		return nil, "", "", err
	}

	base := filepath.Base(path)
	id = strings.TrimSuffix(base, filepath.Ext(base))
	if id == "" {
		id = base
	}

	ifname, err = WireGuardInterfaceName(base)
	if err != nil {
		return nil, "", "", err
	}
	return cfg, id, ifname, nil
}

// WireGuardInterfaceName turns a config's file name into a usable Linux
// interface name, the same way wg-quick does.
//
// The kernel's limit is 15 bytes and it rejects "/" and whitespace, so a name
// that would be refused has to be cut down here rather than at activation time,
// where the failure arrives as an opaque error from NetworkManager.
func WireGuardInterfaceName(base string) (string, error) {
	base = strings.TrimSuffix(base, ".conf")

	var b strings.Builder
	for _, r := range base {
		switch {
		case r == '/' || r == ':' || r == ' ' || r == '\t' || r == '\n':
			b.WriteRune('-')
		case r < 32 || r == 127:
			// Skip: a control character in an interface name is never wanted.
		default:
			b.WriteRune(r)
		}
	}

	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "", fmt.Errorf("the file name gives nothing usable as an interface name")
	}
	if len(name) > 15 {
		name = name[:15]
	}
	return name, nil
}
