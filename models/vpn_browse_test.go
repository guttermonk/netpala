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

func tab() tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyTab} }
func shiftTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyShiftTab} }

// confDir builds a directory holding a config, a non-config and a subdirectory.
func confDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"mullvad-se.conf": testConf,
		"notes.txt":       "not a config",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "archive"), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func freshImport() VpnImport {
	return ModelVpnImport(config.DefaultColors(), config.DefaultKeyBindings())
}

// Typing is still the default: the field holds focus when the popup opens, so
// someone who knows the path just types it.
func TestImportOpensOnTheTypedPath(t *testing.T) {
	m := freshImport()

	if m.Stage != VpnImportPath {
		t.Errorf("Stage = %d, want the path prompt", m.Stage)
	}
	if m.Focus != focusPathField {
		t.Errorf("Focus = %d, want the text field", m.Focus)
	}
	if !m.Path.Focused() {
		t.Error("the text field is not accepting input")
	}
}

// Tab walks the field, Browse and Open, and comes back round.
func TestPathStageFocusRing(t *testing.T) {
	m := freshImport()

	for _, want := range []int{focusBrowse, focusOpen, focusPathField} {
		next, _ := m.Update(tab())
		m = next.(VpnImport)
		if m.Focus != want {
			t.Fatalf("Focus = %d, want %d", m.Focus, want)
		}
	}

	// And backwards.
	next, _ := m.Update(shiftTab())
	if got := next.(VpnImport).Focus; got != focusOpen {
		t.Errorf("shift-tab went to %d, want Open", got)
	}
}

// The import popup is a popup like any other, so the user's own Up/Down move
// through it rather than only the arrows.
func TestImportHonoursReboundKeys(t *testing.T) {
	m := ModelVpnImport(config.DefaultColors(), colemakKeys()) // Up = i, Down = e

	// From the field, the rebound keys are typing -- "e" is a letter someone
	// putting a path in needs.
	next, _ := m.Update(keyOf("e"))
	if got := next.(VpnImport); got.Focus != focusPathField || got.Path.Value() != "e" {
		t.Errorf("Focus = %d, value = %q; want the letter typed into the field",
			got.Focus, got.Path.Value())
	}

	// Once the field is left, they navigate.
	onBrowse, _ := m.Update(tab())
	next, _ = onBrowse.(VpnImport).Update(keyOf("e"))
	if got := next.(VpnImport).Focus; got != focusOpen {
		t.Errorf("Focus = %d, want the rebound Down key to move to Open", got)
	}
	next, _ = onBrowse.(VpnImport).Update(keyOf("i"))
	if got := next.(VpnImport).Focus; got != focusPathField {
		t.Errorf("Focus = %d, want the rebound Up key to move back to the field", got)
	}
}

// The summary has no field, so the configured keys always apply there.
func TestSummaryHonoursReboundKeys(t *testing.T) {
	dir := confDir(t)

	m := ModelVpnImport(config.DefaultColors(), colemakKeys())
	m.Path.SetValue(filepath.Join(dir, "mullvad-se.conf"))
	next, _ := m.Update(enter())

	moved, _ := next.(VpnImport).Update(keyOf("e"))
	if got := moved.(VpnImport).Focus; got != focusCancelImport {
		t.Errorf("Focus = %d, want the rebound key to reach Cancel", got)
	}
}

// The browser is a list, and it has to move on the same keys as every other
// list in the program.
func TestBrowserHonoursReboundKeys(t *testing.T) {
	m := ModelVpnImport(config.DefaultColors(), colemakKeys())

	down := m.Picker.KeyMap.Down
	if !down.Enabled() {
		t.Fatal("the browser has no Down binding")
	}
	if !containsKey(down.Keys(), "e") {
		t.Errorf("browser Down keys = %v, want the configured 'e'", down.Keys())
	}
	if !containsKey(m.Picker.KeyMap.Up.Keys(), "i") {
		t.Errorf("browser Up keys = %v, want the configured 'i'", m.Picker.KeyMap.Up.Keys())
	}
	// The arrows survive, because they always work in a popup.
	if !containsKey(down.Keys(), "down") {
		t.Errorf("browser Down keys = %v, want the arrow kept", down.Keys())
	}
}

func containsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// Left and right belong to the text cursor while the field is being edited; a
// mistyped path could not be corrected in the middle otherwise.
func TestArrowsEditTextRatherThanMoveFocus(t *testing.T) {
	m := freshImport()
	m.Path.SetValue("/etc/wg0.conf")

	next, _ := m.Update(keyOf("left"))
	if got := next.(VpnImport).Focus; got != focusPathField {
		t.Errorf("Focus = %d, want left to stay in the field while typing", got)
	}

	// Once on a button they move between buttons again.
	onBrowse, _ := m.Update(tab())
	next, _ = onBrowse.(VpnImport).Update(keyOf("right"))
	if got := next.(VpnImport).Focus; got != focusOpen {
		t.Errorf("Focus = %d, want right to move to Open", got)
	}
}

// Keystrokes must reach the field only while it holds focus, or pressing "b"
// on the Browse button types a letter instead.
func TestTypingOnlyReachesTheFocusedField(t *testing.T) {
	m := freshImport()

	typed := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	next, _ := m.Update(typed)
	if got := next.(VpnImport).Path.Value(); got != "x" {
		t.Errorf("value = %q, want the keystroke to reach the focused field", got)
	}

	m = next.(VpnImport)
	next, _ = m.Update(tab()) // move to Browse
	next, _ = next.(VpnImport).Update(typed)
	if got := next.(VpnImport).Path.Value(); got != "x" {
		t.Errorf("value = %q, want typing ignored while a button holds focus", got)
	}
}

func TestEnterOnOpenReadsTheTypedPath(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(filepath.Join(dir, "mullvad-se.conf"))
	next, _ := m.Update(tab()) // Browse
	next, _ = next.(VpnImport).Update(tab())
	m = next.(VpnImport) // Open

	next, _ = m.Update(enter())
	if got := next.(VpnImport); got.Stage != VpnImportConfirm {
		t.Errorf("Stage = %d, want the summary", got.Stage)
	}
}

func TestEnterOnBrowseOpensTheBrowser(t *testing.T) {
	m := freshImport()
	next, _ := m.Update(tab()) // Browse
	m = next.(VpnImport)

	next, cmd := m.Update(enter())
	got := next.(VpnImport)

	if got.Stage != VpnImportBrowse {
		t.Fatalf("Stage = %d, want the browser", got.Stage)
	}
	if cmd == nil {
		t.Error("the browser was not told to read a directory")
	}
	if got.Path.Focused() {
		t.Error("the text field still has focus while browsing")
	}
}

// Correcting a nearly-right path should not start over from home.
func TestBrowserOpensBesideWhatIsTyped(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(filepath.Join(dir, "typo.conf"))
	m.openBrowser()

	if m.Picker.CurrentDirectory != dir {
		t.Errorf("browser opened at %q, want the directory of the typed path (%q)",
			m.Picker.CurrentDirectory, dir)
	}
}

func TestBrowserOpensAtTheTypedDirectory(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(dir)
	m.openBrowser()

	if m.Picker.CurrentDirectory != dir {
		t.Errorf("browser opened at %q, want %q", m.Picker.CurrentDirectory, dir)
	}
}

// With nothing typed there is nowhere better than home, which is where a
// browser download lands.
func TestBrowserFallsBackToHome(t *testing.T) {
	m := freshImport()
	m.openBrowser()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory in this environment")
	}
	if m.Picker.CurrentDirectory != home {
		t.Errorf("browser opened at %q, want %q", m.Picker.CurrentDirectory, home)
	}
}

// Only .conf is selectable. Anything else stays visible but disabled, so a
// file with an unexpected name can be seen and typed in by hand.
func TestBrowserOnlySelectsConfigFiles(t *testing.T) {
	m := freshImport()

	if len(m.Picker.AllowedTypes) != 1 || m.Picker.AllowedTypes[0] != wgConfExt {
		t.Errorf("AllowedTypes = %v, want just %q", m.Picker.AllowedTypes, wgConfExt)
	}
	if m.Picker.DirAllowed {
		t.Error("a directory should not be selectable as a config")
	}
	if !m.Picker.FileAllowed {
		t.Error("files must be selectable")
	}
}

// Escape leaves the browser rather than going up a directory, which is what
// the picker's own binding does.
func TestEscapeLeavesTheBrowser(t *testing.T) {
	m := freshImport()
	m.openBrowser()

	next, _ := m.Update(escape())
	got := next.(VpnImport)

	if got.Stage != VpnImportPath {
		t.Errorf("Stage = %d, want back at the path prompt", got.Stage)
	}
	if !got.Path.Focused() {
		t.Error("the text field did not get focus back")
	}
}

// The browser must show where it is, or a list of file names says nothing.
func TestBrowserShowsItsDirectory(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(dir)
	cmd := m.openBrowser()
	m.Picker, _ = m.Picker.Update(cmd())

	view := stripANSI(m.View())
	if !strings.Contains(view, filepath.Base(dir)) {
		t.Errorf("the browser does not say where it is:\n%s", view)
	}
	if !strings.Contains(view, "mullvad-se.conf") {
		t.Errorf("the config is not listed:\n%s", view)
	}
}

// Both stages have to show that Enter does something, which was the whole
// complaint: a field with no button reads as though nothing will happen.
func TestBothStagesShowTheirButtons(t *testing.T) {
	m := freshImport()
	path := stripANSI(m.View())
	for _, want := range []string{"Browse", "Open"} {
		if !strings.Contains(path, want) {
			t.Errorf("the path stage has no %q button:\n%s", want, path)
		}
	}

	dir := confDir(t)
	m.Path.SetValue(filepath.Join(dir, "mullvad-se.conf"))
	next, _ := m.Update(enter())
	confirm := stripANSI(next.(VpnImport).View())
	for _, want := range []string{"Import", "Cancel"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("the summary has no %q button:\n%s", want, confirm)
		}
	}
}

// Focus has to be visible without colour: a palette that flattens two similar
// shades would otherwise leave no indication of what Enter will do.
func TestFocusIsVisibleWithoutColour(t *testing.T) {
	m := freshImport()

	next, _ := m.Update(tab()) // Browse
	browse := stripANSI(next.(VpnImport).View())
	if !strings.Contains(browse, "┏") {
		t.Errorf("the focused button is not marked except by colour:\n%s", browse)
	}

	next2, _ := next.(VpnImport).Update(tab()) // Open
	open := stripANSI(next2.(VpnImport).View())
	if browse == open {
		t.Error("moving focus changed nothing that survives losing colour")
	}
}

// Cancel on the summary goes back to choosing a file rather than importing.
func TestCancelOnTheSummaryDoesNotImport(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(filepath.Join(dir, "mullvad-se.conf"))
	next, _ := m.Update(enter())
	m = next.(VpnImport)

	next, _ = m.Update(tab()) // move to Cancel
	m = next.(VpnImport)
	if m.Focus != focusCancelImport {
		t.Fatalf("Focus = %d, want Cancel", m.Focus)
	}

	next, cmd := m.Update(enter())
	got := next.(VpnImport)
	if got.Stage != VpnImportPath {
		t.Errorf("Stage = %d, want back at the path prompt", got.Stage)
	}
	if cmd != nil {
		if _, submitted := cmd().(common.SubmitVpnImportMsg); submitted {
			t.Error("Cancel imported the profile")
		}
	}
}

// Enter still imports straight away, so the flow someone already knows is
// unchanged by the buttons appearing.
func TestEnterStillImportsFromTheSummary(t *testing.T) {
	dir := confDir(t)

	m := freshImport()
	m.Path.SetValue(filepath.Join(dir, "mullvad-se.conf"))
	next, _ := m.Update(enter())

	if got := next.(VpnImport).Focus; got != focusImport {
		t.Fatalf("Focus = %d, want Import so Enter still works", got)
	}

	_, cmd := next.(VpnImport).Update(enter())
	if cmd == nil {
		t.Fatal("Enter on the summary issued nothing")
	}
	if _, ok := cmd().(common.SubmitVpnImportMsg); !ok {
		t.Errorf("got %T, want SubmitVpnImportMsg", cmd())
	}
}
