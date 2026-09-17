package models

import (
	"netpala/common"
	"netpala/config"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TablesModel is a container model that holds all the main tables.
type TablesModel struct {
	SelectedBox    int
	SelectedEntry  int
	KnownHeight    int
	ScannedHeight  int
	VPNHeight      int
	SecurityHeight int
	DeviceHeight   int

	DeviceData      []common.Device
	VpnData         []common.VpnConnection
	SecurityData    []common.SecurityService
	KnownNetworks   []common.KnownNetwork
	ScannedNetworks []common.ScannedNetwork

	Colors config.Colors
}

func (m TablesModel) Init() tea.Cmd {
	return nil
}

// Titles carry what the ">" markers cannot.
//
// With a tunnel up, two rows are marked connected and nothing says which one
// the machine's traffic actually leaves by -- and "connected" is genuinely
// ambiguous for a VPN, since a split-tunnel profile is up and carrying almost
// nothing. NetworkManager already knows the answer, so the panes say it rather
// than leaving the user to infer it from a marker that means two things.
//
// titleSuffixBudget keeps a long profile name from pushing the title past the
// terminal width, which would leave the box with no rule at all.
const titleSuffixBudget = 28

// exitVPN is the tunnel holding the default route, if one does.
func (m TablesModel) exitVPN() (common.VpnConnection, bool) {
	for _, v := range m.VpnData {
		if v.IsDefaultRoute {
			return v, true
		}
	}
	return common.VpnConnection{}, false
}

// connectedKnowableVPNs counts the tunnels that are up and whose routing
// netpala can actually inspect.
//
// A vendor daemon is excluded on purpose. Its tunnel may well be carrying
// everything -- Mullvad's normally is -- but netpala cannot tell, because
// these providers route with a firewall mark and a policy rule rather than
// through NetworkManager. Counting one here would put "not carrying your
// traffic" on screen over a tunnel that is carrying all of it, which is a
// worse failure than saying nothing.
func (m TablesModel) connectedKnowableVPNs() int {
	n := 0
	for _, v := range m.VpnData {
		if v.Connected && v.KnowsWhatItCarries() {
			n++
		}
	}
	return n
}

// knownTitle says when the network is carrying a tunnel rather than being the
// way out itself.
func (m TablesModel) knownTitle() string {
	exit, ok := m.exitVPN()
	if !ok {
		return "Known Networks"
	}
	return "Known Networks - carrying " + truncateForTitle(exit.Name)
}

// vpnTitle flags the case that looks like success and is not: a tunnel that is
// connected while the machine's traffic still leaves around it.
func (m TablesModel) vpnTitle() string {
	const base = "Virtual Private Networks"
	if _, ok := m.exitVPN(); ok {
		return base
	}
	if m.connectedKnowableVPNs() == 0 {
		return base
	}
	return base + " - connected, but not carrying your traffic"
}

func truncateForTitle(s string) string {
	if len(s) <= titleSuffixBudget {
		return s
	}
	return s[:titleSuffixBudget-3] + "..."
}

func (m TablesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// This model is for viewing only; all updates are handled by NetpalaData
	return m, nil
}

// View renders all tables in order. VPN and Security collapse to nothing when
// they have no rows, matching which panes navigation will visit.
func (m TablesModel) View() string {
	knownNetsTable := TableModel(m.knownTitle(), m.SelectedBox == common.PaneKnown, m.SelectedEntry, m.KnownHeight, m.KnownNetworks, nil, nil, nil, nil, m.Colors)
	scannedNetsTable := TableModel("New Networks", m.SelectedBox == common.PaneScanned, m.SelectedEntry, m.ScannedHeight, nil, m.ScannedNetworks, nil, nil, nil, m.Colors)
	vpnTableModel := TableModel(m.vpnTitle(), m.SelectedBox == common.PaneVPN, m.SelectedEntry, m.VPNHeight, nil, nil, m.VpnData, nil, nil, m.Colors)
	securityTable := TableModel("Security", m.SelectedBox == common.PaneSecurity, m.SelectedEntry, m.SecurityHeight, nil, nil, nil, m.SecurityData, nil, m.Colors)
	deviceTable := TableModel("Device", m.SelectedBox == common.PaneDevice, m.SelectedEntry, m.DeviceHeight, nil, nil, nil, nil, m.DeviceData, m.Colors)

	vpnView := vpnTableModel.View()
	if len(m.VpnData) == 0 {
		vpnView = ""
	}

	securityView := securityTable.View()
	if len(m.SecurityData) == 0 {
		securityView = ""
	}

	// VPN directly under Known Networks: both are lists of saved profiles you
	// connect to, and the VPN columns are laid out against the grid above
	// them. New Networks is for discovery and follows.
	return strings.Join([]string{
		knownNetsTable.View(),
		vpnView,
		scannedNetsTable.View(),
		securityView,
		deviceTable.View(),
	}, "")
}
