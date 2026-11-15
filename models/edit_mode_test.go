package models

import (
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

func savedProfile() common.KnownNetwork {
	return common.KnownNetwork{
		Path:     godbus.ObjectPath("/conn/eduroam"),
		SSID:     "eduroam",
		Security: common.LabelEAP,
	}
}

// enterOn drives a form to one of its fields and presses enter there, which is
// how both popups submit.
func submitAt(m tea.Model, tabs int) tea.Msg {
	for range tabs {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return nil
	}
	return cmd()
}

// The same popup serves two questions, and the answers go to different places:
// one joins a network, the other rewrites a profile. A single message type
// would have left the handler guessing which had been asked.
func TestThePasswordPopupAnswersForWhicheverItWasOpenedFor(t *testing.T) {
	colors := config.DefaultColors()

	joining := ModelPasswordInput(colors)
	joining.Password.SetValue("a key")
	joining.ConfirmValue = true
	if msg := submitAt(joining, 0); func() bool { _, ok := msg.(common.SubmitPasswordMsg); return !ok }() {
		t.Errorf("joining produced %T, want a SubmitPasswordMsg", msg)
	}

	editing := ModelPasswordInput(colors)
	editing.Target = savedProfile()
	editing.SSID = "eduroam"
	editing.Password.SetValue("a new key")
	editing.ConfirmValue = true

	msg := submitAt(editing, 0)
	edit, ok := msg.(common.SubmitSecretEditMsg)
	if !ok {
		t.Fatalf("editing produced %T, want a SubmitSecretEditMsg", msg)
	}
	if edit.Path != savedProfile().Path {
		t.Errorf("Path = %q, want the profile being edited", edit.Path)
	}
	if edit.Value != "a new key" {
		t.Errorf("Value = %q", edit.Value)
	}
}

// Nothing typed is an instruction, so it has to reach the handler as one.
func TestAnEmptyEditStillAnswers(t *testing.T) {
	editing := ModelPasswordInput(config.DefaultColors())
	editing.Target = savedProfile()
	editing.ConfirmValue = true

	msg := submitAt(editing, 0)
	edit, ok := msg.(common.SubmitSecretEditMsg)
	if !ok {
		t.Fatalf("got %T, want a SubmitSecretEditMsg", msg)
	}
	if edit.Value != "" {
		t.Errorf("Value = %q, want the empty string the field holds", edit.Value)
	}
}

// What a blank field means is not guessable, so the popup says it, and says
// which network it is talking about -- the connect prompt never did either.
func TestTheEditPopupSaysWhatItIsAndWhatBlankMeans(t *testing.T) {
	colors := config.DefaultColors()

	editing := ModelPasswordInput(colors)
	editing.Target = savedProfile()
	editing.SSID = "eduroam"
	view := editing.View()

	if !strings.Contains(view, "eduroam") {
		t.Error("the popup does not name the network")
	}
	if !strings.Contains(view, "blank leaves the current one") {
		t.Error("the popup does not say what an empty field does")
	}
	if !strings.Contains(view, "Save") {
		t.Error("the button still says Confirm; this is not a prompt")
	}

	joining := ModelPasswordInput(colors)
	if strings.Contains(joining.View(), "blank leaves") {
		t.Error("the connect prompt claims a blank field keeps something")
	}
}

// The EAP form is the same form either way round, so it has to be told which
// question it is asking -- and its answer has to carry the profile.
func TestTheEapFormAnswersForWhicheverItWasOpenedFor(t *testing.T) {
	colors := config.DefaultColors()

	joining := ModelWpaEapForm(colors)
	joining.SSIDSelected = "eduroam"
	// Five tabs from the method selector to the submit button.
	if msg := submitAt(joining, 5); func() bool { _, ok := msg.(common.SubmitEapFormMsg); return !ok }() {
		t.Errorf("joining produced %T, want a SubmitEapFormMsg", msg)
	}

	editing := ModelWpaEapForm(colors)
	editing.LoadProfile(savedProfile(), common.EapProfileMsg{
		Path: savedProfile().Path, SSID: "eduroam", Identity: "someone@example.net",
	})

	// Three tabs from the identity field, where an edit opens.
	msg := submitAt(editing, 3)
	edit, ok := msg.(common.SubmitEapEditMsg)
	if !ok {
		t.Fatalf("editing produced %T, want a SubmitEapEditMsg", msg)
	}
	if edit.Path != savedProfile().Path {
		t.Errorf("Path = %q, want the profile being edited", edit.Path)
	}
	if edit.Config["identity"] != "someone@example.net" {
		t.Errorf("identity = %q, want what the profile holds", edit.Config["identity"])
	}
}

// The form opens on the profile, not on empty fields: a save that was only
// meant to change the password must not take the identity with it.
func TestLoadingAProfileFillsTheFields(t *testing.T) {
	form := ModelWpaEapForm(config.DefaultColors())
	form.LoadProfile(savedProfile(), common.EapProfileMsg{
		Path:       savedProfile().Path,
		SSID:       "eduroam",
		Identity:   "someone@example.net",
		Password:   "stored",
		CaCert:     "/etc/ssl/certs/ca.pem",
		EapMethod:  "TTLS",
		Phase2Auth: "PAP",
	})

	if got := form.Identity.Value(); got != "someone@example.net" {
		t.Errorf("Identity = %q", got)
	}
	if got := form.Password.Value(); got != "stored" {
		t.Errorf("Password = %q", got)
	}
	if got := form.CaCert.Value(); got != "/etc/ssl/certs/ca.pem" {
		t.Errorf("CaCert = %q", got)
	}
}

// The selectors are the half of this that cannot be assigned: their index is
// unexported, so a profile's own method is reached by stepping onto it. A form
// that opened on the top of the list would change the method of every profile
// saved without one.
func TestTheSelectorsOpenOnTheProfilesOwnValues(t *testing.T) {
	for _, tt := range []struct {
		method, phase2 string
	}{
		{"PEAP", "MSCHAPV2"}, // the first of each list
		{"TTLS", "PAP"},
		{"PWD", "NONE"}, // the last of each list
	} {
		t.Run(tt.method+"/"+tt.phase2, func(t *testing.T) {
			form := ModelWpaEapForm(config.DefaultColors())
			form.LoadProfile(savedProfile(), common.EapProfileMsg{
				Path: savedProfile().Path, EapMethod: tt.method, Phase2Auth: tt.phase2,
			})

			if got := form.EapMethod.Selected().(EAPMethod).Type; got != tt.method {
				t.Errorf("EAP method = %q, want %q", got, tt.method)
			}
			if got := form.Phase2Auth.Selected().(EAPMethod).Type; got != tt.phase2 {
				t.Errorf("phase 2 = %q, want %q", got, tt.phase2)
			}
		})
	}
}

// A method the form does not list leaves the selector where it is rather than
// landing somewhere arbitrary, and the save then says what the selector shows.
func TestAnUnknownMethodLeavesTheSelectorAlone(t *testing.T) {
	form := ModelWpaEapForm(config.DefaultColors())
	form.LoadProfile(savedProfile(), common.EapProfileMsg{
		Path: savedProfile().Path, EapMethod: "FAST",
	})

	if got := form.EapMethod.Selected().(EAPMethod).Type; got != "PEAP" {
		t.Errorf("EAP method = %q, want the first of the list", got)
	}
}

// Both forms name the network they are about. The EAP one did not, and for an
// edit it cannot be inferred: the form opens after a read off the bus rather
// than immediately after the row was chosen.
func TestTheEapFormNamesTheNetwork(t *testing.T) {
	form := ModelWpaEapForm(config.DefaultColors())
	form.LoadProfile(savedProfile(), common.EapProfileMsg{
		Path: savedProfile().Path, SSID: "eduroam",
	})

	if !strings.Contains(form.View(), "eduroam") {
		t.Error("the form does not name the network")
	}
}

// And the button says which of the two things it will do.
func TestTheEapFormsButtonSaysWhichJobItIsDoing(t *testing.T) {
	colors := config.DefaultColors()

	joining := ModelWpaEapForm(colors)
	if view := joining.View(); !strings.Contains(view, "Connect") {
		t.Error("the join form does not offer to connect")
	}

	editing := ModelWpaEapForm(colors)
	editing.LoadProfile(savedProfile(), common.EapProfileMsg{Path: savedProfile().Path})
	view := editing.View()
	if !strings.Contains(view, "Save") {
		t.Error("the edit form does not offer to save")
	}
	if strings.Contains(view, "Connect") {
		t.Error("the edit form still offers to connect")
	}
}
