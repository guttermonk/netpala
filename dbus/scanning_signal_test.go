package dbus

import (
	"netpala/common"
	"netpala/network"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

func propsChanged(iface string, changed map[string]dbus.Variant) *dbus.Signal {
	return &dbus.Signal{
		Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
		Path: dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/2"),
		Body: []any{iface, changed, []string{}},
	}
}

// The flag comes out of the signal, not out of a re-read. NetworkManager
// finishes scanning well inside the round trip, so both the start and the end
// signal would read it back as false and the scan would never look as though it
// had happened.
func TestAScanningSignalIsAnsweredFromItsOwnBody(t *testing.T) {
	for _, scanning := range []bool{true, false} {
		cmds := signalRefresh(nil, propsChanged(network.WifiIF, map[string]dbus.Variant{
			"Scanning": dbus.MakeVariant(scanning),
		}))

		if len(cmds) != 1 {
			t.Fatalf("scanning=%v produced %d commands, want the one message", scanning, len(cmds))
		}
		msg, ok := cmds[0]().(common.ScanningStateMsg)
		if !ok {
			t.Fatalf("got %T, want a ScanningStateMsg", cmds[0]())
		}
		if msg.Scanning != scanning {
			t.Errorf("Scanning = %v, want %v", msg.Scanning, scanning)
		}
		if want := dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/2"); msg.DevicePath != want {
			t.Errorf("DevicePath = %q, want the device that emitted it, %q", msg.DevicePath, want)
		}
	}
}

// Any other wireless property is still worth a refresh, and that is the branch
// the scanning case is carved out of -- it must not swallow the rest.
func TestOtherWirelessPropertiesStillRefresh(t *testing.T) {
	cmds := signalRefresh(nil, propsChanged(network.WifiIF, map[string]dbus.Variant{
		"LastScan": dbus.MakeVariant(int64(1234)),
	}))

	if len(cmds) != 2 {
		t.Fatalf("%d commands, want the device and VPN refreshes", len(cmds))
	}
}

// A body that is not the shape PropertiesChanged promises must not be read as
// one. Every PropertiesChanged on the bus reaches this function, most of them
// from somewhere other than NetworkManager.
func TestAMalformedBodyIsNotReadAsAFlag(t *testing.T) {
	for name, sig := range map[string]*dbus.Signal{
		"no body": {
			Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
			Body: []any{network.WifiIF},
		},
		"wrong dictionary type": {
			Name: "org.freedesktop.DBus.Properties.PropertiesChanged",
			Body: []any{network.WifiIF, "not a dictionary", []string{}},
		},
		"flag of the wrong type": propsChanged(network.WifiIF, map[string]dbus.Variant{
			"Scanning": dbus.MakeVariant("yes"),
		}),
	} {
		t.Run(name, func(t *testing.T) {
			// Falls through to the refresh rather than inventing a flag value.
			if got := len(signalRefresh(nil, sig)); got != 2 {
				t.Errorf("%d commands, want the two refreshes", got)
			}
		})
	}
}

// Scanning is a wireless property. The same flag name arriving on another
// interface is not this signal.
//
// The commands are counted rather than run: the refreshes read NetworkManager,
// and there is no bus in a test. Two of them is the refresh branch; the flag is
// one on its own.
func TestScanningIsOnlyReadFromTheWirelessInterface(t *testing.T) {
	cmds := signalRefresh(nil, propsChanged(network.DevIF, map[string]dbus.Variant{
		"Scanning": dbus.MakeVariant(true),
	}))

	if len(cmds) != 2 {
		t.Errorf("%d commands, want the two refreshes", len(cmds))
	}
}

// And the listener still arms exactly one reader for it, like every other
// signal it answers.
func TestAScanningSignalArmsOneReader(t *testing.T) {
	ch := make(chan *dbus.Signal, 4)
	for range 4 {
		ch <- propsChanged(network.WifiIF, map[string]dbus.Variant{
			"Scanning": dbus.MakeVariant(true),
		})
	}

	msg := WaitForDBusSignal(nil, ch)()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("got %T, want a tea.BatchMsg", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want the re-arm plus the flag", len(batch))
	}
	if len(ch) != 3 {
		t.Errorf("%d signals left of 4; one call consumed %d", len(ch), 4-len(ch))
	}
	if _, ok := batch[1]().(common.ScanningStateMsg); !ok {
		t.Errorf("batch[1] produced %T, want the ScanningStateMsg", batch[1]())
	}
}
