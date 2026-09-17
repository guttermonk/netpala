package models

import (
	"netpala/common"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"
)

func tunnel(name string, connected, defaultRoute bool) common.VpnConnection {
	v := common.VpnConnection{
		Path:      godbus.ObjectPath("/vpn/" + name),
		Name:      name,
		ConnType:  "WireGuard",
		Connected: connected,
		// A tunnel can only hold the default route while it is up.
		IsDefaultRoute: connected && defaultRoute,
	}
	if connected {
		v.ActivePath = godbus.ObjectPath("/active/" + name)
	}
	return v
}

func tablesWith(vpns ...common.VpnConnection) TablesModel {
	return TablesModel{
		VpnData:       vpns,
		KnownNetworks: []common.KnownNetwork{{SSID: "home", Connected: true}},
	}
}

// The wireless row is marked connected whether or not it is the way out, so
// the pane has to say when it is only carrying something else.
func TestKnownTitleNamesTheTunnelItCarries(t *testing.T) {
	m := tablesWith(tunnel("mullvad-se", true, true))

	got := m.knownTitle()
	if !strings.Contains(got, "carrying") || !strings.Contains(got, "mullvad-se") {
		t.Errorf("knownTitle() = %q, want it to name the tunnel it carries", got)
	}
}

func TestKnownTitleIsPlainWithoutATunnel(t *testing.T) {
	for name, m := range map[string]TablesModel{
		"no vpns":              tablesWith(),
		"vpn saved, down":      tablesWith(tunnel("mullvad-se", false, false)),
		"vpn up, not carrying": tablesWith(tunnel("mullvad-se", true, false)),
	} {
		if got := m.knownTitle(); got != "Known Networks" {
			t.Errorf("%s: knownTitle() = %q, want the plain title", name, got)
		}
	}
}

// The case that looks like success and is not: the tunnel is up, the row says
// connected, and traffic is still leaving around it.
func TestVpnTitleFlagsAConnectedTunnelThatCarriesNothing(t *testing.T) {
	m := tablesWith(tunnel("mullvad-se", true, false))

	got := m.vpnTitle()
	if !strings.Contains(got, "not carrying") {
		t.Errorf("vpnTitle() = %q, want it to flag that traffic bypasses the tunnel", got)
	}
}

func TestVpnTitleIsPlainWhenTheTunnelCarriesTraffic(t *testing.T) {
	m := tablesWith(tunnel("mullvad-se", true, true))
	if got := m.vpnTitle(); got != "Virtual Private Networks" {
		t.Errorf("vpnTitle() = %q, want the plain title", got)
	}
}

// A saved-but-down profile is not a problem to report.
func TestVpnTitleIsPlainWhenNothingIsConnected(t *testing.T) {
	m := tablesWith(tunnel("mullvad-se", false, false), tunnel("work", false, false))
	if got := m.vpnTitle(); got != "Virtual Private Networks" {
		t.Errorf("vpnTitle() = %q, want the plain title", got)
	}
}

// With several tunnels up, only one can hold the default route, and the title
// has to name that one rather than whichever comes first.
func TestKnownTitleNamesTheCarryingTunnelNotTheFirst(t *testing.T) {
	m := tablesWith(
		tunnel("work", true, false),
		tunnel("mullvad-se", true, true),
	)
	if got := m.knownTitle(); !strings.Contains(got, "mullvad-se") {
		t.Errorf("knownTitle() = %q, want the tunnel holding the default route", got)
	}
}

// A profile name is arbitrary text and goes straight into the title, so it
// cannot be allowed to push the title past the terminal width.
func TestLongProfileNamesAreTruncatedInTitles(t *testing.T) {
	m := tablesWith(tunnel(strings.Repeat("x", 200), true, true))

	if got := len(m.knownTitle()); got > len("Known Networks - carrying ")+titleSuffixBudget {
		t.Errorf("title is %d characters, want it capped", got)
	}
}

// CalcTitle pads the rule to the terminal width. Measuring the title in bytes
// rather than columns leaves the border short by two for every non-ASCII
// character -- and profile names are now part of the title.
func TestTitleRuleIsMeasuredInColumns(t *testing.T) {
	defer common.SetWindowSizeForTest(0, 0)
	common.SetWindowSizeForTest(80, 40)

	ascii := common.CalcTitle("Known Networks - carrying abcde", false, "#fff", "#fff")
	unicode := common.CalcTitle("Known Networks - carrying äöüéß", false, "#fff", "#fff")

	if a, u := len([]rune(ascii)), len([]rune(unicode)); a != u {
		t.Errorf("a non-ASCII title rendered %d columns against %d for the same width in ASCII", u, a)
	}
}
