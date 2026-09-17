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

// Stages of the import popup.
const (
	// VpnImportPath is the file prompt.
	VpnImportPath = iota
	// VpnImportConfirm shows what the file turns out to contain, before
	// anything is written.
	VpnImportConfirm
)

// VpnImport imports a wg-quick configuration as a NetworkManager profile.
//
// Two stages rather than one. A WireGuard config is not a thing most people
// read before using -- it arrives from a provider as an opaque download -- so
// the second stage says what it actually does: where it connects, whether it
// takes all traffic, and which of its directives NetworkManager cannot carry
// over. That last one matters most. A dropped PostUp is invisible afterwards
// and can be the difference between a tunnel and a tunnel with a killswitch.
type VpnImport struct {
	Stage int
	Path  textinput.Model

	// Filled in once the file parses.
	Config *common.WireGuardConfig
	ID     string
	Ifname string

	ErrText string
	Colors  config.Colors
	Keys    config.KeyBindings
}

func ModelVpnImport(colors config.Colors, keys config.KeyBindings) VpnImport {
	input := newTextInput(colors, "~/mullvad-se.conf", 52, 512)
	input.Focus()

	return VpnImport{
		Stage:  VpnImportPath,
		Path:   input,
		Colors: colors,
		Keys:   keys,
	}
}

func (m VpnImport) Init() tea.Cmd { return textinput.Blink }

func (m VpnImport) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.Path, cmd = m.Path.Update(msg)
		return m, cmd
	}

	switch key.String() {
	case "esc", "ctrl+c":
		// From the summary, back out to the path rather than closing: the
		// usual reason to refuse a summary is a mistyped file.
		if m.Stage == VpnImportConfirm {
			m.Stage = VpnImportPath
			m.Config, m.ID, m.Ifname = nil, "", ""
			m.ErrText = ""
			m.Path.Focus()
			return m, nil
		}
		return m, func() tea.Msg { return common.ExitFormMsg{} }

	case "enter":
		if m.Stage == VpnImportPath {
			cfg, id, ifname, err := common.LoadWireGuardConfig(m.Path.Value())
			if err != nil {
				m.ErrText = err.Error()
				return m, nil
			}
			m.Config, m.ID, m.Ifname = cfg, id, ifname
			m.ErrText = ""
			m.Stage = VpnImportConfirm
			m.Path.Blur()
			return m, nil
		}

		cfg, id, ifname := m.Config, m.ID, m.Ifname
		return m, func() tea.Msg {
			return common.SubmitVpnImportMsg{Config: cfg, ID: id, Ifname: ifname}
		}
	}

	if m.Stage == VpnImportPath {
		var cmd tea.Cmd
		m.Path, cmd = m.Path.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m VpnImport) View() string {
	const inner = 60

	containerStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Padding(0, 1).
		Width(inner + 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(m.Colors.Primary)).
		Align(lipgloss.Center).
		Width(inner)

	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.Inactive))
	alertStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ErrorText)).Width(inner)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.Colors.ActiveText)).Width(inner)

	var rows []string
	if m.Stage == VpnImportPath {
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color(m.Colors.Active)).
			Padding(0, 1).
			Width(inner - 2)

		rows = append(rows,
			titleStyle.Render("Import a WireGuard config"),
			"",
			inputStyle.Render(m.Path.View()),
		)
		if m.ErrText != "" {
			rows = append(rows, "", alertStyle.Render(m.ErrText))
		}
		rows = append(rows, "", descStyle.Width(inner).Align(lipgloss.Center).
			Render("⤶ read the file · ⎋ cancel"))
	} else {
		rows = append(rows, titleStyle.Render(fmt.Sprintf("Import %s?", m.ID)), "")
		for _, line := range m.summary() {
			rows = append(rows, lipgloss.NewStyle().Width(inner).Render(line))
		}
		if warn := m.warnings(); len(warn) > 0 {
			rows = append(rows, "")
			for _, line := range warn {
				rows = append(rows, warnStyle.Render(line))
			}
		}
		rows = append(rows, "", descStyle.Width(inner).Align(lipgloss.Center).
			Render("⤶ import · ⎋ pick another file"))
	}

	return containerStyle.Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// summary describes the profile that would be created.
func (m VpnImport) summary() []string {
	if m.Config == nil {
		return nil
	}

	endpoint := "none given"
	if len(m.Config.Peers) > 0 && m.Config.Peers[0].Endpoint != "" {
		endpoint = m.Config.Peers[0].Endpoint
	}

	routes := "only the peer's allowed IPs"
	if m.Config.RoutesAllTraffic() {
		routes = "all traffic from this machine"
	}

	dns := "unchanged"
	if len(m.Config.DNS) > 0 {
		dns = strings.Join(m.Config.DNS, ", ")
	}

	lines := []string{
		fmt.Sprintf("  Interface   %s", m.Ifname),
		fmt.Sprintf("  Endpoint    %s", endpoint),
		fmt.Sprintf("  Routes      %s", routes),
		fmt.Sprintf("  DNS         %s", dns),
	}
	if len(m.Config.Peers) > 1 {
		lines = append(lines, fmt.Sprintf("  Peers       %d", len(m.Config.Peers)))
	}
	return lines
}

// warnings are the things worth knowing before the profile is written.
func (m VpnImport) warnings() []string {
	if m.Config == nil {
		return nil
	}

	var out []string
	out = append(out, "The private key is stored in the profile, readable by root.")

	if len(m.Config.Unsupported) > 0 {
		out = append(out, fmt.Sprintf(
			"NetworkManager cannot carry over: %s",
			strings.Join(m.Config.Unsupported, ", ")))
	}
	out = append(out, "Imported switched off; connect it from the pane.")
	return out
}
