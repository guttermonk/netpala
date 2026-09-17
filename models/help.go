package models

import (
	"netpala/common"
	"netpala/config"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpPopup lists the keys the status bar has no room for.
//
// The bar gets exactly one row -- the layout budgets on it, and a bar that
// wrapped would push the bottom of the view off the screen -- so it carries
// only navigation, select, help and quit. Everything else is written down
// here, and nowhere else, which is why the bar always advertises the help key.
//
// Grouped by what the keys act on rather than listed flat, because "what can I
// do to this row" is the question being asked. A pane's own actions come
// first, then the ones that work anywhere, then getting around.
type HelpPopup struct {
	// Pane is the section the cursor was in when the popup opened, so the
	// actions shown are the ones that will actually do something.
	Pane   int
	Keys   config.AppKeyMap
	Colors config.Colors
}

func ModelHelpPopup(pane int, keys config.AppKeyMap, colors config.Colors) HelpPopup {
	return HelpPopup{Pane: pane, Keys: keys, Colors: colors}
}

func (m HelpPopup) Init() tea.Cmd { return nil }

// Update closes on anything that reads as "done": the help key again, escape,
// enter or q. A reference popup with a single way out is a small trap.
func (m HelpPopup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "ctrl+c", "enter", "q":
		return m, func() tea.Msg { return common.ExitFormMsg{} }
	}
	// The help key again, whatever it is bound to.
	if key.Matches(k, m.Keys.Help) {
		return m, func() tea.Msg { return common.ExitFormMsg{} }
	}
	return m, nil
}

// paneTitle names the section whose actions are being listed.
func paneTitle(pane int) string {
	switch pane {
	case common.PaneKnown:
		return "Known Networks"
	case common.PaneScanned:
		return "New Networks"
	case common.PaneVPN:
		return "Virtual Private Networks"
	case common.PaneSecurity:
		return "Security"
	case common.PaneDevice:
		return "Device"
	}
	return "This section"
}

const helpWidth = 60

func (m HelpPopup) View() string {
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Padding(0, 1).
		Width(helpWidth + 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Width(helpWidth)

	groupStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.ActiveText)).
		Width(helpWidth)

	rows := []string{titleStyle.Render("Keys"), ""}

	add := func(group string, bindings []key.Binding) {
		bindings = enabled(bindings)
		if len(bindings) == 0 {
			return
		}
		rows = append(rows, groupStyle.Render("  "+group))
		for _, b := range bindings {
			rows = append(rows, m.keyRow(b))
		}
		rows = append(rows, "")
	}

	add(paneTitle(m.Pane), append(
		[]key.Binding{m.Keys.Select}, m.Keys.PaneActions(m.Pane)...))
	add("Anywhere", m.Keys.GlobalActions())
	add("Getting around", []key.Binding{
		m.Keys.Up, m.Keys.Down, m.Keys.NextPane, m.Keys.PrevPane, m.Keys.Quit,
	})

	rows = append(rows, lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Placeholder)).
		Width(helpWidth).
		Align(lipgloss.Center).
		Render("⎋ close"))

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// keyRow renders one binding as "    key    description".
//
// The key column is fixed so the descriptions line up, and wide enough for the
// longest pair of keys the formatter produces.
func (m HelpPopup) keyRow(b key.Binding) string {
	const keyColumn = 12

	keys := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Width(keyColumn).
		Render(b.Help().Key)

	desc := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Placeholder)).
		Render(b.Help().Desc)

	return lipgloss.NewStyle().Width(helpWidth).Render("    " + keys + desc)
}

// enabled drops bindings with no keys, so a binding someone cleared out of
// their config is not advertised as though it worked.
func enabled(bindings []key.Binding) []key.Binding {
	out := make([]key.Binding, 0, len(bindings))
	for _, b := range bindings {
		if len(b.Keys()) > 0 && strings.TrimSpace(b.Help().Desc) != "" {
			out = append(out, b)
		}
	}
	return out
}
