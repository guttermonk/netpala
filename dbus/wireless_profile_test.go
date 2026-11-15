package dbus

import (
	"strings"
	"testing"

	"netpala/common"

	"github.com/godbus/dbus/v5"
)

func scanResult(ssid, security string) common.ScannedNetwork {
	return common.ScannedNetwork{
		Path:     dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/3"),
		SSID:     ssid,
		Security: security,
		Signal:   64,
	}
}

func securitySection(t *testing.T, security, password string) (map[string]dbus.Variant, bool) {
	t.Helper()

	settings := wirelessProfileSettings(scanResult("home", security), password, "uuid")
	sec, ok := settings["802-11-wireless-security"]
	return sec, ok
}

// An open network has nothing to key, and a security section with no key-mgmt
// in it is not the same as no section.
func TestAnOpenNetworkHasNoSecuritySection(t *testing.T) {
	if _, ok := securitySection(t, "open", ""); ok {
		t.Error("an open network was given a security section")
	}
}

func TestPskAndSaeGetTheirOwnKeyManagement(t *testing.T) {
	for _, tt := range []struct {
		security string
		keyMgmt  string
	}{
		{"wpa2-psk", "wpa-psk"},
		{"wpa3-sae", "sae"},
	} {
		t.Run(tt.security, func(t *testing.T) {
			sec, ok := securitySection(t, tt.security, "a good password")
			if !ok {
				t.Fatal("no security section")
			}
			if got, _ := sec["key-mgmt"].Value().(string); got != tt.keyMgmt {
				t.Errorf("key-mgmt = %q, want %q", got, tt.keyMgmt)
			}
			if got, _ := sec["psk"].Value().(string); got != "a good password" {
				t.Errorf("psk = %q, want the password as typed", got)
			}
		})
	}
}

// OWE is encrypted without a shared secret, so it needs the key management and
// no key. It used to get neither: the string a scan makes for it reached the
// password prompt, and the profile was then stored as WPA2-PSK with whatever
// had been typed, which cannot associate with an OWE access point.
func TestOweIsKeyedWithoutAPassword(t *testing.T) {
	sec, ok := securitySection(t, "wpa-owe", "")
	if !ok {
		t.Fatal("no security section; an OWE profile needs key-mgmt")
	}
	if got, _ := sec["key-mgmt"].Value().(string); got != "owe" {
		t.Errorf("key-mgmt = %q, want owe", got)
	}
	if _, present := sec["psk"]; present {
		t.Error("a psk was stored for a network that has none")
	}
}

// A transition-mode access point offers both, and wpa-psk is the one
// nm-settings-dbus(5) documents as covering WPA2 and WPA3 personal.
func TestTransitionModeIsKeyedAsPsk(t *testing.T) {
	sec, ok := securitySection(t, "wpa3-sae / wpa2-psk", "a good password")
	if !ok {
		t.Fatal("no security section")
	}
	if got, _ := sec["key-mgmt"].Value().(string); got != "wpa-psk" {
		t.Errorf("key-mgmt = %q, want wpa-psk", got)
	}
}

// The deprecated pointer in the wireless section is only meant to name a
// section that exists.
func TestOnlyASecuredProfileNamesItsSecuritySection(t *testing.T) {
	secured := wirelessProfileSettings(scanResult("home", "wpa2-psk"), "pw", "uuid")
	if _, ok := secured["802-11-wireless"]["security"]; !ok {
		t.Error("a secured profile does not name its security section")
	}

	open := wirelessProfileSettings(scanResult("cafe", "open"), "", "uuid")
	if _, ok := open["802-11-wireless"]["security"]; ok {
		t.Error("an open profile names a security section it does not have")
	}
}

// The SSID is a byte array here too -- it is the same property as in an EAP
// profile, and NetworkManager rejects the profile rather than the field.
func TestTheWirelessProfileUsesTheDeclaredDBusTypes(t *testing.T) {
	settings := wirelessProfileSettings(scanResult("home", "wpa2-psk"), "pw", "uuid")

	if _, ok := settings["802-11-wireless"]["ssid"].Value().([]byte); !ok {
		t.Errorf("ssid holds %T, want []byte", settings["802-11-wireless"]["ssid"].Value())
	}
	if got, _ := settings["connection"]["id"].Value().(string); got != "home" {
		t.Errorf("connection id = %q, want the SSID", got)
	}
	if got, _ := settings["connection"]["type"].Value().(string); got != "802-11-wireless" {
		t.Errorf("connection type = %q, want 802-11-wireless", got)
	}
}

// The ordinary path gets what the EAP path got: one call, on the access point
// the row was read from. This is the common case -- every home and cafe
// network goes through here -- and a router advertising one SSID on two bands
// is the normal arrangement, so being able to say which one is the point.
func TestTheWirelessProfileIsAddedAndActivatedOnItsAccessPoint(t *testing.T) {
	psk := functionSource(t, readSource(t, "actions.go"), "AddAndConnectToNetworkCmd")

	if !strings.Contains(psk, "AddAndActivateConnection") {
		t.Error("the PSK path no longer uses AddAndActivateConnection")
	}
	if !strings.Contains(psk, "specificObject(string(net.Path))") {
		t.Error("the access point from the scan result is not passed as the specific object")
	}
	if strings.Contains(psk, "Settings.AddConnection") {
		t.Error("the PSK path adds the profile separately again")
	}
	// The giveaway of the two-call form, rather than the name of the command
	// it called: this function's own name contains that of the other one.
	if strings.Contains(psk, "newConnectionPath") {
		t.Error("the PSK path reads the new profile's path back to activate it in a second call")
	}
}
