package models

import (
	"netpala/config"

	tea "github.com/charmbracelet/bubbletea"
)

// navDelta reports how far a key press should move a picker's cursor: -1 for
// up, +1 for down, 0 for anything else.
//
// Two rules, and they differ for a reason:
//
// The arrows and tab always navigate. They are unambiguous inside a popup and
// cannot be meant as text, so they keep working even while an inline field has
// focus - which is what lets someone fill in a custom address and still move
// off the row.
//
// The configured Up/Down bindings navigate only when nothing is focused. They
// are ordinarily letters (k/j by default), so honouring them while the user is
// typing would make those letters unenterable. This branch used to hardcode
// k/j, which silently ignored anyone who had rebound the keys.
func navDelta(keys config.KeyBindings, key tea.KeyMsg, typing bool) int {
	s := key.String()
	switch s {
	case "up", "shift+tab":
		return -1
	case "down", "tab":
		return 1
	}
	if typing {
		return 0
	}
	switch {
	case keys.Up.Matches(s):
		return -1
	case keys.Down.Matches(s):
		return 1
	}
	return 0
}

// moveCursor applies a delta, stopping at the ends of the list rather than
// wrapping, which matches how the main tables behave.
func moveCursor(cursor, delta, length int) int {
	next := cursor + delta
	if next < 0 || next >= length {
		return cursor
	}
	return next
}

// navHint names the keys that move the cursor, for the popup footer.
//
// Advertising only the arrows while the configured keys also work is the
// smaller half of the bug this file fixes: someone who rebound Up and Down has
// no way to learn from the popup that their own keys apply here too.
func navHint(keys config.KeyBindings) string {
	up, down := firstKey(keys.Up), firstKey(keys.Down)
	if up == "" || down == "" {
		return "↑/↓"
	}
	return up + "/" + down
}

// firstKey picks the binding to advertise, preferring a printable key over an
// arrow because the arrows work regardless and need no advertising.
func firstKey(kb config.KeyBinding) string {
	var arrow string
	for _, k := range kb.Keys {
		switch k {
		case "up":
			arrow = "↑"
		case "down":
			arrow = "↓"
		default:
			return k
		}
	}
	return arrow
}
