package models

import (
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func confirmModel(keys config.KeyBindings) Confirmation {
	m := ModelConfirmation(config.DefaultColors(), keys)
	m.Message = "Route all system traffic through Tor?"
	return m
}

func press(m Confirmation, s string) Confirmation {
	next, _ := m.Update(keyOf(s))
	return next.(Confirmation)
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// The consent popups for Tor and I2P are this model. Someone who rebound Up
// and Down had to reach for the arrows, which is exactly what rebinding was
// meant to avoid.
func TestConfirmationHonoursReboundKeys(t *testing.T) {
	m := confirmModel(colemakKeys()) // Up = i, Down = e

	if got := press(m, "e"); !got.Value {
		t.Error("the rebound Down key did not move to Confirm")
	}
	if got := press(press(m, "e"), "i"); got.Value {
		t.Error("the rebound Up key did not move back to Cancel")
	}
}

// The defaults have to keep working for everyone who did not rebind anything.
func TestConfirmationHonoursDefaultKeys(t *testing.T) {
	m := confirmModel(config.DefaultKeyBindings()) // Up = k, Down = j

	if got := press(m, "j"); !got.Value {
		t.Error("the default Down key did not move to Confirm")
	}
	if got := press(press(m, "j"), "k"); got.Value {
		t.Error("the default Up key did not move back to Cancel")
	}
}

// Once a key is rebound away, it should stop acting. Otherwise the old binding
// lingers and two unrelated keys do the same thing.
func TestReboundAwayKeysStopMoving(t *testing.T) {
	m := confirmModel(colemakKeys()) // j and k are no longer bound

	for _, k := range []string{"j", "k"} {
		if got := press(m, k); got.Value {
			t.Errorf("%q still moved the selection after being rebound away", k)
		}
	}
}

// The arrows and tab work regardless of configuration -- they are unambiguous
// inside a popup and are what someone tries first.
func TestConfirmationAlwaysAcceptsArrowsAndTab(t *testing.T) {
	m := confirmModel(colemakKeys())

	for _, k := range []string{"down", "right", "tab"} {
		if got := press(m, k); !got.Value {
			t.Errorf("%q did not move to Confirm", k)
		}
	}
	onConfirm := press(m, "tab")
	for _, k := range []string{"up", "left", "shift+tab"} {
		if got := press(onConfirm, k); got.Value {
			t.Errorf("%q did not move back to Cancel", k)
		}
	}
}

// Escape means no, whichever button the highlight is on.
//
// It used to submit the current selection, so backing out of a consent prompt
// after tabbing to Confirm agreed to it -- and for these prompts agreeing
// starts something that changes where the machine's traffic goes.
func TestEscapeAlwaysDeclines(t *testing.T) {
	onConfirm := press(confirmModel(config.DefaultKeyBindings()), "tab")
	if !onConfirm.Value {
		t.Fatal("precondition: the highlight should be on Confirm")
	}

	_, cmd := onConfirm.Update(keyOf("esc"))
	if cmd == nil {
		t.Fatal("escape issued nothing")
	}
	msg, ok := cmd().(common.SubmitConfirmationMsg)
	if !ok {
		t.Fatalf("got %T, want SubmitConfirmationMsg", cmd())
	}
	if msg.Value {
		t.Error("escape accepted the prompt while the highlight was on Confirm")
	}
}

func TestEnterSubmitsTheHighlightedChoice(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want bool
	}{
		{"cancel by default", nil, false},
		{"confirm after moving", []string{"tab"}, true},
		{"back to cancel", []string{"tab", "shift+tab"}, false},
	} {
		m := confirmModel(config.DefaultKeyBindings())
		for _, k := range tc.keys {
			m = press(m, k)
		}

		_, cmd := m.Update(keyOf("enter"))
		if cmd == nil {
			t.Fatalf("%s: enter issued nothing", tc.name)
		}
		msg := cmd().(common.SubmitConfirmationMsg)
		if msg.Value != tc.want {
			t.Errorf("%s: submitted %v, want %v", tc.name, msg.Value, tc.want)
		}
	}
}

// Cancel is where the highlight starts, so a stray Enter on a consent prompt
// declines rather than agreeing.
func TestConfirmationStartsOnCancel(t *testing.T) {
	if confirmModel(config.DefaultKeyBindings()).Value {
		t.Error("the popup opens with Confirm highlighted")
	}
}

// Someone who rebound the keys cannot discover from the popup that their own
// keys work here unless it says so.
func TestConfirmationNamesTheConfiguredKeys(t *testing.T) {
	view := stripANSI(confirmModel(colemakKeys()).View())
	if !strings.Contains(view, "i/e") {
		t.Errorf("the footer does not name the configured keys:\n%s", view)
	}
}

// Which button Enter will press has to survive losing colour.
func TestConfirmationFocusIsVisibleWithoutColour(t *testing.T) {
	m := confirmModel(config.DefaultKeyBindings())

	cancel := stripANSI(m.View())
	confirm := stripANSI(press(m, "tab").View())

	if cancel == confirm {
		t.Error("moving the highlight changed nothing that survives losing colour")
	}
	if !strings.Contains(cancel, "┏") {
		t.Errorf("the highlighted button is marked by colour alone:\n%s", cancel)
	}
}
