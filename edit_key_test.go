package main

import (
	"netpala/common"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

func editKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")} }

// deliver runs a command and feeds what it produces back into the model, one
// level of batching deep, as the runtime would. Only used on paths whose
// commands are messages rather than D-Bus calls, there being no bus here.
func deliver(m NetpalaData, cmd tea.Cmd) NetpalaData {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, batched := range msg {
			m = deliver(m, batched)
		}
		return m
	default:
		next, _ := m.Update(msg)
		return next.(NetpalaData)
	}
}

// knownModel sits on Known Networks with one profile of the given security.
func knownModel(security string) NetpalaData {
	profile := common.KnownNetwork{
		Path:     godbus.ObjectPath("/conn/somewhere"),
		SSID:     "somewhere",
		Security: security,
	}
	m := safeListener(linkModel(false, []common.KnownNetwork{profile}))
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 0
	return m
}

// Which popup edit opens is the same question joining asks, answered from the
// profile rather than from a scan: a personal network has one key to change,
// an enterprise one has five fields, and the rest have nothing.
func TestEditOpensWhatTheProfileCallsFor(t *testing.T) {
	for _, tt := range []struct {
		security  string
		wantPopup int
		wantCmd   bool
		why       string
	}{
		{common.LabelPSK, popupPassword, false, "one key to change"},
		{common.LabelSAE, popupPassword, false, "one key to change"},
		{common.LabelUnknown, popupPassword, false, "keyed with something, so ask what"},
		{common.LabelEAP, popupNone, true, "the profile has to be read first"},
		{common.LabelEAP192, popupNone, true, "the profile has to be read first"},
		{common.LabelOpen, popupNone, true, "nothing to change, and it says so"},
		{common.LabelOWE, popupNone, true, "nothing to change, and it says so"},
		{common.LabelWEP, popupNone, true, "refused, as joining one is"},
	} {
		t.Run(tt.security, func(t *testing.T) {
			next, cmd := knownModel(tt.security).Update(editKey())
			got := next.(NetpalaData)

			if got.PopupState != tt.wantPopup {
				t.Errorf("PopupState = %d, want %d -- %s", got.PopupState, tt.wantPopup, tt.why)
			}
			if (cmd != nil) != tt.wantCmd {
				t.Errorf("command produced = %v, want %v -- %s", cmd != nil, tt.wantCmd, tt.why)
			}
		})
	}
}

// The popup is opened on the profile, and holds it: the row it came from can
// have moved by the time the answer is given.
func TestTheSecretPopupHoldsTheProfileItWasOpenedOn(t *testing.T) {
	next, _ := knownModel(common.LabelPSK).Update(editKey())
	got := next.(NetpalaData)

	if got.EditTarget.SSID != "somewhere" {
		t.Errorf("EditTarget = %q, want the row under the cursor", got.EditTarget.SSID)
	}
	if got.PasswordForm.Target.Path != godbus.ObjectPath("/conn/somewhere") {
		t.Errorf("the popup holds %q", got.PasswordForm.Target.Path)
	}
	if got.PasswordForm.SSID != "somewhere" {
		t.Error("the popup does not name the network")
	}
}

// An enterprise profile is read off the bus first, so edit opens nothing yet
// and the answer is what opens the form.
func TestAnEapProfileIsReadBeforeTheFormOpens(t *testing.T) {
	next, cmd := knownModel(common.LabelEAP).Update(editKey())
	waiting := next.(NetpalaData)

	if cmd == nil {
		t.Fatal("nothing was asked of the bus")
	}
	if waiting.EditTarget.Path != godbus.ObjectPath("/conn/somewhere") {
		t.Fatalf("EditTarget = %q, want the profile being read", waiting.EditTarget.Path)
	}

	opened, _, handled := waiting.handleDataMsg(common.EapProfileMsg{
		Path:      waiting.EditTarget.Path,
		SSID:      "somewhere",
		Identity:  "someone@example.net",
		EapMethod: "TTLS",
	})
	if !handled {
		t.Fatal("the profile was not handled as a data message")
	}
	if opened.PopupState != popupEapForm {
		t.Errorf("PopupState = %d, want the EAP form", opened.PopupState)
	}
	if opened.Form.Target.Path != waiting.EditTarget.Path {
		t.Error("the form was opened without the profile it is editing")
	}
	if got := opened.Form.Identity.Value(); got != "someone@example.net" {
		t.Errorf("Identity = %q, want the profile's", got)
	}
}

// A read that arrives for something nobody is waiting for is dropped. The
// cursor can be moved, or edit pressed again, while the bus is answering.
func TestAStaleProfileReadIsDropped(t *testing.T) {
	next, _ := knownModel(common.LabelEAP).Update(editKey())
	waiting := next.(NetpalaData)

	for _, tt := range []struct {
		name string
		path godbus.ObjectPath
	}{
		{"another profile's answer", godbus.ObjectPath("/conn/elsewhere")},
		{"an answer with no profile", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := waiting.handleDataMsg(common.EapProfileMsg{Path: tt.path})
			if got.PopupState != popupNone {
				t.Errorf("PopupState = %d, want no popup", got.PopupState)
			}
		})
	}
}

// And one that arrives after something else has taken the screen does not
// shove it aside.
func TestAProfileReadDoesNotTakeOverAnotherPopup(t *testing.T) {
	next, _ := knownModel(common.LabelEAP).Update(editKey())
	waiting := next.(NetpalaData)
	waiting.PopupState = popupPassword // something else got there first

	got, _, _ := waiting.handleDataMsg(common.EapProfileMsg{Path: waiting.EditTarget.Path})

	if got.PopupState != popupPassword {
		t.Errorf("PopupState = %d, want the popup that was already up", got.PopupState)
	}
	if got.EditTarget.Path != "" {
		t.Error("the dropped read left its target behind")
	}
}

// Backing out has to put the popup back as it was, or the next password prompt
// opens in edit mode and writes to a profile instead of joining a network.
func TestCancellingAnEditLeavesNothingBehind(t *testing.T) {
	next, _ := knownModel(common.LabelPSK).Update(editKey())
	open := next.(NetpalaData)

	// Escape is answered with a command that carries the exit message, so the
	// popup closes on the message rather than on the keypress. Delivering it
	// is what the runtime would do next.
	cancelled, cmd := open.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape produced nothing to close the popup with")
	}
	got := deliver(cancelled.(NetpalaData), cmd)

	if got.PopupState != popupNone {
		t.Errorf("PopupState = %d, want no popup", got.PopupState)
	}
	if got.EditTarget.Path != "" {
		t.Errorf("EditTarget = %q, want it let go of", got.EditTarget.Path)
	}
	if got.PasswordForm.Target.Path != "" {
		t.Error("the popup is still in edit mode, so the next connect would write to a profile")
	}
}

// The answer is acted on when it belongs to the popup that was opened, and
// not when it does not.
func TestOnlyTheOpenedProfilesAnswerIsWritten(t *testing.T) {
	next, _ := knownModel(common.LabelPSK).Update(editKey())
	open := next.(NetpalaData)

	answered, cmd := open.Update(common.SubmitSecretEditMsg{
		Path: open.EditTarget.Path, Value: "a new key",
	})
	if cmd == nil {
		t.Error("the edit was not written")
	}
	if got := answered.(NetpalaData); got.EditTarget.Path != "" {
		t.Error("the target outlived the answer")
	}

	elsewhere, cmd := open.Update(common.SubmitSecretEditMsg{
		Path: godbus.ObjectPath("/conn/elsewhere"), Value: "a new key",
	})
	if cmd != nil {
		t.Error("an answer for another profile was written anyway")
	}
	if got := elsewhere.(NetpalaData); got.PopupState != popupNone {
		t.Errorf("PopupState = %d, want the popup closed either way", got.PopupState)
	}
}

// Edit is a Known Networks action. The other panes hold things with no saved
// credentials: a scan result is joined, a unit is toggled, a daemon is not
// netpala's to rewrite.
func TestEditDoesNothingInTheOtherPanes(t *testing.T) {
	for _, pane := range []int{common.PaneScanned, common.PaneVPN, common.PaneSecurity, common.PaneDevice} {
		m := knownModel(common.LabelPSK)
		m.selectedBox = pane

		next, _ := m.Update(editKey())
		if got := next.(NetpalaData); got.PopupState != popupNone {
			t.Errorf("pane %d: PopupState = %d, want no popup", pane, got.PopupState)
		}
	}
}
