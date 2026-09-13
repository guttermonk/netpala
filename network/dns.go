package network

import (
	"encoding/binary"
	"fmt"
	"net"
	"netpala/common"

	"github.com/godbus/dbus/v5"
)

// ipv4ToNBO packs an IPv4 address the way NetworkManager stores it on D-Bus:
// the four wire-order bytes read back as a native-endian uint32.
func ipv4ToNBO(ip net.IP) (uint32, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return 0, false
	}
	return binary.NativeEndian.Uint32(v4), true
}

// nboToIPv4 is the inverse of ipv4ToNBO.
func nboToIPv4(v uint32) string {
	b := make([]byte, 4)
	binary.NativeEndian.PutUint32(b, v)
	return net.IP(b).String()
}

// DNSv4Variant encodes nameservers as NetworkManager's ipv4 "dns" property ("au").
func DNSv4Variant(addrs []string) (dbus.Variant, error) {
	out := make([]uint32, 0, len(addrs))
	for _, a := range addrs {
		n, ok := ipv4ToNBO(net.ParseIP(a))
		if !ok {
			return dbus.Variant{}, fmt.Errorf("%q is not a valid IPv4 address", a)
		}
		out = append(out, n)
	}
	return dbus.MakeVariant(out), nil
}

// DNSv6Variant encodes nameservers as NetworkManager's ipv6 "dns" property ("aay").
func DNSv6Variant(addrs []string) (dbus.Variant, error) {
	out := make([][]byte, 0, len(addrs))
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil || ip.To16() == nil {
			return dbus.Variant{}, fmt.Errorf("%q is not a valid IPv6 address", a)
		}
		out = append(out, []byte(ip.To16()))
	}
	return dbus.MakeVariant(out), nil
}

// DNSServersFromSettings decodes a profile's explicit IPv4 nameservers, if any.
func DNSServersFromSettings(settings map[string]map[string]dbus.Variant) []string {
	sec := settings["ipv4"]
	if sec == nil {
		return nil
	}
	v, ok := sec["dns"]
	if !ok {
		return nil
	}
	raw, ok := v.Value().([]uint32)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, n := range raw {
		out = append(out, nboToIPv4(n))
	}
	return out
}

// DNSModeFromSettings reports which provider a profile's nameservers match.
func DNSModeFromSettings(settings map[string]map[string]dbus.Variant) string {
	return common.MatchDNSProvider(DNSServersFromSettings(settings))
}
