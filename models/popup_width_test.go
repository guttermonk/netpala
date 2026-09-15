package models

import (
	"fmt"
	"netpala/common"
	"netpala/config"
	"strings"
	"testing"
)

// The pickers render at Width(56) with Padding(0, 1), leaving 54 columns of
// content. lipgloss pads every row out to that width, so a row that is too
// long does not look too long in the rendered output - it silently wraps, or
// gets clipped when the overlay is composited against a narrow terminal.
// Measuring the rendered line therefore proves nothing; the content has to be
// measured before it is styled.
const (
	popupContentWidth = 54
	// Two columns of slack, so a row is never flush against the border and a
	// slightly longer label or description does not immediately overflow.
	popupRowBudget = popupContentWidth - 2
)

// macRowWidth mirrors the format string in MacSelect.View.
func macRowWidth(o common.MACOption) int {
	return len([]rune(fmt.Sprintf("%s%-11s %s", "> ", o.Label, o.Desc)))
}

// The Stable row's description was exactly 54 columns wide - a perfect fit with
// no slack, which the overlay clipped on a narrow terminal.
func TestMacOptionRowsFitWithSlack(t *testing.T) {
	for _, nmDefault := range []string{"", "permanent", "preserve", "02:11:22:33:44:55"} {
		for _, o := range common.MACOptionsFor(nmDefault) {
			if got := macRowWidth(o); got > popupRowBudget {
				t.Errorf("nmDefault=%q: %q row is %d columns, over the %d budget:\n  %q",
					nmDefault, o.Label, got, popupRowBudget, o.Desc)
			}
		}
	}
}

func TestDnsProviderRowsFitWithSlack(t *testing.T) {
	// DnsSelect.View uses the same shape plus a liveness annotation.
	const annotation = len(" · not responding")
	for _, p := range common.DNSProvidersFor([]string{"127.0.0.53"}) {
		width := len([]rune(fmt.Sprintf("%s%-11s %s", "> ", p.Label, p.Desc)))
		if p.ID == common.DNSModeDNSCrypt {
			width += annotation
		}
		if width > popupRowBudget {
			t.Errorf("%q row is %d columns, over the %d budget:\n  %q",
				p.Label, width, popupRowBudget, p.Desc)
		}
	}
}

// Naming the resolved default is the only thing distinguishing "Default" from
// "Permanent" on a machine whose global default is permanent.
func TestDefaultRowNamesTheResolvedSetting(t *testing.T) {
	m := ModelMacSelect(config.DefaultColors(), config.DefaultKeyBindings(), "permanent")
	m.SSID = "home"
	m.SelectMode(common.MACModeDefault, "")

	if !strings.Contains(stripANSI(m.View()), "NetworkManager's: permanent") {
		t.Errorf("resolved default not shown:\n%s", stripANSI(m.View()))
	}

	// Unknown must not claim a value it did not read.
	m = ModelMacSelect(config.DefaultColors(), config.DefaultKeyBindings(), "")
	m.SelectMode(common.MACModeDefault, "")
	if strings.Contains(stripANSI(m.View()), "NetworkManager's: ") {
		t.Error("claimed a default even though none could be read")
	}
}

// MACOptionsFor must not rewrite the description every other caller sees.
func TestMACOptionsForDoesNotMutateTheTable(t *testing.T) {
	original := common.MACOptionByID(common.MACModeDefault).Desc
	common.MACOptionsFor("permanent")
	if got := common.MACOptionByID(common.MACModeDefault).Desc; got != original {
		t.Errorf("package-level table mutated: %q -> %q", original, got)
	}
}
