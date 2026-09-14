package main

// Vertical layout budgeting.
//
// Every pane costs a fixed amount of chrome regardless of its contents: the
// title line, the header row, the blank spacer row beneath it, and the bottom
// border. The list panes then render exactly the number of content rows they
// are given, padding with blanks when there is less data than space. The
// panes that size themselves to their contents (VPN, Security, Device) ignore
// the height they are handed and render one row per entry.
//
// Budgeting on those two facts is what keeps the whole view inside the
// terminal instead of relying on a magic constant.
const (
	paneChrome     = 4 // title + header + spacer + bottom border
	statusBarLines = 1
	// minListRows keeps a list usable rather than collapsing it to nothing.
	minListRows = 2
)

// paneLayout is the number of content rows each list pane should render.
type paneLayout struct {
	Known   int
	Scanned int
}

// minimumHeight is the shortest terminal that can show the given panes without
// clipping. Chrome dominates it: every pane costs four lines before a single
// row of data, so five panes need twenty lines no matter how the rest is
// divided. Below this the layout still produces valid heights, but the bottom
// of the view will not be visible.
func minimumHeight(vpnRows, securityRows, deviceRows int) int {
	visiblePanes := 3
	contentRows := deviceRows
	if vpnRows > 0 {
		visiblePanes++
		contentRows += vpnRows
	}
	if securityRows > 0 {
		visiblePanes++
		contentRows += securityRows
	}
	return statusBarLines + visiblePanes*paneChrome + contentRows + 2*minListRows
}

// computeLayout divides a terminal of termHeight rows between the visible
// panes. vpnRows/securityRows are 0 when those panes are hidden.
func computeLayout(termHeight, knownCount, scannedCount, vpnRows, securityRows, deviceRows int) paneLayout {
	// Known, Scanned and Device are always shown.
	visiblePanes := 3
	contentRows := deviceRows
	if vpnRows > 0 {
		visiblePanes++
		contentRows += vpnRows
	}
	if securityRows > 0 {
		visiblePanes++
		contentRows += securityRows
	}

	budget := termHeight - statusBarLines - visiblePanes*paneChrome - contentRows

	known, scanned := splitListRows(budget, knownCount, scannedCount)
	return paneLayout{Known: known, Scanned: scanned}
}

// splitListRows divides the rows left over after the fixed-size panes between
// the two network lists.
//
// The known list gets priority because it is the one you act on; the scan list
// is for discovery and does not need to be half the screen just because it has
// a lot of entries. When space is tight it is capped at a third, but never
// below minListRows so it stays usable.
func splitListRows(budget, knownCount, scannedCount int) (int, int) {
	// Nothing sensible to do with a terminal this short; give each list a
	// single row so the panes still render and nothing indexes out of range.
	if budget < 2*minListRows {
		return 1, 1
	}

	wantKnown := max(knownCount, minListRows)
	wantScanned := max(scannedCount, minListRows)

	// Everything fits. Size the scan list to its contents and let the known
	// list absorb the slack, so the view fills the window without the scan
	// list being padded out with blank rows.
	if wantKnown+wantScanned <= budget {
		return budget - wantScanned, wantScanned
	}

	// Contended. Cap the scan list at a third of the space, then give the
	// known list everything else.
	scanned := min(wantScanned, max(minListRows, budget/3))
	known := budget - scanned

	// If the known list does not need all of that, hand the remainder back.
	if known > wantKnown {
		known = wantKnown
		scanned = budget - known
	}
	return known, scanned
}
