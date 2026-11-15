package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"netpala/common"
	"netpala/dbus"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

// apSignal is the one signal WaitForDBusSignal answers without touching
// D-Bus: it asks for the scan debounce rather than reading NetworkManager.
func apSignal() *godbus.Signal {
	return &godbus.Signal{Name: "org.freedesktop.NetworkManager.Device.Wireless.AccessPointAdded"}
}

// One signal must arm exactly one reader. Both the DeviceUpdateMsg and the
// KnownNetworksUpdateMsg handler used to return a listener, so the batch a
// device signal produces armed two, and every state change during a connect
// left another reader parked on the channel for the rest of the session.
func TestOneSignalArmsOneReader(t *testing.T) {
	ch := make(chan *godbus.Signal, 8)
	for range 8 {
		ch <- apSignal()
	}

	msg := dbus.WaitForDBusSignal(nil, ch)()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("got %T, want a tea.BatchMsg", msg)
	}
	if len(ch) != 7 {
		t.Errorf("%d signals left of 8; one call consumed %d", len(ch), 8-len(ch))
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want the re-arm plus one refresh", len(batch))
	}

	// The re-arm leads the batch, so the next reader is waiting while the
	// refreshes are still in flight rather than after one of them lands.
	if got := batch[0](); !isBatchOf(got, 2) {
		t.Errorf("batch[0] produced %T; it should be the listener, which "+
			"consumes a signal and returns the same shape again", got)
	}
	if len(ch) != 6 {
		t.Errorf("running the re-arm left %d signals, want 6", len(ch))
	}

	// And the other member is the refresh, which must not read the channel.
	switch got := batch[1]().(type) {
	case common.ScannedNetworksUpdateMsg:
		if got != nil {
			t.Errorf("batch[1] produced %#v, want the nil scan trigger", got)
		}
	default:
		t.Errorf("batch[1] produced %T, want a ScannedNetworksUpdateMsg", got)
	}
	if len(ch) != 6 {
		t.Errorf("the refresh consumed a signal; only the listener may")
	}
}

func isBatchOf(msg tea.Msg, n int) bool {
	b, ok := msg.(tea.BatchMsg)
	return ok && len(b) == n
}

// The handlers must not arm one of their own any more.
func TestDataHandlersDoNotArmAListener(t *testing.T) {
	m := withData(0, 0, 0, 0, 1)
	m.DBusSignals = make(chan *godbus.Signal, 1)
	m.DBusSignals <- apSignal()

	for _, tt := range []struct {
		name string
		msg  tea.Msg
	}{
		{"device", common.DeviceUpdateMsg{{Name: "wlan0"}}},
		{"known networks", common.KnownNetworksUpdateMsg{{SSID: "home"}}},
		{"scan results", common.ScannedNetworksUpdateMsg{{SSID: "cafe"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd, handled := m.handleDataMsg(tt.msg)
			if !handled {
				t.Fatal("not handled as a data message")
			}
			if cmd != nil {
				t.Error("handler returned a command; the listener re-arms itself now")
			}
			if len(m.DBusSignals) != 1 {
				t.Error("handler read from the signal channel")
			}
		})
	}
}

// The listener is armed once, at startup, and keeps itself going. Anything
// else arming one adds a second reader that never goes away.
func TestListenerIsArmedInExactlyOnePlace(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "netpala.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var at []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WaitForDBusSignal" {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "dbus" {
			at = append(at, fset.Position(call.Pos()).String())
		}
		return true
	})

	if len(at) != 1 {
		t.Errorf("dbus.WaitForDBusSignal is called %d times, want 1 (Init): %v",
			len(at), at)
	}
}

// The tick has to go through the data funnel. Handled in the main switch it
// never reached its own handler while a popup was open -- every popup forwards
// what it does not recognise to its own model and returns -- and that handler
// is the only thing that re-arms the timer. tea.Tick fires once, so the
// periodic refresh died for the session the first time a prompt was up when
// it landed, which connecting to a network guarantees.
func TestPeriodicRefreshSurvivesAnOpenPopup(t *testing.T) {
	m := linkModel(false, nil) // needs a Config: the tick re-reads the VPN providers

	_, cmd, handled := m.handleDataMsg(common.PeriodicRefreshMsg{})
	if !handled {
		t.Fatal("PeriodicRefreshMsg is not a data message; a popup will swallow it")
	}
	if cmd == nil {
		t.Error("the tick issued no refresh, and nothing re-armed the timer")
	}
}
