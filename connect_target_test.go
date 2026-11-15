package main

import (
	"netpala/common"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

// The access point a scan result came from has to reach the form, because the
// form is what builds the profile NetworkManager is asked to activate. Several
// access points can share an SSID -- two bands of the same router, or a floor
// of them in an office -- and the one the signal column was describing is the
// only one the user chose.
func TestSelectingAnEapNetworkCarriesItsAccessPoint(t *testing.T) {
	m := safeListener(linkModel(false, nil))
	m.ScannedNetworks = []common.ScannedNetwork{{
		Path:     godbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/7"),
		SSID:     "eduroam",
		Security: "wpa2-eap",
		Signal:   71,
	}}
	m.selectedBox = common.PaneScanned
	m.SelectedEntry = 0

	next, _ := m.Update(selectKey())
	got := next.(NetpalaData)

	if got.PopupState != 0 {
		t.Fatalf("PopupState = %d, want the EAP form", got.PopupState)
	}
	if got.Form.SSIDSelected != "eduroam" {
		t.Errorf("SSIDSelected = %q, want eduroam", got.Form.SSIDSelected)
	}
	if want := "/org/freedesktop/NetworkManager/AccessPoint/7"; got.Form.APSelected != want {
		t.Errorf("APSelected = %q, want %q", got.Form.APSelected, want)
	}
}

// A scan result's Path is the access point. The profile path a deletion needs
// is a different thing entirely, and it used to be carried in the same field
// by packing a KnownNetwork into a ScannedNetwork.
func TestDeletingAKnownNetworkTargetsTheProfile(t *testing.T) {
	m := safeListener(linkModel(false, []common.KnownNetwork{
		net("home", true, ""),
		net("cafe", false, ""),
	}))
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 1

	next, _ := m.Update(removeKey())
	got := next.(NetpalaData)

	if got.PopupState != 1 || got.confirmAction != confirmDeleteNetwork {
		t.Fatalf("PopupState = %d, confirmAction = %d; want the delete prompt",
			got.PopupState, got.confirmAction)
	}
	if got.NetworkTarget.SSID != "cafe" {
		t.Errorf("NetworkTarget is %q, want the row under the cursor", got.NetworkTarget.SSID)
	}
	if want := godbus.ObjectPath("/conn/cafe"); got.NetworkTarget.Path != want {
		t.Errorf("NetworkTarget.Path = %q, want the profile path %q", got.NetworkTarget.Path, want)
	}
	if !strings.Contains(got.Confirmation.Message, "cafe") {
		t.Errorf("prompt does not name the network: %q", got.Confirmation.Message)
	}

	// Answering yes issues the deletion and lets go of the target, so a second
	// prompt cannot act on the first one's network.
	answered, cmd := got.Update(common.SubmitConfirmationMsg{Value: true})
	after := answered.(NetpalaData)
	if cmd == nil {
		t.Error("confirming the prompt issued no deletion")
	}
	if after.NetworkTarget.SSID != "" {
		t.Errorf("NetworkTarget still holds %q after the answer", after.NetworkTarget.SSID)
	}
}
