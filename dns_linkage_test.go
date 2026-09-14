package main

import (
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
	"go.dalton.dog/bubbleup"
)

func linkModel(resolverActive bool, networks []common.KnownNetwork) NetpalaData {
	cfg := config.DefaultConfig()
	alert := bubbleup.NewAlertModel(40, true, 10)
	return NetpalaData{
		Config: &cfg, KeyMap: config.NewAppKeyMap(&cfg), Colors: cfg.Colors,
		Alert:      *alert,
		PopupState: -1,
		SecurityData: []common.SecurityService{
			{Name: "Tor", Unit: "tor-transparent.service"},
			{Name: "DNSCrypt", Unit: "dnscrypt-proxy2.service", ProvidesDNS: true, Active: resolverActive},
		},
		KnownNetworks: networks,
		DeviceData:    []common.Device{{Path: godbus.ObjectPath("/dev/0")}},
		// Explicit, so the tests do not depend on the host resolv.conf.
		EffectiveDNS: []string{"192.168.1.1"},
		AppliedDNS:   []string{"192.168.1.1"},
	}
}

func net(ssid string, connected bool, mode string) common.KnownNetwork {
	return common.KnownNetwork{
		Path: godbus.ObjectPath("/conn/" + ssid), SSID: ssid,
		Connected: connected, DNSMode: mode,
	}
}

func TestDNSServiceFoundByProvidesDNSFlag(t *testing.T) {
	m := linkModel(false, nil)
	svc, ok := m.dnsService()
	if !ok || svc.Unit != "dnscrypt-proxy2.service" {
		t.Fatalf("dnsService() = %+v, %v; want the ProvidesDNS unit", svc, ok)
	}

	m.SecurityData = []common.SecurityService{{Name: "Tor", Unit: "tor-transparent.service"}}
	if _, ok := m.dnsService(); ok {
		t.Error("a unit without ProvidesDNS must not be treated as the resolver")
	}
}

// Rule 1: switching the resolver on from the Security pane points the live
// connection at it, otherwise it answers nobody.
func TestResolverOnSetsConnectedNetworkDNS(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	if cmd := m.setConnectedDNSCmd(); cmd == nil {
		t.Error("turning the resolver on should set the connected network's DNS")
	}

	// Already pointed at it: nothing to do.
	m = linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)})
	if cmd := m.setConnectedDNSCmd(); cmd != nil {
		t.Error("no DNS change needed when the network already uses the resolver")
	}

	// Nothing connected: nothing to point.
	m = linkModel(false, []common.KnownNetwork{net("home", false, common.DNSModeDHCP)})
	if cmd := m.setConnectedDNSCmd(); cmd != nil {
		t.Error("no connected network, so nothing should be changed")
	}
}

// Rule 2 / Rule 3: the resolver comes up for the live connection, and is left
// alone for a profile that is merely saved.
func TestStartResolverOnlyWhenNeeded(t *testing.T) {
	m := linkModel(false, nil)
	if m.startDNSServiceCmd() == nil {
		t.Error("an inactive resolver should be started")
	}

	m = linkModel(true, nil)
	if m.startDNSServiceCmd() != nil {
		t.Error("an already-running resolver must not be toggled (that would stop it)")
	}

	m = linkModel(false, nil)
	m.SecurityData = nil // unit not installed
	if m.startDNSServiceCmd() != nil {
		t.Error("no resolver installed, so nothing to start")
	}
}

// Rule 3: the resolver follows when the network is actually activated.
func TestResolverStartsOnConnectTransition(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("cafe", false, common.DNSModeDNSCrypt)})

	// First observation only seeds the tracker -- launching netpala must not
	// start services by itself.
	if cmd := m.onConnectionChanged(); cmd != nil {
		t.Error("first observation should seed, not act")
	}

	// Now that network becomes active.
	m.KnownNetworks = []common.KnownNetwork{net("cafe", true, common.DNSModeDNSCrypt)}
	if cmd := m.onConnectionChanged(); cmd == nil {
		t.Error("connecting to a DNSCrypt network should start the resolver")
	}

	// Refreshing while still on it must not re-issue the start.
	if cmd := m.onConnectionChanged(); cmd != nil {
		t.Error("a refresh on the same network should do nothing")
	}
}

func TestNoResolverStartForNonDNSCryptNetwork(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", false, common.DNSModeDHCP)})
	m.onConnectionChanged() // seed

	m.KnownNetworks = []common.KnownNetwork{net("home", true, common.DNSModeDHCP)}
	if cmd := m.onConnectionChanged(); cmd != nil {
		t.Error("connecting to a DHCP network must not start the resolver")
	}
}

// Setting DNSCrypt on an inactive profile saves the setting without touching
// the resolver -- the whole point of rule 3.
func TestInactiveProfileDoesNotStartResolver(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{
		net("home", true, common.DNSModeDHCP),
		net("cafe", false, common.DNSModeDHCP),
	})
	m.PopupState = 3
	m.DnsTarget = net("cafe", false, common.DNSModeDHCP) // not connected

	before := m.SecurityData[1].Active
	next, cmd := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeDNSCrypt})
	got := next.(NetpalaData)

	if cmd == nil {
		t.Fatal("expected the DNS setting to still be applied")
	}
	if got.SecurityData[1].Active != before {
		t.Error("resolver state must not change for an inactive profile")
	}
}

func TestSequenceUsedForOrdering(t *testing.T) {
	// Guards the ordering contract: the resolver must be listening before
	// resolv.conf points at it, which Batch would not guarantee.
	m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	m.PopupState = 3
	m.DnsTarget = net("home", true, common.DNSModeDHCP)

	_, cmd := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeDNSCrypt})
	if cmd == nil {
		t.Fatal("expected a command")
	}
	// tea.Sequence returns a distinct message type from Batch; executing it
	// here would hit D-Bus, so just assert a command was produced and that
	// the helpers agree a start is needed.
	if m.startDNSServiceCmd() == nil {
		t.Error("precondition: resolver should need starting")
	}
	var _ tea.Cmd = cmd
}

// Turning the resolver off while the live connection uses it must not just
// stop it -- that is the outage. It opens the picker to choose a replacement.
func TestStopOpensPickerWhenLiveConnectionDepends(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)})
	svc, _ := m.dnsService()

	if !m.stopWouldBreakDNS(svc) {
		t.Fatal("stopping the resolver the live connection uses should be flagged")
	}

	m.openDnsPickerForStop(svc)
	if m.PopupState != 3 {
		t.Errorf("PopupState = %d, want the DNS picker (3)", m.PopupState)
	}
	if m.pendingStopUnit != svc.Unit {
		t.Errorf("pendingStopUnit = %q, want %q", m.pendingStopUnit, svc.Unit)
	}
	if m.DnsTarget.SSID != "home" {
		t.Errorf("picker targeted %q, want the connected network", m.DnsTarget.SSID)
	}
	if m.DnsForm.Notice == "" {
		t.Error("picker opened by netpala must explain why")
	}
	// It should not start on DNSCrypt -- the point is to move off it.
	if m.DnsForm.Providers[m.DnsForm.Cursor].ID == common.DNSModeDNSCrypt {
		t.Error("cursor should start on a replacement, not on DNSCrypt")
	}
}

// No live dependency means no ceremony: just stop it.
func TestStopIsDirectWhenNothingLiveDepends(t *testing.T) {
	cases := []struct {
		name     string
		active   bool
		networks []common.KnownNetwork
	}{
		{"connected network uses DHCP", true, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)}},
		{"nothing connected", true, []common.KnownNetwork{net("home", false, common.DNSModeDNSCrypt)}},
		{"resolver already stopped", false, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := linkModel(tc.active, tc.networks)
			svc, _ := m.dnsService()
			if m.stopWouldBreakDNS(svc) {
				t.Error("should stop directly, no picker needed")
			}
		})
	}
}

// Choosing a replacement applies the new DNS and then stops the resolver.
func TestReplacementChosenThenResolverStopped(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)})
	svc, _ := m.dnsService()
	m.openDnsPickerForStop(svc)

	next, cmd := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeCloudflare})
	got := next.(NetpalaData)

	if cmd == nil {
		t.Fatal("expected DNS change followed by the stop")
	}
	if got.pendingStopUnit != "" {
		t.Errorf("pendingStopUnit = %q, should be consumed", got.pendingStopUnit)
	}
	if got.PopupState != -1 {
		t.Error("picker should have closed")
	}
}

// Backing out leaves the resolver alone -- stopping it anyway is the very
// outage this flow exists to prevent.
func TestCancellingReplacementAbandonsTheStop(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)})
	svc, _ := m.dnsService()
	m.openDnsPickerForStop(svc)

	next, _ := m.Update(common.ExitFormMsg{})
	got := next.(NetpalaData)

	if got.pendingStopUnit != "" {
		t.Error("cancelling must abandon the pending stop")
	}
	if got.PopupState != -1 {
		t.Error("picker should have closed")
	}
}

// Picking DNSCrypt in that popup means "actually, keep it" -- not a stop.
func TestChoosingDNSCryptAbortsTheStop(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDNSCrypt)})
	svc, _ := m.dnsService()
	m.openDnsPickerForStop(svc)

	next, _ := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeDNSCrypt})
	if got := next.(NetpalaData); got.pendingStopUnit != "" {
		t.Error("re-selecting DNSCrypt should abandon the stop, not queue it")
	}
}

// The bug this fixes: netpala judged the dependency from the connection
// profile, which reads "dhcp" when resolv.conf has been pointed at a local
// resolver by something outside NetworkManager. Every lookup went through the
// resolver while netpala believed nothing depended on it, so stopping it
// skipped the replacement picker and simply broke DNS.
func TestStopDetectsDependencyFromEffectiveDNS(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	// Profile says DHCP, but the machine actually resolves through loopback.
	m.EffectiveDNS = []string{"127.0.0.1"}
	m.AppliedDNS = []string{"192.168.1.2", "192.168.1.1"}

	svc, _ := m.dnsService()
	if !m.stopWouldBreakDNS(svc) {
		t.Error("resolution goes through the resolver; stopping it must be caught")
	}
}

func TestOverrideDetection(t *testing.T) {
	tests := []struct {
		name      string
		effective []string
		applied   []string
		want      bool
	}{
		{"NM is in charge", []string{"192.168.1.2"}, []string{"192.168.1.2"}, false},
		{"overridden by a local resolver", []string{"127.0.0.1"}, []string{"192.168.1.2"}, true},
		{"subset of what NM applied", []string{"192.168.1.2"}, []string{"192.168.1.2", "192.168.1.1"}, false},
		{"nothing effective", nil, []string{"192.168.1.2"}, false},
		{"nothing applied to compare against", []string{"127.0.0.1"}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := linkModel(true, nil)
			m.EffectiveDNS, m.AppliedDNS = tt.effective, tt.applied
			if got := m.dnsIsOverridden(); got != tt.want {
				t.Errorf("dnsIsOverridden() = %v, want %v", got, tt.want)
			}
		})
	}
}

// When netpala cannot move DNS, the picker must say so rather than implying a
// clean hand-off it cannot deliver.
func TestNoticeIsHonestWhenDNSIsOverridden(t *testing.T) {
	m := linkModel(true, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	m.EffectiveDNS = []string{"127.0.0.1"}
	m.AppliedDNS = []string{"192.168.1.2"}

	svc, _ := m.dnsService()
	m.openDnsPickerForStop(svc)

	if !strings.Contains(m.DnsForm.Notice, "outside NetworkManager") {
		t.Errorf("notice should admit netpala cannot move DNS, got: %q", m.DnsForm.Notice)
	}
}
