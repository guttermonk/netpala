package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type StatusBarData struct {
	Err error
	// Pane is the section the cursor is in, so the bar can advertise only
	// the keys that will do something there.
	Pane   int
	KeyMap config.AppKeyMap
	Colors config.Colors
}

func ModelStatusBar(keyMap config.AppKeyMap, colors config.Colors) StatusBarData {
	return StatusBarData{
		Err:    nil,
		KeyMap: keyMap,
		Colors: colors,
	}
}

func (m StatusBarData) Init() tea.Cmd {
	return nil
}

func (m StatusBarData) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter, tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	// We handle errors just like any other message
	case common.ErrMsg:
		return m, nil
	}

	return m, cmd
}

func (m StatusBarData) View() string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.HelpText))
	return renderShortHelp("|", style, style, m.KeyMap.PaneHelp(m.Pane))
}

// renderShortHelp lays the key hints out on exactly one line.
//
// Spacing is generous when the terminal has room and collapses when it does
// not. The MaxHeight is the backstop rather than decoration: the layout
// budgets a single row for the status bar, so a bar that wrapped would push
// the bottom of the device table off the screen.
func renderShortHelp(sep string, keyStyle lipgloss.Style, descStyle lipgloss.Style, keybinds []key.Binding) string {
	totalWidth := common.WindowDimensions().Width

	entries := make([]string, 0, len(keybinds))
	for _, k := range keybinds {
		entries = append(entries, fmt.Sprintf("%s %s",
			keyStyle.Render(k.Help().Key),
			descStyle.Bold(true).Render(k.Help().Desc)))
	}
	if len(entries) == 0 {
		return ""
	}

	// Too narrow to show everything: drop hints rather than let the line be
	// cut mid-word. Removal works backwards from the second-to-last entry so
	// the first hint and Quit always survive - being unable to see how to
	// leave is the worst thing to lose.
	width := func(es []string) int {
		w := lipgloss.Width(sep) * (len(es) - 1)
		for _, e := range es {
			w += lipgloss.Width(e)
		}
		return w
	}
	for len(entries) > 2 && width(entries) > totalWidth {
		entries = append(entries[:len(entries)-2], entries[len(entries)-1])
	}

	// Width of the entries themselves, before any padding.
	content := 0
	for _, e := range entries {
		content += lipgloss.Width(e)
	}
	separators := lipgloss.Width(sep) * (len(entries) - 1)

	// Spread the slack between entries, but only as much as actually fits.
	pad := 0
	if slack := totalWidth - content - separators; slack > 0 {
		pad = slack / (len(entries) * 2)
	}
	if pad > 0 {
		gap := strings.Repeat(" ", pad)
		for i := range entries {
			entries[i] = gap + entries[i] + gap
		}
	}

	return lipgloss.NewStyle().
		Width(totalWidth).
		MaxWidth(totalWidth).
		MaxHeight(1).
		Align(lipgloss.Center).
		Render(strings.Join(entries, sep))
}

// ShortHelp implements help.KeyMap for backwards compatibility
func (m StatusBarData) ShortHelp() []key.Binding {
	return m.KeyMap.ShortHelp()
}

// FullHelp implements help.KeyMap for backwards compatibility
func (m StatusBarData) FullHelp() [][]key.Binding {
	return m.KeyMap.FullHelp()
}
