package main

import (
	"netpala/common"
	"strings"
	"testing"
)

// resolverModel sits on the Security pane with the cursor on the DNSCrypt
// unit, with a live connection resolving through DHCP.
func resolverModel(networks []common.KnownNetwork) NetpalaData {
	m := linkModel(false, networks)
	m.selectedBox = common.PaneSecurity
	// linkModel lists Tor first, DNSCrypt second.
	m.SelectedEntry = 1
	return safeListener(m)
}

func home(connected bool, mode string) []common.KnownNetwork {
	return []common.KnownNetwork{net("home", connected, mode)}
}

// Starting the resolver rewrites the network profile and moves every lookup on
// the machine. That is more than the keypress said it would do, so it asks.
func TestStartingTheResolverAsksBeforeMovingDNS(t *testing.T) {
	m := resolverModel(home(true, common.DNSModeDHCP))

	next, cmd := m.Update(selectKey())
	got := next.(NetpalaData)

	if got.PopupState != 1 {
		t.Fatalf("PopupState = %d, want the confirmation popup", got.PopupState)
	}
	if got.confirmAction != confirmApplyResolver {
		t.Error("popup opened without recording that it is the DNS question")
	}
	if got.pendingStartUnit != "dnscrypt-proxy2.service" {
		t.Errorf("pendingStartUnit = %q", got.pendingStartUnit)
	}
	if cmd != nil {
		t.Error("the service was toggled before the user answered")
	}
}

// The prompt has to say that declining still starts the service, or "no" reads
// as "cancel" and nobody would ever pick it.
func TestResolverPromptSaysItStartsEitherWay(t *testing.T) {
	m := resolverModel(home(true, common.DNSModeDHCP))
	next, _ := m.Update(selectKey())

	msg := next.(NetpalaData).Confirmation.Message
	if !strings.Contains(msg, "home") {
		t.Errorf("prompt does not name the network: %q", msg)
	}
	if !strings.Contains(msg, "either way") {
		t.Errorf("prompt does not say the service starts regardless: %q", msg)
	}
}

func TestAcceptingPointsTheConnectionAtTheResolver(t *testing.T) {
	m := resolverModel(home(true, common.DNSModeDHCP))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	if !startedAService(t, cmd) {
		t.Error("accepting issued no command")
	}
	if got := next.(NetpalaData); got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
}

// Declining is an answer, not an abort. The resolver still starts -- running
// it for something else to use is a legitimate thing to want.
func TestDecliningStillStartsTheResolver(t *testing.T) {
	m := resolverModel(home(true, common.DNSModeDHCP))
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)

	next, cmd := m.Update(common.SubmitConfirmationMsg{Value: false})
	if !startedAService(t, cmd) {
		t.Error("declining the DNS question also skipped starting the service")
	}
	if got := next.(NetpalaData); got.PopupState != -1 {
		t.Errorf("PopupState = %d, want the popup closed", got.PopupState)
	}
}

// Nothing to decide when there is no connection to move, or when it already
// resolves through the unit. Asking anyway would be a prompt with one answer.
func TestNoQuestionWhenThereIsNothingToMove(t *testing.T) {
	for name, networks := range map[string][]common.KnownNetwork{
		"nothing connected":   home(false, common.DNSModeDHCP),
		"already on dnscrypt": home(true, common.DNSModeDNSCrypt),
		"no networks at all":  nil,
	} {
		m := resolverModel(networks)

		next, cmd := m.Update(selectKey())
		if got := next.(NetpalaData); got.PopupState == 1 {
			t.Errorf("%s: asked a question with only one answer", name)
		}
		if cmd == nil {
			t.Errorf("%s: the service was not started", name)
		}
	}
}

// Stopping is never gated, and the resolver has its own separate flow for
// moving DNS off itself first.
func TestStoppingTheResolverDoesNotAskTheDNSQuestion(t *testing.T) {
	m := linkModel(true, home(true, common.DNSModeDHCP)) // resolver running
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 1
	m = safeListener(m)

	next, _ := m.Update(selectKey())
	if got := next.(NetpalaData); got.confirmAction == confirmApplyResolver {
		t.Error("stopping the resolver asked whether to point DNS at it")
	}
}

// A unit that answers DNS and also has consent text asks twice: once for
// starting it, once for what that does to DNS. Folding the second into the
// first would hide it behind a "yes" about something else.
func TestConsentAndDNSAreSeparateQuestions(t *testing.T) {
	svc := common.SecurityService{
		Name: "DNSCrypt", Unit: "dnscrypt-proxy2.service",
		ProvidesDNS: true,
		Confirm:     "Send every lookup to a third party?",
	}

	m := linkModel(false, home(true, common.DNSModeDHCP))
	m.SecurityData = []common.SecurityService{svc}
	m.selectedBox = common.PaneSecurity
	m.SelectedEntry = 0
	m = safeListener(m)

	// First press: the consent prompt.
	next, _ := m.Update(selectKey())
	m = next.(NetpalaData)
	if m.confirmAction != confirmStartService {
		t.Fatalf("confirmAction = %d, want the consent prompt first", m.confirmAction)
	}

	// Accepting it must not start the unit yet -- it raises the DNS question.
	next, _ = m.Update(common.SubmitConfirmationMsg{Value: true})
	m = next.(NetpalaData)
	if m.PopupState != 1 || m.confirmAction != confirmApplyResolver {
		t.Fatalf("PopupState = %d, confirmAction = %d; want the DNS question next",
			m.PopupState, m.confirmAction)
	}

	// Answering that one gets on with it.
	_, cmd := m.Update(common.SubmitConfirmationMsg{Value: true})
	if !startedAService(t, cmd) {
		t.Error("answering both questions did not start the service")
	}
}
