package common

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// DNS provider identifiers. These are derived from a profile's saved
// nameservers rather than stored anywhere, so they stay in sync with changes
// made outside netpala (nmcli, GNOME settings, etc).
const (
	DNSModeDHCP       = "dhcp"
	DNSModeCloudflare = "cloudflare"
	DNSModeGoogle     = "google"
	DNSModeDNSCrypt   = "dnscrypt"
	DNSModeCustom     = "custom"
)

// DefaultDNSCryptAddresses is where dnscrypt-proxy listens out of the box.
// resolv.conf has no syntax for a port, so the proxy has to be on port 53.
var DefaultDNSCryptAddresses = []string{"127.0.0.1"}

// DNSProvider is a selectable set of nameservers. Empty V4/V6 means the profile
// carries no explicit DNS and falls back to whatever DHCP hands out.
type DNSProvider struct {
	ID    string
	Label string // short form, for the known-networks table
	Desc  string // long form, for the picker
	V4    []string
	V6    []string
}

// DNSProviders is the list offered by the switcher, in display order.
var DNSProviders = []DNSProvider{
	{
		ID:    DNSModeDHCP,
		Label: "DHCP",
		Desc:  "from the router",
	},
	{
		ID:    DNSModeCloudflare,
		Label: "Cloudflare",
		Desc:  "1.1.1.1, 1.0.0.1",
		V4:    []string{"1.1.1.1", "1.0.0.1"},
		V6:    []string{"2606:4700:4700::1111", "2606:4700:4700::1001"},
	},
	{
		ID:    DNSModeGoogle,
		Label: "Google",
		Desc:  "8.8.8.8, 8.8.4.4",
		V4:    []string{"8.8.8.8", "8.8.4.4"},
		V6:    []string{"2001:4860:4860::8888", "2001:4860:4860::8844"},
	},
	{
		ID:    DNSModeDNSCrypt,
		Label: "DNSCrypt",
		Desc:  "local encrypted proxy",
		V4:    DefaultDNSCryptAddresses,
	},
	{
		ID:    DNSModeCustom,
		Label: "Custom",
		Desc:  "your own servers",
	},
}

// DNSProvidersFor returns the provider list with the DNSCrypt entry pointed at
// the configured listener, so a proxy on something other than 127.0.0.1 still
// works. An empty list leaves the built-in default in place.
func DNSProvidersFor(dnscryptAddrs []string) []DNSProvider {
	out := make([]DNSProvider, len(DNSProviders))
	copy(out, DNSProviders)

	if len(dnscryptAddrs) == 0 {
		return out
	}
	var v4, v6 []string
	for _, a := range dnscryptAddrs {
		ip := net.ParseIP(a)
		if ip == nil {
			continue
		}
		if ip.To4() != nil {
			v4 = append(v4, a)
		} else {
			v6 = append(v6, a)
		}
	}
	if len(v4) == 0 && len(v6) == 0 {
		return out
	}
	for i := range out {
		if out[i].ID == DNSModeDNSCrypt {
			out[i].V4, out[i].V6 = v4, v6
			out[i].Desc = strings.Join(dnscryptAddrs, ", ")
		}
	}
	return out
}

// ResolvConfPath is where the system's effective resolvers are published.
const ResolvConfPath = "/etc/resolv.conf"

// resolvConfForTest lets tests point the reader at a fixture.
var resolvConfForTest = ResolvConfPath

// SystemResolvers reports the nameservers the machine will actually use.
//
// This is not necessarily what the connection profile asked for. resolv.conf
// is assembled by resolvconf (or systemd-resolved, or whatever else writes it)
// from several registered sources, and NetworkManager is only one of them --
// a distro-level setting can outrank it entirely.
func SystemResolvers() []string {
	data, err := os.ReadFile(resolvConfForTest)
	if err != nil {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			if net.ParseIP(fields[1]) != nil {
				servers = append(servers, fields[1])
			}
		}
	}
	return servers
}

// IsLoopbackDNS reports whether every server is a loopback address, which is
// what pointing at a local proxy looks like from NetworkManager's side.
func IsLoopbackDNS(servers []string) bool {
	if len(servers) == 0 {
		return false
	}
	for _, s := range servers {
		ip := net.ParseIP(s)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}

// DNSProviderByID looks up a provider, falling back to DHCP.
func DNSProviderByID(id string) DNSProvider {
	for _, p := range DNSProviders {
		if p.ID == id {
			return p
		}
	}
	return DNSProviders[0]
}

// DNSProviderByIDFor looks up a provider in the list built for the configured
// DNSCrypt address, falling back to DHCP.
func DNSProviderByIDFor(id string, dnscryptAddrs []string) DNSProvider {
	list := DNSProvidersFor(dnscryptAddrs)
	for _, p := range list {
		if p.ID == id {
			return p
		}
	}
	return list[0]
}

// DNSModeLabel is the short name shown in the known-networks table.
func DNSModeLabel(id string) string {
	return DNSProviderByID(id).Label
}

// MatchDNSProvider reports which provider a set of IPv4 nameservers matches.
// No servers means DHCP; anything unrecognised reads back as custom.
func MatchDNSProvider(servers []string) string {
	if len(servers) == 0 {
		return DNSModeDHCP
	}
	// Any all-loopback set means a local proxy, whatever address it was
	// configured on. Checked before the table so a non-default listener still
	// reads back as DNSCrypt rather than Custom.
	if IsLoopbackDNS(servers) {
		return DNSModeDNSCrypt
	}

	have := make(map[string]struct{}, len(servers))
	for _, s := range servers {
		have[s] = struct{}{}
	}
	for _, p := range DNSProviders {
		if len(p.V4) == 0 || len(p.V4) != len(have) {
			continue
		}
		match := true
		for _, a := range p.V4 {
			if _, ok := have[a]; !ok {
				match = false
				break
			}
		}
		if match {
			return p.ID
		}
	}
	return DNSModeCustom
}

// ParseDNSServers splits a user-supplied list of addresses (comma, space or
// semicolon separated) into IPv4 and IPv6 buckets.
func ParseDNSServers(s string) (v4 []string, v6 []string, err error) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return nil, nil, fmt.Errorf("no DNS servers given")
	}
	for _, f := range fields {
		ip := net.ParseIP(f)
		if ip == nil {
			return nil, nil, fmt.Errorf("%q is not a valid IP address", f)
		}
		if ip.To4() != nil {
			v4 = append(v4, f)
		} else {
			v6 = append(v6, f)
		}
	}
	return v4, v6, nil
}
