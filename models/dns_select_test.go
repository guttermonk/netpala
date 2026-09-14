package models

import (
	"errors"
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(t *testing.T, m DnsSelect, keys ...string) (DnsSelect, tea.Msg) {
	t.Helper()
	var last tea.Msg
	for _, k := range keys {
		next, cmd := m.Update(keyMsg(k))
		m = next.(DnsSelect)
		if cmd != nil {
			last = cmd()
		}
	}
	return m, last
}

func newForm() DnsSelect {
	m := ModelDnsSelect(config.DefaultColors(), nil)
	m.SSID = "home-wifi"
	m.SelectProvider(common.DNSModeDHCP, nil)
	return m
}

func TestDnsSelectPicksProvider(t *testing.T) {
	m := newForm()

	// DHCP is the starting row; one step down lands on Cloudflare.
	_, msg := send(t, m, "down", "enter")
	got, ok := msg.(common.SubmitDnsMsg)
	if !ok {
		t.Fatalf("got %T, want SubmitDnsMsg", msg)
	}
	if got.ProviderID != common.DNSModeCloudflare {
		t.Errorf("ProviderID = %q, want %q", got.ProviderID, common.DNSModeCloudflare)
	}
}

func TestDnsSelectEscCancels(t *testing.T) {
	_, msg := send(t, newForm(), "esc")
	if _, ok := msg.(common.ExitFormMsg); !ok {
		t.Fatalf("got %T, want ExitFormMsg", msg)
	}
}

func TestDnsSelectCursorClamps(t *testing.T) {
	m, _ := send(t, newForm(), "up", "up", "up")
	if m.Cursor != 0 {
		t.Errorf("cursor = %d after pressing up at the top, want 0", m.Cursor)
	}

	m, _ = send(t, newForm(), "down", "down", "down", "down", "down")
	if m.Cursor != len(common.DNSProviders)-1 {
		t.Errorf("cursor = %d after overshooting, want %d", m.Cursor, len(common.DNSProviders)-1)
	}
}

func TestDnsSelectCustomFocusesInput(t *testing.T) {
	m := newForm()
	if m.Custom.Focused() {
		t.Error("custom input should start blurred while DHCP is selected")
	}

	// Walk down to Custom, the last row.
	steps := make([]string, len(common.DNSProviders)-1)
	for i := range steps {
		steps[i] = "down"
	}
	m, _ = send(t, m, steps...)
	if !m.Custom.Focused() {
		t.Fatal("custom input should be focused once Custom is highlighted")
	}

	// Typing goes into the field, not the list.
	m, _ = send(t, m, "9", ".", "9", ".", "9", ".", "9")
	if m.Custom.Value() != "9.9.9.9" {
		t.Fatalf("custom value = %q, want 9.9.9.9", m.Custom.Value())
	}
	if m.Cursor != len(common.DNSProviders)-1 {
		t.Errorf("typing moved the cursor to %d", m.Cursor)
	}

	m, msg := send(t, m, "enter")
	got, ok := msg.(common.SubmitDnsMsg)
	if !ok {
		t.Fatalf("got %T, want SubmitDnsMsg", msg)
	}
	if got.ProviderID != common.DNSModeCustom || got.Custom != "9.9.9.9" {
		t.Errorf("submitted %+v, want custom/9.9.9.9", got)
	}
}

func TestDnsSelectRejectsBadCustomInput(t *testing.T) {
	m := newForm()
	steps := make([]string, len(common.DNSProviders)-1)
	for i := range steps {
		steps[i] = "down"
	}
	m, _ = send(t, m, steps...)
	m, _ = send(t, m, "z", "z", "z")

	m, msg := send(t, m, "enter")
	if _, ok := msg.(common.SubmitDnsMsg); ok {
		t.Fatal("invalid input should not submit")
	}
	if m.ErrText == "" {
		t.Error("expected an inline error message")
	}
	if !strings.Contains(m.View(), "not a valid IP") {
		t.Error("error should be visible in the rendered popup")
	}
}

func TestDnsSelectPreselectsCurrentProvider(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), nil)
	m.SelectProvider(common.DNSModeGoogle, nil)
	if common.DNSProviders[m.Cursor].ID != common.DNSModeGoogle {
		t.Errorf("cursor on %q, want google", common.DNSProviders[m.Cursor].ID)
	}

	m = ModelDnsSelect(config.DefaultColors(), nil)
	m.SelectProvider(common.DNSModeCustom, []string{"9.9.9.9", "149.112.112.112"})
	if m.Custom.Value() != "9.9.9.9, 149.112.112.112" {
		t.Errorf("custom field = %q, want the saved servers", m.Custom.Value())
	}
	if !m.Custom.Focused() {
		t.Error("custom field should be focused when the profile already uses custom DNS")
	}
}

// dnscryptRow walks the cursor onto the DNSCrypt entry.
func dnscryptRow(t *testing.T, m DnsSelect) DnsSelect {
	t.Helper()
	for i, p := range m.Providers {
		if p.ID == common.DNSModeDNSCrypt {
			for j := 0; j < i; j++ {
				next, _ := m.Update(keyMsg("down"))
				m = next.(DnsSelect)
			}
			return m
		}
	}
	t.Fatal("no dnscrypt row")
	return m
}

func TestDnsSelectProbesOnLanding(t *testing.T) {
	m := dnscryptRow(t, newForm())
	if !strings.Contains(m.View(), "checking") {
		t.Error("landing on DNSCrypt should show a checking status")
	}

	// A dead proxy reports its state in the row.
	next, _ := m.Update(common.DnsProbeMsg{Err: errors.New("no response from 127.0.0.1:53")})
	m = next.(DnsSelect)
	if !strings.Contains(m.View(), "not responding") {
		t.Errorf("dead proxy not surfaced:\n%s", m.View())
	}

	next, _ = m.Update(common.DnsProbeMsg{})
	if !strings.Contains(next.(DnsSelect).View(), "running") {
		t.Error("live proxy should show as running")
	}
}

func TestDnsSelectDeadProxyNeedsTwoEnters(t *testing.T) {
	m := dnscryptRow(t, newForm())
	next, _ := m.Update(common.DnsProbeMsg{Err: errors.New("no response")})
	m = next.(DnsSelect)

	m, msg := send(t, m, "enter")
	if _, ok := msg.(common.SubmitDnsMsg); ok {
		t.Fatal("first enter on a dead proxy must not apply")
	}
	if !strings.Contains(m.ErrText, "again") {
		t.Errorf("expected a confirm prompt, got %q", m.ErrText)
	}

	_, msg = send(t, m, "enter")
	got, ok := msg.(common.SubmitDnsMsg)
	if !ok {
		t.Fatal("second enter should apply anyway")
	}
	if got.ProviderID != common.DNSModeDNSCrypt {
		t.Errorf("applied %q, want dnscrypt", got.ProviderID)
	}
}

func TestDnsSelectLiveProxyAppliesImmediately(t *testing.T) {
	m := dnscryptRow(t, newForm())
	next, _ := m.Update(common.DnsProbeMsg{})
	m = next.(DnsSelect)

	_, msg := send(t, m, "enter")
	if _, ok := msg.(common.SubmitDnsMsg); !ok {
		t.Fatal("a live proxy should apply on the first enter")
	}
}

func TestDnsSelectMovingAwayDisarms(t *testing.T) {
	m := dnscryptRow(t, newForm())
	next, _ := m.Update(common.DnsProbeMsg{Err: errors.New("no response")})
	m = next.(DnsSelect)

	m, _ = send(t, m, "enter") // arms the confirm
	m, _ = send(t, m, "up")    // leaving the row should cancel it
	m = dnscryptRow(t, m)

	m, msg := send(t, m, "enter")
	if _, ok := msg.(common.SubmitDnsMsg); ok {
		t.Fatal("confirm should not survive leaving and returning to the row")
	}
	if m.ErrText == "" {
		t.Error("expected the prompt again after re-arming")
	}
}

func TestDnsSelectStaleProbeIgnored(t *testing.T) {
	m := newForm() // cursor on DHCP
	next, _ := m.Update(common.DnsProbeMsg{Err: errors.New("no response")})
	if strings.Contains(next.(DnsSelect).View(), "not responding") {
		t.Error("a probe result arriving after the cursor moved must be ignored")
	}
}

func TestDnsSelectUsesConfiguredDnscryptAddress(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), []string{"127.0.0.53"})
	m.SSID = "home"
	m.SelectProvider(common.DNSModeDHCP, nil)
	if !strings.Contains(dnscryptRow(t, m).View(), "127.0.0.53") {
		t.Error("configured listener should appear in the picker")
	}
}

// ">" marks what the network is set to, matching the Known Networks and
// Security tables. It used to follow the cursor, which made it look as though
// the setting had already changed just from scrolling the list.
func TestMarkerTracksCurrentNotCursor(t *testing.T) {
	m := ModelDnsSelect(config.DefaultColors(), nil)
	m.SSID = "home"
	m.SelectProvider(common.DNSModeCloudflare, nil)

	markedRow := func(v string) string {
		for _, line := range strings.Split(v, "\n") {
			if strings.Contains(stripANSI(line), "> ") {
				return strings.TrimSpace(stripANSI(line))
			}
		}
		return ""
	}

	if got := markedRow(m.View()); !strings.Contains(got, "Cloudflare") {
		t.Fatalf("on open, marker should be on Cloudflare, got %q", got)
	}

	// Scroll away; the marker must not follow.
	for range 3 {
		next, _ := m.Update(keyMsg("down"))
		m = next.(DnsSelect)
	}
	if got := markedRow(m.View()); !strings.Contains(got, "Cloudflare") {
		t.Errorf("after scrolling, marker moved to %q; it must stay on the current setting", got)
	}
	if m.Providers[m.Cursor].ID == common.DNSModeCloudflare {
		t.Error("precondition: the cursor should have moved off Cloudflare")
	}
}

// With the marker pinned, the highlight bar is the only cursor indicator, so
// it has to actually be drawn.
func TestCursorHighlightIsRendered(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor) // no TTY under test
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := ModelDnsSelect(config.DefaultColors(), nil)
	m.SSID = "home"
	m.SelectProvider(common.DNSModeDHCP, nil)
	next, _ := m.Update(keyMsg("down"))
	m = next.(DnsSelect)

	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "48;2;") { // a background colour was set
			if !strings.Contains(stripANSI(line), "Cloudflare") {
				t.Errorf("highlight is on %q, want the cursor row", strings.TrimSpace(stripANSI(line)))
			}
			return
		}
	}
	t.Error("no highlighted row rendered; the cursor would be invisible")
}

func stripANSI(s string) string {
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
