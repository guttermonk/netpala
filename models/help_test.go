package models

import (
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func helpPopup(pane int) HelpPopup {
	cfg := config.DefaultConfig()
	return ModelHelpPopup(pane, config.NewAppKeyMap(&cfg), cfg.Colors)
}

// Every binding has to be discoverable somewhere. The status bar carries the
// basics and the popup carries the rest -- a key in neither exists only for
// whoever read the source.
func TestNoBindingIsUndiscoverable(t *testing.T) {
	cfg := config.DefaultConfig()
	km := config.NewAppKeyMap(&cfg)

	// Every binding the program has, by description.
	all := map[string]bool{}
	for _, b := range km.ShortHelp() {
		all[b.Help().Desc] = true
	}
	for _, b := range []key.Binding{km.SetMac, km.ImportVpn, km.Help} {
		all[b.Help().Desc] = true
	}

	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		shown := map[string]bool{}
		for _, b := range km.PaneHelp(pane) {
			shown[b.Help().Desc] = true
		}
		view := stripANSI(helpPopup(pane).View())

		for desc := range all {
			if shown[desc] || strings.Contains(view, desc) {
				continue
			}
			// Pane-specific actions are allowed to be absent where they do
			// nothing; what must not happen is a key that works here and is
			// named nowhere.
			if inBindings(km.PaneActions(pane), desc) {
				t.Errorf("pane %d: %q works here but appears in neither the bar nor the popup",
					pane, desc)
			}
		}
	}
}

func inBindings(bindings []key.Binding, desc string) bool {
	for _, b := range bindings {
		if b.Help().Desc == desc {
			return true
		}
	}
	return false
}

// The popup answers "what can I do to this row", so a pane's own actions have
// to be there.
func TestHelpListsThePanesOwnActions(t *testing.T) {
	for pane, want := range map[int][]string{
		common.PaneKnown: {"Dis/Connect", "Remove", "Auto", "Hidden", "DNS", "MAC"},
		common.PaneVPN:   {"Dis/Connect", "Remove", "Auto", "DNS"},
	} {
		view := stripANSI(helpPopup(pane).View())
		for _, desc := range want {
			if !strings.Contains(view, desc) {
				t.Errorf("pane %d help does not list %q:\n%s", pane, desc, view)
			}
		}
	}
}

// A key that does nothing in this pane is worse than one that is not
// mentioned: the popup is meant to answer what this row accepts.
func TestHelpOmitsActionsThatDoNothingHere(t *testing.T) {
	vpn := stripANSI(helpPopup(common.PaneVPN).View())
	for _, desc := range []string{"Hidden", "MAC"} {
		if strings.Contains(vpn, desc) {
			t.Errorf("VPN help lists %q, a wireless-profile setting:\n%s", desc, vpn)
		}
	}

	security := stripANSI(helpPopup(common.PaneSecurity).View())
	for _, desc := range []string{"Remove", "Auto", "DNS", "MAC", "Hidden"} {
		if strings.Contains(security, desc) {
			t.Errorf("Security help lists %q, which does nothing there:\n%s", desc, security)
		}
	}
}

// The keys that work anywhere, and the ones for getting around, are listed for
// every pane.
func TestHelpAlwaysListsGlobalsAndNavigation(t *testing.T) {
	for _, pane := range []int{
		common.PaneKnown, common.PaneScanned,
		common.PaneVPN, common.PaneSecurity, common.PaneDevice,
	} {
		view := stripANSI(helpPopup(pane).View())
		for _, desc := range []string{"Scan", "Import", "Up", "Down", "Next", "Prev", "Quit"} {
			if !strings.Contains(view, desc) {
				t.Errorf("pane %d help does not list %q:\n%s", pane, desc, view)
			}
		}
	}
}

// It names the section, so a popup that appeared over one pane cannot be read
// as applying to another.
func TestHelpNamesTheSection(t *testing.T) {
	for pane, want := range map[int]string{
		common.PaneKnown:    "Known Networks",
		common.PaneScanned:  "New Networks",
		common.PaneVPN:      "Virtual Private Networks",
		common.PaneSecurity: "Security",
		common.PaneDevice:   "Device",
	} {
		if view := stripANSI(helpPopup(pane).View()); !strings.Contains(view, want) {
			t.Errorf("pane %d help does not name %q:\n%s", pane, want, view)
		}
	}
}

// A reference popup with one way out is a small trap, so several things close
// it -- including the key that opened it.
func TestHelpClosesOnAnythingThatMeansDone(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyRunes, Runes: []rune{'?'}}, // the help key again
	} {
		_, cmd := helpPopup(common.PaneKnown).Update(k)
		if cmd == nil {
			t.Errorf("%q did not close the popup", k.String())
			continue
		}
		if _, ok := cmd().(common.ExitFormMsg); !ok {
			t.Errorf("%q produced %T, want ExitFormMsg", k.String(), cmd())
		}
	}
}

// A rebound help key has to close it too, not just "?".
func TestHelpClosesOnTheReboundHelpKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KeyBindings.Help = config.KeyBinding{Keys: []string{"f1"}, Help: "Help"}
	m := ModelHelpPopup(common.PaneKnown, config.NewAppKeyMap(&cfg), cfg.Colors)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	if cmd == nil {
		t.Fatal("the rebound help key did not close the popup")
	}
	if _, ok := cmd().(common.ExitFormMsg); !ok {
		t.Errorf("got %T, want ExitFormMsg", cmd())
	}
}

// Nothing else does anything -- it is a reference, not a menu.
func TestHelpIgnoresOtherKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'d'}},
		{Type: tea.KeyRunes, Runes: []rune{'a'}},
		{Type: tea.KeyBackspace},
	} {
		if _, cmd := helpPopup(common.PaneKnown).Update(k); cmd != nil {
			t.Errorf("%q did something in a read-only popup", k.String())
		}
	}
}

// A binding someone cleared out of their config must not be advertised as
// though it worked.
func TestHelpSkipsClearedBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KeyBindings.SetMac = config.KeyBinding{Keys: nil, Help: "MAC"}
	m := ModelHelpPopup(common.PaneKnown, config.NewAppKeyMap(&cfg), cfg.Colors)

	if view := stripANSI(m.View()); strings.Contains(view, "MAC") {
		t.Errorf("help lists a binding with no keys:\n%s", view)
	}
}

// The popup is an overlay on a pane layout that budgets its own rows, so a row
// wider than the container would be clipped rather than look wrong.
func TestHelpRowsFitTheContainer(t *testing.T) {
	for _, pane := range []int{common.PaneKnown, common.PaneVPN} {
		for _, line := range strings.Split(stripANSI(helpPopup(pane).View()), "\n") {
			if got := len([]rune(line)); got > helpWidth+4 {
				t.Errorf("row is %d columns, over the container:\n%q", got, line)
			}
		}
	}
}
