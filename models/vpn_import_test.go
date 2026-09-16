package models

import (
	"netpala/common"
	"netpala/config"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const testConf = `[Interface]
PrivateKey = qGZk4xFQ3l7pXvBn8hJtYcRmWs2Dv0aNpLuEi9OgXFo=
Address = 10.64.0.2/32
DNS = 10.64.0.1
PostUp = iptables -I OUTPUT ! -o %i -j REJECT

[Peer]
PublicKey = /vN1Hq8YpKxTzRcLmWdFgBnJs3Qa7ZuEi0OpXvYtCkM=
AllowedIPs = 0.0.0.0/0
Endpoint = 185.65.135.170:51820
`

func importModel(t *testing.T, conf string) (VpnImport, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mullvad-se.conf")
	if err := os.WriteFile(path, []byte(conf), 0600); err != nil {
		t.Fatal(err)
	}
	return ModelVpnImport(config.DefaultColors(), config.DefaultKeyBindings()), path
}

func enter() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyEnter} }
func escape() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

// The file is read and summarised before anything is written. A provider's
// config is an opaque download; the summary is the only chance to see what it
// actually does.
func TestReadingTheFileMovesToTheSummary(t *testing.T) {
	m, path := importModel(t, testConf)
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	got := next.(VpnImport)

	if got.Stage != VpnImportConfirm {
		t.Fatalf("Stage = %d, want the summary", got.Stage)
	}
	if got.ID != "mullvad-se" || got.Ifname != "mullvad-se" {
		t.Errorf("ID = %q, Ifname = %q", got.ID, got.Ifname)
	}

	view := stripANSI(got.View())
	for _, want := range []string{
		"185.65.135.170:51820", // where it connects
		"all traffic",          // what it routes
		"10.64.0.1",            // its DNS
		"private key",          // where the key ends up
		"postup",               // what NM cannot carry over
		"switched off",         // that it will not connect by itself
	} {
		if !strings.Contains(strings.ToLower(view), strings.ToLower(want)) {
			t.Errorf("summary does not mention %q:\n%s", want, view)
		}
	}
}

// Nothing may be written until the summary has been seen and accepted.
func TestTheFirstEnterWritesNothing(t *testing.T) {
	m, path := importModel(t, testConf)
	m.Path.SetValue(path)

	_, cmd := m.Update(enter())
	if cmd != nil {
		t.Error("reading the file issued a command; the summary had not been seen yet")
	}
}

func TestSecondEnterSubmitsTheParsedConfig(t *testing.T) {
	m, path := importModel(t, testConf)
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	next, cmd := next.(VpnImport).Update(enter())

	if cmd == nil {
		t.Fatal("accepting the summary issued nothing")
	}
	msg, ok := cmd().(common.SubmitVpnImportMsg)
	if !ok {
		t.Fatalf("got %T, want SubmitVpnImportMsg", cmd())
	}
	if msg.ID != "mullvad-se" || msg.Config == nil {
		t.Errorf("submitted %+v", msg)
	}
	if got := next.(VpnImport); got.Stage != VpnImportConfirm {
		t.Errorf("Stage = %d; the popup should still be showing when it submits", got.Stage)
	}
}

// A mistyped path is the usual reason to refuse a summary, so escaping from it
// goes back to the prompt rather than closing the popup outright.
func TestEscapeFromSummaryReturnsToThePathPrompt(t *testing.T) {
	m, path := importModel(t, testConf)
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	next, cmd := next.(VpnImport).Update(escape())

	got := next.(VpnImport)
	if got.Stage != VpnImportPath {
		t.Errorf("Stage = %d, want the path prompt", got.Stage)
	}
	if got.Config != nil {
		t.Error("the previous file is still loaded")
	}
	if cmd != nil {
		if _, closed := cmd().(common.ExitFormMsg); closed {
			t.Error("escaping from the summary closed the popup instead of going back")
		}
	}
}

func TestEscapeFromThePathPromptClosesThePopup(t *testing.T) {
	m := ModelVpnImport(config.DefaultColors(), config.DefaultKeyBindings())

	_, cmd := m.Update(escape())
	if cmd == nil {
		t.Fatal("escape did nothing")
	}
	if _, ok := cmd().(common.ExitFormMsg); !ok {
		t.Errorf("got %T, want ExitFormMsg", cmd())
	}
}

// The error belongs on the prompt, where the path can be corrected, rather
// than closing the popup and making the user start again.
func TestAnUnreadableFileStaysOnThePrompt(t *testing.T) {
	m := ModelVpnImport(config.DefaultColors(), config.DefaultKeyBindings())
	m.Path.SetValue(filepath.Join(t.TempDir(), "nope.conf"))

	next, cmd := m.Update(enter())
	got := next.(VpnImport)

	if got.Stage != VpnImportPath {
		t.Errorf("Stage = %d, want to stay on the prompt", got.Stage)
	}
	if got.ErrText == "" {
		t.Error("no error shown")
	}
	if cmd != nil {
		t.Error("a command was issued for a file that could not be read")
	}
	if !strings.Contains(stripANSI(got.View()), "nope.conf") {
		t.Error("the error is not visible in the popup")
	}
}

func TestAMalformedConfigStaysOnThePrompt(t *testing.T) {
	m, path := importModel(t, "[Interface]\nnonsense\n")
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	if got := next.(VpnImport); got.Stage != VpnImportPath || got.ErrText == "" {
		t.Errorf("Stage = %d, ErrText = %q; want to stay put with a reason",
			got.Stage, got.ErrText)
	}
}

// A split-tunnel config must not claim to take everything.
func TestSummaryDistinguishesSplitTunnel(t *testing.T) {
	m, path := importModel(t, `[Interface]
PrivateKey = abc=
Address = 10.0.0.2/24
[Peer]
PublicKey = def=
AllowedIPs = 10.0.0.0/24
`)
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	view := stripANSI(next.(VpnImport).View())

	if strings.Contains(view, "all traffic") {
		t.Errorf("a split-tunnel config claimed to route everything:\n%s", view)
	}
}

// lipgloss pads every row to the container width, so an over-long row does not
// look over-long in the output -- it wraps, or gets clipped by the overlay.
func TestImportRowsFitTheContainer(t *testing.T) {
	m, path := importModel(t, testConf)
	m.Path.SetValue(path)

	next, _ := m.Update(enter())
	for _, line := range strings.Split(stripANSI(next.(VpnImport).View()), "\n") {
		if got := len([]rune(line)); got > 64 {
			t.Errorf("row is %d columns, over the container:\n%q", got, line)
		}
	}
}
