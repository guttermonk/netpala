package main

import (
	"netpala/common"
	"netpala/models"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

// scannedWith puts one network in New Networks with the cursor on it.
func scannedWith(security string) NetpalaData {
	m := safeListener(linkModel(false, nil))
	// The real one, because opening it focuses the field and a zero textinput
	// has no cursor to focus.
	m.PasswordForm = models.ModelPasswordInput(m.Colors)
	m.ScannedNetworks = []common.ScannedNetwork{{
		Path:     godbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/9"),
		SSID:     "somewhere",
		Security: security,
		Signal:   58,
	}}
	m.selectedBox = common.PaneScanned
	m.SelectedEntry = 0
	return m
}

const (
	popupNone     = -1
	popupEapForm  = 0
	popupPassword = 2
)

// Which popup a scan result opens is a decision about the mechanism, not about
// the spelling of a display string. Each of these used to be matched exactly,
// so anything written differently fell through to the password prompt.
func TestEachMechanismReachesTheRightPrompt(t *testing.T) {
	for _, tt := range []struct {
		security string
		want     int
		why      string
	}{
		{"wpa2-psk", popupPassword, "a key to type"},
		{"wpa3-sae", popupPassword, "a key to type"},
		{"wpa2-eap", popupEapForm, "identity and certificate, not a key"},
		{"wpa3-eap-192", popupEapForm, "enterprise, so the EAP form rather than a password"},
		{"wpa2-eap / wpa2-psk", popupEapForm, "enterprise wins over the key it also offers"},
		{"open", popupNone, "nothing to ask for"},
		{"wpa-owe", popupNone, "encrypted, but with no shared secret to type"},
		{"wpa3-sae / wpa2-psk", popupPassword, "transition mode still has a key"},
	} {
		t.Run(tt.security, func(t *testing.T) {
			next, cmd := scannedWith(tt.security).Update(selectKey())
			got := next.(NetpalaData)

			if got.PopupState != tt.want {
				t.Errorf("PopupState = %d, want %d -- %s", got.PopupState, tt.want, tt.why)
			}
			// No popup means it went straight at the network instead.
			if tt.want == popupNone && cmd == nil {
				t.Error("no popup and no connection either")
			}
		})
	}
}

// And the EAP form is handed the network either way round, so the profile it
// builds activates on the access point that was selected.
func TestTheEapFormIsSetUpForWhicheverEapStringArrived(t *testing.T) {
	for _, security := range []string{"wpa2-eap", "wpa3-eap-192"} {
		next, _ := scannedWith(security).Update(selectKey())
		got := next.(NetpalaData)

		if got.Form.SSIDSelected != "somewhere" {
			t.Errorf("%s: SSIDSelected = %q", security, got.Form.SSIDSelected)
		}
		if want := "/org/freedesktop/NetworkManager/AccessPoint/9"; got.Form.APSelected != want {
			t.Errorf("%s: APSelected = %q, want %q", security, got.Form.APSelected, want)
		}
	}
}
