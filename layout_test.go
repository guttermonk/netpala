package main

import (
	"netpala/common"
	"netpala/config"
	"netpala/models"
	"strings"
	"testing"
)

// renderedLines is what the layout claims the view will occupy. It mirrors the
// accounting in computeLayout, so if the two ever disagree the tests below
// catch it rather than the user discovering a scrollbar.
func renderedLines(l paneLayout, vpnRows, securityRows, deviceRows int) int {
	panes := 3
	content := l.Known + l.Scanned + deviceRows
	if vpnRows > 0 {
		panes++
		content += vpnRows
	}
	if securityRows > 0 {
		panes++
		content += securityRows
	}
	return statusBarLines + panes*paneChrome + content
}

// The whole point: at or above minimumHeight the view must fit exactly, and
// below it the layout must still produce usable heights rather than garbage.
func TestLayoutNeverOverflows(t *testing.T) {
	heights := []int{10, 14, 18, 24, 30, 40, 50, 60, 80, 120}
	counts := []int{0, 1, 3, 8, 20, 60}

	for _, h := range heights {
		for _, known := range counts {
			for _, scanned := range counts {
				for _, vpn := range []int{0, 2} {
					for _, sec := range []int{0, 2} {
						l := computeLayout(h, known, scanned, vpn, sec, 1)
						got := renderedLines(l, vpn, sec, 1)

						if h >= minimumHeight(vpn, sec, 1) && got > h {
							t.Errorf("term=%d known=%d scanned=%d vpn=%d sec=%d: view is %d lines",
								h, known, scanned, vpn, sec, got)
						}
						if l.Known < 1 || l.Scanned < 1 {
							t.Errorf("term=%d known=%d scanned=%d: pane collapsed to %+v",
								h, known, scanned, l)
						}
					}
				}
			}
		}
	}
}

// minimumHeight must be tight: one row shorter and it genuinely cannot fit,
// exactly that tall and it does. A loose bound would hide real clipping.
func TestMinimumHeightIsExact(t *testing.T) {
	for _, vpn := range []int{0, 2} {
		for _, sec := range []int{0, 2} {
			minH := minimumHeight(vpn, sec, 1)

			l := computeLayout(minH, 60, 60, vpn, sec, 1)
			if got := renderedLines(l, vpn, sec, 1); got != minH {
				t.Errorf("vpn=%d sec=%d: at minimumHeight %d the view is %d lines",
					vpn, sec, minH, got)
			}
		}
	}
}

// A short scan list should not be padded out to half the screen -- that was
// the complaint that prompted this.
func TestScanListSizedToContentWhenItFits(t *testing.T) {
	l := computeLayout(50, 6, 3, 0, 0, 1)
	if l.Scanned != 3 {
		t.Errorf("scanned = %d, want exactly its 3 entries", l.Scanned)
	}
	if l.Known <= l.Scanned {
		t.Errorf("known (%d) should absorb the slack, not scanned (%d)", l.Known, l.Scanned)
	}
}

// With lots of both, the known list still gets the larger share.
func TestKnownGetsPriorityWhenContended(t *testing.T) {
	l := computeLayout(30, 40, 40, 0, 0, 1)
	if l.Known <= l.Scanned {
		t.Errorf("known = %d, scanned = %d; known should win the split", l.Known, l.Scanned)
	}
	if l.Scanned < minListRows {
		t.Errorf("scanned = %d, below the %d floor", l.Scanned, minListRows)
	}
}

// Growing the terminal must give the rows to the lists, not waste them.
func TestLayoutUsesAddedRows(t *testing.T) {
	small := computeLayout(24, 40, 40, 0, 0, 1)
	large := computeLayout(48, 40, 40, 0, 0, 1)

	if large.Known+large.Scanned <= small.Known+small.Scanned {
		t.Errorf("24-row term gave %d list rows, 48-row gave %d; should grow",
			small.Known+small.Scanned, large.Known+large.Scanned)
	}
}

// Showing the optional panes must come out of the lists, not off the bottom.
func TestOptionalPanesTakeFromLists(t *testing.T) {
	without := computeLayout(40, 30, 30, 0, 0, 1)
	withBoth := computeLayout(40, 30, 30, 2, 2, 1)

	if withBoth.Known+withBoth.Scanned >= without.Known+without.Scanned {
		t.Error("VPN and Security panes did not reduce the space given to the lists")
	}
	if got := renderedLines(withBoth, 2, 2, 1); got > 40 {
		t.Errorf("view is %d lines in a 40-row terminal", got)
	}
}

// A list with no entries still needs a row so the pane renders.
func TestEmptyListsStillGetRows(t *testing.T) {
	l := computeLayout(40, 0, 0, 0, 0, 1)
	if l.Known < 1 || l.Scanned < 1 {
		t.Errorf("empty lists collapsed: %+v", l)
	}
}

// Tiny terminals cannot fit everything; they must degrade rather than produce
// negative heights that would panic the slice windowing.
func TestTinyTerminalDegradesSafely(t *testing.T) {
	for _, h := range []int{1, 5, 8, 12, 16} {
		l := computeLayout(h, 10, 10, 2, 2, 1)
		if l.Known < 1 || l.Scanned < 1 {
			t.Errorf("term=%d produced %+v; heights must stay >= 1", h, l)
		}
	}
}

// The tests above check computeLayout against renderedLines, but both share
// the same model of how much chrome a pane costs. This one renders the real
// tables and counts the lines, so a wrong model cannot pass by agreeing with
// itself.
func TestLayoutMatchesActualRendering(t *testing.T) {
	mk := func(known, scanned, vpn, sec, dev int) models.TablesModel {
		return models.TablesModel{
			KnownNetworks:   make([]common.KnownNetwork, known),
			ScannedNetworks: make([]common.ScannedNetwork, scanned),
			VpnData:         make([]common.VpnConnection, vpn),
			SecurityData:    make([]common.SecurityService, sec),
			DeviceData:      make([]common.Device, dev),
			Colors:          config.DefaultColors(),
		}
	}

	cases := []struct{ h, known, scanned, vpn, sec, dev int }{
		{24, 5, 3, 0, 0, 1},
		{24, 40, 40, 0, 0, 1},
		{40, 12, 6, 0, 0, 1},
		{40, 40, 40, 2, 0, 1},
		{60, 3, 30, 2, 2, 1},
		{80, 0, 0, 0, 0, 1},
		{50, 60, 60, 0, 2, 1},
	}

	for _, c := range cases {
		l := computeLayout(c.h, c.known, c.scanned, c.vpn, c.sec, c.dev)

		tm := mk(c.known, c.scanned, c.vpn, c.sec, c.dev)
		tm.KnownHeight = l.Known
		tm.ScannedHeight = l.Scanned
		tm.VPNHeight = c.vpn
		tm.SecurityHeight = c.sec
		tm.DeviceHeight = c.dev

		actual := strings.Count(tm.View(), "\n")
		predicted := renderedLines(l, c.vpn, c.sec, c.dev) - statusBarLines

		if actual != predicted {
			t.Errorf("term=%d known=%d scanned=%d vpn=%d sec=%d: rendered %d lines, layout budgeted %d",
				c.h, c.known, c.scanned, c.vpn, c.sec, actual, predicted)
		}
		if total := actual + statusBarLines; c.h >= minimumHeight(c.vpn, c.sec, c.dev) && total > c.h {
			t.Errorf("term=%d: real view is %d lines, overflows", c.h, total)
		}
	}
}
