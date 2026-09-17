package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Stages of the import popup.
const (
	// VpnImportPath is the file prompt: type a path, or open the browser.
	VpnImportPath = iota
	// VpnImportConfirm shows what the file turns out to contain, before
	// anything is written.
	VpnImportConfirm
	// VpnImportBrowse is the file browser, for when you do not remember where
	// the download went.
	VpnImportBrowse
)

// Focus positions on the path stage, in tab order.
const (
	focusPathField = iota
	focusBrowse
	focusOpen
)

// Focus positions on the confirm stage.
const (
	focusImport = iota
	focusCancelImport
)

// wgConfExt is what a wg-quick configuration is called. Files that do not match
// are still listed in the browser, greyed out, rather than hidden: seeing that
// the directory holds something named differently is how you work out to type
// the path instead.
const wgConfExt = ".conf"

// VpnImport imports a wg-quick configuration as a NetworkManager profile.
//
// Two stages rather than one. A WireGuard config is not a thing most people
// read before using -- it arrives from a provider as an opaque download -- so
// the second stage says what it actually does: where it connects, whether it
// takes all traffic, and which of its directives NetworkManager cannot carry
// over. That last one matters most. A dropped PostUp is invisible afterwards
// and can be the difference between a tunnel and a tunnel with a killswitch.
type VpnImport struct {
	Stage  int
	Focus  int
	Path   textinput.Model
	Picker filepicker.Model

	// Filled in once the file parses.
	Config *common.WireGuardConfig
	ID     string
	Ifname string

	ErrText string
	Colors  config.Colors
	Keys    config.KeyBindings
}

func ModelVpnImport(colors config.Colors, keys config.KeyBindings) VpnImport {
	input := newTextInput(colors, "~/mullvad-se.conf", 52, 512)
	input.Focus()

	return VpnImport{
		Stage:  VpnImportPath,
		Focus:  focusPathField,
		Path:   input,
		Picker: newFilePicker(colors, keys),
		Colors: colors,
		Keys:   keys,
	}
}

// newFilePicker builds the browser, styled from the user's colours.
//
// Only .conf is selectable, because that is what wg-quick writes and what every
// provider ships. Anything else is shown greyed out rather than hidden, so a
// file with an unexpected name is visible and can be typed in by hand.
func newFilePicker(colors config.Colors, keys config.KeyBindings) filepicker.Model {
	fp := filepicker.New()

	// The browser is a list like any other, so the user's own Up/Down move
	// through it. The arrows are kept alongside because they always work in a
	// popup, and the picker's other bindings (h/l, backspace) are left as they
	// are -- they move between directories rather than between rows.
	km := filepicker.DefaultKeyMap()
	km.Up = key.NewBinding(key.WithKeys(append([]string{"up"}, keys.Up.Keys...)...))
	km.Down = key.NewBinding(key.WithKeys(append([]string{"down"}, keys.Down.Keys...)...))
	fp.KeyMap = km

	fp.AllowedTypes = []string{wgConfExt}
	fp.DirAllowed = false
	fp.FileAllowed = true
	fp.ShowPermissions = false
	fp.ShowSize = false
	// Fixed rather than auto: the popup is an overlay on a pane layout that
	// budgets its own rows, and a list that grew with the directory would push
	// the bottom of the view off a short terminal.
	fp.AutoHeight = false
	fp.SetHeight(8)

	s := filepicker.DefaultStyles()
	s.Cursor = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.ActiveText))
	s.Directory = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Active))
	s.File = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Primary))
	s.DisabledFile = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Placeholder))
	s.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.ActiveText)).Bold(true)
	s.EmptyDirectory = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colors.Placeholder)).
		PaddingLeft(2).
		SetString("No files here.")
	fp.Styles = s

	return fp
}

func (m VpnImport) Init() tea.Cmd { return textinput.Blink }

// startDir is where the browser opens.
//
// Whatever is already typed, if it names a real directory, so correcting a
// nearly-right path does not start over from home. Otherwise the home
// directory, which is where a browser download lands.
func (m VpnImport) startDir() string {
	if typed := strings.TrimSpace(m.Path.Value()); typed != "" {
		expanded := common.ExpandPath(typed)
		if info, err := os.Stat(expanded); err == nil && info.IsDir() {
			return expanded
		}
		if dir := filepath.Dir(expanded); dir != "" {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				return dir
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

// openBrowser switches to the file browser and points it somewhere useful.
func (m *VpnImport) openBrowser() tea.Cmd {
	m.Picker = newFilePicker(m.Colors, m.Keys)
	m.Picker.CurrentDirectory = m.startDir()
	m.Stage = VpnImportBrowse
	m.ErrText = ""
	m.Path.Blur()
	return m.Picker.Init()
}

// load reads and parses the named file, moving to the summary on success and
// leaving the reason on screen on failure.
func (m *VpnImport) load(path string) {
	cfg, id, ifname, err := common.LoadWireGuardConfig(path)
	if err != nil {
		m.ErrText = err.Error()
		m.Stage = VpnImportPath
		m.Focus = focusPathField
		m.Path.Focus()
		return
	}
	m.Config, m.ID, m.Ifname = cfg, id, ifname
	m.ErrText = ""
	m.Stage = VpnImportConfirm
	m.Focus = focusImport
	m.Path.Blur()
}

func (m VpnImport) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.Stage {
	case VpnImportBrowse:
		return m.updateBrowse(msg)
	case VpnImportConfirm:
		return m.updateConfirm(msg)
	default:
		return m.updatePath(msg)
	}
}

func (m VpnImport) updatePath(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.Path, cmd = m.Path.Update(msg)
		return m, cmd
	}

	switch key.String() {
	case "esc", "ctrl+c":
		return m, func() tea.Msg { return common.ExitFormMsg{} }

	case "enter":
		if m.Focus == focusBrowse {
			return m, m.openBrowser()
		}
		m.load(m.Path.Value())
		return m, nil
	}

	typing := m.Focus == focusPathField

	// Left and right move between the buttons, but only once the field has
	// been left. While it holds focus they belong to the text cursor, or a
	// mistyped path could not be corrected in the middle.
	if !typing {
		switch key.String() {
		case "left":
			return m.moveFocus(-1), nil
		case "right":
			return m.moveFocus(1), nil
		}
	}

	// navDelta honours the user's own Up/Down, and ignores them while the
	// field is focused so those letters stay typeable.
	if d := navDelta(m.Keys, key, typing); d != 0 {
		return m.moveFocus(d), nil
	}

	if typing {
		var cmd tea.Cmd
		m.Path, cmd = m.Path.Update(msg)
		return m, cmd
	}
	return m, nil
}

// moveFocus steps around the path stage's three positions, keeping the text
// field accepting input only while it holds the focus -- otherwise "b" on the
// Browse button would type a letter instead of pressing it.
func (m VpnImport) moveFocus(delta int) VpnImport {
	m.Focus = stepFocus(m.Focus, delta, 3)
	if m.Focus == focusPathField {
		m.Path.Focus()
	} else {
		m.Path.Blur()
	}
	return m
}

func (m VpnImport) updateBrowse(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Escape leaves the browser rather than going up a directory, which is
	// what the picker's own binding would do. Backspace and left still go up,
	// so nothing is lost.
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c":
			m.Stage = VpnImportPath
			m.Focus = focusPathField
			m.Path.Focus()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.Picker, cmd = m.Picker.Update(msg)

	// Selecting a file is the same as having typed its path: fill the field in
	// so the choice is visible, then read it.
	if picked, path := m.Picker.DidSelectFile(msg); picked {
		m.Path.SetValue(path)
		m.load(path)
		return m, cmd
	}

	// Tried to select something that is not a .conf. Say so rather than
	// appearing to ignore the keypress.
	if wrong, path := m.Picker.DidSelectDisabledFile(msg); wrong {
		m.ErrText = fmt.Sprintf("%s is not a %s file", filepath.Base(path), wgConfExt)
		return m, cmd
	}

	return m, cmd
}

func (m VpnImport) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "esc", "ctrl+c":
		// From the summary, back out to the path rather than closing: the
		// usual reason to refuse a summary is a mistyped file.
		m.Stage = VpnImportPath
		m.Focus = focusPathField
		m.Config, m.ID, m.Ifname = nil, "", ""
		m.ErrText = ""
		m.Path.Focus()
		return m, nil

	case "left":
		m.Focus = stepFocus(m.Focus, -1, 2)
		return m, nil
	case "right":
		m.Focus = stepFocus(m.Focus, 1, 2)
		return m, nil

	case "enter":
		if m.Focus == focusCancelImport {
			m.Stage = VpnImportPath
			m.Focus = focusPathField
			m.Config, m.ID, m.Ifname = nil, "", ""
			m.Path.Focus()
			return m, nil
		}
		cfg, id, ifname := m.Config, m.ID, m.Ifname
		return m, func() tea.Msg {
			return common.SubmitVpnImportMsg{Config: cfg, ID: id, Ifname: ifname}
		}
	}

	// No text field on this stage, so the configured keys always apply.
	if d := navDelta(m.Keys, key, false); d != 0 {
		m.Focus = stepFocus(m.Focus, d, 2)
	}
	return m, nil
}

// stepFocus moves around a ring of n positions.
func stepFocus(current, delta, n int) int {
	return (current + delta + n) % n
}

func (m VpnImport) View() string {
	const inner = 60

	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Padding(0, 1).
		Width(inner + 2)

	var rows []string
	switch m.Stage {
	case VpnImportBrowse:
		rows = m.browseRows(inner)
	case VpnImportConfirm:
		rows = m.confirmRows(inner)
	default:
		rows = m.pathRows(inner)
	}

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m VpnImport) titleStyle(inner int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Width(inner)
}

func (m VpnImport) hintStyle(inner int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Placeholder)).
		Width(inner).
		Align(lipgloss.Center)
}

func (m VpnImport) alertStyle(inner int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ErrorText)).Width(inner)
}

func (m VpnImport) button(label string, focused bool) string {
	return renderButton(m.Colors, label, focused)
}

func (m VpnImport) buttonRow(inner int, buttons ...string) string {
	return renderButtonRow(inner, buttons...)
}

func (m VpnImport) pathRows(inner int) []string {
	inputColour := m.Colors.Inactive
	if m.Focus == focusPathField {
		inputColour = m.Colors.Active
	}
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(inputColour)).
		Padding(0, 1).
		Width(inner - 2)

	rows := []string{
		m.titleStyle(inner).Render("Import a WireGuard config"),
		"",
		inputStyle.Render(m.Path.View()),
		"",
		m.buttonRow(inner,
			m.button("Browse", m.Focus == focusBrowse),
			m.button("Open", m.Focus == focusOpen),
		),
	}
	if m.ErrText != "" {
		rows = append(rows, "", m.alertStyle(inner).Render(m.ErrText))
	}
	rows = append(rows, "", m.hintStyle(inner).
		Render("⇥ move · ⤶ choose · ⎋ cancel"))
	return rows
}

func (m VpnImport) browseRows(inner int) []string {
	dir := m.Picker.CurrentDirectory
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}

	rows := []string{
		m.titleStyle(inner).Render("Choose a WireGuard config"),
		lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.Colors.Placeholder)).
			Width(inner).
			Render(truncateLeft(dir, inner)),
		"",
		lipgloss.NewStyle().Width(inner).MaxWidth(inner).Render(m.Picker.View()),
	}
	if m.ErrText != "" {
		rows = append(rows, m.alertStyle(inner).Render(m.ErrText))
	}
	rows = append(rows, "", m.hintStyle(inner).
		Render("↑↓ move · ⤶ open · ⌫ up · ⎋ type a path instead"))
	return rows
}

func (m VpnImport) confirmRows(inner int) []string {
	rows := []string{m.titleStyle(inner).Render(fmt.Sprintf("Import %s?", m.ID)), ""}
	for _, line := range m.summary() {
		rows = append(rows, lipgloss.NewStyle().Width(inner).Render(line))
	}
	if warn := m.warnings(); len(warn) > 0 {
		rows = append(rows, "")
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ActiveText)).Width(inner)
		for _, line := range warn {
			rows = append(rows, warnStyle.Render(line))
		}
	}
	rows = append(rows,
		"",
		m.buttonRow(inner,
			m.button("Import", m.Focus == focusImport),
			m.button("Cancel", m.Focus == focusCancelImport),
		),
		"",
		m.hintStyle(inner).Render("⇥ move · ⤶ choose · ⎋ pick another file"),
	)
	return rows
}

// truncateLeft keeps the end of a path, which is the part that identifies it.
func truncateLeft(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	return "..." + string(runes[len(runes)-(width-3):])
}

// summary describes the profile that would be created.
func (m VpnImport) summary() []string {
	if m.Config == nil {
		return nil
	}

	endpoint := "none given"
	if len(m.Config.Peers) > 0 && m.Config.Peers[0].Endpoint != "" {
		endpoint = m.Config.Peers[0].Endpoint
	}

	routes := "only the peer's allowed IPs"
	if m.Config.RoutesAllTraffic() {
		routes = "all traffic from this machine"
	}

	dns := "unchanged"
	if len(m.Config.DNS) > 0 {
		dns = strings.Join(m.Config.DNS, ", ")
	}

	lines := []string{
		fmt.Sprintf("  Interface   %s", m.Ifname),
		fmt.Sprintf("  Endpoint    %s", endpoint),
		fmt.Sprintf("  Routes      %s", routes),
		fmt.Sprintf("  DNS         %s", dns),
	}
	if len(m.Config.Peers) > 1 {
		lines = append(lines, fmt.Sprintf("  Peers       %d", len(m.Config.Peers)))
	}
	return lines
}

// warnings are the things worth knowing before the profile is written.
func (m VpnImport) warnings() []string {
	if m.Config == nil {
		return nil
	}

	var out []string
	out = append(out, "The private key is stored in the profile, readable by root.")

	if len(m.Config.Unsupported) > 0 {
		out = append(out, fmt.Sprintf(
			"NetworkManager cannot carry over: %s",
			strings.Join(m.Config.Unsupported, ", ")))
	}
	out = append(out, "Imported switched off; connect it from the pane.")
	return out
}
