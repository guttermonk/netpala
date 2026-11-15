package models

import (
	"netpala/config"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// secretInputs are the fields that take a key or a password. The EAP form had
// always masked its one; the connect popup had not, which is the bug these
// pin. Anything added here later has to be masked at construction, not on the
// first keystroke -- the placeholder is visible before that.
func secretInputs(colors config.Colors) map[string]textinput.Model {
	return map[string]textinput.Model{
		"connect popup": ModelPasswordInput(colors).Password,
		"eap password":  ModelWpaEapForm(colors).Password,
	}
}

func TestSecretInputsAreMasked(t *testing.T) {
	for name, in := range secretInputs(config.DefaultColors()) {
		if in.EchoMode != textinput.EchoPassword {
			t.Errorf("%s: echo mode is %v, want EchoPassword", name, in.EchoMode)
		}
		if in.EchoCharacter == 0 {
			t.Errorf("%s: no echo character, so a masked field renders blank", name)
		}
	}
}

// The field is one thing, what reaches the screen is another: the popup draws
// the input through a border style, and this is what a shoulder actually sees.
func TestPasswordViewDoesNotShowTheKey(t *testing.T) {
	m := ModelPasswordInput(config.DefaultColors())
	m.Password.SetValue("correct horse battery")
	m.Password.Focus()

	if view := m.View(); strings.Contains(view, "correct horse battery") {
		t.Error("the typed key is in the rendered popup")
	}
}

func TestCtrlPUnmasksAndMasksAgain(t *testing.T) {
	colors := config.DefaultColors()
	ctrlP := tea.KeyMsg{Type: tea.KeyCtrlP}

	m := ModelPasswordInput(colors)
	m.Password.SetValue("hunter2")
	m.Password.Focus()

	shown, _ := m.Update(ctrlP)
	first := shown.(PasswordInput)
	if first.Password.EchoMode != textinput.EchoNormal {
		t.Fatal("ctrl+p did not unmask the field")
	}
	if !strings.Contains(first.View(), "hunter2") {
		t.Error("unmasked, but the view still hides the key")
	}
	// The key that toggles must not also land in the field.
	if got := first.Password.Value(); got != "hunter2" {
		t.Errorf("value is %q, want %q -- ctrl+p was typed into the input", got, "hunter2")
	}

	hidden, _ := first.Update(ctrlP)
	second := hidden.(PasswordInput)
	if second.Password.EchoMode != textinput.EchoPassword {
		t.Error("ctrl+p did not mask the field again")
	}
	if strings.Contains(second.View(), "hunter2") {
		t.Error("masked again, but the key is still on screen")
	}
}

// Whichever way round it is, the popup has to say so, or the only way to find
// out is to type a key and look.
func TestThePopupSaysWhichWayTheToggleGoes(t *testing.T) {
	m := ModelPasswordInput(config.DefaultColors())
	m.Password.Focus()

	masked := m.View()
	if !strings.Contains(masked, "^P show") {
		t.Error("masked: no hint that ctrl+p shows the key")
	}

	shown, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if view := shown.(PasswordInput).View(); !strings.Contains(view, "^P hide") {
		t.Error("unmasked: no hint that ctrl+p hides the key again")
	}
}
