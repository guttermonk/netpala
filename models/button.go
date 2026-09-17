package models

import (
	"netpala/config"

	"github.com/charmbracelet/lipgloss"
)

// Buttons for the popups that offer a choice.
//
// Shared so a button looks and behaves the same wherever it appears, and so
// the focus treatment is decided once.

// buttonWidth is wide enough for the longest label the popups use
// ("Dis/Connect" is not one of them) with room to breathe.
const buttonWidth = 16

// renderButton draws one button, marked when it holds focus.
//
// The focused one gets a heavier border as well as a different colour. Colour
// alone would be the only signal otherwise, which is no signal at all to
// someone whose palette flattens the two shades or who cannot easily tell them
// apart -- and on these popups the highlighted button is what Enter will do,
// which for a consent prompt decides where the machine's traffic goes.
func renderButton(colors config.Colors, label string, focused bool) string {
	style := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Padding(0, 2).
		Width(buttonWidth)

	if focused {
		return style.
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color(colors.ActiveText)).
			Foreground(lipgloss.Color(colors.ActiveText)).
			Bold(true).
			Render(label)
	}
	return style.
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colors.Inactive)).
		Foreground(lipgloss.Color(colors.Primary)).
		Render(label)
}

// renderButtonRow centres a set of buttons, with a gap so they do not read as
// one wide box.
func renderButtonRow(width int, buttons ...string) string {
	spaced := make([]string, 0, len(buttons)*2-1)
	for i, b := range buttons {
		if i > 0 {
			spaced = append(spaced, "    ")
		}
		spaced = append(spaced, b)
	}
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, spaced...))
}
