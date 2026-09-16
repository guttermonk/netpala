package main

import (
	"netpala/common"
	"netpala/models"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

func dnsKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}}
}

// plainText drops the styling so a rendered popup can be searched for the text
// it is supposed to be showing.
func plainText(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "m")
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

func vpnWithDNS(name string, connected bool, mode string, servers []string) common.VpnConnection {
	v := vpn(name, connected)
	v.DNSMode = mode
	v.DNSServers = servers
	if connected {
		v.ActivePath = godbus.ObjectPath("/active/" + name)
	}
	return v
}

// A tunnel carries its own resolvers, and while it is up those are what the
// machine uses. The picker has to reach them, not just the network underneath.
func TestDnsPickerOpensOnTheVpnPane(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", true, common.DNSModeCustom, []string{"10.64.0.1"}))

	next, _ := m.Update(dnsKey())
	got := next.(NetpalaData)

	if got.PopupState != 3 {
		t.Fatalf("PopupState = %d, want the DNS picker", got.PopupState)
	}
	if !got.DnsTarget.IsVPN {
		t.Error("the picker did not record that it is acting on a VPN")
	}
	if got.DnsTarget.Path != godbus.ObjectPath("/vpn/mullvad-se") {
		t.Errorf("DnsTarget.Path = %q", got.DnsTarget.Path)
	}
	if got.DnsTarget.Label != "mullvad-se" {
		t.Errorf("DnsTarget.Label = %q, want the profile name", got.DnsTarget.Label)
	}
	if !strings.Contains(plainText(got.DnsForm.View()), "mullvad-se") {
		t.Error("the picker does not name what it is acting on")
	}
}

// The live activation is what has to be cycled for a DNS change to take
// effect, so it has to be carried on the target.
func TestVpnTargetCarriesItsActivation(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", true, common.DNSModeDHCP, nil))

	next, _ := m.Update(dnsKey())
	if got := next.(NetpalaData); got.DnsTarget.ActivePath != godbus.ObjectPath("/active/mullvad-se") {
		t.Errorf("ActivePath = %q, want the live activation", got.DnsTarget.ActivePath)
	}
}

// "DHCP" is meaningless inside a tunnel -- nothing hands out resolvers there.
// The row means "none of its own", and has to say so.
func TestVpnPickerRewordsTheEmptyOption(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", false, common.DNSModeDHCP, nil))

	next, _ := m.Update(dnsKey())
	view := plainText(next.(NetpalaData).DnsForm.View())

	if strings.Contains(view, "DHCP") {
		t.Errorf("the VPN picker offers DHCP, which a tunnel does not have:\n%s", view)
	}
	if !strings.Contains(view, "None") {
		t.Errorf("no replacement for the empty option:\n%s", view)
	}
}

// The wireless picker must keep its own wording.
func TestNetworkPickerStillSaysDHCP(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
	m.selectedBox = common.PaneKnown
	m = safeListener(m)

	next, _ := m.Update(dnsKey())
	view := plainText(next.(NetpalaData).DnsForm.View())

	if !strings.Contains(view, "DHCP") {
		t.Errorf("the network picker lost its DHCP option:\n%s", view)
	}
}

// DNSProvidersForVpn must copy rather than edit the package-level table, or
// the wireless picker starts saying "None" too.
func TestVpnProviderListDoesNotMutateTheTable(t *testing.T) {
	original := common.DNSProviderByID(common.DNSModeDHCP).Label
	common.DNSProvidersForVpn(nil)
	if got := common.DNSProviderByID(common.DNSModeDHCP).Label; got != original {
		t.Errorf("package-level table mutated: %q -> %q", original, got)
	}
}

func TestApplyingDnsToAVpnIssuesACommand(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", true, common.DNSModeDHCP, nil))
	next, _ := m.Update(dnsKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeCloudflare})
	if cmd == nil {
		t.Error("applying DNS to a VPN issued nothing")
	}
	if got := next.(NetpalaData); got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
}

// Pointing a tunnel at the local resolver still has to bring that resolver up.
// The proxy has to be listening whoever is going to send it queries, so this
// is not a wireless-only concern.
func TestDNSCryptForAVpnStillStartsTheResolver(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", true, common.DNSModeDHCP, nil))
	m.SecurityData = []common.SecurityService{
		{Name: "DNSCrypt", Unit: "dnscrypt-proxy2.service", ProvidesDNS: true},
	}
	m.KnownNetworks = []common.KnownNetwork{net("home", true, common.DNSModeDHCP)}

	if m.startDNSServiceCmd() == nil {
		t.Fatal("precondition: the resolver should need starting")
	}

	next, _ := m.Update(dnsKey())
	m = next.(NetpalaData)

	_, cmd := m.Update(common.SubmitDnsMsg{ProviderID: common.DNSModeDNSCrypt})
	if cmd == nil {
		t.Error("choosing DNSCrypt for a live tunnel issued nothing")
	}
}

// Backing out must leave the profile alone and clear the target, so a later
// keypress cannot apply to whatever was last looked at.
func TestCancellingTheVpnPickerChangesNothing(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", true, common.DNSModeDHCP, nil))

	next, _ := m.Update(dnsKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.ExitFormMsg{})
	got := next.(NetpalaData)

	if got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
	if cmd != nil {
		t.Error("cancelling issued a command")
	}
}

// Pressing the key on a pane that holds no profiles must not index into an
// empty list or open a picker aimed at nothing.
func TestDnsKeyDoesNothingOnPanesWithoutProfiles(t *testing.T) {
	for name, pane := range map[string]int{
		"security": common.PaneSecurity,
		"device":   common.PaneDevice,
		"scanned":  common.PaneScanned,
	} {
		m := linkModel(false, []common.KnownNetwork{net("home", true, common.DNSModeDHCP)})
		m.selectedBox = pane
		m = safeListener(m)

		next, cmd := m.Update(dnsKey())
		if got := next.(NetpalaData); got.PopupState == 3 {
			t.Errorf("%s: opened the DNS picker", name)
		}
		if cmd != nil {
			t.Errorf("%s: issued a command", name)
		}
	}
}

func TestDnsKeyOnAnEmptyVpnPaneDoesNothing(t *testing.T) {
	m := vpnModel()

	next, cmd := m.Update(dnsKey())
	if got := next.(NetpalaData); got.PopupState == 3 {
		t.Error("opened a picker for a profile that is not there")
	}
	if cmd != nil {
		t.Error("issued a command against an empty pane")
	}
}

// The VPN row has to show what it currently resolves through, or picking a new
// value is done blind.
func TestVpnTableShowsTheProfilesDNS(t *testing.T) {
	defer common.SetWindowSizeForTest(0, 0)
	common.SetWindowSizeForTest(120, 40)

	rows := common.FormatVpnData([]common.VpnConnection{
		vpnWithDNS("mullvad-se", true, common.DNSModeCustom, []string{"10.64.0.1"}),
		vpnWithDNS("plain", false, common.DNSModeDHCP, nil),
	})

	if got := strings.TrimSpace(rows[2][4]); got != "Custom" {
		t.Errorf("DNS cell = %q, want Custom", got)
	}
	// "DHCP" would be a lie for a tunnel.
	if got := strings.TrimSpace(rows[3][4]); got != "None" {
		t.Errorf("DNS cell = %q, want None", got)
	}
}

// The picker opens on whatever the profile is already set to.
func TestVpnPickerStartsOnTheCurrentSetting(t *testing.T) {
	m := vpnModel(vpnWithDNS("mullvad-se", false, common.DNSModeCloudflare,
		[]string{"1.1.1.1", "1.0.0.1"}))

	next, _ := m.Update(dnsKey())
	form := next.(NetpalaData).DnsForm

	if form.Current != common.DNSModeCloudflare {
		t.Errorf("picker opened on %q, want the profile's current provider", form.Current)
	}
	var _ models.DnsSelect = form
}
