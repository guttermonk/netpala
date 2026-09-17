package main

import (
	"netpala/common"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

func vpn(name string, connected bool) common.VpnConnection {
	return common.VpnConnection{
		Path:        godbus.ObjectPath("/vpn/" + name),
		Name:        name,
		ConnType:    "WireGuard",
		Connected:   connected,
		AutoConnect: true,
		Endpoint:    "185.65.135.170:51820",
	}
}

// vpnModel builds a model sitting on the VPN pane.
func vpnModel(vpns ...common.VpnConnection) NetpalaData {
	m := linkModel(false, nil)
	m.VpnData = vpns
	m.selectedBox = common.PaneVPN
	m.SelectedEntry = 0
	return safeListener(m)
}

func removeKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyBackspace} }
func autoKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
}

func TestRemovingAVpnAsksFirst(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false))

	next, cmd := m.Update(removeKey())
	got := next.(NetpalaData)

	if got.PopupState != 1 {
		t.Fatalf("PopupState = %d, want the confirmation popup", got.PopupState)
	}
	if got.confirmAction != confirmDeleteVpn {
		t.Error("popup opened without recording that it deletes a VPN")
	}
	if got.VpnTarget.Path != godbus.ObjectPath("/vpn/mullvad-se") {
		t.Errorf("VpnTarget = %q, want the selected profile", got.VpnTarget.Path)
	}
	if !strings.Contains(got.Confirmation.Message, "mullvad-se") {
		t.Errorf("prompt does not name the profile, got %q", got.Confirmation.Message)
	}
	if cmd != nil {
		t.Error("a command was issued before the user answered")
	}
}

// Deleting a WireGuard profile destroys the private key with it, which is not
// something netpala can undo.
func TestDeletingWireGuardWarnsAboutTheKey(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false))
	next, _ := m.Update(removeKey())

	msg := next.(NetpalaData).Confirmation.Message
	if !strings.Contains(msg, "private key") {
		t.Errorf("no mention of the private key in %q", msg)
	}
}

func TestDeletingAPluginVpnDoesNotMentionAKey(t *testing.T) {
	profile := vpn("work", false)
	profile.ConnType = "OPENVPN"

	m := vpnModel(profile)
	next, _ := m.Update(removeKey())

	msg := next.(NetpalaData).Confirmation.Message
	if strings.Contains(msg, "private key") {
		t.Errorf("OpenVPN profile warned about a key it does not store: %q", msg)
	}
}

func TestConfirmingDeletesTheVpn(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false))
	next, _ := m.Update(removeKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	got := next.(NetpalaData)

	if !startedAService(t, cmd) { // a batch of action + re-armed listener
		t.Error("confirming issued no delete command")
	}
	if got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
	if got.VpnTarget.Path != "" {
		t.Error("target not cleared after deleting")
	}
}

func TestCancellingDoesNotDeleteTheVpn(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false))
	next, _ := m.Update(removeKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: false})
	got := next.(NetpalaData)

	if startedAService(t, cmd) {
		t.Error("cancelling deleted the profile anyway")
	}
	if got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
}

// The prompt can sit on screen while refreshes rewrite VpnData underneath it.
// Acting on the row index rather than the recorded profile would delete
// whatever had moved into that position.
func TestDeleteTargetsTheProfileNotTheRow(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false), vpn("work", false))
	next, _ := m.Update(removeKey())
	m = next.(NetpalaData)

	// A refresh lands and reorders the pane.
	m.VpnData = []common.VpnConnection{vpn("work", false), vpn("mullvad-se", false)}

	next, _ = m.Update(common.SubmitConfirmationMsg{Value: true})
	if got := next.(NetpalaData); got.VpnTarget.Path != "" {
		t.Error("target not cleared")
	}
	// The recorded path is what the command closed over; the reorder above
	// must not have changed which profile that was.
	if m.VpnTarget.Path != godbus.ObjectPath("/vpn/mullvad-se") {
		t.Errorf("target moved to %q after a refresh", m.VpnTarget.Path)
	}
}

func TestAutoConnectTogglesOnTheVpnPane(t *testing.T) {
	m := vpnModel(vpn("mullvad-se", false))

	next, cmd := m.Update(autoKey())
	if cmd == nil {
		t.Error("the auto-connect key did nothing on a VPN row")
	}
	if got := next.(NetpalaData); got.PopupState != -1 {
		t.Errorf("PopupState = %d, want no popup", got.PopupState)
	}
}

// An empty pane is never selected, but the guards still have to hold: a key
// press must not index into a list that is not there.
func TestVpnKeysOnAnEmptyPaneDoNothing(t *testing.T) {
	m := vpnModel()

	for name, k := range map[string]tea.KeyMsg{"remove": removeKey(), "auto": autoKey()} {
		next, cmd := m.Update(k)
		if got := next.(NetpalaData); got.PopupState != -1 {
			t.Errorf("%s: opened a popup for a profile that is not there", name)
		}
		if cmd != nil {
			t.Errorf("%s: issued a command against an empty pane", name)
		}
	}
}

// Remove belongs to profiles netpala created. A systemd unit is installed by
// the system and is not netpala's to delete.
func TestRemoveDoesNothingOnTheSecurityPane(t *testing.T) {
	m := securityModel(i2p(false))

	next, cmd := m.Update(removeKey())
	if got := next.(NetpalaData); got.PopupState == 1 {
		t.Error("remove opened a confirmation on the Security pane")
	}
	if cmd != nil {
		t.Error("remove issued a command on the Security pane")
	}
}

func helpKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
}

// The help popup is where the keys the status bar has no room for are written
// down, so it has to open from every pane.
func TestHelpOpensFromEveryPane(t *testing.T) {
	for name, pane := range map[string]int{
		"known":    common.PaneKnown,
		"scanned":  common.PaneScanned,
		"vpn":      common.PaneVPN,
		"security": common.PaneSecurity,
		"device":   common.PaneDevice,
	} {
		m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
		m.VpnProfiles = []common.VpnConnection{vpn("mullvad-se", false)}
		m.rebuildVpnData()
		m.selectedBox = pane
		m = safeListener(m)

		next, _ := m.Update(helpKey())
		got := next.(NetpalaData)
		if got.PopupState != 6 {
			t.Errorf("%s: PopupState = %d, want the help popup", name, got.PopupState)
		}
		if got.HelpForm.Pane != pane {
			t.Errorf("%s: help opened for pane %d, want %d", name, got.HelpForm.Pane, pane)
		}
	}
}

// And closing it puts you back where you were.
func TestClosingHelpReturnsToThePane(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	m.selectedBox = common.PaneKnown
	m = safeListener(m)

	next, _ := m.Update(helpKey())
	m = next.(NetpalaData)

	next, _ = m.Update(common.ExitFormMsg{})
	got := next.(NetpalaData)
	if got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
	if got.selectedBox != common.PaneKnown {
		t.Errorf("selectedBox = %d, want the pane unchanged", got.selectedBox)
	}
}

// "?" must not be swallowed by a pane action while the popup is open, and the
// popup must not act on keys meant for it.
func TestHelpDoesNotLeakKeysToThePane(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	m.selectedBox = common.PaneKnown
	m = safeListener(m)

	next, _ := m.Update(helpKey())
	m = next.(NetpalaData)

	// "d" is the DNS key; while help is open it must not open the DNS picker.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got := next.(NetpalaData); got.PopupState != 6 {
		t.Errorf("PopupState = %d, want help still open", got.PopupState)
	}
}
