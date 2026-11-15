package dbus

import (
	"testing"

	"netpala/common"

	"github.com/godbus/dbus/v5"
)

// savedPsk is a profile as NetworkManager hands one over, including the two
// fields that cannot be sent back.
func savedPsk() map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{
		"connection":      {"id": dbus.MakeVariant("home")},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("home"))},
		"802-11-wireless-security": {
			"key-mgmt": dbus.MakeVariant("wpa-psk"),
			"psk":      dbus.MakeVariant("the old one"),
		},
		"ipv4": {
			"method":    dbus.MakeVariant("auto"),
			"addresses": dbus.MakeVariant([][]uint32{{1, 2, 3}}),
			"routes":    dbus.MakeVariant([][]uint32{{4, 5, 6}}),
		},
	}
}

func savedEap() map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{
		"connection":      {"id": dbus.MakeVariant("eduroam")},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte("eduroam"))},
		"802-1x": {
			"eap":            dbus.MakeVariant([]string{"peap"}),
			"phase2-auth":    dbus.MakeVariant("mschapv2"),
			"identity":       dbus.MakeVariant("someone@example.net"),
			"password":       dbus.MakeVariant("the old one"),
			"password-flags": dbus.MakeVariant(uint32(0)),
			"ca-cert":        dbus.MakeVariant([]byte("file:///etc/ssl/certs/old.pem\x00")),
		},
	}
}

func str(t *testing.T, section map[string]dbus.Variant, key string) string {
	t.Helper()
	v, ok := section[key]
	if !ok {
		t.Fatalf("%s absent", key)
	}
	s, _ := v.Value().(string)
	return s
}

// The key goes in, and the key management does not move: this edit changes the
// password, not what kind of network it is. The property is "psk" for SAE as
// well, which is why there is nothing to choose between here.
func TestANewKeyReplacesTheOldOne(t *testing.T) {
	settings := savedPsk()
	if err := applySecretToSettings(settings, "the new one"); err != nil {
		t.Fatal(err)
	}

	sec := settings["802-11-wireless-security"]
	if got := str(t, sec, "psk"); got != "the new one" {
		t.Errorf("psk = %q, want the new one", got)
	}
	if got := str(t, sec, "key-mgmt"); got != "wpa-psk" {
		t.Errorf("key-mgmt = %q, want it left alone", got)
	}
	if flags, _ := sec["psk-flags"].Value().(uint32); flags != 0 {
		t.Errorf("psk-flags = %d, want 0 -- a secret the system stores", flags)
	}
}

// Nothing to write a key into, and adding a section would mean guessing which
// mechanism it should name.
func TestAProfileWithNoKeyIsRefused(t *testing.T) {
	settings := map[string]map[string]dbus.Variant{
		"connection": {"id": dbus.MakeVariant("cafe")},
	}
	if err := applySecretToSettings(settings, "a key"); err == nil {
		t.Error("an open profile accepted a password")
	}
}

// The round-trip fields NetworkManager cannot take back. Every writer in the
// program strips them; an edit that forgot to would fail on the type of a
// field the user never touched.
func TestTheEditWritersStripTheFieldsThatCannotGoBack(t *testing.T) {
	settings := savedPsk()
	cleanSettingsForUpdate(settings)

	for _, field := range []string{"addresses", "routes"} {
		if _, present := settings["ipv4"][field]; present {
			t.Errorf("ipv4 %s survived the clean", field)
		}
	}
	if got := str(t, settings["ipv4"], "method"); got != "auto" {
		t.Errorf("ipv4 method = %q, want auto -- only the two go", got)
	}
}

// What the form shows is written; what it does not carry is left as it was.
func TestAnEapEditWritesTheFieldsTheFormCarries(t *testing.T) {
	settings := savedEap()
	applyEapEditToSettings(settings, map[string]string{
		"eap":         "TTLS",
		"phase2-auth": "PAP",
		"identity":    "someone-else@example.net",
		"password":    "the new one",
		"ca_cert":     "/etc/ssl/certs/new.pem",
	})

	sec := settings["802-1x"]
	methods, _ := sec["eap"].Value().([]string)
	if len(methods) != 1 || methods[0] != "ttls" {
		t.Errorf("eap = %#v, want [ttls] -- lower case, as the supplicant wants", sec["eap"].Value())
	}
	if got := str(t, sec, "phase2-auth"); got != "pap" {
		t.Errorf("phase2-auth = %q, want pap", got)
	}
	if got := str(t, sec, "identity"); got != "someone-else@example.net" {
		t.Errorf("identity = %q", got)
	}
	if got := str(t, sec, "password"); got != "the new one" {
		t.Errorf("password = %q", got)
	}
	cert, _ := sec["ca-cert"].Value().([]byte)
	if string(cert) != "file:///etc/ssl/certs/new.pem\x00" {
		t.Errorf("ca-cert = %q, want the new path encoded as bytes with its terminator", cert)
	}
}

// The rule the popup states. A form that was not allowed to read the stored
// secret shows an empty field, so an empty field cannot mean "store nothing".
func TestAnEmptyPasswordLeavesTheStoredOneAlone(t *testing.T) {
	settings := savedEap()
	applyEapEditToSettings(settings, map[string]string{
		"eap":      "PEAP",
		"identity": "someone@example.net",
		"password": "",
	})

	if got := str(t, settings["802-1x"], "password"); got != "the old one" {
		t.Errorf("password = %q, want the stored one untouched", got)
	}
}

// Clearing them is an instruction, unlike clearing the password: the fields
// show what the profile holds, so an empty one means the profile should not
// hold it either. NONE is the form's way of saying there is no inner method.
func TestClearingTheCertificateOrInnerMethodRemovesIt(t *testing.T) {
	settings := savedEap()
	applyEapEditToSettings(settings, map[string]string{
		"eap":         "TLS",
		"identity":    "someone@example.net",
		"phase2-auth": "NONE",
		"ca_cert":     "",
	})

	sec := settings["802-1x"]
	if _, present := sec["phase2-auth"]; present {
		t.Error("phase2-auth survived being set to NONE")
	}
	if _, present := sec["ca-cert"]; present {
		t.Error("ca-cert survived being cleared")
	}
}

// A form that could not read a field must not blank it on save.
func TestAnEapEditWithNothingToSayChangesNothing(t *testing.T) {
	settings := savedEap()
	before := str(t, settings["802-1x"], "identity")

	applyEapEditToSettings(settings, map[string]string{})

	sec := settings["802-1x"]
	if got := str(t, sec, "identity"); got != before {
		t.Errorf("identity = %q, want %q", got, before)
	}
	methods, _ := sec["eap"].Value().([]string)
	if len(methods) != 1 || methods[0] != "peap" {
		t.Errorf("eap = %#v, want the stored [peap]", sec["eap"].Value())
	}
}

// The form opens on the profile, so the profile has to be read back into the
// shape the form shows -- including the parts NetworkManager stores
// differently to how they were typed.
func TestAProfileReadsBackIntoTheFormsFields(t *testing.T) {
	profile := eapProfileFromSettings(savedEap())

	if profile.EapMethod != "PEAP" {
		t.Errorf("EapMethod = %q, want PEAP -- upper case, as the form lists them", profile.EapMethod)
	}
	if profile.Phase2Auth != "MSCHAPV2" {
		t.Errorf("Phase2Auth = %q, want MSCHAPV2", profile.Phase2Auth)
	}
	if profile.Identity != "someone@example.net" {
		t.Errorf("Identity = %q", profile.Identity)
	}
	if profile.CaCert != "/etc/ssl/certs/old.pem" {
		t.Errorf("CaCert = %q, want the path without the scheme or the terminator", profile.CaCert)
	}
	// The secret is not in GetSettings, and its absence is not an error.
	if profile.Password != "" {
		t.Errorf("Password = %q, want empty until GetSecrets answers", profile.Password)
	}
}

// A profile that is not what it was asked about, or malformed, must come back
// empty rather than half-read.
func TestAProfileWithNoEapSectionReadsBackEmpty(t *testing.T) {
	profile := eapProfileFromSettings(savedPsk())

	if profile != (common.EapProfileMsg{}) {
		t.Errorf("got %#v, want the zero profile", profile)
	}
}
