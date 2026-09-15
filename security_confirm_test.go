package main

import (
	"netpala/common"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	godbus "github.com/godbus/dbus/v5"
)

// safeListener pre-loads the signal channel with a signal that
// WaitForDBusSignal answers without touching D-Bus, so the commands returned
// by Update can actually be run in a test.
func safeListener(m NetpalaData) NetpalaData {
	ch := make(chan *godbus.Signal, 1)
	ch <- &godbus.Signal{Name: "org.freedesktop.NetworkManager.Device.Wireless.AccessPointAdded"}
	m.DBusSignals = ch
	return m
}

// securityModel builds a model sitting on the Security pane with one unit.
func securityModel(svc common.SecurityService) NetpalaData {
	m := linkModel(false, nil)
	m.SecurityData = []common.SecurityService{svc}
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 0
	return safeListener(m)
}

func selectKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

// startedAService reports whether a command batch carries an action alongside
// the re-armed listener. Running the outer batch yields a BatchMsg holding the
// children, so nothing underneath it is executed - which matters, because the
// children talk to D-Bus and there is no bus in a test.
//
// Only valid for the confirmation paths, which batch. The immediate-toggle
// paths return a bare command that cannot be run here, so those assert only
// that one was issued.
func startedAService(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	batch, ok := cmd().(tea.BatchMsg)
	return ok && len(batch) > 1
}

func i2p(active bool) common.SecurityService {
	return common.SecurityService{
		Name: "I2P", Unit: "i2pd.service", Active: active,
		Confirm: "Make this machine an I2P router?\n\nShared: your IP address.",
	}
}

// Starting a service that asks for consent must not start it yet.
func TestStartingAServiceWithConfirmAsksFirst(t *testing.T) {
	m := securityModel(i2p(false))

	next, cmd := m.Update(selectKey())
	got := next.(NetpalaData)

	if got.PopupState != 1 {
		t.Fatalf("PopupState = %d, want the confirmation popup", got.PopupState)
	}
	if got.confirmAction != confirmStartService {
		t.Error("popup opened without recording that it starts a service")
	}
	if got.pendingStartUnit != "i2pd.service" {
		t.Errorf("pendingStartUnit = %q, want i2pd.service", got.pendingStartUnit)
	}
	if !strings.Contains(got.Confirmation.Message, "Shared:") {
		t.Errorf("consent text not shown, got %q", got.Confirmation.Message)
	}
	if cmd != nil {
		t.Error("a command was issued before the user answered")
	}
}

func TestConfirmingStartsTheService(t *testing.T) {
	m := securityModel(i2p(false))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	got := next.(NetpalaData)

	if !startedAService(t, cmd) {
		t.Error("confirming did not start the service")
	}
	if got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
	if got.pendingStartUnit != "" {
		t.Error("pending unit not cleared")
	}
}

func TestCancellingDoesNotStartTheService(t *testing.T) {
	m := securityModel(i2p(false))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: false})
	got := next.(NetpalaData)

	if startedAService(t, cmd) {
		t.Error("cancelling started the service anyway")
	}
	if got.pendingStartUnit != "" {
		t.Error("pending unit not cleared after cancelling")
	}
}

// Consent is for starting. Turning something off needs none, and demanding it
// would put a prompt between the user and switching a thing back off.
func TestStoppingNeverAsks(t *testing.T) {
	m := securityModel(i2p(true)) // already running

	next, cmd := m.Update(selectKey())
	if got := next.(NetpalaData); got.PopupState == 1 {
		t.Error("stopping a service asked for confirmation")
	}
	if cmd == nil {
		t.Error("stopping issued no command at all")
	}
}

func TestServiceWithoutConfirmTogglesImmediately(t *testing.T) {
	m := securityModel(common.SecurityService{Name: "DNSCrypt", Unit: "dnscrypt-proxy.service"})

	next, cmd := m.Update(selectKey())
	if got := next.(NetpalaData); got.PopupState == 1 {
		t.Error("a service with no consent text still prompted")
	}
	if cmd == nil {
		t.Error("no toggle issued")
	}
}

// The prompt can sit on screen for a while. If the unit is started from
// systemctl in the meantime, acting on the stale row would stop the very
// thing the user just agreed to start.
func TestConfirmIgnoresAUnitThatStartedMeanwhile(t *testing.T) {
	m := securityModel(i2p(false))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	// A refresh lands while the prompt is up.
	m.SecurityData = []common.SecurityService{i2p(true)}

	_, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	if startedAService(t, cmd) {
		t.Error("toggled a unit that was already running; that would stop it")
	}
}

// A unit that vanished from the pane entirely must not be acted on either.
func TestConfirmIgnoresAVanishedUnit(t *testing.T) {
	m := securityModel(i2p(false))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	m.SecurityData = nil

	_, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	if startedAService(t, cmd) {
		t.Error("acted on a unit that is no longer present")
	}
}

// The popup is shared with network deletion, so that path must still work.
func TestDeleteConfirmationStillDeletes(t *testing.T) {
	m := linkModel(false, []common.KnownNetwork{net("home", false, common.DNSModeDHCP)})
	m = safeListener(m)
	m.selectedBox = common.PaneKnown
	m.SelectedEntry = 0

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := next.(NetpalaData)
	if got.PopupState != 1 {
		t.Fatalf("PopupState = %d, want the confirmation popup", got.PopupState)
	}
	if got.confirmAction != confirmDeleteNetwork {
		t.Fatal("delete opened the popup without recording that it deletes")
	}

	_, cmd := got.Update(common.SubmitConfirmationMsg{Value: true})
	if !startedAService(t, cmd) { // same shape: a batch of action + listener
		t.Error("confirming a delete issued no delete command")
	}
}
