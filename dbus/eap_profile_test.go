package dbus

import (
	"os"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func readSource(t *testing.T, path string) string {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// functionSource is one top-level function, from its declaration to the start
// of the next one.
func functionSource(t *testing.T, src, name string) string {
	t.Helper()

	start := strings.Index(src, "\nfunc "+name+"(")
	if start < 0 {
		t.Fatalf("%s not found", name)
	}
	rest := src[start+1:]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		return rest[:end]
	}
	return rest
}

func eapSection(t *testing.T, config map[string]string) map[string]dbus.Variant {
	t.Helper()

	settings := eapProfileSettings(config, "eduroam", "PEAP", "someone@example.net", "uuid")
	sec, ok := settings["802-1x"]
	if !ok {
		t.Fatal("no 802-1x section in an EAP profile")
	}
	return sec
}

// nm-settings-dbus(5) on 802-1x ca-cert: byte array, and under the path scheme
// "the full UTF-8 encoded path of the certificate, prefixed with the string
// \"file://\" and ending with a terminating NUL byte". A string gets as far as
// compiling and no further -- NetworkManager rejects the profile, so the
// network cannot be saved at all, and the error names the connection rather
// than the field.
func TestCaCertIsABytePathWithItsTerminator(t *testing.T) {
	v, ok := eapSection(t, map[string]string{"ca_cert": "/etc/ssl/certs/ca.pem"})["ca-cert"]
	if !ok {
		t.Fatal("ca-cert absent although a path was given")
	}

	b, ok := v.Value().([]byte)
	if !ok {
		t.Fatalf("ca-cert holds %T, want []byte -- the property is a byte array", v.Value())
	}
	if got := string(b); got != "file:///etc/ssl/certs/ca.pem\x00" {
		t.Errorf("ca-cert = %q, want the file:// path with a terminating NUL", got)
	}
}

// Unset and empty are different things to NetworkManager: unset means the
// server certificate is not validated, and empty is undefined. Only one of
// them is a decision anyone made.
func TestNoCaCertLeavesThePropertyUnset(t *testing.T) {
	for _, given := range []string{"", "   ", "\t"} {
		if _, ok := eapSection(t, map[string]string{"ca_cert": given})["ca-cert"]; ok {
			t.Errorf("ca-cert present for %q, want the key absent", given)
		}
	}
}

// Whitespace round a pasted path would otherwise be sent as part of it, and
// the failure is a certificate that cannot be read rather than a bad path.
func TestCaCertPathIsTrimmed(t *testing.T) {
	v := eapSection(t, map[string]string{"ca_cert": "  /etc/ssl/certs/ca.pem\n"})["ca-cert"]
	b, _ := v.Value().([]byte)
	if got := string(b); got != "file:///etc/ssl/certs/ca.pem\x00" {
		t.Errorf("ca-cert = %q, want the path without the surrounding whitespace", got)
	}
}

// The form offers them in the case it displays; wpa_supplicant wants them in
// lower case.
func TestEapAndPhase2AreLowercased(t *testing.T) {
	sec := eapSection(t, map[string]string{"phase2-auth": "MSCHAPV2"})

	methods, ok := sec["eap"].Value().([]string)
	if !ok || len(methods) != 1 {
		t.Fatalf("eap holds %#v, want a one-element []string", sec["eap"].Value())
	}
	if methods[0] != "peap" {
		t.Errorf("eap = %q, want %q", methods[0], "peap")
	}
	if got, _ := sec["phase2-auth"].Value().(string); got != "mschapv2" {
		t.Errorf("phase2-auth = %q, want %q", got, "mschapv2")
	}
}

// NONE is the form's way of saying there is no inner method, not a method
// called none.
func TestPhase2NoneIsNotSent(t *testing.T) {
	for _, given := range []string{"", "NONE"} {
		if _, ok := eapSection(t, map[string]string{"phase2-auth": given})["phase2-auth"]; ok {
			t.Errorf("phase2-auth present for %q, want the key absent", given)
		}
	}
}

// The access point the SSID was picked from, or NetworkManager's "/" for no
// preference. Several access points can carry one SSID, and without this the
// choice between them is not the one the signal column described.
func TestSpecificObjectIsTheAccessPointOrNoPreference(t *testing.T) {
	const ap = "/org/freedesktop/NetworkManager/AccessPoint/7"

	if got := specificObject(ap); string(got) != ap {
		t.Errorf("specificObject(%q) = %q, want it passed through", ap, got)
	}
	if got := specificObject(""); string(got) != "/" {
		t.Errorf(`specificObject("") = %q, want "/"`, got)
	}
}

// The profile is stored by NetworkManager, so a key of the wrong type is
// rejected whole. These are the ones with a type that is not the obvious one.
func TestTheProfileUsesTheDeclaredDBusTypes(t *testing.T) {
	settings := eapProfileSettings(map[string]string{}, "eduroam", "TTLS", "someone", "uuid")

	if _, ok := settings["802-11-wireless"]["ssid"].Value().([]byte); !ok {
		t.Errorf("802-11-wireless ssid holds %T, want []byte",
			settings["802-11-wireless"]["ssid"].Value())
	}
	if _, ok := settings["802-1x"]["eap"].Value().([]string); !ok {
		t.Errorf("802-1x eap holds %T, want []string", settings["802-1x"]["eap"].Value())
	}
	if id, _ := settings["connection"]["id"].Value().(string); id != "eduroam" {
		t.Errorf("connection id = %q, want the SSID", id)
	}
}

// Activation is part of the add now. Asking NetworkManager to activate a
// profile it has just been given, in a second call, races its own autoconnect:
// the profile is created with autoconnect on, so it may already be coming up.
func TestTheEapProfileIsAddedAndActivatedInOneCall(t *testing.T) {
	src := readSource(t, "actions.go")
	eap := functionSource(t, src, "AddAndConnectEAPCmd")

	if !strings.Contains(eap, "AddAndActivateConnection") {
		t.Error("the EAP path no longer uses AddAndActivateConnection")
	}
	if strings.Contains(eap, "Settings.AddConnection") {
		t.Error("the EAP path adds the profile separately again")
	}
	if strings.Contains(eap, "ConnectToNetworkCmd") {
		t.Error("the EAP path activates in a second call again")
	}
}
