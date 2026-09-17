package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MacSelect is the MAC address picker shown for a known network. Highlighting
// "Explicit" focuses an inline input, the same shape as the DNS picker.
type MacSelect struct {
	SSID     string
	Cursor   int
	Options  []common.MACOption
	Explicit textinput.Model
	ErrText  string
	// Current is the mode the profile is set to, marked with ">". Distinct
	// from Cursor, which is only where the highlight sits.
	Current string
	Colors  config.Colors
	// Keys is the user's navigation configuration, honoured here so a rebound
	// Up/Down works in the popup as well as in the tables behind it.
	Keys config.KeyBindings
}

// ModelMacSelect builds the picker. nmDefault is what NetworkManager's global
// wifi.cloned-mac-address resolves to, used to describe the "Default" row; ""
// leaves it described in general terms.
func ModelMacSelect(colors config.Colors, keys config.KeyBindings, nmDefault string) MacSelect {
	input := newTextInput(colors, "02:11:22:33:44:55", 34, 17)

	return MacSelect{
		Options:  common.MACOptionsFor(nmDefault),
		Explicit: input,
		Colors:   colors,
		Keys:     keys,
	}
}

// SelectMode records the profile's current mode and puts the cursor on it.
func (m *MacSelect) SelectMode(mode, address string) {
	m.Current = mode
	for i, o := range m.Options {
		if o.ID == mode {
			m.Cursor = i
			break
		}
	}
	if mode == common.MACModeExplicit {
		m.Explicit.SetValue(address)
	}
	m.syncFocus()
}

func (m *MacSelect) syncFocus() {
	if m.Options[m.Cursor].ID == common.MACModeExplicit {
		m.Explicit.Focus()
	} else {
		m.Explicit.Blur()
	}
}

func (m *MacSelect) onCursorMove() {
	m.ErrText = ""
	m.syncFocus()
}

func (m MacSelect) Init() tea.Cmd {
	return textinput.Blink
}

func (m MacSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return common.ExitFormMsg{} }

		case "enter":
			option := m.Options[m.Cursor]
			if option.ID == common.MACModeExplicit {
				if _, err := common.ParseMAC(m.Explicit.Value()); err != nil {
					m.ErrText = err.Error()
					return m, nil
				}
			}
			value := m.Explicit.Value()
			return m, func() tea.Msg {
				return common.SubmitMacMsg{ModeID: option.ID, Explicit: value}
			}
		}

		if d := navDelta(m.Keys, key, m.Explicit.Focused()); d != 0 {
			m.Cursor = moveCursor(m.Cursor, d, len(m.Options))
			m.onCursorMove()
			return m, nil
		}
	}

	// Anything that did not navigate is typing, so it belongs to the input.
	if m.Explicit.Focused() {
		var cmd tea.Cmd
		m.Explicit, cmd = m.Explicit.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m MacSelect) View() string {
	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Padding(0, 1).
		Width(56)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Width(54)

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(m.Colors.SelectionBg)).
		Foreground(lipgloss.Color(m.Colors.ActiveText)).
		Width(54)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Width(54)

	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.Inactive))
	alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ErrorText))

	rows := []string{
		titleStyle.Render(fmt.Sprintf("MAC address for %s", m.SSID)),
		"",
	}
	for i, o := range m.Options {
		// ">" marks what the network is set to; the cursor is the highlight
		// bar, as everywhere else in netpala.
		marker := "  "
		if o.ID == m.Current {
			marker = "> "
		}
		style := normalStyle
		if i == m.Cursor {
			style = selectedStyle
		}
		rows = append(rows, style.Render(
			fmt.Sprintf("%s%-11s %s", marker, o.Label, descStyle.Render(o.Desc))))
	}

	if m.Options[m.Cursor].ID == common.MACModeExplicit {
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(m.Colors.Active)).
			Padding(0, 1).
			Width(52)
		rows = append(rows, "", inputStyle.Render(m.Explicit.View()))
	}

	if m.ErrText != "" {
		rows = append(rows, alertStyle.Width(54).Render(m.ErrText))
	}

	rows = append(rows, "", descStyle.Width(54).Align(lipgloss.Center).
		Render(navHint(m.Keys)+" choose · ⤶ apply · ⎋ cancel"))

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
