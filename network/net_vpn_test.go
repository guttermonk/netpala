package network

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func peer(fields map[string]any) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(fields))
	for k, v := range fields {
		out[k] = dbus.MakeVariant(v)
	}
	return out
}

// NetworkManager leaves out any property sitting at its default, and
// connection.autoconnect defaults to true. Reading the absent key as false
// would report the opposite of what the profile actually does.
func TestAutoconnectAbsentMeansOn(t *testing.T) {
	if !autoconnectFrom(map[string]dbus.Variant{}) {
		t.Error("a profile with no autoconnect key reported as off; NM's default is on")
	}
	if !autoconnectFrom(map[string]dbus.Variant{"id": dbus.MakeVariant("wg0")}) {
		t.Error("an unrelated key changed the autoconnect default")
	}
}

func TestAutoconnectReadWhenPresent(t *testing.T) {
	for _, want := range []bool{true, false} {
		settings := map[string]dbus.Variant{"autoconnect": dbus.MakeVariant(want)}
		if got := autoconnectFrom(settings); got != want {
			t.Errorf("autoconnectFrom(%v) = %v, want %v", want, got, want)
		}
	}
}

// A property of the wrong type is a profile netpala cannot read rather than a
// profile that is switched off, so it falls back to the default.
func TestAutoconnectIgnoresAWrongType(t *testing.T) {
	settings := map[string]dbus.Variant{"autoconnect": dbus.MakeVariant("yes")}
	if !autoconnectFrom(settings) {
		t.Error("a non-boolean autoconnect was read as off rather than as unreadable")
	}
}

func TestWireguardEndpointFromFirstPeer(t *testing.T) {
	wg := map[string]dbus.Variant{
		"peers": dbus.MakeVariant([]map[string]dbus.Variant{
			peer(map[string]any{"public-key": "abc=", "endpoint": "185.65.135.170:51820"}),
		}),
	}
	if got := wireguardEndpoint(wg); got != "185.65.135.170:51820" {
		t.Errorf("wireguardEndpoint = %q, want the peer's endpoint", got)
	}
}

// A mesh config has no single far end. Showing only the first peer would read
// as though it were the whole story.
func TestWireguardEndpointCountsTheOtherPeers(t *testing.T) {
	wg := map[string]dbus.Variant{
		"peers": dbus.MakeVariant([]map[string]dbus.Variant{
			peer(map[string]any{"endpoint": "10.0.0.1:51820"}),
			peer(map[string]any{"endpoint": "10.0.0.2:51820"}),
			peer(map[string]any{"endpoint": "10.0.0.3:51820"}),
		}),
	}
	if got := wireguardEndpoint(wg); got != "10.0.0.1:51820 +2" {
		t.Errorf("wireguardEndpoint = %q, want the first endpoint and a count of the rest", got)
	}
}

// A peer with no endpoint is a valid listen-only config, not a broken profile.
func TestWireguardEndpointAbsentIsNotAnError(t *testing.T) {
	for name, wg := range map[string]map[string]dbus.Variant{
		"no wireguard section": {},
		"no peers":             {"peers": dbus.MakeVariant([]map[string]dbus.Variant{})},
		"peer without endpoint": {"peers": dbus.MakeVariant([]map[string]dbus.Variant{
			peer(map[string]any{"public-key": "abc="}),
		})},
	} {
		if got := wireguardEndpoint(wg); got != "" {
			t.Errorf("%s: wireguardEndpoint = %q, want empty", name, got)
		}
	}
}

// The key is the plugin's own business, so each one has to be recognised by
// name. An unknown plugin shows nothing rather than guessing.
func TestPluginEndpointPerPlugin(t *testing.T) {
	for name, tc := range map[string]struct {
		data map[string]string
		want string
	}{
		"openvpn":     {map[string]string{"remote": "vpn.example.com"}, "vpn.example.com"},
		"openconnect": {map[string]string{"gateway": "gw.example.com"}, "gw.example.com"},
		"vpnc":        {map[string]string{"IPSec gateway": "ipsec.example.com"}, "ipsec.example.com"},
		"unknown":     {map[string]string{"server-thing": "who.knows"}, ""},
		"empty":       {map[string]string{}, ""},
		"blank value": {map[string]string{"remote": "   "}, ""},
	} {
		vpn := map[string]dbus.Variant{"data": dbus.MakeVariant(tc.data)}
		if got := pluginEndpoint(vpn); got != tc.want {
			t.Errorf("%s: pluginEndpoint = %q, want %q", name, got, tc.want)
		}
	}
}

func TestPluginEndpointWithoutDataSection(t *testing.T) {
	if got := pluginEndpoint(map[string]dbus.Variant{}); got != "" {
		t.Errorf("pluginEndpoint = %q, want empty", got)
	}
}

// vpnEndpoint has to look in a different place for each connection type: the
// two never carry the endpoint in the same field.
func TestVpnEndpointPicksTheRightSection(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"wireguard": {"peers": dbus.MakeVariant([]map[string]dbus.Variant{
			peer(map[string]any{"endpoint": "wg.example.com:51820"}),
		})},
		"vpn": {"data": dbus.MakeVariant(map[string]string{"remote": "ovpn.example.com"})},
	}

	if got := vpnEndpoint("wireguard", settings); got != "wg.example.com:51820" {
		t.Errorf("wireguard: got %q", got)
	}
	if got := vpnEndpoint("vpn", settings); got != "ovpn.example.com" {
		t.Errorf("vpn: got %q", got)
	}
}
