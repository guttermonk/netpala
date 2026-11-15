package main

import (
	"netpala/common"
	"testing"
)

func scanned(ssid string, signal int) common.ScannedNetwork {
	return common.ScannedNetwork{SSID: ssid, Signal: signal, Security: "wpa2-psk"}
}

// scanModel sits on New Networks with the cursor on the given row.
func scanModel(row int, networks ...common.ScannedNetwork) NetpalaData {
	m := safeListener(linkModel(false, nil))
	m.ScannedNetworks = networks
	m.selectedBox = common.PaneScanned
	m.SelectedEntry = row
	return m
}

func rowUnderCursor(t *testing.T, m NetpalaData) string {
	t.Helper()
	if m.SelectedEntry < 0 || m.SelectedEntry >= len(m.ScannedNetworks) {
		t.Fatalf("cursor at %d, outside a list of %d", m.SelectedEntry, len(m.ScannedNetworks))
	}
	return m.ScannedNetworks[m.SelectedEntry].SSID
}

// A scan arriving while the list is being read must not move the cursor onto a
// different network. Access points come and go constantly, so one dropping out
// above the cursor shifts every row below it up, and the next Enter would
// connect to whatever had moved into that row.
func TestACursorKeepsItsNetworkWhenAnotherDisappears(t *testing.T) {
	m := scanModel(2,
		scanned("strong", 90),
		scanned("passing", 70),
		scanned("wanted", 50),
	)
	if got := rowUnderCursor(t, m); got != "wanted" {
		t.Fatalf("precondition: cursor on %q", got)
	}

	next, _, handled := m.handleDataMsg(common.ScannedNetworksUpdateMsg{
		scanned("strong", 90),
		scanned("wanted", 50),
	})
	if !handled {
		t.Fatal("scan results not handled as a data message")
	}

	if got := rowUnderCursor(t, next); got != "wanted" {
		t.Errorf("cursor now on %q, want it still on wanted", got)
	}
}

// The same when one appears above it, which is the more common of the two.
func TestACursorKeepsItsNetworkWhenAnotherAppears(t *testing.T) {
	m := scanModel(0, scanned("wanted", 50))

	next, _, _ := m.handleDataMsg(common.ScannedNetworksUpdateMsg{
		scanned("newcomer", 80),
		scanned("wanted", 50),
	})

	if got := rowUnderCursor(t, next); got != "wanted" {
		t.Errorf("cursor now on %q, want it still on wanted", got)
	}
}

// A network that goes while the cursor is on it has nothing to hold, and the
// cursor has to end up somewhere inside the shorter list.
func TestTheCursorSurvivesItsOwnNetworkGoing(t *testing.T) {
	m := scanModel(1, scanned("strong", 90), scanned("going", 50))

	next, _, _ := m.handleDataMsg(common.ScannedNetworksUpdateMsg{
		scanned("strong", 90),
	})

	if next.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d, want 0 in a list of 1", next.SelectedEntry)
	}
	if got := rowUnderCursor(t, next); got != "strong" {
		t.Errorf("cursor on %q, want strong", got)
	}
}

// Only New Networks is held this way. The cursor in another pane has nothing to
// do with the scan list and must not be dragged about by it.
func TestAScanDoesNotMoveTheCursorInAnotherPane(t *testing.T) {
	m := safeListener(linkModel(false, []common.KnownNetwork{
		net("home", true, ""), net("cafe", false, ""),
	}))
	m.ScannedNetworks = []common.ScannedNetwork{scanned("one", 60), scanned("two", 50)}
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 1

	next, _, _ := m.handleDataMsg(common.ScannedNetworksUpdateMsg{scanned("two", 50)})

	if next.SelectedEntry != 1 {
		t.Errorf("SelectedEntry = %d, want the Known Networks cursor left alone", next.SelectedEntry)
	}
}
