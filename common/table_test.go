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
		if len(header) != 5 {
			t.Errorf("width %d: %d columns, want 5", w, len(header))
		}
		for _, want := range []string{"Name", "Type", "Endpoint", "Auto"} {
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
