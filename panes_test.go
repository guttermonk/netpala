package main

import (
	"netpala/common"
	"netpala/config"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.dalton.dog/bubbleup"
)

func withData(vpns, security, known, scanned, devices int) NetpalaData {
	m := NetpalaData{}
	m.VpnData = make([]common.VpnConnection, vpns)
	m.SecurityData = make([]common.SecurityService, security)
	m.KnownNetworks = make([]common.KnownNetwork, known)
	m.ScannedNetworks = make([]common.ScannedNetwork, scanned)
	m.DeviceData = make([]common.Device, devices)
	return m
}

func TestVisiblePanesHidesEmptyOnes(t *testing.T) {
	tests := []struct {
		name           string
		vpns, security int
		want           []int
	}{
		{
			name: "neither vpn nor security",
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneDevice},
		},
		{
			name: "vpn only", vpns: 2,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneVPN, common.PaneDevice},
		},
		{
			name: "security only", security: 2,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneSecurity, common.PaneDevice},
		},
		{
			name: "both", vpns: 1, security: 1,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneVPN, common.PaneSecurity, common.PaneDevice},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := withData(tt.vpns, tt.security, 3, 3, 1)
			if got := m.visiblePanes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("visiblePanes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The old navigation hardcoded four panes and patched around an empty VPN
// list. Cycling must now land on every visible pane and no hidden one.
func TestStepPaneCyclesOnlyVisible(t *testing.T) {
	m := withData(0, 2, 3, 3, 1) // security shown, vpn hidden
	m.selectedBox = common.PaneKnown

	var seen []int
	for range 4 {
		m.stepPane(1)
		seen = append(seen, m.selectedBox)
	}

	want := []int{common.PaneScanned, common.PaneSecurity, common.PaneDevice, common.PaneKnown}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("forward cycle = %v, want %v", seen, want)
	}

	seen = nil
	for range 4 {
		m.stepPane(-1)
		seen = append(seen, m.selectedBox)
	}
	want = []int{common.PaneDevice, common.PaneSecurity, common.PaneScanned, common.PaneKnown}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("backward cycle = %v, want %v", seen, want)
	}
}

func TestStepPaneResetsEntry(t *testing.T) {
	m := withData(0, 2, 5, 5, 1)
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 4

	m.stepPane(1)
	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d after changing pane, want 0", m.SelectedEntry)
	}
}

// A pane that disappears while selected must not strand the cursor there,
// or Select would index into an empty slice.
func TestClampSelectionLeavesHiddenPane(t *testing.T) {
	m := withData(0, 2, 3, 3, 1)
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 1

	m.SecurityData = nil // unit vanished between refreshes
	m.clampSelection()

	if m.selectedBox != common.PaneKnown || m.SelectedEntry != 0 {
		t.Errorf("selection = pane %d entry %d, want pane %d entry 0",
			m.selectedBox, m.SelectedEntry, common.PaneKnown)
	}
}

func TestClampSelectionShrinksEntry(t *testing.T) {
	m := withData(0, 3, 3, 3, 1)
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 2

	m.SecurityData = make([]common.SecurityService, 1)
	m.clampSelection()

	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d after the pane shrank to 1 row, want 0", m.SelectedEntry)
	}
	if m.selectedBox != common.PaneSecurity {
		t.Error("should have stayed on the Security pane")
	}
}

func TestClampSelectionHandlesEmptyPane(t *testing.T) {
	m := withData(0, 1, 0, 0, 1)
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 3

	m.clampSelection()
	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d for an empty pane, want 0", m.SelectedEntry)
	}
}

func TestPaneEntryCount(t *testing.T) {
	m := withData(1, 2, 3, 4, 5)
	for pane, want := range map[int]int{
		common.PaneKnown:    3,
		common.PaneScanned:  4,
		common.PaneVPN:      1,
		common.PaneSecurity: 2,
		common.PaneDevice:   5,
	} {
		if got := m.paneEntryCount(pane); got != want {
			t.Errorf("paneEntryCount(%d) = %d, want %d", pane, got, want)
		}
	}
}

func dnsService(active, providesDNS bool) common.SecurityService {
	return common.SecurityService{
		Name: "DNSCrypt", Unit: "dnscrypt-proxy2.service",
		Active: active, ProvidesDNS: providesDNS,
	}
}

func TestSecurityStopWarningOnlyWhenDependedOn(t *testing.T) {
	tests := []struct {
		name     string
		svc      common.SecurityService
		known    []common.KnownNetwork
		wantWarn bool
	}{
		{
			name: "stopping a resolver networks use warns",
			svc:  dnsService(true, true),
			known: []common.KnownNetwork{
				{SSID: "home", DNSMode: common.DNSModeDNSCrypt},
			},
			wantWarn: true,
		},
		{
			name:  "no network uses loopback DNS",
			svc:   dnsService(true, true),
			known: []common.KnownNetwork{{SSID: "home", DNSMode: common.DNSModeDHCP}},
		},
		{
			name: "unit does not provide DNS",
			svc:  dnsService(true, false),
			known: []common.KnownNetwork{
				{SSID: "home", DNSMode: common.DNSModeDNSCrypt},
			},
		},
		{
			name: "starting a stopped unit never warns",
			svc:  dnsService(false, true),
			known: []common.KnownNetwork{
				{SSID: "home", DNSMode: common.DNSModeDNSCrypt},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NetpalaData{KnownNetworks: tt.known}
			got := m.securityStopWarning(tt.svc)
			if (got != "") != tt.wantWarn {
				t.Errorf("warning = %q, wantWarn = %v", got, tt.wantWarn)
			}
		})
	}
}

// The live connection is the one that breaks immediately, so it must be named
// first and flagged, not buried among saved profiles.
func TestSecurityStopWarningNamesConnectedFirst(t *testing.T) {
	m := NetpalaData{KnownNetworks: []common.KnownNetwork{
		{SSID: "cafe", DNSMode: common.DNSModeDNSCrypt},
		{SSID: "home", DNSMode: common.DNSModeDNSCrypt, Connected: true},
	}}

	got := m.securityStopWarning(dnsService(true, true))
	if !strings.Contains(got, "home (connected)") {
		t.Errorf("connected network not flagged: %q", got)
	}
	if strings.Index(got, "home") > strings.Index(got, "cafe") {
		t.Errorf("connected network should be named first: %q", got)
	}
}

func TestSecurityStopWarningTruncatesLongLists(t *testing.T) {
	var known []common.KnownNetwork
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		known = append(known, common.KnownNetwork{SSID: s, DNSMode: common.DNSModeDNSCrypt})
	}
	m := NetpalaData{KnownNetworks: known}

	// Checked as one span: single letters like "e" appear all over the
	// surrounding prose, so substring checks on them prove nothing.
	got := m.securityStopWarning(dnsService(true, true))
	if !strings.Contains(got, "for a, b, c and 2 more -") {
		t.Errorf("expected the first three names then a count, got: %q", got)
	}
}

func TestNetworksDependingOnIgnoresNonLoopbackProfiles(t *testing.T) {
	m := NetpalaData{KnownNetworks: []common.KnownNetwork{
		{SSID: "cloudflare-net", DNSMode: common.DNSModeCloudflare},
		{SSID: "custom-net", DNSMode: common.DNSModeCustom},
		{SSID: "dhcp-net", DNSMode: common.DNSModeDHCP},
		{SSID: "local-net", DNSMode: common.DNSModeDNSCrypt},
	}}

	got := m.networksDependingOn(dnsService(true, true))
	if !reflect.DeepEqual(got, []string{"local-net"}) {
		t.Errorf("dependants = %v, want [local-net]", got)
	}
}

// selectKey drives the real Update loop. The returned command is deliberately
// not executed: the toggle path would call out to D-Bus.
func selectKey(t *testing.T, m NetpalaData) NetpalaData {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(NetpalaData)
}

func securityModel(svc common.SecurityService, known []common.KnownNetwork) NetpalaData {
	cfg := config.DefaultConfig()
	alert := bubbleup.NewAlertModel(40, true, 10)
	return NetpalaData{
		Config: &cfg, KeyMap: config.NewAppKeyMap(&cfg), Colors: cfg.Colors,
		Alert:      *alert,
		PopupState: -1,
		// Two devices so the Down key has somewhere to go in the Security pane.
		SecurityData:  []common.SecurityService{svc, {Name: "Other", Unit: "other.service"}},
		KnownNetworks: known,
		selectedBox:   common.PaneSecurity,
	}
}

func TestSecurityToggleNeedsTwoPressesWhenDependedOn(t *testing.T) {
	m := securityModel(
		dnsService(true, true),
		[]common.KnownNetwork{{SSID: "home", DNSMode: common.DNSModeDNSCrypt, Connected: true}},
	)

	m = selectKey(t, m)
	if m.securityArmedUnit != "dnscrypt-proxy2.service" {
		t.Fatalf("first press should arm the unit, got %q", m.securityArmedUnit)
	}

	m = selectKey(t, m)
	if m.securityArmedUnit != "" {
		t.Errorf("second press should disarm and toggle, got %q", m.securityArmedUnit)
	}
}

func TestSecurityToggleImmediateWhenNothingDependsOnIt(t *testing.T) {
	m := securityModel(
		dnsService(true, true),
		[]common.KnownNetwork{{SSID: "home", DNSMode: common.DNSModeDHCP}},
	)

	if m = selectKey(t, m); m.securityArmedUnit != "" {
		t.Errorf("no dependants means no confirmation, got %q", m.securityArmedUnit)
	}
}

// A confirmation given for one row must not be spendable on another.
func TestSecurityArmClearedByNavigation(t *testing.T) {
	known := []common.KnownNetwork{{SSID: "home", DNSMode: common.DNSModeDNSCrypt}}

	for _, nav := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"down", tea.KeyMsg{Type: tea.KeyDown}},
		{"up", tea.KeyMsg{Type: tea.KeyUp}},
		{"next pane", tea.KeyMsg{Type: tea.KeyTab}},
	} {
		t.Run(nav.name, func(t *testing.T) {
			m := securityModel(dnsService(true, true), known)
			m = selectKey(t, m)
			if m.securityArmedUnit == "" {
				t.Fatal("precondition: should be armed")
			}

			next, _ := m.Update(nav.key)
			if got := next.(NetpalaData).securityArmedUnit; got != "" {
				t.Errorf("%s should disarm, got %q", nav.name, got)
			}
		})
	}
}

// A refresh arriving between presses invalidates the confirmation too.
func TestSecurityArmClearedByRefresh(t *testing.T) {
	m := securityModel(
		dnsService(true, true),
		[]common.KnownNetwork{{SSID: "home", DNSMode: common.DNSModeDNSCrypt}},
	)
	m = selectKey(t, m)

	next, _ := m.Update(common.SecurityUpdateMsg(m.SecurityData))
	if got := next.(NetpalaData).securityArmedUnit; got != "" {
		t.Errorf("refresh should disarm, got %q", got)
	}
}
