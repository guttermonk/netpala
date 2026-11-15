package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mritd/bubbles/selector"
)

type WpaEapForm struct {
	EapMethod  selector.Model
	Phase2Auth selector.Model
	Identity   textinput.Model
	Password   textinput.Model
	CaCert     textinput.Model
	focused    int

	SSIDSelected string
	// APSelected is the access point the SSID was chosen from, passed through
	// to NetworkManager so it activates on that one rather than whichever it
	// would otherwise pick out of the ones sharing the name.
	APSelected string
	// Target is the saved profile being edited, and empty when the form is
	// being used to join a network that has just been found. The fields are
	// the same either way -- a profile holds what the form asks for -- so the
	// form is the same form, and this says which question it is answering.
	Target         common.KnownNetwork
	EapSelected    bool
	Phase2Selected bool
	DisableForm    func()
	Colors         config.Colors
}

// editing reports whether the form is changing a saved profile.
func (m WpaEapForm) editing() bool { return m.Target.Path != "" }

type EAPMethod struct {
	Type string
}

func ModelWpaEapForm(colors config.Colors) WpaEapForm {
	Identity := newTextInput(colors, "Identity", 32, 256)

	Password := newTextInput(colors, "Password", 32, 256)
	Password.EchoMode = textinput.EchoPassword
	Password.EchoCharacter = '*'

	CaCert := newTextInput(colors, "e.g. /etc/ssl/certs/ca.pem", 32, 512)

	m := WpaEapForm{
		Identity:       Identity,
		Password:       Password,
		CaCert:         CaCert,
		focused:        0,
		EapSelected:    false,
		Phase2Selected: false,
		Colors:         colors,
		EapMethod: selector.Model{
			Data: []any{
				EAPMethod{Type: "PEAP"},
				EAPMethod{Type: "TTLS"},
				EAPMethod{Type: "TLS"},
				EAPMethod{Type: "PWD"},
			},
			PerPage:    4,
			HeaderFunc: emptyFunc,
			FooterFunc: emptyFunc,
		},
		Phase2Auth: selector.Model{
			Data: []any{
				EAPMethod{Type: "MSCHAPV2"},
				EAPMethod{Type: "PAP"},
				EAPMethod{Type: "CHAP"},
				EAPMethod{Type: "MSCHAP"},
				EAPMethod{Type: "NONE"},
			},
			PerPage:    5,
			HeaderFunc: emptyFunc,
			FooterFunc: emptyFunc,
		},
	}

	// Set the functions that need access to colors
	m.EapMethod.SelectedFunc = makeSelectedFunc(colors)
	m.EapMethod.UnSelectedFunc = makeUnselectedFunc(colors)
	m.EapMethod.FinishedFunc = makeCompletedFunc([]string{
		"PEAP",
		"TTLS",
		"TLS",
		"PWD",
	}, colors)

	m.Phase2Auth.SelectedFunc = makeUnselectedFunc(colors)
	m.Phase2Auth.UnSelectedFunc = makeUnselectedFunc(colors)
	m.Phase2Auth.FinishedFunc = makeCompletedFunc([]string{
		"MSCHAPV2",
		"PAP",
		"CHAP",
		"MSCHAP",
		"NONE",
	}, colors)

	return m
}

func (m WpaEapForm) Init() tea.Cmd {
	return textinput.Blink
}

// LoadProfile opens the form on a saved profile rather than on empty fields.
//
// The password arrives empty when NetworkManager would not hand the secret
// over, and an empty field already means "leave the stored one alone", so
// there is nothing to say about the difference.
func (m *WpaEapForm) LoadProfile(target common.KnownNetwork, profile common.EapProfileMsg) {
	m.Target = target
	m.SSIDSelected = profile.SSID
	// An edit activates the profile, which names its own access point.
	m.APSelected = ""

	m.Identity.SetValue(profile.Identity)
	m.Password.SetValue(profile.Password)
	m.CaCert.SetValue(profile.CaCert)

	selectOption(&m.EapMethod, profile.EapMethod)
	selectOption(&m.Phase2Auth, profile.Phase2Auth)

	// Starting on the identity rather than on the method: the fields most
	// likely to be the reason for opening this are the credentials, and the
	// two selectors are already on what the profile holds.
	m.focused = 2
	m.EapMethod.SelectedFunc = makeUnselectedFunc(m.Colors)
	m.Phase2Auth.SelectedFunc = makeUnselectedFunc(m.Colors)
	m.Identity.Focus()
}

// selectOption moves a selector onto the named option.
//
// By pressing down, because that is the only way in: the selector's index is
// unexported and it has no setter, so a profile's current method could
// otherwise not be shown at all -- and a form that opens on PEAP for a TTLS
// profile would quietly change it on save.
func selectOption(sel *selector.Model, option string) {
	if option == "" {
		return
	}
	// The selector's first Update is its initialiser, and it consumes that
	// message rather than acting on it, so the first keypress of a run would
	// otherwise be the one that got lost -- leaving every profile one option
	// short of its own.
	initialised, _ := sel.Update(nil)
	*sel = *initialised

	for i, data := range sel.Data {
		method, ok := data.(EAPMethod)
		if !ok || !strings.EqualFold(method.Type, option) {
			continue
		}
		for range i {
			updated, _ := sel.Update(tea.KeyMsg{Type: tea.KeyDown})
			*sel = *updated
		}
		return
	}
}

func (m WpaEapForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// Handle global key presses for focus switching and quitting first.
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "enter":
			switch m.focused {
			case 0:
				m.EapSelected = true
			case 1:
				m.Phase2Selected = true
			}
		// --- focus switching ---
		case "tab", "shift+tab":
			if key.String() == "shift+tab" {
				m.focused = (m.focused + 5) % 6
			} else {
				m.focused = (m.focused + 1) % 6
			}

			// Update focus state for text inputs
			m.Identity.Blur()
			m.Password.Blur()
			m.CaCert.Blur()

			switch m.focused {
			case 0:
				m.EapMethod.SelectedFunc = makeSelectedFunc(m.Colors)
				m.Phase2Auth.SelectedFunc = makeUnselectedFunc(m.Colors)
			case 1:
				m.EapMethod.SelectedFunc = makeUnselectedFunc(m.Colors)
				m.Phase2Auth.SelectedFunc = makeSelectedFunc(m.Colors)
			case 2:
				m.Identity.Focus()
				m.EapMethod.SelectedFunc = makeUnselectedFunc(m.Colors)
				m.Phase2Auth.SelectedFunc = makeUnselectedFunc(m.Colors)
			case 3:
				m.Password.Focus()
			case 4:
				m.CaCert.Focus()
			}
			// Don't pass the tab key to the component itself
			return m, nil

		// --- select all (Ctrl+A) ---
		case "ctrl+a":
			switch m.focused {
			case 2:
				ti := m.Identity
				ti.SetCursor(len(ti.Value()))
				m.Identity = ti
			case 3:
				ti := m.Password
				ti.SetCursor(len(ti.Value()))
				m.Password = ti
			case 4:
				ti := m.CaCert
				ti.SetCursor(len(ti.Value()))
				m.CaCert = ti
			}
			return m, nil
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return common.ExitFormMsg{} }
		}
	}

	var sm *selector.Model

	// 1. Pass non-key messages (like WindowSizeMsg) to selectors so they can render.
	if _, ok := msg.(tea.KeyMsg); !ok {
		sm, cmd = m.EapMethod.Update(msg)
		m.EapMethod = *sm
		cmds = append(cmds, cmd)

		sm, cmd = m.Phase2Auth.Update(msg)
		m.Phase2Auth = *sm
		cmds = append(cmds, cmd)
	}

	// 2. Pass *all* messages to text inputs; they handle focus internally.
	m.Identity, cmd = m.Identity.Update(msg)
	cmds = append(cmds, cmd)
	m.Password, cmd = m.Password.Update(msg)
	cmds = append(cmds, cmd)
	m.CaCert, cmd = m.CaCert.Update(msg)
	cmds = append(cmds, cmd)

	// 3. Only pass key-press messages to the *focused* selector.
	if _, ok := msg.(tea.KeyMsg); ok {
		switch m.focused {
		case 0:
			sm, cmd = m.EapMethod.Update(msg)
			m.EapMethod = *sm
			cmds = append(cmds, cmd)

			if msg.(tea.KeyMsg).String() == "enter" {
				m.focused++
				m.EapMethod.SelectedFunc = makeUnselectedFunc(m.Colors)
				m.Phase2Auth.SelectedFunc = makeSelectedFunc(m.Colors)
			}
		case 1:
			sm, cmd = m.Phase2Auth.Update(msg)
			m.Phase2Auth = *sm
			cmds = append(cmds, cmd)

			if msg.(tea.KeyMsg).String() == "enter" {
				m.EapMethod.SelectedFunc = makeUnselectedFunc(m.Colors)
				m.Phase2Auth.SelectedFunc = makeUnselectedFunc(m.Colors)
				m.Identity.Focus()
				m.focused++
			}
		case 5:
			// Submit form
			config := map[string]string{
				"ssid":        m.SSIDSelected,
				"ap_path":     m.APSelected,
				"eap":         m.EapMethod.Selected().(EAPMethod).Type,
				"phase2-auth": m.Phase2Auth.Selected().(EAPMethod).Type,
				"identity":    m.Identity.Value(),
				"password":    m.Password.Value(),
				"ca_cert":     m.CaCert.Value(),
			}

			// Send the message with the data back to the parent
			if m.editing() {
				target := m.Target
				return m, func() tea.Msg {
					return common.SubmitEapEditMsg{Path: target.Path, Config: config}
				}
			}
			return m, func() tea.Msg {
				return common.SubmitEapFormMsg{Config: config}
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m WpaEapForm) View() string {
	inactiveBorderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Inactive)).
		Padding(0, 1)

	activeBorderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Primary)).
		Padding(0, 1)

	inactiveLabelStyle := lipgloss.NewStyle().
		Bold(false).
		Foreground(lipgloss.Color(m.Colors.Primary))

	activeLabelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(m.Colors.ActiveText))

	formStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(m.Colors.Active)).
		Padding(0, 1)

	// Always render all text boxes, just change the style.
	EapMethodLabel := inactiveLabelStyle.Render("EAP Method:")
	phase2Label := inactiveLabelStyle.Render("Phase 2 (inner-auth):")

	IdentityLabel := inactiveLabelStyle.Render("Identity:")
	IdentityBox := inactiveBorderStyle.Render(m.Identity.View())

	PasswordLabel := inactiveLabelStyle.Render("\nPassword:")
	PasswordBox := inactiveBorderStyle.Render(m.Password.View())

	CaCertLabel := inactiveLabelStyle.Render("\nCA Certificate:")
	CaCertBox := inactiveBorderStyle.Render(m.CaCert.View())

	// What the button does, which is not the same thing in the two modes: an
	// edit writes the profile and leaves connecting to the key that does that.
	submit := "Connect"
	if m.editing() {
		submit = "Save"
	}
	submitLabel := inactiveBorderStyle.
		Width(36).
		Align(lipgloss.Center).
		Render(submit)

	switch m.focused {
	case 0:
		EapMethodLabel = activeLabelStyle.Render("EAP Method:")
	case 1:
		phase2Label = activeLabelStyle.Render("Phase 2 (inner-auth):")
	case 2:
		IdentityLabel = activeLabelStyle.Render("Identity:")
		IdentityBox = activeBorderStyle.Render(m.Identity.View())
	case 3:
		PasswordLabel = activeLabelStyle.Render("\nPassword:")
		PasswordBox = activeBorderStyle.Render(m.Password.View())
	case 4:
		CaCertLabel = activeLabelStyle.Render("\nCA Certificate:")
		CaCertBox = activeBorderStyle.Render(m.CaCert.View())
	case 5:
		submitLabel = activeBorderStyle.
			Width(36).
			Bold(true).
			Align(lipgloss.Center).
			BorderForeground(lipgloss.Color(m.Colors.ActiveText)).
			Render(submit)
	}
	// --- END NEW LOGIC ---
	eapStr := strings.TrimSuffix(strings.Replace(m.EapMethod.View(), "\n", "", 2), "\n")
	phase2Str := strings.TrimSuffix(strings.Replace(m.Phase2Auth.View(), "\n", "", 2), "\n")
	if m.EapSelected {
		eapStr = m.EapMethod.View()
	}
	if m.Phase2Selected {
		phase2Str = m.Phase2Auth.View()
	}

	// We perform the string alterations below the remove the spacing reserved for the header and footer of the selector
	// The network, which the form never said. It is answerable for a form that
	// opens one keypress after a row was chosen; it is not for one that opens
	// after a read off the bus, and five fields of credentials are worth being
	// sure which profile they belong to.
	title := ""
	if m.SSIDSelected != "" {
		title = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(m.Colors.ActiveText)).
			Width(36).
			Align(lipgloss.Center).
			Render(m.SSIDSelected)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		EapMethodLabel,
		eapStr,

		phase2Label,
		phase2Str,

		IdentityLabel,
		IdentityBox,

		PasswordLabel,
		PasswordBox,

		CaCertLabel,
		CaCertBox,

		submitLabel,
	)
	return formStyle.Render(content)
}

func makeSelectedFunc(colors config.Colors) func(selector.Model, any, int) string {
	return func(sm selector.Model, obj any, gdIndex int) string {
		str := obj.(EAPMethod).Type
		return lipgloss.NewStyle().Bold(false).Background(lipgloss.Color(colors.SelectionBg)).Foreground(lipgloss.Color(colors.Inactive)).Render(fmt.Sprintf(" %d. %s", gdIndex+1, str))
	}
}

func makeUnselectedFunc(colors config.Colors) func(selector.Model, any, int) string {
	return func(sm selector.Model, obj any, gdIndex int) string {
		str := obj.(EAPMethod).Type
		return lipgloss.NewStyle().Bold(false).Foreground(lipgloss.Color(colors.Primary)).Render(fmt.Sprintf(" %d. %s", gdIndex+1, str))
	}
}

func emptyFunc(m selector.Model, obj any, gdIndex int) string {
	return ""
}

func makeCompletedFunc(options []string, colors config.Colors) func(any) string {
	return func(selected any) string {
		str := ""

		for i, option := range options {
			if option == selected.(EAPMethod).Type {
				str += lipgloss.NewStyle().Foreground(lipgloss.Color(colors.ActiveText)).Render(fmt.Sprintf("%s  %d. %s", "»", i+1, option)) + "\n"
			} else {
				str += fmt.Sprintf("  %s", makeUnselectedFunc(colors)(selector.Model{}, EAPMethod{Type: option}, i)) + "\n"
			}
		}

		return str
	}
}
