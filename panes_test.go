package main

import (
	"netpala/common"
	"reflect"
	"testing"
)

func withData(vpns, security, known, scanned, devices int) NetpalaData {
	m := NetpalaData{}
	m.VpnData = make([]common.VpnConnection, vpns)
	m.SecurityData = make([]common.SecurityService, security)
	m.KnownNetworks = make([]common.KnownNetwork, known)
	m.ScannedNetworks = make([]common.ScannedNetwork, scanned)
	m.DeviceData = make([]common.Device, devices)
	return m
}

func TestVisiblePanesHidesEmptyOnes(t *testing.T) {
	tests := []struct {
		name           string
		vpns, security int
		want           []int
	}{
		{
			name: "neither vpn nor security",
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneDevice},
		},
		{
			name: "vpn only", vpns: 2,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneVPN, common.PaneDevice},
		},
		{
			name: "security only", security: 2,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneSecurity, common.PaneDevice},
		},
		{
			name: "both", vpns: 1, security: 1,
			want: []int{common.PaneKnown, common.PaneScanned, common.PaneVPN, common.PaneSecurity, common.PaneDevice},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := withData(tt.vpns, tt.security, 3, 3, 1)
			if got := m.visiblePanes(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("visiblePanes() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The old navigation hardcoded four panes and patched around an empty VPN
// list. Cycling must now land on every visible pane and no hidden one.
func TestStepPaneCyclesOnlyVisible(t *testing.T) {
	m := withData(0, 2, 3, 3, 1) // security shown, vpn hidden
	m.selectedBox = common.PaneKnown

	var seen []int
	for range 4 {
		m.stepPane(1)
		seen = append(seen, m.selectedBox)
	}

	want := []int{common.PaneScanned, common.PaneSecurity, common.PaneDevice, common.PaneKnown}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("forward cycle = %v, want %v", seen, want)
	}

	seen = nil
	for range 4 {
		m.stepPane(-1)
		seen = append(seen, m.selectedBox)
	}
	want = []int{common.PaneDevice, common.PaneSecurity, common.PaneScanned, common.PaneKnown}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("backward cycle = %v, want %v", seen, want)
	}
}

func TestStepPaneResetsEntry(t *testing.T) {
	m := withData(0, 2, 5, 5, 1)
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 4

	m.stepPane(1)
	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d after changing pane, want 0", m.SelectedEntry)
	}
}

// A pane that disappears while selected must not strand the cursor there,
// or Select would index into an empty slice.
func TestClampSelectionLeavesHiddenPane(t *testing.T) {
	m := withData(0, 2, 3, 3, 1)
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 1

	m.SecurityData = nil // unit vanished between refreshes
	m.clampSelection()

	if m.selectedBox != common.PaneKnown || m.SelectedEntry != 0 {
		t.Errorf("selection = pane %d entry %d, want pane %d entry 0",
			m.selectedBox, m.SelectedEntry, common.PaneKnown)
	}
}

func TestClampSelectionShrinksEntry(t *testing.T) {
	m := withData(0, 3, 3, 3, 1)
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 2

	m.SecurityData = make([]common.SecurityService, 1)
	m.clampSelection()

	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d after the pane shrank to 1 row, want 0", m.SelectedEntry)
	}
	if m.selectedBox != common.PaneSecurity {
		t.Error("should have stayed on the Security pane")
	}
}

func TestClampSelectionHandlesEmptyPane(t *testing.T) {
	m := withData(0, 1, 0, 0, 1)
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 3

	m.clampSelection()
	if m.SelectedEntry != 0 {
		t.Errorf("SelectedEntry = %d for an empty pane, want 0", m.SelectedEntry)
	}
}

func TestPaneEntryCount(t *testing.T) {
	m := withData(1, 2, 3, 4, 5)
	for pane, want := range map[int]int{
		common.PaneKnown:    3,
		common.PaneScanned:  4,
		common.PaneVPN:      1,
		common.PaneSecurity: 2,
		common.PaneDevice:   5,
	} {
		if got := m.paneEntryCount(pane); got != want {
			t.Errorf("paneEntryCount(%d) = %d, want %d", pane, got, want)
		}
	}
}
