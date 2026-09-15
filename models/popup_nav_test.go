package models

import (
	"netpala/common"
	"netpala/config"
	"testing"
)

// colemakKeys is the shape of a real rebind: the navigation letters are not
// k/j. The pickers used to hardcode k/j, so a user with this config could only
// move with the arrows and every letter press did nothing.
func colemakKeys() config.KeyBindings {
	kb := config.DefaultKeyBindings()
	kb.Up = config.KeyBinding{Keys: []string{"i", "up"}, Help: "Up"}
	kb.Down = config.KeyBinding{Keys: []string{"e", "down"}, Help: "Down"}
	return kb
}

func TestDnsSelectHonoursReboundKeys(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), colemakKeys(), nil)
	m.SSID = "home"
	m.SelectProvider(common.DNSModeDHCP, nil) // cursor at 0

	m, _ = send(t, m, "e", "e")
	if m.Cursor != 2 {
		t.Fatalf("after two configured Down presses cursor = %d, want 2", m.Cursor)
	}
	m, _ = send(t, m, "i")
	if m.Cursor != 1 {
		t.Errorf("after a configured Up press cursor = %d, want 1", m.Cursor)
	}

	// The keys this picker used to hardcode must no longer move anything,
	// otherwise the rebind is only half honoured.
	before := m.Cursor
	m, _ = send(t, m, "j", "k")
	if m.Cursor != before {
		t.Errorf("j/k still navigate at %d despite being unbound", m.Cursor)
	}
}

func TestMacSelectHonoursReboundKeys(t *testing.T) {
	m := ModelMacSelect(config.DefaultColors(), colemakKeys(), "")
	m.SSID = "home"
	m.SelectMode(common.MACModeDefault, "")

	next, _ := m.Update(keyMsg("e"))
	m = next.(MacSelect)
	if m.Cursor != 1 {
		t.Fatalf("configured Down gave cursor %d, want 1", m.Cursor)
	}

	next, _ = m.Update(keyMsg("i"))
	if got := next.(MacSelect).Cursor; got != 0 {
		t.Errorf("configured Up gave cursor %d, want 0", got)
	}
}

// The default config binds k/j, so the behaviour everyone had before must be
// unchanged now that it comes from configuration rather than a literal.
func TestDefaultConfigStillNavigatesWithKJ(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), config.DefaultKeyBindings(), nil)
	m.SelectProvider(common.DNSModeDHCP, nil)

	m, _ = send(t, m, "j")
	if m.Cursor != 1 {
		t.Fatalf("j gave cursor %d, want 1", m.Cursor)
	}
	m, _ = send(t, m, "k")
	if m.Cursor != 0 {
		t.Errorf("k gave cursor %d, want 0", m.Cursor)
	}
}

// Arrows and tab are not configurable and must work whatever Up/Down are bound
// to - including while a text field has focus, which letter bindings cannot do.
func TestArrowsWorkRegardlessOfBindings(t *testing.T) {
	keys := config.DefaultKeyBindings()
	keys.Up = config.KeyBinding{Keys: []string{"i"}}
	keys.Down = config.KeyBinding{Keys: []string{"e"}}

	m := ModelDnsSelect(config.DefaultColors(), keys, nil)
	m.SelectProvider(common.DNSModeDHCP, nil)

	m, _ = send(t, m, "down")
	if m.Cursor != 1 {
		t.Errorf("arrow Down gave cursor %d, want 1", m.Cursor)
	}
	m, _ = send(t, m, "up")
	if m.Cursor != 0 {
		t.Errorf("arrow Up gave cursor %d, want 0", m.Cursor)
	}
}

// A letter bound to navigation must still be typeable into the inline field,
// or a custom resolver containing it could never be entered.
func TestReboundLettersTypeIntoFocusedField(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), colemakKeys(), nil)
	m.SelectProvider(common.DNSModeCustom, nil)
	if !m.Custom.Focused() {
		t.Fatal("precondition: the custom field should be focused")
	}
	cursor := m.Cursor

	m, _ = send(t, m, "e", "i")
	if m.Cursor != cursor {
		t.Errorf("typing moved the list cursor to %d", m.Cursor)
	}
	if got := m.Custom.Value(); got != "ei" {
		t.Errorf("custom field = %q, want %q; the letters were swallowed as navigation", got, "ei")
	}

	// The arrows still have to escape the field.
	m, _ = send(t, m, "up")
	if m.Cursor == cursor {
		t.Error("arrow Up did not move off the custom row while it was focused")
	}
}

func TestMacExplicitFieldAcceptsReboundLetters(t *testing.T) {
	m := ModelMacSelect(config.DefaultColors(), colemakKeys(), "")
	m.SelectMode(common.MACModeExplicit, "")
	if !m.Explicit.Focused() {
		t.Fatal("precondition: the explicit field should be focused")
	}
	cursor := m.Cursor

	next, _ := m.Update(keyMsg("e"))
	m = next.(MacSelect)
	if m.Cursor != cursor {
		t.Errorf("typing moved the list cursor to %d", m.Cursor)
	}
	if got := m.Explicit.Value(); got != "e" {
		t.Errorf("explicit field = %q, want %q", got, "e")
	}
}

func TestMoveCursorStopsAtTheEnds(t *testing.T) {
	if got := moveCursor(0, -1, 3); got != 0 {
		t.Errorf("moving up from the top gave %d, want 0 (no wrap)", got)
	}
	if got := moveCursor(2, 1, 3); got != 2 {
		t.Errorf("moving down from the bottom gave %d, want 2 (no wrap)", got)
	}
}
