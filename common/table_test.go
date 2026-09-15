package common

import (
	"strings"
	"testing"
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
func TestNoColumnTruncatesItsContent(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, w := range []int{80, 100, 120, 140} {
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
	SetWindowSizeForTest(80, 40)

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

// Name is the only flexible column, so it takes whatever is left. It must not
// be squeezed to nothing on a narrow terminal.
func TestNameColumnKeepsUsableWidth(t *testing.T) {
	defer SetWindowSizeForTest(0, 0)

	for _, tc := range []struct{ width, min int }{
		{80, 14},
		{100, 30},
		{140, 60},
	} {
		SetWindowSizeForTest(tc.width, 40)
		header := FormatKnownNetworksData(sampleNetworks(), 0, 3)[0]
		if got := len(header[1]); got < tc.min {
			t.Errorf("width %d: Name column is %d, want at least %d", tc.width, got, tc.min)
		}
	}
}
