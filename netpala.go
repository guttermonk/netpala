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

// What an accepted confirmation popup carries out.
const (
	confirmDeleteNetwork = iota
	confirmStartService
	confirmDeleteVpn
	confirmApplyResolver
)

type NetpalaData struct {
	Width, Height int
	selectedBox   int
	SelectedEntry int

	DeviceData []common.Device
	// VpnData is what the pane shows: NetworkManager profiles followed by
	// vendor daemons. Kept as one slice so navigation, selection and the
	// layout all work on a single list.
	//
	// The two halves are held separately because they refresh independently --
	// profiles on NetworkManager's signals, providers on the timer -- and an
	// update to one must not discard the other.
	VpnData         []common.VpnConnection
	VpnProfiles     []common.VpnConnection
	VpnProviders    []common.VpnConnection
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
	MacForm      models.MacSelect
	VpnForm      models.VpnImport

	SelectedNetwork common.ScannedNetwork
	DnsTarget       common.DNSTarget
	MacTarget       common.KnownNetwork
	// VpnTarget is the profile a confirmed deletion will remove. Held by value
	// rather than by row index for the same reason pendingStartUnit is: a
	// refresh can rewrite VpnData while the prompt is up, and a stale index
	// would delete whatever had moved into that row.
	VpnTarget common.VpnConnection
	// NMMacDefault is what NetworkManager's global wifi.cloned-mac-address
	// resolves to, read once so the MAC picker can name what "Default" means.
	NMMacDefault string

	// Unit waiting to be stopped once the live connection has been moved off
	// it. Set when the DNS picker is opened to choose a replacement resolver,
	// cleared if that choice is cancelled.
	pendingStopUnit string

	// What the confirmation popup will do if accepted. One popup serves both
	// deleting a network and starting a service that asks for consent, so the
	// intent has to be recorded when it opens rather than inferred later.
	confirmAction int
	// Unit awaiting consent before being started, for confirmStartService.
	pendingStartUnit string

	// Which profile was live at the last refresh, so connecting to a network
	// can be told apart from merely refreshing while already on it.
	lastConnectedPath godbus.ObjectPath
	connectionTracked bool
	PopupState        int // -1: none, 0: eap, 1: confirm, 2: password, 3: dns, 4: mac, 5: vpn import

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
func loadInitialData(Conn *godbus.Conn, securityServices []common.SecurityServiceConfig, vpnProviders []common.VpnProviderConfig) tea.Cmd {
	return func() tea.Msg {
		// Step 1: Fetch all data first to ensure we have both lists.
		devices := network.GetDevicesData(Conn)
		vpns := network.GetVpnData(Conn)
		providers := network.GetVpnProviders(Conn, vpnProviders)
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
			func() tea.Msg { return common.VpnProvidersUpdateMsg(providers) },
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

// openStartConfirmation puts the service's consent text on screen and records
// which unit is waiting on the answer.
//
// The unit is remembered by name rather than by index: refreshes rewrite
// SecurityData while the popup is open, and a stale index would start whatever
// happened to land in that row.
func (m *NetpalaData) openStartConfirmation(svc common.SecurityService) {
	m.PopupState = 1
	m.confirmAction = confirmStartService
	m.pendingStartUnit = svc.Unit
	m.Confirmation = models.ModelConfirmation(m.Colors)
	m.Confirmation.Message = svc.Confirm
	m.Overlay = updateOverlayModel(*m, &m.Confirmation)
}

// refreshVpnProvidersCmd re-reads the vendor daemons. Needs the configuration,
// which is why it lives here rather than in the dbus package with the rest.
func (m NetpalaData) refreshVpnProvidersCmd() tea.Cmd {
	providers := m.Config.VPN.Providers
	conn := m.Conn
	return func() tea.Msg {
		return common.VpnProvidersUpdateMsg(network.GetVpnProviders(conn, providers))
	}
}

// rebuildVpnData joins the two sources into the list the pane shows.
//
// Profiles first, so the rows netpala can actually manage are the ones under
// the cursor by default, and so the order does not shuffle when a daemon
// appears or goes away.
func (m *NetpalaData) rebuildVpnData() {
	combined := make([]common.VpnConnection, 0, len(m.VpnProfiles)+len(m.VpnProviders))
	combined = append(combined, m.VpnProfiles...)
	combined = append(combined, m.VpnProviders...)
	m.VpnData = combined
	m.clampSelection()
}

// selectedVpn is the row under the cursor, if the VPN pane holds one.
func (m NetpalaData) selectedVpn() (common.VpnConnection, bool) {
	if m.selectedBox != common.PaneVPN || m.SelectedEntry >= len(m.VpnData) {
		return common.VpnConnection{}, false
	}
	return m.VpnData[m.SelectedEntry], true
}

// notForDaemonsCmd explains why a key did nothing on a vendor-daemon row.
//
// Silence would be worse: the key works on the row above, so nothing happening
// reads as a bug rather than as "there is no profile here to change".
func (m NetpalaData) notForDaemonsCmd(action string, v common.VpnConnection) tea.Cmd {
	return m.Alert.NewAlertCmd(bubbleup.InfoKey, fmt.Sprintf(
		"%s is run by %s, not by NetworkManager - there is no profile to %s",
		v.Name, v.Unit, action))
}

// vpnNameTaken reports whether a VPN profile of this name already exists.
func (m NetpalaData) vpnNameTaken(id string) bool {
	for _, v := range m.VpnData {
		if v.Name == id {
			return true
		}
	}
	return false
}

// openDeleteVpnConfirmation asks before removing a saved VPN profile.
//
// Always asks, unlike the Security pane's start-only gate: deleting is not a
// toggle, and there is nothing to switch back on afterwards. A WireGuard
// profile in particular takes its private key with it, and netpala has no way
// to put that back.
func (m *NetpalaData) openDeleteVpnConfirmation(vpn common.VpnConnection) {
	m.PopupState = 1
	m.confirmAction = confirmDeleteVpn
	m.VpnTarget = vpn
	m.Confirmation = models.ModelConfirmation(m.Colors)

	msg := fmt.Sprintf("Are you sure you want to delete the VPN connection '%s'?\n", vpn.Name)
	if vpn.ConnType == "WireGuard" {
		msg += "\nIts private key is stored in the profile and will go with it.\n"
	}
	m.Confirmation.Message = msg

	m.Overlay = updateOverlayModel(*m, &m.Confirmation)
}

// toggleSecurityCmd starts or stops a unit, and nothing else.
func (m NetpalaData) toggleSecurityCmd(svc common.SecurityService) tea.Cmd {
	return dbus.ToggleSecurityServiceCmd(m.Conn, svc, m.Config.Security.Services)
}

// toggleSecurityWithResolverCmd starts the local resolver and points the live
// connection at it.
//
// Sequenced rather than batched: the resolver has to be listening before
// resolv.conf starts naming it, or there is a window where every lookup on the
// machine goes to a port with nothing behind it.
func (m NetpalaData) toggleSecurityWithResolverCmd(svc common.SecurityService) tea.Cmd {
	toggle := m.toggleSecurityCmd(svc)
	setDns := m.setConnectedDNSCmd()
	if setDns == nil {
		return toggle
	}
	return tea.Sequence(toggle, setDns)
}

// shouldAskApplyResolver reports whether starting this unit raises a question
// worth putting to the user: it answers DNS, and the live connection is not
// pointed at it yet.
func (m NetpalaData) shouldAskApplyResolver(svc common.SecurityService) bool {
	if !svc.ProvidesDNS || svc.Active {
		return false
	}
	// setConnectedDNSCmd is nil when there is nothing connected, or when the
	// connection already resolves through it -- in both cases there is nothing
	// to decide.
	return m.setConnectedDNSCmd() != nil
}

// openApplyResolverConfirmation asks whether the live connection should start
// resolving through the local resolver being switched on.
//
// Netpala used to do this without asking, on the reasoning that a resolver
// answering nobody is pointless. But it rewrites the network profile and moves
// every lookup on the machine, which is a larger thing than "start a service"
// and not what the keypress said it would do. Asking also leaves room for the
// legitimate answer of running the resolver for something else to use.
//
// Declining still starts the unit. The question is only about DNS.
func (m *NetpalaData) openApplyResolverConfirmation(svc common.SecurityService) {
	ssid := ""
	if current, ok := m.connectedNetwork(); ok {
		ssid = current.SSID
	}

	m.PopupState = 1
	m.confirmAction = confirmApplyResolver
	m.pendingStartUnit = svc.Unit
	m.Confirmation = models.ModelConfirmation(m.Colors)
	m.Confirmation.Message = fmt.Sprintf(
		"Resolve %s through %s?\n\n"+
			"%s is starting either way. This is only about whether "+
			"this machine's DNS queries are sent to it.\n\n"+
			"Declining leaves DNS as it is; you can switch the network over "+
			"later with the DNS key.\n",
		ssid, svc.Name, svc.Name)

	m.Overlay = updateOverlayModel(*m, &m.Confirmation)
}

// securityServiceByUnit finds a unit in the current pane data.
func (m NetpalaData) securityServiceByUnit(unit string) (common.SecurityService, bool) {
	for _, s := range m.SecurityData {
		if s.Unit == unit {
			return s, true
		}
	}
	return common.SecurityService{}, false
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
	network, _ := m.connectedNetwork()
	target := common.DNSTargetFromNetwork(network)
	m.DnsTarget = target
	m.pendingStopUnit = svc.Unit

	m.DnsForm = models.ModelDnsSelectFor(m.Colors, m.Config.KeyBindings,
		m.Config.DNS.DnscryptAddresses, target)

	if m.dnsIsOverridden() {
		// Changing the profile will not move resolv.conf, so promising a clean
		// hand-off would be a lie. Say what netpala can and cannot do.
		m.DnsForm.Notice = fmt.Sprintf(
			"%s answers DNS, but resolv.conf is set outside NetworkManager - "+
				"changing this profile will not move it. Stopping anyway will break lookups.",
			svc.Name)
	} else {
		m.DnsForm.Notice = fmt.Sprintf("Switching %s off - pick DNS for %s first", svc.Name, target.Label)
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

	// Read once: it is a file on disk that only changes with a NetworkManager
	// reload, and the picker needs it every time it opens.
	nmMacDefault := network.WifiMACDefault()

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

		NMMacDefault: nmMacDefault,

		PasswordForm: models.ModelPasswordInput(cfg.Colors),
		Form:         models.ModelWpaEapForm(cfg.Colors),
		DnsForm:      models.ModelDnsSelect(cfg.Colors, cfg.KeyBindings, cfg.DNS.DnscryptAddresses),
		MacForm:      models.ModelMacSelect(cfg.Colors, cfg.KeyBindings, nmMacDefault),
		VpnForm:      models.ModelVpnImport(cfg.Colors, cfg.KeyBindings),
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
		loadInitialData(m.Conn, m.Config.Security.Services, m.Config.VPN.Providers),
		dbus.RefreshTicker(),
		dbus.WaitForDBusSignal(m.Conn, m.DBusSignals),
	)
}

// Update forwards every message to the alert model before doing anything else,
// then runs the real logic.
//
// The alert fade is driven by a 100ms tick that the alert model re-arms only
// when it is handed one, so a branch that returns without forwarding stops the
// fade for good - and nothing ever restarts it. Every popup did exactly that,
// which meant the first picker a user opened left all later alerts stuck at
// their initial dim blend, unreadable against the background.
func (m NetpalaData) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updatedAlert, alertCmd := m.Alert.Update(msg)
	m.Alert = updatedAlert.(bubbleup.AlertModel)

	next, cmd := m.update(msg)
	if alertCmd == nil {
		return next, cmd
	}
	if cmd == nil {
		return next, alertCmd
	}
	return next, tea.Batch(cmd, alertCmd)
}

func (m NetpalaData) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

			action := m.confirmAction
			unit := m.pendingStartUnit
			m.pendingStartUnit = ""

			listen := dbus.WaitForDBusSignal(m.Conn, m.DBusSignals)

			// Declining this one is an answer rather than an abort: the unit
			// starts either way, and "no" only means DNS stays where it is.
			// Handled before the general cancel path for that reason.
			if action == confirmApplyResolver {
				svc, ok := m.securityServiceByUnit(unit)
				if !ok || svc.Active {
					return m, listen
				}
				if msg.Value {
					return m, tea.Batch(m.toggleSecurityWithResolverCmd(svc), listen)
				}
				return m, tea.Batch(m.toggleSecurityCmd(svc), listen)
			}

			if !msg.Value { // User cancelled
				return m, listen
			}

			switch action {
			case confirmStartService:
				// Re-read the unit rather than trusting what was on screen:
				// a refresh may have landed, or it may have been started from
				// systemctl while the prompt was up, in which case toggling
				// now would stop the very thing being consented to.
				svc, ok := m.securityServiceByUnit(unit)
				if !ok || svc.Active {
					return m, listen
				}
				// Consent covered starting it. If it also answers DNS, whether
				// this machine should resolve through it is a second question,
				// so it gets a second prompt rather than being folded into the
				// first one's "yes".
				if m.shouldAskApplyResolver(svc) {
					m.openApplyResolverConfirmation(svc)
					return m, listen
				}
				return m, tea.Batch(m.toggleSecurityCmd(svc), listen)

			case confirmDeleteVpn:
				target := m.VpnTarget
				m.VpnTarget = common.VpnConnection{}
				// Deleting a live VPN is allowed; NetworkManager tears the
				// tunnel down as part of removing the profile. The Wi-Fi
				// connection underneath it is untouched either way.
				return m, tea.Batch(dbus.DeleteConnectionCmd(m.Conn, target.Path), listen)

			default: // confirmDeleteNetwork
				// NOTE: Ensure m.SelectedNetwork holds the correct data before entering state 1
				deleteCmd := dbus.DeleteConnectionCmd(m.Conn, m.SelectedNetwork.Path)
				return m, tea.Batch(deleteCmd, listen)
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
			m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.KeyBindings, m.Config.DNS.DnscryptAddresses)

			// Backing out of "pick a replacement" means the resolver stays up.
			// Stopping it anyway is the exact outage this flow exists to avoid.
			m.pendingStopUnit = ""
			return m, nil

		case common.SubmitDnsMsg:
			m.PopupState = -1
			target := m.DnsTarget
			m.DnsForm = models.ModelDnsSelect(m.Colors, m.Config.KeyBindings, m.Config.DNS.DnscryptAddresses)

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

			// Only re-activate when this profile is the live connection. A VPN
			// is cycled through its own active connection rather than on a
			// device, so the two take different commands.
			var setDns tea.Cmd
			if target.IsVPN {
				setDns = dbus.SetVpnDnsCmd(m.Conn, target.Path, target.ActivePath,
					provider, v4, v6, target.Connected)
			} else {
				devicePath := godbus.ObjectPath("/")
				if target.Connected && len(m.DeviceData) > 0 {
					devicePath = m.DeviceData[0].Path
				}
				setDns = dbus.SetDnsCmd(m.Conn, target.Path, devicePath, provider, v4, v6, target.Connected)
			}

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
	case 4:
		// Handle the MAC address picker
		switch msg := msg.(type) {
		case common.ExitFormMsg:
			m.PopupState = -1
			m.MacForm = models.ModelMacSelect(m.Colors, m.Config.KeyBindings, m.NMMacDefault)
			return m, nil

		case common.SubmitMacMsg:
			m.PopupState = -1
			target := m.MacTarget
			m.MacForm = models.ModelMacSelect(m.Colors, m.Config.KeyBindings, m.NMMacDefault)

			// The address is chosen when the interface associates, so the
			// connection has to be rebuilt for a change to take effect.
			devicePath := godbus.ObjectPath("/")
			if target.Connected && len(m.DeviceData) > 0 {
				devicePath = m.DeviceData[0].Path
			}
			return m, dbus.SetMacCmd(m.Conn, target.Path, devicePath,
				msg.ModeID, msg.Explicit, target.Connected)

		default:
			var newMacForm tea.Model
			newMacForm, cmd = m.MacForm.Update(msg)
			m.MacForm = newMacForm.(models.MacSelect)
			return m, cmd
		}
	case 5:
		// Handle the WireGuard import popup
		switch msg := msg.(type) {
		case common.ExitFormMsg:
			m.PopupState = -1
			m.VpnForm = models.ModelVpnImport(m.Colors, m.Config.KeyBindings)
			return m, nil

		case common.SubmitVpnImportMsg:
			m.PopupState = -1
			m.VpnForm = models.ModelVpnImport(m.Colors, m.Config.KeyBindings)

			// The name is already taken by another profile. NetworkManager
			// would accept the duplicate and leave two identical-looking rows
			// fighting over one interface, so refuse it here where the reason
			// can still be explained.
			if m.vpnNameTaken(msg.ID) {
				return m, func() tea.Msg {
					return common.ErrMsg{Err: fmt.Errorf(
						"a VPN connection called %q already exists; rename the file or delete that profile first", msg.ID)}
				}
			}
			return m, dbus.AddWireGuardCmd(m.Conn, msg.Config, msg.ID, msg.Ifname)

		default:
			var newVpnForm tea.Model
			newVpnForm, cmd = m.VpnForm.Update(msg)
			m.VpnForm = newVpnForm.(models.VpnImport)
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
			// Vendor daemons are read from systemd and the kernel rather than
			// from NetworkManager, so no D-Bus signal announces their changes.
			// The tick is the only thing that notices.
			m.refreshVpnProvidersCmd(),
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
				selectedVpn := m.VpnData[m.SelectedEntry]
				if selectedVpn.Kind == common.VpnKindDaemon {
					return m, dbus.ToggleVpnProviderCmd(m.Conn, selectedVpn, !selectedVpn.Connected)
				}
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

				// Some services change what leaves the machine, which is not
				// visible from the pane once they are running. Starting one
				// asks first; stopping never does.
				if !selectedSvc.Active && selectedSvc.Confirm != "" {
					m.openStartConfirmation(selectedSvc)
					return m, nil
				}

				// Starting the local resolver can also move every DNS query on
				// the machine. That is a bigger thing than the keypress said,
				// so it is asked rather than assumed.
				if m.shouldAskApplyResolver(selectedSvc) {
					m.openApplyResolverConfirmation(selectedSvc)
					return m, nil
				}

				return m, m.toggleSecurityCmd(selectedSvc)
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
				m.confirmAction = confirmDeleteNetwork
				m.Confirmation = models.ModelConfirmation(m.Colors)
				m.Confirmation.Message = fmt.Sprintf("Are you sure you want to delete the known network '%s'?\n", m.SelectedNetwork.SSID)

				m.Overlay = updateOverlayModel(m, &m.Confirmation)
				return m, nil
			} else if m.selectedBox == common.PaneVPN && len(m.VpnData) > 0 {
				// A vendor daemon is not netpala's to delete: removing it
				// means uninstalling a package.
				target := m.VpnData[m.SelectedEntry]
				if target.Kind == common.VpnKindDaemon {
					return m, m.notForDaemonsCmd("delete", target)
				}
				m.openDeleteVpnConfirmation(target)
				return m, nil
			}
		}

		// Toggle Auto-Connect action (known networks and VPN profiles)
		if m.Config.KeyBindings.ToggleAutoConnect.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				selectedNetwork := m.KnownNetworks[m.SelectedEntry]
				return m, dbus.ToggleAutoConnectCmd(m.Conn, selectedNetwork.Path, selectedNetwork.AutoConnect)
			} else if m.selectedBox == common.PaneVPN && len(m.VpnData) > 0 {
				selectedVpn := m.VpnData[m.SelectedEntry]
				if selectedVpn.Kind == common.VpnKindDaemon {
					// Whether a vendor daemon reconnects on boot is its own
					// setting, kept in its own config; there is no
					// NetworkManager profile with an autoconnect flag.
					return m, m.notForDaemonsCmd("set auto-connect on", selectedVpn)
				}
				return m, dbus.ToggleVpnAutoConnectCmd(m.Conn, selectedVpn.Path, selectedVpn.AutoConnect)
			}
		}

		// Toggle Hidden action (only for known networks)
		if m.Config.KeyBindings.ToggleHidden.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				selectedNetwork := m.KnownNetworks[m.SelectedEntry]
				return m, dbus.ToggleHiddenCmd(m.Conn, selectedNetwork.Path, selectedNetwork.Hidden)
			}
		}

		// DNS provider switcher. A VPN profile carries its own resolvers, and
		// while the tunnel is up those are what the machine uses - so the
		// picker has to reach them too, not just the network underneath.
		if m.Config.KeyBindings.SetDns.Matches(keyStr) {
			var target common.DNSTarget
			switch {
			case m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0:
				target = common.DNSTargetFromNetwork(m.KnownNetworks[m.SelectedEntry])
			case m.selectedBox == common.PaneVPN && len(m.VpnData) > 0:
				selected := m.VpnData[m.SelectedEntry]
				if selected.Kind == common.VpnKindDaemon {
					// The provider sets its own resolvers from inside its
					// daemon. There is no profile here to rewrite.
					return m, m.notForDaemonsCmd("change DNS on", selected)
				}
				target = common.DNSTargetFromVpn(selected)
			default:
				return m, nil
			}
			m.DnsTarget = target

			m.DnsForm = models.ModelDnsSelectFor(m.Colors, m.Config.KeyBindings,
				m.Config.DNS.DnscryptAddresses, target)
			m.DnsForm.SelectProvider(target.Mode, target.Servers)

			// If the profile already uses DNSCrypt, check the proxy is up
			// before the user has a chance to re-apply it.
			probeCmd := m.DnsForm.ProbeCmdIfNeeded()

			m.PopupState = 3
			m.Overlay = updateOverlayModel(m, &m.DnsForm)
			return m, probeCmd
		}

		// Import a WireGuard config. Works from any pane on purpose: the VPN
		// pane hides itself while it is empty, which is exactly the situation
		// someone importing their first tunnel is in.
		if m.Config.KeyBindings.ImportVpn.Matches(keyStr) {
			m.VpnForm = models.ModelVpnImport(m.Colors, m.Config.KeyBindings)
			m.PopupState = 5
			m.Overlay = updateOverlayModel(m, &m.VpnForm)
			return m, m.VpnForm.Init()
		}

		// MAC address switcher (only for known networks)
		if m.Config.KeyBindings.SetMac.Matches(keyStr) {
			if m.selectedBox == common.PaneKnown && len(m.KnownNetworks) > 0 {
				m.MacTarget = m.KnownNetworks[m.SelectedEntry]

				m.MacForm = models.ModelMacSelect(m.Colors, m.Config.KeyBindings, m.NMMacDefault)
				m.MacForm.SSID = m.MacTarget.SSID
				m.MacForm.SelectMode(m.MacTarget.MACMode, m.MacTarget.MACAddress)

				m.PopupState = 4
				m.Overlay = updateOverlayModel(m, &m.MacForm)
				return m, nil
			}
		}
	}

	// The alert model is fed at the top of Update, so there is nothing to do
	// here but fall through.
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
	case 4:
		m.Overlay = updateOverlayModel(m, &m.MacForm)
		return m.Alert.Render(m.Overlay.View() + m.StatusBar.View())
	case 5:
		m.Overlay = updateOverlayModel(m, &m.VpnForm)
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
		m.VpnProfiles = msg
		m.rebuildVpnData()
		return m, nil, true

	case common.VpnProvidersUpdateMsg:
		m.VpnProviders = msg
		m.rebuildVpnData()
		return m, nil, true

	case common.RefreshVpnProvidersMsg:
		return m, m.refreshVpnProvidersCmd(), true

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

	case common.MacRevertedMsg:
		// Not an ErrMsg: nothing the user did was wrong, and the situation has
		// already been repaired. They still have to be told, because the
		// setting they chose is not the one now in effect.
		return m, m.Alert.NewAlertCmd(bubbleup.WarnKey, dbus.MacRevertedText(msg)), true

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
