package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Liveness states for the local DNSCrypt proxy.
const (
	probeIdle = iota
	probeChecking
	probeAlive
	probeDead
)

// DnsSelect is the DNS provider picker shown for a known network. Highlighting
// "Custom" focuses an inline input so the whole thing stays one popup.
type DnsSelect struct {
	SSID      string
	Cursor    int
	Providers []common.DNSProvider
	Custom    textinput.Model
	ErrText   string
	// Current is the provider the profile is set to, marked with ">". Distinct
	// from Cursor, which is only where the highlight sits.
	Current string
	Colors  config.Colors

	// Notice explains why the picker opened when netpala opened it rather
	// than the user. Without it a popup appearing on its own is a mystery.
	Notice string

	// DNSCrypt points at a daemon netpala does not manage, so its liveness is
	// probed in the background and applying a dead one needs a second Enter.
	probeState int
	armed      bool
}

func ModelDnsSelect(colors config.Colors, dnscryptAddrs []string) DnsSelect {
	input := textinput.New()
	input.Placeholder = "9.9.9.9, 149.112.112.112"
	input.Prompt = ""
	input.Width = 34
	input.CharLimit = 256

	return DnsSelect{
		Providers: common.DNSProvidersFor(dnscryptAddrs),
		Custom:    input,
		Colors:    colors,
	}
}

// SelectProvider records which provider the profile is currently using and
// positions the cursor on it, pre-filling the custom field when that's what
// it's using.
//
// Current is kept separate from Cursor because they are different things: the
// cursor is where you are looking, Current is what the network is actually
// set to. Moving the cursor must not make it look as though the setting
// changed before you have chosen anything.
func (m *DnsSelect) SelectProvider(mode string, servers []string) {
	m.Current = mode
	for i, p := range m.Providers {
		if p.ID == mode {
			m.Cursor = i
			break
		}
	}
	if mode == common.DNSModeCustom {
		m.Custom.SetValue(strings.Join(servers, ", "))
	}
	m.syncFocus()
}

func (m *DnsSelect) syncFocus() {
	if m.Providers[m.Cursor].ID == common.DNSModeCustom {
		m.Custom.Focus()
	} else {
		m.Custom.Blur()
	}
}

// ProbeCmdIfNeeded returns a liveness check when the cursor is sitting on
// DNSCrypt, so the caller can run it as soon as the popup opens.
func (m *DnsSelect) ProbeCmdIfNeeded() tea.Cmd {
	if m.Providers[m.Cursor].ID != common.DNSModeDNSCrypt {
		return nil
	}
	m.probeState = probeChecking
	addrs := m.Providers[m.Cursor].V4
	return func() tea.Msg {
		return common.DnsProbeMsg{Addrs: addrs, Err: common.ProbeResolvers(addrs)}
	}
}

// onCursorMove resets the transient state and kicks off a probe when the
// cursor lands on DNSCrypt, so the warning is visible before Enter is pressed.
func (m *DnsSelect) onCursorMove() tea.Cmd {
	m.ErrText = ""
	m.armed = false
	m.probeState = probeIdle
	m.syncFocus()
	return m.ProbeCmdIfNeeded()
}

func (m DnsSelect) Init() tea.Cmd {
	return textinput.Blink
}

func (m DnsSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if probe, ok := msg.(common.DnsProbeMsg); ok {
		// A result that arrives after the cursor moved elsewhere is stale.
		if m.Providers[m.Cursor].ID == common.DNSModeDNSCrypt {
			if probe.Err != nil {
				m.probeState = probeDead
			} else {
				m.probeState = probeAlive
			}
		}
		return m, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return common.ExitFormMsg{} }

		case "up", "shift+tab":
			if m.Cursor > 0 {
				m.Cursor--
			}
			return m, m.onCursorMove()

		case "down", "tab":
			if m.Cursor < len(m.Providers)-1 {
				m.Cursor++
			}
			return m, m.onCursorMove()

		case "enter":
			provider := m.Providers[m.Cursor]

			if provider.ID == common.DNSModeCustom {
				if _, _, err := common.ParseDNSServers(m.Custom.Value()); err != nil {
					m.ErrText = err.Error()
					return m, nil
				}
			}

			// Applying a dead proxy takes out name resolution entirely, so make
			// the second press deliberate rather than silently going through.
			if provider.ID == common.DNSModeDNSCrypt && m.probeState != probeAlive && !m.armed {
				m.armed = true
				if m.probeState == probeChecking {
					m.ErrText = "still checking · press ⤶ again to apply anyway"
				} else {
					m.ErrText = "proxy not responding · press ⤶ again to apply anyway"
				}
				return m, nil
			}

			value := m.Custom.Value()
			return m, func() tea.Msg {
				return common.SubmitDnsMsg{ProviderID: provider.ID, Custom: value}
			}
		}
	}

	// j/k would be swallowed by the text input while Custom is focused, so the
	// list is navigated with the arrows/tab only. Everything else goes to input.
	if m.Custom.Focused() {
		var cmd tea.Cmd
		m.Custom, cmd = m.Custom.Update(msg)
		cmds = append(cmds, cmd)
	} else if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
			cmds = append(cmds, m.onCursorMove())
		case "j":
			if m.Cursor < len(m.Providers)-1 {
				m.Cursor++
			}
			cmds = append(cmds, m.onCursorMove())
		}
	}

	return m, tea.Batch(cmds...)
}

// statusFor annotates the DNSCrypt row with what the probe found.
func (m DnsSelect) statusFor(p common.DNSProvider) string {
	if p.ID != common.DNSModeDNSCrypt {
		return ""
	}
	switch m.probeState {
	case probeChecking:
		return " (checking)"
	case probeAlive:
		return " (running)"
	case probeDead:
		return " (not responding)"
	}
	return ""
}

func (m DnsSelect) View() string {
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
		titleStyle.Render(fmt.Sprintf("DNS for %s", m.SSID)),
		"",
	}
	if m.Notice != "" {
		rows = append(rows,
			lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ActiveText)).
				Width(54).Render(m.Notice),
			"")
	}
	for i, p := range m.Providers {
		// ">" marks what the network is set to, matching the Known Networks
		// and Security tables. The cursor is the highlight bar, as it is in
		// those tables too.
		marker := "  "
		if p.ID == m.Current {
			marker = "> "
		}
		style := normalStyle
		if i == m.Cursor {
			style = selectedStyle
		}

		statusStyle := descStyle
		if m.probeState == probeDead {
			statusStyle = alertStyle
		}

		label := fmt.Sprintf("%s%-11s %s%s",
			marker, p.Label, descStyle.Render(p.Desc), statusStyle.Render(m.statusFor(p)))
		rows = append(rows, style.Render(label))
	}

	if m.Providers[m.Cursor].ID == common.DNSModeCustom {
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(m.Colors.Active)).
			Padding(0, 1).
			Width(52)
		rows = append(rows, "", inputStyle.Render(m.Custom.View()))
	}

	if m.ErrText != "" {
		rows = append(rows, alertStyle.Width(54).Render(m.ErrText))
	}

	rows = append(rows, "", descStyle.Width(54).Align(lipgloss.Center).
		Render("↑/↓ choose · ⤶ apply · ⎋ cancel"))

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}
