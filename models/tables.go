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

func (m TablesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// This model is for viewing only; all updates are handled by NetpalaData
	return m, nil
}

// View renders all tables in order. VPN and Security collapse to nothing when
// they have no rows, matching which panes navigation will visit.
func (m TablesModel) View() string {
	knownNetsTable := TableModel("Known Networks", m.SelectedBox == common.PaneKnown, m.SelectedEntry, m.KnownHeight, m.KnownNetworks, nil, nil, nil, nil, m.Colors)
	scannedNetsTable := TableModel("New Networks", m.SelectedBox == common.PaneScanned, m.SelectedEntry, m.ScannedHeight, nil, m.ScannedNetworks, nil, nil, nil, m.Colors)
	vpnTableModel := TableModel("Virtual Private Networks", m.SelectedBox == common.PaneVPN, m.SelectedEntry, m.VPNHeight, nil, nil, m.VpnData, nil, nil, m.Colors)
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

	return strings.Join([]string{
		knownNetsTable.View(),
		scannedNetsTable.View(),
		vpnView,
		securityView,
		deviceTable.View(),
	}, "")
}
