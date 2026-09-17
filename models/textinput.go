package models

import (
	"netpala/config"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// newTextInput builds a text input styled from the user's colours.
//
// Every input in netpala goes through here so none of them can be left on the
// bubbles defaults, which is what made the placeholder text unreadable: the
// library styles it ANSI 240, a dark grey that vanishes against a dark or
// mid-grey terminal background. Five popups each had their own input and every
// one of them had the problem, so it is fixed once here rather than five times
// and then forgotten on the sixth.
func newTextInput(colors config.Colors, placeholder string, width, charLimit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = ""
	in.Width = width
	in.CharLimit = charLimit
	in.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Placeholder))
	in.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Primary))
	return in
}
