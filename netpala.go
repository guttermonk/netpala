package main

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"netpala/dbus"
	"netpala/models"
	"netpala/network"
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

	// What the machine actually resolves through, and what NetworkManager
	// configured. They differ when something outside NM owns resolv.conf.
	EffectiveDNS []string
	AppliedDNS   []string

	Tables    models.TablesModel
	StatusBar models.StatusBarData

	Overlay      overlay.Model
	Form         models.WpaEapForm
	Confirmation models.Confirmation
	PasswordForm models.PasswordInput
	DnsForm      models.DnsSelect

	SelectedNetwork common.ScannedNetwork
	DnsTarget       common.KnownNetwork

	// Unit waiting to be stopped once the live connection has been moved off
	// it. Set when the DNS picker is opened to choose a replacement resolver,
	// cleared if that choice is cancelled.
	pendingStopUnit string

	// Which profile was live at the last refresh, so connecting to a network
	// can be told apart from merely refreshing while already on it.
	lastConnectedPath godbus.ObjectPath
	connectionTracked bool
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
			func() tea.Msg {
				return common.DnsStateMsg{
					Effective: common.SystemResolvers(),
					Applied:   network.AppliedResolvers(Conn),
				}
			},
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

// dnsService returns the configured unit that answers DNS on loopback, if one
// is installed. It is the other half of the DNSCrypt option in the DNS picker:
// the profile setting says where to send queries, this unit is what answers
// them, and netpala keeps the two in step for the live connection.
func (m NetpalaData) dnsService() (common.SecurityService, bool) {
	for _, s := range m.SecurityData {
		if s.ProvidesDNS {
			return s, true
		}
	}
	return common.SecurityService{}, false
}

// connectedNetwork returns the profile currently in use.
func (m NetpalaData) connectedNetwork() (common.KnownNetwork, bool) {
	for _, n := range m.KnownNetworks {
		if n.Connected {
			return n, true
		}
	}
	return common.KnownNetwork{}, false
}

// startDNSServiceCmd starts the local resolver if it is installed and not
// already running. Returns nil when there is nothing to do, so callers can
// hand the result straight to tea.Sequence.
func (m NetpalaData) startDNSServiceCmd() tea.Cmd {
	svc, ok := m.dnsService()
	if !ok || svc.Active {
		return nil
	}
	return dbus.ToggleSecurityServiceCmd(m.Conn, svc, m.Config.Security.Services)
}

// setConnectedDNSCmd points the live connection at the local resolver. Used
// when the resolver is switched on from the Security pane, so turning it on
// actually takes effect instead of only arming it for later.
func (m NetpalaData) setConnectedDNSCmd() tea.Cmd {
	target, ok := m.connectedNetwork()
	if !ok || target.DNSMode == common.DNSModeDNSCrypt {
		return nil // nothing connected, or already pointed at it
	}

	provider := common.DNSProviderByIDFor(common.DNSModeDNSCrypt, m.Config.DNS.DnscryptAddresses)

	devicePath := godbus.ObjectPath("/")
	if len(m.DeviceData) > 0 {
		devicePath = m.DeviceData[0].Path
	}
	return dbus.SetDnsCmd(m.Conn, target.Path, devicePath, provider, provider.V4, provider.V6, true)
}

// onConnectionChanged brings the local resolver up when netpala connects to a
// network whose profile asks for it.
//
// This is what makes setting DNSCrypt on an inactive profile meaningful: the
// setting is saved now and the resolver follows when that network is actually
// activated. Only transitions count, so a refresh while already connected does
// not keep re-issuing the start, and the first observation after launch just
// seeds the tracker rather than starting services merely because netpala ran.
func (m *NetpalaData) onConnectionChanged() tea.Cmd {
	current, _ := m.connectedNetwork()

	if !m.connectionTracked {
		m.connectionTracked = true
		m.lastConnectedPath = current.Path
		return nil
	}
	if current.Path == m.lastConnectedPath {
		return nil
	}
	m.lastConnectedPath = current.Path

	if current.DNSMode != common.DNSModeDNSCrypt {
		return nil
	}
	return m.startDNSServiceCmd()
}

// stopWouldBreakDNS reports whether stopping this unit would leave the live
// connection resolving through something that is no longer there.
//
// Saved profiles that are not connected do not count: their resolver is
// started again by onConnectionChanged when they are next activated.
func (m NetpalaData) stopWouldBreakDNS(svc common.SecurityService) bool {
	if !svc.ProvidesDNS || !svc.Active {
		return false
	}
	// The profile is only half the answer. resolv.conf can be pointed at a
	// local resolver by something outside NetworkManager entirely, in which
	// case the profile still reads "dhcp" while every lookup on the machine
	// goes through this unit. Ask what is actually resolving.
	if m.resolverIsInUse() {
		return true
	}
	current, ok := m.connectedNetwork()
	return ok && current.DNSMode == common.DNSModeDNSCrypt
}

// resolverIsInUse reports whether the local resolver is one of the servers the
// system is currently sending queries to.
//
// Matched against the addresses the resolver is configured to listen on, not
// against "any loopback address": a machine can be resolving through
// systemd-resolved on 127.0.0.53 while this unit listens on 127.0.0.1, and
// stopping it then breaks nothing. Any overlap counts, because a resolver
// listed at all is in the query path.
func (m NetpalaData) resolverIsInUse() bool {
	if m.Config == nil || len(m.EffectiveDNS) == 0 {
		return false
	}
	listening := make(map[string]struct{}, len(m.Config.DNS.DnscryptAddresses))
	for _, a := range m.Config.DNS.DnscryptAddresses {
		listening[a] = struct{}{}
	}
	for _, e := range m.EffectiveDNS {
		if _, ok := listening[e]; ok {
			return true
		}
	}
	return false
}

// dnsIsOverridden reports whether resolv.conf is being driven by something
// other than NetworkManager, which means changing the connection profile will
// not move DNS and netpala cannot clear the way before stopping a resolver.
func (m NetpalaData) dnsIsOverridden() bool {
	effective := m.EffectiveDNS
	if len(effective) == 0 {
		return false
	}
	applied := m.AppliedDNS
	if len(applied) == 0 {
		return false // nothing to compare against; assume NM is in charge
	}

	have := make(map[string]struct{}, len(applied))
	for _, a := range applied {
		have[a] = struct{}{}
	}
	for _, e := range effective {
		if _, ok := have[e]; !ok {
			return true
		}
	}
	return false
}

// openDnsPickerForStop asks where DNS should go before the resolver is taken
// away, so there is no window where resolv.conf points at a dead listener.
// The unit is stopped only once a replacement has been applied.
func (m *NetpalaData) openDnsPickerForStop(svc common.SecurityService) {
	target, _ := m.connectedNetwork()
	m.DnsTarget = target
	m.pendingStopUnit = svc.Unit

	m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.DNS.DnscryptAddresses)
	m.DnsForm.SSID = target.SSID

	if m.dnsIsOverridden() {
		// Changing the profile will not move resolv.conf, so promising a clean
		// hand-off would be a lie. Say what netpala can and cannot do.
		m.DnsForm.Notice = fmt.Sprintf(
			"%s answers DNS, but resolv.conf is set outside NetworkManager - "+
				"changing this profile will not move it. Stopping anyway will break lookups.",
			svc.Name)
	} else {
		m.DnsForm.Notice = fmt.Sprintf("Switching %s off - pick DNS for %s first", svc.Name, target.SSID)
	}
	// Start on DHCP rather than the current DNSCrypt selection: the point of
	// this popup is to move off it.
	m.DnsForm.SelectProvider(common.DNSModeDHCP, nil)

	m.PopupState = 3
	m.Overlay = updateOverlayModel(*m, &m.DnsForm)
}

// clampSelection keeps the cursor inside the pane after a refresh shrinks it,
// and moves off a pane that has just been hidden.
func (m *NetpalaData) clampSelection() {
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

	// Data refreshes describe the world, not the popup, so they are applied
	// whether or not one is open. Leaving them to the popup dispatch meant
	// every popup's default branch forwarded them into a form that ignores
	// them - the table went stale, and because the KnownNetworksUpdateMsg
	// handler is what re-arms the D-Bus signal listener, swallowing one also
	// killed automatic refreshes until netpala was restarted.
	if next, dataCmd, handled := m.handleDataMsg(msg); handled {
		return next, dataCmd
	}

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

			// Backing out of "pick a replacement" means the resolver stays up.
			// Stopping it anyway is the exact outage this flow exists to avoid.
			m.pendingStopUnit = ""
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
			setDns := dbus.SetDnsCmd(m.Conn, target.Path, devicePath, provider, v4, v6, target.Connected)

			// Choosing DNSCrypt for the live connection also brings up the
			// resolver that has to answer those queries. For a profile that is
			// not connected the setting is saved but the resolver is left
			// alone - it comes up when that network is activated.
			if provider.ID == common.DNSModeDNSCrypt && target.Connected {
				// Picking DNSCrypt while being asked to move off it means the
				// user changed their mind; abandon the pending stop.
				m.pendingStopUnit = ""

				if start := m.startDNSServiceCmd(); start != nil {
					// Sequence, not Batch: the resolver has to be listening
					// before resolv.conf starts pointing at it.
					return m, tea.Sequence(start, setDns)
				}
				return m, setDns
			}

			// A replacement was chosen for a resolver that is on its way out.
			// Apply the new DNS first, then stop the unit, so there is never a
			// moment where resolv.conf points at a listener that has gone.
			if m.pendingStopUnit != "" {
				unit := m.pendingStopUnit
				m.pendingStopUnit = ""

				for _, svc := range m.SecurityData {
					if svc.Unit == unit && svc.Active {
						stop := dbus.ToggleSecurityServiceCmd(m.Conn, svc, m.Config.Security.Services)
						return m, tea.Sequence(setDns, stop)
					}
				}
			}

			return m, setDns

		default:
			var newDnsForm tea.Model
			newDnsForm, cmd = m.DnsForm.Update(msg)
			m.DnsForm = newDnsForm.(models.DnsSelect)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
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
			func() tea.Msg {
				return common.DnsStateMsg{
					Effective: common.SystemResolvers(),
					Applied:   network.AppliedResolvers(m.Conn),
				}
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
			if m.SelectedEntry > 0 {
				m.SelectedEntry--
			}
			return m, nil
		}

		// Down navigation
		if m.Config.KeyBindings.Down.Matches(keyStr) {
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

				// Stopping the resolver the live connection is using would
				// leave resolv.conf pointing at nothing. Rather than warn and
				// let it happen, ask where DNS should go instead and stop the
				// resolver only once the connection has been moved off it.
				if m.stopWouldBreakDNS(selectedSvc) {
					m.openDnsPickerForStop(selectedSvc)
					return m, nil
				}

				toggle := dbus.ToggleSecurityServiceCmd(m.Conn, selectedSvc, m.Config.Security.Services)

				// Switching the local resolver on should actually route
				// queries to it, otherwise it sits there answering nobody.
				// Sequenced so it is listening before resolv.conf changes.
				if selectedSvc.ProvidesDNS && !selectedSvc.Active {
					if setDns := m.setConnectedDNSCmd(); setDns != nil {
						return m, tea.Sequence(toggle, setDns)
					}
				}

				return m, toggle
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
	// The VPN, Security and Device panes render one row per entry regardless
	// of the height they are given, so the layout budgets on their real sizes
	// and hands whatever is left to the two network lists.
	layout := computeLayout(
		common.WindowDimensions().Height,
		len(m.KnownNetworks), len(m.ScannedNetworks),
		len(m.VpnData), len(m.SecurityData), len(m.DeviceData),
	)

	m.Tables.SelectedBox = m.selectedBox
	m.StatusBar.Pane = m.selectedBox
	m.Tables.SelectedEntry = m.SelectedEntry
	m.Tables.KnownHeight = layout.Known
	m.Tables.ScannedHeight = layout.Scanned
	m.Tables.VPNHeight = len(m.VpnData)
	m.Tables.SecurityHeight = len(m.SecurityData)
	m.Tables.DeviceHeight = len(m.DeviceData)

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

// handleDataMsg applies the messages that describe system state rather than
// user input: device, network, VPN, security and DNS refreshes, plus the
// timers that drive them and any error worth surfacing.
//
// These must land whether or not a popup is open, so Update dispatches them
// before the popup switch. handled is false for anything else, leaving key
// presses and window resizes to the popup and the main switch.
func (m NetpalaData) handleDataMsg(msg tea.Msg) (NetpalaData, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case common.DeviceUpdateMsg:
		m.DeviceData = msg
		return m, dbus.WaitForDBusSignal(m.Conn, m.DBusSignals), true

	case common.VpnUpdateMsg:
		m.VpnData = msg
		m.clampSelection()
		return m, nil, true

	case common.SecurityUpdateMsg:
		m.SecurityData = msg
		m.clampSelection()
		return m, nil, true

	case common.DnsStateMsg:
		m.EffectiveDNS = msg.Effective
		m.AppliedDNS = msg.Applied
		return m, nil, true

	case common.KnownNetworksUpdateMsg:
		m.FilterKnownFromScanned()
		m.KnownNetworks = msg
		m.clampSelection()

		cmds := []tea.Cmd{dbus.WaitForDBusSignal(m.Conn, m.DBusSignals)}
		if onConnect := m.onConnectionChanged(); onConnect != nil {
			cmds = append(cmds, onConnect)
		}
		return m, tea.Batch(cmds...), true

	case common.ScannedNetworksUpdateMsg:
		// The `nil` message is the trigger from the listener.
		if msg == nil {
			debounceCmd := tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return common.PerformScanRefreshMsg{}
			})
			// Re-arm the main listener right away, but start the debounce timer.
			return m, tea.Batch(dbus.WaitForDBusSignal(m.Conn, m.DBusSignals), debounceCmd), true
		}
		// This is the actual data from a completed scan.
		m.ScannedNetworks = msg
		m.FilterKnownFromScanned()

		// No need to re-arm listener here, as it's handled by the debounce logic.
		return m, nil, true

	case common.PerformScanRefreshMsg:
		// The debounce timer fired, now perform the scan.
		return m, dbus.GetScanResults(m.Conn), true

	case common.ErrMsg:
		// 1. Generate the command. This command produces the internal 'alertMsg'
		//    that the AlertModel is waiting for.
		alertCmd := m.Alert.NewAlertCmd(bubbleup.ErrorKey, "Error: "+msg.Err.Error())

		// 2. Update the main model's error state.
		m.Err = msg.Err

		// 3. Return the command.
		// NOTE: Do NOT call m.Alert.Update(msg) here.
		return m, alertCmd, true

	case common.RefreshKnownNetworksMsg:
		return m, func() tea.Msg {
			return common.KnownNetworksUpdateMsg(network.GetKnownNetworks(m.Conn))
		}, true

	}
	return m, nil, false
}
