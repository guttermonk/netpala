package main

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"netpala/dbus"
	"netpala/models"
	"netpala/network"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
	overlay "github.com/rmhubbert/bubbletea-overlay"
	"go.dalton.dog/bubbleup"
)

type NetpalaData struct {
	Width, Height int
	selectedBox   int
	SelectedEntry int

	DeviceData      []common.Device
	VpnData         []common.VpnConnection
	SecurityData    []common.SecurityService
	KnownNetworks   []common.KnownNetwork
	ScannedNetworks []common.ScannedNetwork

	Tables    models.TablesModel
	StatusBar models.StatusBarData

	Overlay      overlay.Model
	Form         models.WpaEapForm
	Confirmation models.Confirmation
	PasswordForm models.PasswordInput
	DnsForm      models.DnsSelect

	SelectedNetwork common.ScannedNetwork
	DnsTarget       common.KnownNetwork

	// Unit that has been warned about and is cleared for stopping on the next
	// select press. Reset whenever the selection moves, so the confirmation
	// cannot be spent on a different row than the one it was given for.
	securityArmedUnit string
	PopupState        int // -1: no popup, 0: form, 1: confirm, 2: password, 3: dns

	Alert               bubbleup.AlertModel
	InitialLoadComplete bool
	Conn                *godbus.Conn
	Err                 error
	DBusSignals         chan *godbus.Signal

	// Configuration
	Config *config.Config
	KeyMap config.AppKeyMap
	Colors config.Colors
}

// The initial command to load all data at startup.
func loadInitialData(Conn *godbus.Conn, securityServices []common.SecurityServiceConfig) tea.Cmd {
	return func() tea.Msg {
		// Step 1: Fetch all data first to ensure we have both lists.
		devices := network.GetDevicesData(Conn)
		vpns := network.GetVpnData(Conn)
		security := network.GetSecurityServices(Conn, securityServices)
		known := network.GetKnownNetworks(Conn)
		scanned := network.GetScannedNetworks(Conn)

		// Step 2: Perform the filtering logic on the initial data.
		knownSSIDs := make(map[string]struct{})
		for _, k := range known {
			knownSSIDs[k.SSID] = struct{}{}
		}

		var filteredScanned []common.ScannedNetwork
		for _, s := range scanned {
			if _, exists := knownSSIDs[s.SSID]; !exists {
				filteredScanned = append(filteredScanned, s)
			}
		}
		common.SortDevicesBySignal(filteredScanned)

		// Step 3: Send the final, filtered data to the UI in a single batch.
		return tea.BatchMsg{
			func() tea.Msg { return common.DeviceUpdateMsg(devices) },
			func() tea.Msg { return common.KnownNetworksUpdateMsg(known) },
			func() tea.Msg { return common.ScannedNetworksUpdateMsg(filteredScanned) },
			func() tea.Msg { return common.VpnUpdateMsg(vpns) },
			func() tea.Msg { return common.SecurityUpdateMsg(security) },
		}
	}
}

// visiblePanes lists the panes currently on screen, in display order. VPN and
// Security disappear when empty, so navigation has to be driven by this rather
// than by a fixed count.
func (m NetpalaData) visiblePanes() []int {
	panes := []int{common.PaneKnown, common.PaneScanned}
	if len(m.VpnData) > 0 {
		panes = append(panes, common.PaneVPN)
	}
	if len(m.SecurityData) > 0 {
		panes = append(panes, common.PaneSecurity)
	}
	return append(panes, common.PaneDevice)
}

// paneEntryCount is how many selectable rows a pane holds.
func (m NetpalaData) paneEntryCount(pane int) int {
	switch pane {
	case common.PaneKnown:
		return len(m.KnownNetworks)
	case common.PaneScanned:
		return len(m.ScannedNetworks)
	case common.PaneVPN:
		return len(m.VpnData)
	case common.PaneSecurity:
		return len(m.SecurityData)
	case common.PaneDevice:
		return len(m.DeviceData)
	}
	return 0
}

// stepPane moves the selection by delta panes, skipping hidden ones. If the
// current pane just disappeared, it falls back to the first visible one.
func (m *NetpalaData) stepPane(delta int) {
	panes := m.visiblePanes()

	// A confirmation is only valid for the row it was given for.
	m.securityArmedUnit = ""

	current := 0
	for i, p := range panes {
		if p == m.selectedBox {
			current = i
			break
		}
	}

	m.selectedBox = panes[(current+delta+len(panes))%len(panes)]
	m.SelectedEntry = 0
}

// networksDependingOn lists the saved profiles whose DNS would stop resolving
// if this unit were stopped. A profile pointed at loopback is relying on some
// local daemon; if that daemon is the one being stopped, name resolution dies
// with it.
func (m NetpalaData) networksDependingOn(svc common.SecurityService) []string {
	if !svc.ProvidesDNS {
		return nil
	}
	var names []string
	for _, n := range m.KnownNetworks {
		if n.DNSMode != common.DNSModeDNSCrypt {
			continue
		}
		// The live connection is the one that breaks immediately, so it goes
		// first and is called out.
		if n.Connected {
			names = append([]string{n.SSID + " (connected)"}, names...)
			continue
		}
		names = append(names, n.SSID)
	}
	return names
}

// securityStopWarning describes what stopping this unit would take down, or ""
// if nothing depends on it. Starting a unit can never break anything, so the
// guard only applies in the stop direction.
func (m NetpalaData) securityStopWarning(svc common.SecurityService) string {
	if !svc.Active {
		return ""
	}
	affected := m.networksDependingOn(svc)
	if len(affected) == 0 {
		return ""
	}

	shown := affected
	suffix := ""
	if len(shown) > 3 {
		shown, suffix = shown[:3], fmt.Sprintf(" and %d more", len(affected)-3)
	}
	return fmt.Sprintf("%s resolves DNS for %s%s - stopping it breaks name resolution. Press select again to stop anyway.",
		svc.Name, strings.Join(shown, ", "), suffix)
}

// clampSelection keeps the cursor inside the pane after a refresh shrinks it,
// and moves off a pane that has just been hidden.
func (m *NetpalaData) clampSelection() {
	m.securityArmedUnit = ""
	visible := false
	for _, p := range m.visiblePanes() {
		if p == m.selectedBox {
			visible = true
			break
		}
	}
	if !visible {
		m.selectedBox = common.PaneKnown
		m.SelectedEntry = 0
		return
	}
	if limit := m.paneEntryCount(m.selectedBox); m.SelectedEntry > limit-1 {
		m.SelectedEntry = max(limit-1, 0)
	}
}

func (m *NetpalaData) FilterKnownFromScanned() {
	// Create a map of known SSIDs for fast lookups (more efficient than a nested loop)
	knownSSIDs := make(map[string]struct{})
	for _, known := range m.KnownNetworks {
		knownSSIDs[known.SSID] = struct{}{}
	}

	// Build a new slice containing only the networks we want to keep.
	// This is more efficient than deleting elements from the slice in-place.
	var filteredScanned []common.ScannedNetwork
	for _, scanned := range m.ScannedNetworks {
		if _, exists := knownSSIDs[scanned.SSID]; !exists {
			filteredScanned = append(filteredScanned, scanned)
		}
	}
	m.ScannedNetworks = filteredScanned
}

func NetpalaModel() NetpalaData {
	Conn, err := godbus.SystemBus()
	if err != nil {
		return NetpalaData{Err: fmt.Errorf("failed to Connect to D-Bus: %w", err)}
	}

	sigChan := make(chan *godbus.Signal, 10)
	Conn.Signal(sigChan)

	rules := []string{
		"type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged'",
		"type='signal',interface='org.freedesktop.NetworkManager.Device',member='StateChanged'",
		"type='signal',interface='org.freedesktop.NetworkManager',member='DeviceAdded'",
		"type='signal',interface='org.freedesktop.NetworkManager',member='DeviceRemoved'",
		"type='signal',interface='org.freedesktop.NetworkManager.Settings',member='NewConnection'",
		"type='signal',interface='org.freedesktop.NetworkManager.Settings',member='ConnectionRemoved'",
		"type='signal',interface='org.freedesktop.NetworkManager.Device.Wireless',member='AccessPointAdded'",
		"type='signal',interface='org.freedesktop.NetworkManager.Device.Wireless',member='AccessPointRemoved'",
	}
	busObject := Conn.BusObject()
	for _, rule := range rules {
		call := busObject.Call("org.freedesktop.DBus.AddMatch", 0, rule)
		if call.Err != nil {
			err = fmt.Errorf("could not add match rule '%s': %w", rule, call.Err)
			break
		}
	}

	// Load configuration
	cfg, _ := config.Load()
	keyMap := config.NewAppKeyMap(cfg)

	alert := bubbleup.NewAlertModel(40, true, 10)

	return NetpalaData{
		Conn:        Conn,
		Err:         err,
		DBusSignals: sigChan,
		Alert:       *alert,

		DeviceData:      []common.Device{},
		VpnData:         []common.VpnConnection{},
		SecurityData:    []common.SecurityService{},
		KnownNetworks:   []common.KnownNetwork{},
		ScannedNetworks: []common.ScannedNetwork{},

		Tables:    models.TablesModel{},
		StatusBar: models.ModelStatusBar(keyMap, cfg.Colors),

		PasswordForm: models.ModelPasswordInput(cfg.Colors),
		Form:         models.ModelWpaEapForm(cfg.Colors),
		DnsForm:      models.ModelDnsSelect(cfg.Colors, cfg.DNS.DnscryptAddresses),
		Overlay: overlay.Model{
			XPosition: overlay.Left,
			YPosition: overlay.Center,
			XOffset:   0,
			YOffset:   0,
		},

		PopupState:          -1,
		InitialLoadComplete: false,

		Config: cfg,
		KeyMap: keyMap,
		Colors: cfg.Colors,
	}
}

func (m NetpalaData) Init() tea.Cmd {
	return tea.Batch(
		m.Alert.Init(),
		loadInitialData(m.Conn, m.Config.Security.Services),
		dbus.RefreshTicker(),
		dbus.WaitForDBusSignal(m.Conn, m.DBusSignals),
	)
}

func (m NetpalaData) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.PopupState {
	case 0:
		// Handle the EAP form popup state
		switch msg := msg.(type) {
		case common.ExitFormMsg:
			m.PopupState = -1
			m.Form = models.ModelWpaEapForm(m.Colors)

			var formCmd tea.Cmd
			var newForm tea.Model
			newForm, formCmd = m.Form.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
			m.Form = newForm.(models.WpaEapForm)
			return m, formCmd
		case common.SubmitEapFormMsg:
			m.SelectedEntry = 0
			m.PopupState = -1
			m.Form = models.ModelWpaEapForm(m.Colors)

			// Re-initialize the new form with the window size
			var formCmd tea.Cmd
			var newForm tea.Model
			newForm, formCmd = m.Form.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
			m.Form = newForm.(models.WpaEapForm)

			// Get the Wi-Fi device to connect with
			if len(m.DeviceData) == 0 {
				return m, func() tea.Msg {
					return common.ErrMsg{Err: fmt.Errorf("no wifi device found to connect with")}
				}
			}
			wifiDevice := m.DeviceData[0]

			// Add the EAP connection config from the message
			// and combine it with the form's init command.
			eapCmd := dbus.AddAndConnectEAPCmd(m.Conn, msg.Config, wifiDevice.Path)
			return m, tea.Batch(formCmd, eapCmd)
		}

		var newForm tea.Model
		newForm, cmd = m.Form.Update(msg)
		m.Form = newForm.(models.WpaEapForm)
		return m, cmd
	case 1:
		// Handle the confirmation popup state
		switch msg := msg.(type) {
		case common.SubmitConfirmationMsg:
			m.PopupState = -1                                   // Exit popup
			m.Confirmation = models.ModelConfirmation(m.Colors) // Reset

			if msg.Value { // User confirmed
				// Delete the known network
				// NOTE: Ensure m.SelectedNetwork holds the correct data before entering state 1
				deleteCmd := dbus.DeleteConnectionCmd(m.Conn, m.SelectedNetwork.Path)
				// Return delete command AND re-arm listener
				return m, tea.Batch(deleteCmd, dbus.WaitForDBusSignal(m.Conn, m.DBusSignals))
			} else { // User cancelled
				// Just return and re-arm listener
				return m, dbus.WaitForDBusSignal(m.Conn, m.DBusSignals)
			}

		default: // If it's not a SubmitConfirmationMsg...
			// Forward the *original* message down to the confirmation model
			var newConfirmation tea.Model
			newConfirmation, cmd = m.Confirmation.Update(msg)
			m.Confirmation = newConfirmation.(models.Confirmation)
			// Return the confirmation model and any command it produced
			return m, cmd
		}
	case 2:
		// Handle the password popup state
		switch msg := msg.(type) {
		case common.ExitFormMsg:
			m.PopupState = -1
			m.Form = models.ModelWpaEapForm(m.Colors)

			var formCmd tea.Cmd
			var newForm tea.Model
			newForm, formCmd = m.Form.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
			m.Form = newForm.(models.WpaEapForm)
			return m, formCmd
		case common.SubmitPasswordMsg:
			m.PopupState = -1                                    // Exit popup
			m.PasswordForm = models.ModelPasswordInput(m.Colors) // Reset

			if len(m.DeviceData) == 0 {
				return m, func() tea.Msg {
					return common.ErrMsg{Err: fmt.Errorf("no wifi device found")}
				}
			}

			wifiDevice := m.DeviceData[0]
			// Use the stored network to Connect, not the current selection
			return m, dbus.AddAndConnectToNetworkCmd(m.Conn, m.SelectedNetwork, msg.Value, wifiDevice.Path)

		default: // If it's not a SubmitConfirmationMsg...
			// Forward the *original* message down to the confirmation model
			var newPasswordInput tea.Model
			newPasswordInput, cmd = m.PasswordForm.Update(msg)
			m.PasswordForm = newPasswordInput.(models.PasswordInput)
			// Return the confirmation model and any command it produced
			return m, cmd
		}
	case 3:
		// Handle the DNS provider picker
		switch msg := msg.(type) {
		case common.ExitFormMsg:
			m.PopupState = -1
			m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.DNS.DnscryptAddresses)
			return m, nil

		case common.SubmitDnsMsg:
			m.PopupState = -1
			target := m.DnsTarget
			m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.DNS.DnscryptAddresses)

			// Resolve against the configured list, not the package defaults, so
			// a DNSCrypt proxy on a non-default address is honoured.
			provider := common.DNSProviderByIDFor(msg.ProviderID, m.Config.DNS.DnscryptAddresses)
			v4, v6 := provider.V4, provider.V6
			if provider.ID == common.DNSModeCustom {
				parsedV4, parsedV6, err := common.ParseDNSServers(msg.Custom)
				if err != nil {
					return m, func() tea.Msg { return common.ErrMsg{Err: err} }
				}
				v4, v6 = parsedV4, parsedV6
			}

			// Only re-activate when this profile is the live connection.
			devicePath := godbus.ObjectPath("/")
			if target.Connected && len(m.DeviceData) > 0 {
				devicePath = m.DeviceData[0].Path
			}

			return m, dbus.SetDnsCmd(m.Conn, target.Path, devicePath, provider, v4, v6, target.Connected)

		default:
			var newDnsForm tea.Model
			newDnsForm, cmd = m.DnsForm.Update(msg)
			m.DnsForm = newDnsForm.(models.DnsSelect)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case common.DeviceUpdateMsg:
		m.DeviceData = msg
		return m, dbus.WaitForDBusSignal(m.Conn, m.DBusSignals)

	case common.VpnUpdateMsg:
		m.VpnData = msg
		m.clampSelection()

	case common.SecurityUpdateMsg:
		m.SecurityData = msg
		m.clampSelection()

	case common.KnownNetworksUpdateMsg:
		m.FilterKnownFromScanned()
		m.KnownNetworks = msg

		return m, dbus.WaitForDBusSignal(m.Conn, m.DBusSignals)

	case common.ScannedNetworksUpdateMsg:
		// The `nil` message is the trigger from the listener.
		if msg == nil {
			debounceCmd := tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return common.PerformScanRefreshMsg{}
			})
			// Re-arm the main listener right away, but start the debounce timer.
			return m, tea.Batch(dbus.WaitForDBusSignal(m.Conn, m.DBusSignals), debounceCmd)
		}
		// This is the actual data from a completed scan.
		m.ScannedNetworks = msg
		m.FilterKnownFromScanned()

		// No need to re-arm listener here, as it's handled by the debounce logic.
		return m, nil

	case common.PerformScanRefreshMsg:
		// The debounce timer fired, now perform the scan.
		return m, dbus.GetScanResults(m.Conn)

	case common.ErrMsg:
		// 1. Generate the command. This command produces the internal 'alertMsg'
		//    that the AlertModel is waiting for.
		alertCmd := m.Alert.NewAlertCmd(bubbleup.ErrorKey, "Error: "+msg.Err.Error())

		// 2. Update the main model's error state.
		m.Err = msg.Err

		// 3. Return the command.
		// NOTE: Do NOT call m.Alert.Update(msg) here.
		return m, alertCmd

	case common.RefreshKnownNetworksMsg:
		return m, func() tea.Msg {
			return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(m.Conn))
		}

	case tea.WindowSizeMsg:
		var cmds []tea.Cmd

		m.Width = msg.Width
		m.Height = msg.Height

		var formCmd tea.Cmd
		var newForm tea.Model
		newForm, formCmd = m.Form.Update(msg)
		m.Form = newForm.(models.WpaEapForm)
		cmds = append(cmds, formCmd)

		var statusCmd tea.Cmd
		var newStatus tea.Model
		newStatus, statusCmd = m.StatusBar.Update(msg)
		m.StatusBar = newStatus.(models.StatusBarData)
		cmds = append(cmds, statusCmd)

		return m, tea.Batch(cmds...)

	case common.PeriodicRefreshMsg:
		return m, tea.Batch(
			dbus.RefreshAllData(m.Conn),
			// Re-read unit state so a systemctl change made elsewhere shows up.
			func() tea.Msg {
				return common.SecurityUpdateMsg(
					network.GetSecurityServices(m.Conn, m.Config.Security.Services))
			},
			dbus.RefreshTicker(),
		)

	case tea.KeyMsg:
		keyStr := msg.String()

		// Quit action
		if m.Config.KeyBindings.Quit.Matches(keyStr) {
			m.Conn.RemoveSignal(m.DBusSignals)
			m.Conn.Close()
			return m, tea.Quit
		}

		// Scan action
		if m.Config.KeyBindings.Scan.Matches(keyStr) {
			var cmds []tea.Cmd
			cmds = append(cmds, dbus.RequestScan(m.Conn))
			cmds = append(cmds, func() tea.Msg {
				return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(m.Conn))
			})
			return m, tea.Batch(cmds...)
		}

		// Up navigation
		if m.Config.KeyBindings.Up.Matches(keyStr) {
			m.securityArmedUnit = ""
			if m.SelectedEntry > 0 {
				m.SelectedEntry--
			}
			return m, nil
		}

		// Down navigation
		if m.Config.KeyBindings.Down.Matches(keyStr) {
			m.securityArmedUnit = ""
			if m.SelectedEntry < m.paneEntryCount(m.selectedBox)-1 {
				m.SelectedEntry++
			}
			return m, nil
		}

		// Previous pane navigation
		if m.Config.KeyBindings.PrevPane.Matches(keyStr) {
			m.stepPane(-1)
			return m, nil
		}

		// Next pane navigation
		if m.Config.KeyBindings.NextPane.Matches(keyStr) {
			m.stepPane(1)
			return m, nil
		}

		// Select/Connect action
		if m.Config.KeyBindings.Select.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 && len(m.DeviceData) > 0 {
				// Connect to known network
				selectedNetwork := m.KnownNetworks[m.SelectedEntry]
				wifiDevice := m.DeviceData[0]
				return m, dbus.ConnectToNetworkCmd(m.Conn, selectedNetwork.Path, wifiDevice.Path)
			} else if m.selectedBox == common.PaneScanned && len(m.ScannedNetworks) > 0 && len(m.DeviceData) > 0 {
				// Store the selected network before entering typing mode
				m.SelectedNetwork = m.ScannedNetworks[m.SelectedEntry]

				switch m.SelectedNetwork.Security {
				case "wpa2-eap":
					m.Form.SSIDSelected = m.SelectedNetwork.SSID
					m.PopupState = 0

					m.Overlay = updateOverlayModel(m, &m.Form)
					return m, nil
				case "open":
					// Open network, connect directly
					wifiDevice := m.DeviceData[0]
					return m, dbus.AddAndConnectToNetworkCmd(m.Conn, m.SelectedNetwork, "", wifiDevice.Path)
				case "owe":
					// Opportunistically encrypted network, connect directly
					wifiDevice := m.DeviceData[0]
					return m, dbus.AddAndConnectToNetworkCmd(m.Conn, m.SelectedNetwork, "", wifiDevice.Path)
				default:
					// Most common case: prompt for password
					m.PopupState = 2
					m.PasswordForm.Password.Focus()

					m.Overlay = updateOverlayModel(m, &m.PasswordForm)
					return m, nil
				}
			} else if m.selectedBox == common.PaneVPN && len(m.VpnData) > 0 {
				// Toggle VPN
				selectedVpn := m.VpnData[m.SelectedEntry]
				return m, dbus.ToggleVpnCmd(m.Conn, selectedVpn.Path, selectedVpn.ActivePath, !selectedVpn.Connected)
			} else if m.selectedBox == common.PaneSecurity && len(m.SecurityData) > 0 {
				// Toggle a systemd unit (Tor, DNSCrypt, ...)
				selectedSvc := m.SecurityData[m.SelectedEntry]

				// Stopping a resolver that networks point at kills DNS, so
				// warn once and make the second press deliberate.
				if warning := m.securityStopWarning(selectedSvc); warning != "" && m.securityArmedUnit != selectedSvc.Unit {
					m.securityArmedUnit = selectedSvc.Unit
					return m, m.Alert.NewAlertCmd(bubbleup.WarnKey, warning)
				}

				m.securityArmedUnit = ""
				return m, dbus.ToggleSecurityServiceCmd(m.Conn, selectedSvc, m.Config.Security.Services)
			} else if m.selectedBox == common.PaneDevice && len(m.DeviceData) > 0 {
				// Enable/Disable Wifi Card
				return m, dbus.ToggleWifiCmd(m.Conn, !m.DeviceData[0].Powered)
			}
		}

		// Remove/Delete action
		if m.Config.KeyBindings.Remove.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				// Delete known network
				m.SelectedNetwork = common.ScannedNetwork{
					Path:     m.KnownNetworks[m.SelectedEntry].Path,
					SSID:     m.KnownNetworks[m.SelectedEntry].SSID,
					BSSID:    m.KnownNetworks[m.SelectedEntry].BSSID,
					Security: m.KnownNetworks[m.SelectedEntry].Security,
					Signal:   m.KnownNetworks[m.SelectedEntry].Signal,
				}
				m.PopupState = 1
				m.Confirmation = models.ModelConfirmation(m.Colors)
				m.Confirmation.Message = fmt.Sprintf("Are you sure you want to delete the known network '%s'?\n", m.SelectedNetwork.SSID)

				m.Overlay = updateOverlayModel(m, &m.Confirmation)
				return m, nil
			}
		}

		// Toggle Auto-Connect action (only for known networks)
		if m.Config.KeyBindings.ToggleAutoConnect.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				selectedNetwork := m.KnownNetworks[m.SelectedEntry]
				return m, dbus.ToggleAutoConnectCmd(m.Conn, selectedNetwork.Path, selectedNetwork.AutoConnect)
			}
		}

		// Toggle Hidden action (only for known networks)
		if m.Config.KeyBindings.ToggleHidden.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				selectedNetwork := m.KnownNetworks[m.SelectedEntry]
				return m, dbus.ToggleHiddenCmd(m.Conn, selectedNetwork.Path, selectedNetwork.Hidden)
			}
		}

		// DNS provider switcher (only for known networks)
		if m.Config.KeyBindings.SetDns.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				m.DnsTarget = m.KnownNetworks[m.SelectedEntry]

				m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.DNS.DnscryptAddresses)
				m.DnsForm.SSID = m.DnsTarget.SSID
				m.DnsForm.SelectProvider(m.DnsTarget.DNSMode, m.DnsTarget.DNSServers)

				// If the profile already uses DNSCrypt, check the proxy is up
				// before the user has a chance to re-apply it.
				probeCmd := m.DnsForm.ProbeCmdIfNeeded()

				m.PopupState = 3
				m.Overlay = updateOverlayModel(m, &m.DnsForm)
				return m, probeCmd
			}
		}
	}

	var updatedAlert tea.Model
	updatedAlert, cmd = m.Alert.Update(msg)
	m.Alert = updatedAlert.(bubbleup.AlertModel)
	return m, cmd
}

func (m NetpalaData) View() string {
	netsHeight := common.WindowDimensions().Height - 14
	if len(m.VpnData) > 0 {
		netsHeight -= 5
	}
	if len(m.SecurityData) > 0 {
		netsHeight -= 3 + len(m.SecurityData)
	}

	m.Tables.SelectedBox = m.selectedBox
	m.Tables.SelectedEntry = m.SelectedEntry
	m.Tables.KnownHeight = netsHeight / 2
	m.Tables.ScannedHeight = netsHeight - (netsHeight / 2)
	m.Tables.VPNHeight = 5
	m.Tables.SecurityHeight = 3 + len(m.SecurityData)
	m.Tables.DeviceHeight = 5

	m.Tables.DeviceData = m.DeviceData
	m.Tables.VpnData = m.VpnData
	m.Tables.SecurityData = m.SecurityData
	m.Tables.KnownNetworks = m.KnownNetworks
	m.Tables.ScannedNetworks = m.ScannedNetworks
	m.Tables.Colors = m.Colors

	switch m.PopupState {
	case 0:
		m.Overlay = updateOverlayModel(m, &m.Form)
		return m.Alert.Render(m.Overlay.View() + m.StatusBar.View())
	case 1:
		m.Overlay = updateOverlayModel(m, &m.Confirmation)
		return m.Alert.Render(m.Overlay.View() + m.StatusBar.View())
	case 2:
		m.Overlay = updateOverlayModel(m, &m.PasswordForm)
		return m.Alert.Render(m.Overlay.View() + m.StatusBar.View())
	case 3:
		m.Overlay = updateOverlayModel(m, &m.DnsForm)
		return m.Alert.Render(m.Overlay.View() + m.StatusBar.View())
	default:
		return m.Alert.Render(m.Tables.View() + m.StatusBar.View())
	}
}

func main() {
	tea.NewProgram(NetpalaModel(), tea.WithAltScreen()).Run()
}

func updateOverlayModel(m NetpalaData, popup tea.Model) overlay.Model {
	newOverlay := overlay.Model{
		Background: &m.Tables,
		Foreground: popup,
		XPosition:  overlay.Left,
		YPosition:  overlay.Center,
		XOffset:    common.CalculatePadding(popup.View()),
		YOffset:    0,
	}

	newOverlay.Update(tea.WindowSizeMsg{Width: m.Width, Height: m.Height})
	return newOverlay
}
