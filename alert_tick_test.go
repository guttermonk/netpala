package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.dalton.dog/bubbleup"
)

// alertTick returns a real bubbleup tick. Its type is unexported, so the only
// way to get one is to run the command the alert model arms itself with.
func alertTick(t *testing.T, m NetpalaData) tea.Msg {
	t.Helper()
	cmd := m.Alert.Init()
	if cmd == nil {
		t.Fatal("alert model armed no tick")
	}
	return cmd()
}

// The alert fade is driven by a 100ms tick that the alert model re-arms only
// when it is handed one. Any branch of Update that returns without forwarding
// the message stops the loop for good - and because nothing restarts it, every
// later alert renders stuck at its initial dim blend.
//
// This is what made the MAC revert warning unreadable: selecting a mode means
// opening the picker, the open picker swallowed the next tick, and the warning
// arrived ten seconds later with the fade long dead.
func TestAlertTickSurvivesAnOpenPopup(t *testing.T) {
	for _, popup := range []struct {
		name  string
		state int
	}{
		{"no popup", -1},
		{"dns picker", 3},
		{"mac picker", 4},
		{"password", 2},
		{"confirmation", 1},
	} {
		t.Run(popup.name, func(t *testing.T) {
			m := linkModel(false, nil)
			m.PopupState = popup.state

			tick := alertTick(t, m)
			_, cmd := m.Update(tick)
			if cmd == nil {
				t.Fatal("the tick was consumed without re-arming; the fade stops here " +
					"and every later alert stays dim")
			}

			// Re-arming has to keep producing ticks, not fire once.
			if next := cmd(); next == nil {
				t.Fatal("re-armed command produced no follow-up tick")
			}
		})
	}
}

// An alert clears itself only from the tick branch, so the dead fade loop also
// meant a warning stayed on screen forever with no way to dismiss it.
func TestAlertExpires(t *testing.T) {
	m := linkModel(false, nil)
	m.Alert = *bubbleup.NewAlertModel(40, true, 0) // expire immediately

	// Raise an alert the way the revert path does.
	raise := m.Alert.NewAlertCmd(bubbleup.WarnKey, "wlp3s0 would not take a Random MAC address")
	next, _ := m.Update(raise())
	m = next.(NetpalaData)

	if got := m.Alert.Render("content"); got == "content" {
		t.Fatal("precondition: the alert should be on screen before any tick")
	}

	next, _ = m.Update(alertTick(t, m))
	m = next.(NetpalaData)

	if got := m.Alert.Render("content"); got != "content" {
		t.Errorf("alert still rendered after expiry:\n%s", got)
	}
}

// The same thing has to hold with a picker open, which is when the revert
// warning actually arrives.
func TestAlertExpiresWithPopupOpen(t *testing.T) {
	m := linkModel(false, nil)
	m.Alert = *bubbleup.NewAlertModel(40, true, 0)
	m.PopupState = 4 // mac picker

	raise := m.Alert.NewAlertCmd(bubbleup.WarnKey, "reverted")
	next, _ := m.Update(raise())
	m = next.(NetpalaData)

	next, _ = m.Update(alertTick(t, m))
	if got := next.(NetpalaData).Alert.Render("content"); got != "content" {
		t.Errorf("alert survived expiry while a popup was open:\n%s", got)
	}
}
