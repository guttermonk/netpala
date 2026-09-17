package common

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func sampleNetworks() []KnownNetwork {
	return []KnownNetwork{
		{SSID: "home-wifi", Security: "wpa2-psk", DNSMode: DNSModeDNSCrypt,
			MACMode: MACModeStable, AutoConnect: true, Signal: 92, Connected: true},
		{SSID: "cafe-guest", Security: "wpa2-psk", DNSMode: DNSModeCloudflare,
			MACMode: MACModeRandom, Signal: 65},
		{SSID: "office-5g", Security: "wpa2-eap", DNSMode: DNSModeDHCP,
			MACMode: MACModePermanent, AutoConnect: true, Signal: 41},
	}
}

// The MAC column is not conditional on terminal width; it is shown wherever
// the table is.
func TestMACColumnShownAtEveryWidth(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{60, 80, 100, 120, 140, 200} {
		SetWindowSizeForTest(w, 40)
		header := FormatKnownNetworksData(sampleNetworks(), 0, 3)[0]

		var found bool
		for _, cell := range header {
			if strings.TrimSpace(cell) == "MAC" {
				found = true
			}
		}
		if !found {
			t.Errorf("width %d: MAC column missing from header %q", w, header)
		}
		if len(header) != 8 {
			t.Errorf("width %d: %d columns, want 8", w, len(header))
		}
	}
}

// Every column must be wide enough for its own header and for the longest
// value it can hold, or the table quietly shows "Cloud..." instead of the
// provider name.
//
// Only guaranteed from KnownDetailColumnsFitFrom upwards. The detail columns
// are an equal division of two thirds of the pane, so on a narrow terminal
// they are all as narrow as six columns of that space allows, and the longest
// DNS label does not fit. TestNarrowTerminalTruncatesRatherThanReflows pins
// what happens below the line.
func TestNoColumnTruncatesItsContent(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{KnownDetailColumnsFitFrom, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)
		rows := FormatKnownNetworksData(sampleNetworks(), 0, 3)

		// Row 0 is the header, row 1 the blank spacer.
		for _, row := range rows[2:] {
			if len(row) < 8 {
				continue // padding row
			}
			// The Name column is allowed to truncate; it holds arbitrary SSIDs.
			for i, cell := range row {
				if i == 1 {
					continue
				}
				if strings.HasSuffix(cell, "...") {
					t.Errorf("width %d: column %d truncated to %q", w, i, cell)
				}
			}
		}
	}
}

// The longest label each fixed column can ever hold must fit. Checked against
// the label tables rather than a sample, so adding a provider or MAC mode with
// a longer name fails here rather than silently truncating in the UI.
func TestFixedColumnsFitTheirLongestLabel(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)
	SetWindowSizeForTest(KnownDetailColumnsFitFrom, 40)

	header := FormatKnownNetworksData(nil, 0, 0)[0]
	widthOf := func(col int) int { return len(header[col]) }

	const dnsCol, macCol = 3, 4

	for _, p := range DNSProviders {
		if got := len(p.Label); got > widthOf(dnsCol) {
			t.Errorf("DNS label %q is %d wide, column is %d", p.Label, got, widthOf(dnsCol))
		}
	}
	for _, o := range MACOptions {
		if got := len(o.Label); got > widthOf(macCol) {
			t.Errorf("MAC label %q is %d wide, column is %d", o.Label, got, widthOf(macCol))
		}
	}
}

// columnStarts is where each column begins, measured from the left of the pane.
func columnStarts(header []string) []int {
	starts := make([]int, 0, len(header))
	at := 0
	for _, cell := range header {
		starts = append(starts, at)
		at += lipgloss.Width(cell)
	}
	return starts
}

// The VPN pane sits directly under Known Networks, so its columns land on that
// grid: four columns over the six above them, each beginning where its
// counterpart does.
//
//	Known:  │ Name │ Security │ DNS │ MAC  │ Hidden │ Auto │ Signal │
//	VPN:    │ Name │    Type      │  Endpoint   │  DNS   │  Auto  │
func TestVpnColumnsLineUpWithKnownNetworks(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	// Indices into the known-networks header.
	const (
		kName     = 1
		kSecurity = 2
		kMAC      = 4
		kAuto     = 6
		kSignal   = 7
	)
	// Indices into the VPN header.
	const (
		vName     = 1
		vType     = 2
		vEndpoint = 3
		vDNS      = 4
		vAuto     = 5
	)

	for _, w := range []int{66, 80, 85, 100, 108, 120, 140, 200} {
		SetWindowSizeForTest(w, 40)

		known := columnStarts(FormatKnownNetworksData(nil, 0, 0)[0])
		vpn := columnStarts(FormatVpnData(nil)[0])

		for _, pair := range []struct {
			label      string
			vpnCol     int
			knownCol   int
			knownLabel string
		}{
			{"Name", vName, kName, "Name"},
			{"Type", vType, kSecurity, "Security"},
			{"Endpoint", vEndpoint, kMAC, "MAC"},
			{"DNS", vDNS, kAuto, "Auto"},
			{"Auto", vAuto, kSignal, "Signal"},
		} {
			if vpn[pair.vpnCol] != known[pair.knownCol] {
				t.Errorf("width %d: VPN %s starts at %d, Known %s at %d",
					w, pair.label, vpn[pair.vpnCol], pair.knownLabel, known[pair.knownCol])
			}
		}
	}
}

// Every pane's first label starts in the same column.
//
// Known Networks, VPN and Security each reserve a 5-wide marker for the ">"
// that shows what is live. New Networks has nothing to mark -- a scan result
// is not connected to anything -- and used to start its Name hard against the
// left border, five columns adrift of every other pane.
func TestEveryPaneStartsItsFirstLabelInTheSameColumn(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{38, 60, 80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)

		panes := map[string][]string{
			"known":    FormatKnownNetworksData(nil, 0, 0)[0],
			"scanned":  FormatScannedNetworksData(nil, 0, 0)[0],
			"security": FormatSecurityData(nil)[0],
			"vpn":      FormatVpnData(nil)[0],
		}

		for name, header := range panes {
			if got := lipgloss.Width(header[0]); got != markerWidth {
				t.Errorf("width %d: %s reserves %d columns for the marker, want %d",
					w, name, got, markerWidth)
			}
			// And the marker column is blank in the header everywhere, so it
			// reads as a gutter rather than an unnamed column.
			if strings.TrimSpace(header[0]) != "" {
				t.Errorf("width %d: %s has a label in the marker column: %q",
					w, name, header[0])
			}
		}
	}
}

// The blank column has to be in the data rows too, or the values slide left
// out from under their headers.
func TestScannedRowsCarryTheMarkerColumn(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)
	SetWindowSizeForTest(80, 40)

	rows := FormatScannedNetworksData([]ScannedNetwork{
		{SSID: "cafe-guest", Security: "wpa2-psk", Signal: 65},
	}, 0, 1)

	header := rows[0]
	row := rows[2] // 0 is the header, 1 the spacer
	if len(row) != len(header) {
		t.Fatalf("row has %d cells against %d headers", len(row), len(header))
	}
	if strings.TrimSpace(row[0]) != "" {
		t.Errorf("the marker cell is not blank: %q", row[0])
	}
	if strings.TrimSpace(row[1]) != "cafe-guest" {
		t.Errorf("Name landed in cell %q, want it under the Name header", row[1])
	}
}

// Name takes the first third in the VPN pane too, and the four detail columns
// fill exactly the remaining two thirds.
func TestVpnNameTakesTheFirstThird(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{66, 80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)
		header := FormatVpnData(nil)[0]

		total := w - 2
		if got := lipgloss.Width(header[0]) + lipgloss.Width(header[1]); got != total/3 {
			t.Errorf("width %d: marker plus Name is %d, want the first third (%d)",
				w, got, total/3)
		}

		sum := 0
		for _, cell := range header {
			sum += lipgloss.Width(cell)
		}
		if sum != total {
			t.Errorf("width %d: columns total %d, want %d", w, sum, total)
		}
	}
}

// padHeaders centres each header in exactly its column and puts nothing
// between columns, so a label as wide as its column runs straight into the
// next one -- which is how "Auto-Connect" ended up touching "Signal". Checked
// for every table rather than for the one that broke.
func TestHeadersDoNotTouchTheNextColumn(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)

		for name, header := range map[string][]string{
			"known":   FormatKnownNetworksData(sampleNetworks(), 0, 3)[0],
			"vpn":     FormatVpnData(sampleVpns())[0],
			"scanned": FormatScannedNetworksData(nil, 0, 0)[0],
		} {
			for i := 0; i < len(header)-1; i++ {
				left, right := header[i], header[i+1]
				if left == "" || strings.TrimSpace(right) == "" {
					continue // spacer columns have no label to collide
				}
				if !strings.HasSuffix(left, " ") && !strings.HasPrefix(right, " ") {
					t.Errorf("width %d: %s header %q runs into %q",
						w, name, strings.TrimSpace(left), strings.TrimSpace(right))
				}
			}
		}
	}
}

// columnEdges is where each column ends, measured from the left of the pane.
func columnEdges(header []string) []int {
	var edges []int
	at := 0
	for _, cell := range header {
		at += lipgloss.Width(cell)
		edges = append(edges, at)
	}
	return edges
}

// New Networks and Security are read one above the other, so their columns
// have to break on the same fractions. Any two sets of widths that merely
// look similar drift apart at some terminal size.
func TestScannedAndSecurityBreakOnTheSameThirds(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	// Below 38 columns the marker no longer fits inside the first third and
	// the Service floor takes over, so the two stop agreeing. Nothing in this
	// UI is usable at that size -- the status bar alone needs more.
	for _, w := range []int{38, 40, 60, 80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)

		scanned := columnEdges(FormatScannedNetworksData(nil, 0, 0)[0])
		security := columnEdges(FormatSecurityData(nil)[0])

		// Both carry a leading marker column -- New Networks has nothing to
		// mark, but reserves it so its first label starts where every other
		// pane's does.
		if len(scanned) != 4 || len(security) != 4 {
			t.Fatalf("width %d: %d scanned columns, %d security", w, len(scanned), len(security))
		}
		for i := range scanned {
			if scanned[i] != security[i] {
				t.Errorf("width %d: column %d ends at %d in New Networks and %d in Security",
					w, i, scanned[i], security[i])
			}
		}
	}
}

// Equal thirds, with the remainder going to the last column so the row still
// fills the pane exactly rather than leaving a ragged right edge.
func TestThirdsFillTheWidthExactly(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{60, 79, 80, 81, 100, 137} {
		SetWindowSizeForTest(w, 40)

		total := 0
		for _, cell := range FormatScannedNetworksData(nil, 0, 0)[0] {
			total += lipgloss.Width(cell)
		}
		if want := w - 2; total != want {
			t.Errorf("width %d: columns total %d, want %d", w, total, want)
		}

		a, b, c := thirds()
		if a != b {
			t.Errorf("width %d: first two thirds differ (%d, %d)", w, a, b)
		}
		if c-a > 2 || c < a {
			t.Errorf("width %d: last third is %d against %d for the others", w, c, a)
		}
	}
}

// The marker eats into the first third, so on a narrow terminal that column
// can be squeezed below its own header and wrap onto a second line -- which
// the layout, budgeting one row per service, has no room for.
func TestSecurityServiceColumnNeverFallsBelowItsHeader(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{20, 30, 40, 60, 80} {
		SetWindowSizeForTest(w, 40)
		header := FormatSecurityData(nil)[0]
		if got := lipgloss.Width(header[1]); got < lipgloss.Width("Service") {
			t.Errorf("width %d: Service column is %d, narrower than its header", w, got)
		}
	}
}

func sampleVpns() []VpnConnection {
	return []VpnConnection{
		{Name: "mullvad-se", ConnType: "WireGuard", Endpoint: "185.65.135.170:51820",
			AutoConnect: true, Connected: true},
		{Name: "work", ConnType: "OPENCONNECT", Endpoint: "vpn.example.com"},
	}
}

// Which server you are on is the thing the pane most needs to say, and it is
// not derivable from the profile name.
func TestVpnTableShowsEndpointAndAutoConnect(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{80, 100, 140} {
		SetWindowSizeForTest(w, 40)
		rows := FormatVpnData(sampleVpns())

		header := rows[0]
		if len(header) != 6 {
			t.Errorf("width %d: %d columns, want 6", w, len(header))
		}
		for _, want := range []string{"Name", "Type", "Endpoint", "DNS", "Auto"} {
			var found bool
			for _, cell := range header {
				if strings.TrimSpace(cell) == want {
					found = true
				}
			}
			if !found {
				t.Errorf("width %d: %q column missing from header %q", w, want, header)
			}
		}
	}
}

// Name and Endpoint hold arbitrary text and may truncate. The fixed columns
// must not: a Type shown as "OPENCO..." tells the user nothing.
func TestVpnFixedColumnsDoNotTruncate(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	const nameCol, endpointCol = 1, 3

	for _, w := range []int{80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)

		for _, row := range FormatVpnData(sampleVpns())[2:] {
			for i, cell := range row {
				if i == nameCol || i == endpointCol {
					continue
				}
				if strings.HasSuffix(cell, "...") {
					t.Errorf("width %d: column %d truncated to %q", w, i, cell)
				}
			}
		}
	}
}

// Name takes the first third of the pane, less the marker column that sits
// inside it. It no longer absorbs the slack: the detail columns divide the
// remaining two thirds evenly however wide the terminal gets.
func TestNameTakesTheFirstThird(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{60, 80, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)
		header := FormatKnownNetworksData(sampleNetworks(), 0, 3)[0]

		total := w - 2
		wantName := total/3 - markerWidth
		if got := lipgloss.Width(header[1]); got != wantName {
			t.Errorf("width %d: Name column is %d, want %d (a third less the marker)",
				w, got, wantName)
		}
		// The marker plus Name is the first third exactly, which is what keeps
		// the boundary in step with the panes below.
		if got := lipgloss.Width(header[0]) + lipgloss.Width(header[1]); got != total/3 {
			t.Errorf("width %d: first boundary at %d, want %d", w, got, total/3)
		}
	}
}

// The six detail columns divide the remaining two thirds evenly, to within the
// one column of remainder that cannot be split six ways.
func TestDetailColumnsAreEqual(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{60, 80, 85, 100, 120, 140} {
		SetWindowSizeForTest(w, 40)
		header := FormatKnownNetworksData(nil, 0, 0)[0]

		minW, maxW, sum := 1<<30, 0, 0
		for _, cell := range header[2:] {
			c := lipgloss.Width(cell)
			minW, maxW, sum = min(minW, c), max(maxW, c), sum+c
		}
		if maxW-minW > 1 {
			t.Errorf("width %d: detail columns range %d-%d, want them equal", w, minW, maxW)
		}

		total := w - 2
		if want := total - total/3; sum != want {
			t.Errorf("width %d: detail columns total %d, want the remaining two thirds (%d)",
				w, sum, want)
		}
	}
}

// lipgloss wraps a header that does not fit its column rather than truncating
// it, and a wrapped header costs a whole extra row that the layout has not
// budgeted. No header may ever be wider than its own column.
func TestNoHeaderIsWiderThanItsColumn(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	// 66 is the narrowest terminal where every header still fits its column.
	// Below that the equal splits squeeze "Security" under its own 8
	// characters and it wraps -- nothing to fix, since the status bar alone
	// needs 80 columns for the VPN pane, so the view is already unusable.
	for _, w := range []int{66, 80, 85, 100, 107, 108, 120, 140, 200} {
		SetWindowSizeForTest(w, 40)

		for name, header := range map[string][]string{
			"known":    FormatKnownNetworksData(sampleNetworks(), 0, 3)[0],
			"vpn":      FormatVpnData(sampleVpns())[0],
			"scanned":  FormatScannedNetworksData(nil, 0, 0)[0],
			"security": FormatSecurityData(nil)[0],
		} {
			for i, cell := range header {
				if strings.Contains(cell, "\n") {
					t.Errorf("width %d: %s header %d wrapped onto a second line: %q",
						w, name, i, cell)
				}
			}
		}
	}
}

// The auto-connect column is titled in full where it fits and shortened where
// it does not, so the header never wraps and never loses meaning needlessly.
func TestAutoHeaderAdaptsToItsColumn(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, tc := range []struct {
		width int
		want  string
	}{
		{80, "Auto"},
		{100, "Auto"},
		{107, "Auto"},
		{108, "Auto-Connect"}, // the column reaches 12 here
		{140, "Auto-Connect"},
		{200, "Auto-Connect"},
	} {
		SetWindowSizeForTest(tc.width, 40)
		if got := autoHeader(); got != tc.want {
			t.Errorf("width %d: autoHeader() = %q, want %q", tc.width, got, tc.want)
		}
		// Whichever form it picks has to fit.
		if got, col := lipgloss.Width(autoHeader()), knownWidths()[6]; got > col {
			t.Errorf("width %d: header is %d wide, column is %d", tc.width, got, col)
		}
	}
}

// Below the threshold the cells truncate. That is the accepted cost of an even
// split, but it must stay a truncation -- a cell that wrapped would take a
// second line, and the layout budgets exactly one row per network.
func TestNarrowTerminalTruncatesRatherThanReflows(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)
	SetWindowSizeForTest(80, 40)

	rows := FormatKnownNetworksData(sampleNetworks(), 0, 3)
	for _, row := range rows {
		for _, cell := range row {
			if strings.Contains(cell, "\n") {
				t.Errorf("cell wrapped onto a second line: %q", cell)
			}
		}
	}

	// And the truncation is the DNS column, as documented.
	if got := lipgloss.Width(rows[0][3]); got >= 10 {
		t.Errorf("DNS column is %d at 80 columns; the documented threshold of %d is wrong",
			got, KnownDetailColumnsFitFrom)
	}
}
