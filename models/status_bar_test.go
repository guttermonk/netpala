package models

import (
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func stripEscapes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "m")
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func barFor(pane int) string {
	cfg := config.DefaultConfig()
	sb := ModelStatusBar(config.NewAppKeyMap(&cfg), cfg.Colors)
	sb.Pane = pane
	return stripEscapes(sb.View())
}

// The layout budgets exactly one row for the status bar. A bar that wrapped
// would push the bottom of the device table off the screen, so this is a
// layout invariant rather than a cosmetic preference.
func TestStatusBarIsAlwaysOneLine(t *testing.T) {
	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		bar := barFor(pane)
		if got := strings.Count(bar, "\n"); got != 0 {
			t.Errorf("pane %d rendered %d extra lines:\n%s", pane, got, bar)
		}
	}
}

// However narrow it gets, you must still be able to see how to quit.
func TestQuitHintSurvivesTruncation(t *testing.T) {
	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		if bar := barFor(pane); !strings.Contains(bar, "Quit") {
			t.Errorf("pane %d dropped the Quit hint:\n%s", pane, bar)
		}
	}
}

// Which keys apply to a pane is a property of the keymap, independent of how
// many happen to fit on screen. Checked against PaneHelp rather than the
// rendered bar, which drops hints on a narrow terminal.
func TestOnlyApplicableKeysAreOffered(t *testing.T) {
	cfg := config.DefaultConfig()
	km := config.NewAppKeyMap(&cfg)

	helpFor := func(pane int) []string {
		var out []string
		for _, b := range km.PaneHelp(pane) {
			out = append(out, b.Help().Desc)
		}
		return out
	}
	has := func(hints []string, want string) bool {
		for _, h := range hints {
			if h == want {
				return true
			}
		}
		return false
	}

	// The bar gets one row, so it carries only what you need to move around
	// and act on a row. Everything else lives in the help popup, and listing
	// it here as well is what pushed pane navigation off the end at 80
	// columns.
	advanced := []string{"Remove", "Auto", "DNS", "MAC", "Hidden", "Scan", "Import"}

	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		hints := helpFor(pane)
		for _, h := range advanced {
			if has(hints, h) {
				t.Errorf("pane %d puts %q in the status bar; it belongs in the help popup: %v",
					pane, h, hints)
			}
		}
		// And the way to find them has to be advertised, since the popup is
		// the only place they are written down.
		if !has(hints, "Help") {
			t.Errorf("pane %d does not advertise the help key, got %v", pane, hints)
		}
	}

	// Every pane must still offer a way out and a way between panes.
	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		hints := helpFor(pane)
		for _, required := range []string{"Quit", "Next", "Prev"} {
			if !has(hints, required) {
				t.Errorf("pane %d should offer %q, got %v", pane, required, hints)
			}
		}
	}
}

// Pane switching is basic navigation and should be visible wherever there is
// room for it.
func TestPaneNavigationIsAdvertised(t *testing.T) {
	for _, pane := range []int{common.PaneScanned, common.PaneVPN, common.PaneSecurity, common.PaneDevice} {
		bar := barFor(pane)
		if !strings.Contains(bar, "Next") || !strings.Contains(bar, "Prev") {
			t.Errorf("pane %d should show pane navigation:\n%s", pane, bar)
		}
	}
}

// The default bindings put tab and shift+tab on pane switching, and the help
// formatter renders them as the arrow glyphs.
func TestTabIsShownForPaneSwitching(t *testing.T) {
	cfg := config.DefaultConfig()
	km := config.NewAppKeyMap(&cfg)

	if got := km.NextPane.Help().Key; !strings.Contains(got, "⇥") {
		t.Errorf("NextPane help key = %q, want the tab glyph", got)
	}
	if got := km.PrevPane.Help().Key; !strings.Contains(got, "⇤") {
		t.Errorf("PrevPane help key = %q, want the shift-tab glyph", got)
	}

	if bar := barFor(common.PaneVPN); !strings.Contains(bar, "⇥") {
		t.Errorf("status bar should show the tab glyph:\n%s", bar)
	}
}

// Dropping starts from the end but must never remove the first hint or Quit.
func TestNarrowBarKeepsFirstAndLastHints(t *testing.T) {
	style := lipgloss.NewStyle()
	cfg := config.DefaultConfig()
	km := config.NewAppKeyMap(&cfg)

	bar := stripEscapes(renderShortHelp("|", style, style, km.PaneHelp(common.PaneKnown)))
	if !strings.Contains(bar, "Up") {
		t.Errorf("first hint dropped:\n%s", bar)
	}
	if !strings.Contains(bar, "Quit") {
		t.Errorf("Quit dropped:\n%s", bar)
	}
}
