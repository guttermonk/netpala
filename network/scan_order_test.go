package network

import (
	"netpala/common"
	"testing"

	"github.com/godbus/dbus/v5"
)

func ap(ssid string, signal int, path string) common.ScannedNetwork {
	return common.ScannedNetwork{
		SSID:   ssid,
		Signal: signal,
		Path:   dbus.ObjectPath(path),
	}
}

func ssids(networks []common.ScannedNetwork) []string {
	out := make([]string, 0, len(networks))
	for _, n := range networks {
		out = append(out, n.SSID)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The order has to be a function of the scan and nothing else. It used to come
// out of a map iteration sorted on signal alone, so every network sharing a
// strength with another -- most of a scan, once the weak end is counted -- sat
// in a different place each time, and the list reshuffled while being read.
func TestEqualSignalsOrderTheSameWayEveryTime(t *testing.T) {
	scan := []common.ScannedNetwork{
		ap("delta", 40, "/ap/1"),
		ap("alpha", 40, "/ap/2"),
		ap("charlie", 40, "/ap/3"),
		ap("bravo", 40, "/ap/4"),
	}
	want := []string{"alpha", "bravo", "charlie", "delta"}

	// Many runs, because the old failure was a map iteration: one run could
	// come out in any order, including the right one.
	for range 50 {
		if got := ssids(removeDuplicates(scan)); !equal(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// And the same list however it arrives: the access points are enumerated per
// device, in whatever order NetworkManager hands them over.
func TestOrderDoesNotDependOnTheScanOrder(t *testing.T) {
	forwards := []common.ScannedNetwork{
		ap("strong", 90, "/ap/1"),
		ap("middling", 55, "/ap/2"),
		ap("weak", 20, "/ap/3"),
	}
	backwards := []common.ScannedNetwork{
		ap("weak", 20, "/ap/3"),
		ap("middling", 55, "/ap/2"),
		ap("strong", 90, "/ap/1"),
	}

	want := []string{"strong", "middling", "weak"}
	if got := ssids(removeDuplicates(forwards)); !equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if got := ssids(removeDuplicates(backwards)); !equal(got, want) {
		t.Errorf("reversed input gave %v, want %v", got, want)
	}
}

// One SSID can be several access points -- two bands of one router, a floor of
// them in an office. The strongest is the one worth showing, and now also the
// one its path points at, because that is what the connection activates on.
func TestTheStrongestAccessPointRepresentsItsSSID(t *testing.T) {
	result := removeDuplicates([]common.ScannedNetwork{
		ap("office", 30, "/ap/far"),
		ap("office", 85, "/ap/near"),
		ap("office", 60, "/ap/middle"),
	})

	if len(result) != 1 {
		t.Fatalf("%d rows for one SSID, want 1", len(result))
	}
	if result[0].Signal != 85 {
		t.Errorf("signal = %d, want the strongest access point's 85", result[0].Signal)
	}
	if got := string(result[0].Path); got != "/ap/near" {
		t.Errorf("path = %q, want the strongest access point's %q", got, "/ap/near")
	}
}
