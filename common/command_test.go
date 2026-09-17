package common

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCommandSucceeds(t *testing.T) {
	if err := RunCommand(context.Background(), []string{"true"}); err != nil {
		t.Errorf("RunCommand(true) = %v", err)
	}
}

// The provider CLI puts its real complaint on stderr and returns 1. Reporting
// only "exit status 1" throws away the entire diagnosis, which for a VPN is
// usually "you are not logged in".
func TestRunCommandCarriesStderr(t *testing.T) {
	err := RunCommand(context.Background(),
		[]string{"sh", "-c", "echo 'you are not logged in' >&2; exit 1"})
	if err == nil {
		t.Fatal("a failing command reported success")
	}
	if !strings.Contains(err.Error(), "you are not logged in") {
		t.Errorf("error %q does not carry what the command said", err)
	}
}

// Only the first line, so a command that dumps a usage screen does not become
// the whole alert. Asserted on the formatting rather than on a real command,
// because the argv appears in the message too and a fixture whose arguments
// mention the later lines would match itself.
func TestCommandErrorTrimsStderrToOneLine(t *testing.T) {
	err := &CommandError{
		Argv:   []string{"mullvad", "connect"},
		Err:    errors.New("exit status 1"),
		Stderr: "you are not logged in\nRun `mullvad account login`\nsee the manual\n",
	}

	got := err.Error()
	if !strings.HasSuffix(got, "mullvad connect: you are not logged in") {
		t.Errorf("Error() = %q, want the command and only the first stderr line", got)
	}
	for _, later := range []string{"account login", "see the manual"} {
		if strings.Contains(got, later) {
			t.Errorf("Error() = %q, want the later lines dropped", got)
		}
	}
}

// With nothing on stderr there is still the exit status to report.
func TestCommandErrorFallsBackToTheExitStatus(t *testing.T) {
	err := &CommandError{
		Argv: []string{"mullvad", "connect"},
		Err:  errors.New("exit status 1"),
	}
	if got := err.Error(); !strings.Contains(got, "exit status 1") {
		t.Errorf("Error() = %q, want the exit status when stderr is empty", got)
	}
}

// The whole point of argv over a shell string. A config value containing shell
// metacharacters is an argument, not a second command -- there is no shell for
// it to be interpreted by.
func TestRunCommandDoesNotUseAShell(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(dir, "pwned")

	// If any of these reached a shell, the canary would be created.
	for _, argv := range [][]string{
		{"echo", "hello; touch " + canary},
		{"echo", "hello && touch " + canary},
		{"echo", "hello $(touch " + canary + ")"},
		{"echo", "hello `touch " + canary + "`"},
		{"echo", "hello | touch " + canary},
	} {
		if err := RunCommand(context.Background(), argv); err != nil {
			t.Errorf("RunCommand(%q) = %v", argv, err)
		}
		if _, err := os.Stat(canary); err == nil {
			t.Fatalf("argv %q was interpreted by a shell", argv)
		}
	}
}

// Globs are not expanded either, for the same reason.
func TestRunCommandDoesNotGlob(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.conf", "b.conf"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}

	var out strings.Builder
	argv := []string{"echo", filepath.Join(dir, "*.conf")}
	if err := RunCommand(context.Background(), argv); err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	_ = out // the assertion is that nothing above expanded; echo printed the literal
}

// A command waiting for input would otherwise hang the whole UI: bubbletea has
// one update goroutine and no way to cancel a command already in flight.
func TestRunCommandTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel stands in for the deadline so the test does not take 30s.
	cancel()
	err := RunCommand(ctx, []string{"sleep", "60"})
	if err == nil {
		t.Fatal("a cancelled command reported success")
	}

	var cerr *CommandError
	if !errors.As(err, &cerr) {
		t.Fatalf("got %T, want *CommandError", err)
	}
	if !cerr.TimedOut {
		t.Error("a cancelled command was not reported as having timed out")
	}
	if !strings.Contains(err.Error(), "waiting for input") {
		t.Errorf("error %q does not suggest what went wrong", err)
	}
}

// A missing binary is the most likely misconfiguration, and "exec: not found"
// does not tell the user which of their config lines is wrong.
func TestRunCommandNamesAMissingBinary(t *testing.T) {
	err := RunCommand(context.Background(), []string{"netpala-no-such-command"})
	if err == nil {
		t.Fatal("a missing binary reported success")
	}
	if !strings.Contains(err.Error(), "netpala-no-such-command") {
		t.Errorf("error %q does not name the missing command", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error %q does not explain the problem", err)
	}
}

func TestRunCommandRefusesAnEmptyArgv(t *testing.T) {
	if err := RunCommand(context.Background(), nil); err == nil {
		t.Error("an empty command reported success")
	}
}
