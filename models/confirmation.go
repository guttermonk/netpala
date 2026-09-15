package models

import (
	"netpala/common"
	"netpala/config"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Confirmation struct {
	Message string
	Value   bool
	Colors  config.Colors
}

func ModelConfirmation(colors config.Colors) Confirmation {
	return Confirmation{
		Value:  false,
		Colors: colors,
	}
}
func (m Confirmation) Init() tea.Cmd {
	return nil
}

func (m Confirmation) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle global key presses for focus switching and quitting first.
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "esc", "ctrl+c", "enter":
			return m, func() tea.Msg { return common.SubmitConfirmationMsg{Value: m.Value} }
		case "tab", "right":
			m.Value = true
		case "shift+tab", "left":
			m.Value = false
		}
	}

	return m, cmd
}

// confirmWidth is the popup's content width. Wide enough that a paragraph of
// consent text does not turn into a column of three-word lines.
const confirmWidth = 60

func (m Confirmation) View() string {
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Padding(0, 1).
		Width(confirmWidth)

	inactiveBorderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Inactive)).
		Align(lipgloss.Center).
		Padding(0, 3).
		Width(18)

	activeBorderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.ActiveText)).
		Align(lipgloss.Center).
		Padding(0, 3).
		Width(18)

	confirmButton := inactiveBorderStyle.Render("Confirm")
	cancelButton := activeBorderStyle.Render("Cancel")

	if m.Value {
		confirmButton = activeBorderStyle.Render("Confirm")
		cancelButton = inactiveBorderStyle.Render("Cancel")
	}

	// A one-line question reads best centred, but centring several paragraphs
	// leaves both edges ragged and is genuinely hard to read - which matters
	// most for exactly the messages that are long, since those are the ones
	// asking the user to understand something before agreeing to it. A blank
	// line between the prose and the buttons keeps them from looking like part
	// of the last sentence.
	message := strings.TrimRight(m.Message, "\n")
	if strings.Contains(message, "\n\n") {
		message = lipgloss.NewStyle().
			Width(confirmWidth - 2).
			Align(lipgloss.Left).
			Render(message)
	}

	// Centred explicitly: a full-width left-aligned message sets the block
	// width, which would otherwise pull the buttons over to the left with it.
	buttons := lipgloss.NewStyle().
		Width(confirmWidth - 2).
		Align(lipgloss.Center).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, cancelButton, confirmButton))

	return containerStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left, message, "", buttons),
	)
}
