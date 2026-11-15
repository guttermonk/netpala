package models

import (
	"netpala/common"
	"netpala/config"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PasswordInput struct {
	Message string
	// SSID names the network being joined, or the profile being edited. The
	// popup used to say neither: it asked for a password without saying what
	// for, which is answerable only because it opens one keypress after the
	// row was chosen.
	SSID         string
	Password     textinput.Model
	ConfirmValue bool
	// Target is the saved profile whose key is being changed, and empty when
	// this is the prompt for a network being joined. The two differ in what
	// the popup says, what an empty field means, and where the answer goes:
	// joining needs a key, editing treats a blank as "leave it alone".
	Target common.KnownNetwork
	// showPassword unmasks the field, for when the only way to tell whether a
	// long pre-shared key was typed correctly is to look at it.
	showPassword bool
	Colors       config.Colors
}

// editing reports whether this popup is changing a saved profile's key.
func (m PasswordInput) editing() bool { return m.Target.Path != "" }

func ModelPasswordInput(colors config.Colors) PasswordInput {
	Input := newTextInput(colors, "Enter Password...", 31, 64)
	// Masked, like the EAP form's password field. This popup was the one input
	// in netpala that echoed a key in the clear, which is the wrong default for
	// something people open in front of other people.
	Input.EchoMode = textinput.EchoPassword
	Input.EchoCharacter = '*'

	return PasswordInput{
		Password:     Input,
		ConfirmValue: false,
		Colors:       colors,
	}
}

func (m PasswordInput) Init() tea.Cmd {
	return textinput.Blink
}

func (m PasswordInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle global key presses for focus switching and quitting first.
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "tab", "shift+tab", "left", "right":
			m.ConfirmValue = !m.ConfirmValue
		case "esc", "ctrl+c":
			cmds = append(cmds, func() tea.Msg { return common.ExitFormMsg{} })
		case "ctrl+p":
			m.showPassword = !m.showPassword
			if m.showPassword {
				m.Password.EchoMode = textinput.EchoNormal
			} else {
				m.Password.EchoMode = textinput.EchoPassword
			}
			// Returning here keeps the key out of the field below.
			return m, nil
		case "enter":
			if !m.ConfirmValue {
				cmds = append(cmds, func() tea.Msg { return common.ExitFormMsg{} })
				break
			}
			value, target := m.Password.Value(), m.Target
			if m.editing() {
				cmds = append(cmds, func() tea.Msg {
					return common.SubmitSecretEditMsg{Path: target.Path, Value: value}
				})
				break
			}
			cmds = append(cmds, func() tea.Msg { return common.SubmitPasswordMsg{Value: value} })
		}
	}

	var cmd tea.Cmd
	m.Password, cmd = m.Password.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m PasswordInput) View() string {
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Padding(0, 1).
		Width(50)

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

	// "Save" rather than "Confirm" when the key is being changed: confirming
	// is what you do to a password you have just been asked for, and this
	// popup is answering a different question.
	confirmLabel := "Confirm"
	if m.editing() {
		confirmLabel = "Save"
	}
	confirmButton := inactiveBorderStyle.Render(confirmLabel)
	cancelButton := activeBorderStyle.Render("Cancel")

	if m.ConfirmValue {
		confirmButton = activeBorderStyle.Render(confirmLabel)
		cancelButton = inactiveBorderStyle.Render("Cancel")
	}

	hint := "^P show"
	if m.showPassword {
		hint = "^P hide"
	}
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.Placeholder))

	rows := []string{}
	if m.SSID != "" {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.Colors.ActiveText)).
			Bold(true).
			Render(m.SSID))
	}
	if m.Message != "" {
		rows = append(rows, m.Message)
	}
	rows = append(rows,
		activeBorderStyle.Width(38).BorderForeground(lipgloss.Color(m.Colors.Active)).Render(m.Password.View()),
		hintStyle.Render(hint),
	)
	// Said in the popup rather than left to be discovered, because a blank
	// field is an instruction here and the obvious reading of it is wrong.
	if m.editing() {
		rows = append(rows, hintStyle.Render("blank leaves the current one"))
	}
	rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Center,
		cancelButton, confirmButton,
	))

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
