package common

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Running a command named in the configuration file.
//
// netpala talks to NetworkManager and systemd over D-Bus everywhere else, and
// deliberately: a D-Bus call has a typed interface, a defined error and a
// polkit policy behind it. This is the one exception in the whole program, and
// it exists because vendor VPN daemons have no D-Bus API. Starting
// mullvad-daemon connects nothing; `mullvad connect` does.
//
// Two properties keep the exception from being worse than it has to be:
//
//   - It is argv, never a shell string. Nothing is word-split, globbed or
//     interpolated, so a config value containing a semicolon is an argument
//     rather than a second command. There is no shell to inject into.
//   - It has a deadline. `tailscale up` on an unauthenticated node prints a
//     URL and waits for a browser. Bubbletea runs the update loop on one
//     goroutine with no way to cancel a command already in flight, so without
//     a deadline that hangs the entire UI with no way out but SIGKILL.

// CommandTimeout bounds a provider command. Generous, because bringing a
// tunnel up genuinely can take several seconds on a slow link, but finite.
const CommandTimeout = 30 * time.Second

// CommandError carries what the command actually said.
//
// A provider CLI puts its real complaint on stderr and returns 1, so reporting
// only "exit status 1" throws away the entire diagnosis -- which for a VPN is
// usually "you are not logged in" or "no account configured".
type CommandError struct {
	Argv     []string
	Err      error
	Stderr   string
	TimedOut bool
}

func (e *CommandError) Error() string {
	name := strings.Join(e.Argv, " ")
	if e.TimedOut {
		return fmt.Sprintf("%s did not finish within %s - it may be waiting for input", name, CommandTimeout)
	}
	if detail := firstLine(e.Stderr); detail != "" {
		return fmt.Sprintf("%s: %s", name, detail)
	}
	return fmt.Sprintf("%s: %v", name, e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

// RunCommand executes argv directly, with no shell.
func RunCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("no command configured")
	}

	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()

	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return nil
	}

	// Distinguish "the command said no" from "the command never answered".
	// They need different things from the user.
	if ctx.Err() != nil {
		return &CommandError{Argv: argv, Err: ctx.Err(), TimedOut: true}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return &CommandError{Argv: argv, Err: err,
			Stderr: fmt.Sprintf("%s is not installed or not on PATH", argv[0])}
	}
	return &CommandError{Argv: argv, Err: err, Stderr: stderr.String()}
}

// firstLine trims a multi-line stderr down to something that fits an alert.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
