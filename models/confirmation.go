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
	// Keys is the user's navigation configuration, honoured here as it is in
	// the pickers, so a rebound Up/Down moves between the buttons.
	Keys config.KeyBindings
}

func ModelConfirmation(colors config.Colors, keys config.KeyBindings) Confirmation {
	return Confirmation{
		Value:  false,
		Colors: colors,
		Keys:   keys,
	}
}
func (m Confirmation) Init() tea.Cmd {
	return nil
}

func (m Confirmation) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "enter":
		return m, func() tea.Msg { return common.SubmitConfirmationMsg{Value: m.Value} }

	case "esc", "ctrl+c":
		// Always no, whichever button the highlight happens to be on.
		//
		// Escape used to submit the current selection, so backing out of a
		// consent prompt after tabbing to Confirm agreed to it -- which for
		// these prompts means starting something that changes where the
		// machine's traffic goes.
		return m, func() tea.Msg { return common.SubmitConfirmationMsg{Value: false} }

	case "left":
		m.Value = false
		return m, nil
	case "right":
		m.Value = true
		return m, nil
	}

	// The buttons sit side by side, so "forward" is Confirm and "back" is
	// Cancel. There is no text field here, so the configured keys always
	// apply -- nothing can be meant as typing.
	if d := navDelta(m.Keys, key, false); d != 0 {
		m.Value = d > 0
	}
	return m, nil
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

	cancelButton := renderButton(m.Colors, "Cancel", !m.Value)
	confirmButton := renderButton(m.Colors, "Confirm", m.Value)

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
	buttons := renderButtonRow(confirmWidth-2, cancelButton, confirmButton)

	// Naming the keys is the other half of honouring them: someone who rebound
	// Up and Down has no way to learn from the popup that their own keys work
	// here, and the arrows are not what they reach for.
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Placeholder)).
		Width(confirmWidth - 2).
		Align(lipgloss.Center).
		Render(navHint(m.Keys) + " choose · ⤶ accept · ⎋ cancel")

	return containerStyle.Render(
		lipgloss.JoinVertical(lipgloss.Left, message, "", buttons, "", hint),
	)
}
