package main

import (
	"netpala/common"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func daemon(name string, connected bool) common.VpnConnection {
	return common.VpnConnection{
		Kind:       common.VpnKindDaemon,
		Name:       name,
		ConnType:   "Daemon",
		Unit:       name + "-daemon.service",
		Iface:      "wg0-" + name,
		Connect:    []string{name, "connect"},
		Disconnect: []string{name, "disconnect"},
		Connected:  connected,
	}
}

// providerModel puts the cursor on a vendor-daemon row.
func providerModel(rows ...common.VpnConnection) NetpalaData {
	m := linkModel(false, nil)
	m.VpnProfiles = nil
	m.VpnProviders = rows
	m.rebuildVpnData()
	m.selectedBox = common.PaneVPN
	m.SelectedEntry = 0
	return safeListener(m)
}

// Profiles first, then daemons, so the rows netpala can manage sit under the
// cursor by default and the order does not shuffle as daemons come and go.
func TestVpnPaneListsProfilesThenDaemons(t *testing.T) {
	m := linkModel(false, nil)
	m.VpnProfiles = []common.VpnConnection{vpn("mullvad-se", false)}
	m.VpnProviders = []common.VpnConnection{daemon("mullvad", true)}
	m.rebuildVpnData()

	if len(m.VpnData) != 2 {
		t.Fatalf("%d rows, want both sources", len(m.VpnData))
	}
	if m.VpnData[0].Kind != common.VpnKindProfile || m.VpnData[1].Kind != common.VpnKindDaemon {
		t.Errorf("rows out of order: %v, %v", m.VpnData[0].Kind, m.VpnData[1].Kind)
	}
}

// Each half refreshes on its own trigger, so an update to one must not drop
// the other.
func TestRefreshingOneSourceKeepsTheOther(t *testing.T) {
	m := linkModel(false, nil)
	m.VpnProfiles = []common.VpnConnection{vpn("mullvad-se", false)}
	m.VpnProviders = []common.VpnConnection{daemon("mullvad", true)}
	m.rebuildVpnData()
	m = safeListener(m)

	// A NetworkManager refresh lands.
	next, _, _ := m.handleDataMsg(common.VpnUpdateMsg{vpn("work", false)})
	if len(next.VpnData) != 2 {
		t.Errorf("a profile refresh dropped the daemon row: %d rows", len(next.VpnData))
	}

	// And a provider refresh.
	next, _, _ = next.handleDataMsg(common.VpnProvidersUpdateMsg(nil))
	if len(next.VpnData) != 1 {
		t.Errorf("a provider refresh left stale rows: %d", len(next.VpnData))
	}
}

func TestSelectTogglesADaemonRow(t *testing.T) {
	m := providerModel(daemon("mullvad", false))

	_, cmd := m.Update(selectKey())
	if cmd == nil {
		t.Error("select did nothing on a vendor-daemon row")
	}
}

// Deleting one would mean uninstalling a package, which is not netpala's to
// do. Saying so beats a keypress that silently does nothing.
func TestRemoveOnADaemonRowExplainsItself(t *testing.T) {
	m := providerModel(daemon("mullvad", true))

	next, cmd := m.Update(removeKey())
	got := next.(NetpalaData)

	if got.PopupState == 1 {
		t.Error("opened a delete confirmation for a vendor daemon")
	}
	if cmd == nil {
		t.Error("remove gave no feedback at all on a daemon row")
	}
}

// Auto-connect and DNS live in the provider's own configuration; there is no
// NetworkManager profile behind the row to change.
func TestProfileOnlyKeysAreRefusedOnDaemonRows(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{
		"auto-connect": autoKey(),
		"dns":          dnsKey(),
	} {
		m := providerModel(daemon("mullvad", true))

		next, cmd := m.Update(key)
		got := next.(NetpalaData)

		if got.PopupState == 3 {
			t.Errorf("%s: opened the DNS picker on a daemon row", name)
		}
		if cmd == nil {
			t.Errorf("%s: no feedback on a daemon row", name)
		}
	}
}

// A profile row in the same pane must keep working.
func TestProfileRowsStillAcceptProfileKeys(t *testing.T) {
	m := linkModel(false, nil)
	m.VpnProfiles = []common.VpnConnection{vpn("mullvad-se", false)}
	m.VpnProviders = []common.VpnConnection{daemon("mullvad", true)}
	m.rebuildVpnData()
	m.selectedBox = common.PaneVPN
	m.SelectedEntry = 0 // the profile
	m = safeListener(m)

	next, _ := m.Update(removeKey())
	if got := next.(NetpalaData); got.PopupState != 1 {
		t.Errorf("PopupState = %d, want the delete confirmation for a profile row", got.PopupState)
	}
}

// netpala cannot see how a vendor daemon routes -- these providers use a
// firewall mark and a policy rule rather than NetworkManager. Reporting "not
// carrying your traffic" over a tunnel that is carrying all of it would be
// worse than saying nothing.
func TestDaemonRowsMakeNoClaimAboutRouting(t *testing.T) {
	d := daemon("mullvad", true)
	if d.KnowsWhatItCarries() {
		t.Error("a vendor daemon claimed netpala can see what it carries")
	}
	if !vpn("mullvad-se", true).KnowsWhatItCarries() {
		t.Error("a NetworkManager profile should be inspectable")
	}
}

// The table must not assert values netpala has not read.
func TestDaemonRowsShowUnknownRatherThanNone(t *testing.T) {
	defer common.SetWindowSizeForTest(0, 0)
	common.SetWindowSizeForTest(120, 40)

	rows := common.FormatVpnData([]common.VpnConnection{daemon("mullvad", true)})
	row := rows[2]

	for i, col := range map[int]string{3: "Endpoint", 4: "DNS", 5: "Auto"} {
		if got := strings.TrimSpace(row[i]); got != "-" {
			t.Errorf("%s cell = %q, want \"-\" for a daemon row", col, got)
		}
	}
	// "None" in the DNS column would be an outright lie: a provider almost
	// always pins its own resolvers.
	if strings.Contains(strings.Join(row, " "), "None") {
		t.Error("a daemon row claimed it sets no DNS")
	}
}
