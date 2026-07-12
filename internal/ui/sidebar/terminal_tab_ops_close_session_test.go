package sidebar

// Tests for the closeSessionIfUnattached kill guard in terminal_tab_ops.go.
// They live in their own file because terminal_tab_ops_test.go is at the
// 500-line lint limit. The guard's kill-vs-keep decision (has clients → keep;
// error → keep; deadline → keep; unattached → kill) is exercised tmux-free by
// swapping the package-var seams for fakes; the empty-session no-op stays in
// terminal_tab_ops_test.go.

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// swapCloseSessionSeams installs fakes for closeSessionIfUnattached's seam
// vars and restores the production defaults when the test ends. The sleep is
// a no-op and the poll deadline is zero so the deadline branch is reachable
// without any real waiting.
func swapCloseSessionSeams(t *testing.T, hasClients func(string, tmux.Options) (bool, error), kill func(string, tmux.Options) error) {
	t.Helper()
	prevHasClients := closeSessionHasClientsFn
	prevKill := closeKillSessionFn
	prevSleep := closeSessionSleep
	prevDeadline := closeSessionPollDeadline
	t.Cleanup(func() {
		closeSessionHasClientsFn = prevHasClients
		closeKillSessionFn = prevKill
		closeSessionSleep = prevSleep
		closeSessionPollDeadline = prevDeadline
	})
	closeSessionHasClientsFn = hasClients
	closeKillSessionFn = kill
	closeSessionSleep = func(time.Duration) {}
	closeSessionPollDeadline = 0
}

func TestCloseSessionIfUnattachedDecision(t *testing.T) {
	tests := []struct {
		name       string
		hasClients func(string, tmux.Options) (bool, error)
		wantKill   bool
	}{
		{
			// No clients attached: the session is orphaned and must be killed.
			name:       "unattached session is killed",
			hasClients: func(string, tmux.Options) (bool, error) { return false, nil },
			wantKill:   true,
		},
		{
			// Clients stay attached past the poll deadline: the session is shared
			// (another amux instance or the user) and must be kept alive.
			name:       "attached session past deadline is kept",
			hasClients: func(string, tmux.Options) (bool, error) { return true, nil },
			wantKill:   false,
		},
		{
			// Client query failed: fail closed and keep the session.
			name: "has-clients error keeps the session",
			hasClients: func(string, tmux.Options) (bool, error) {
				return false, errors.New("tmux exec failed")
			},
			wantKill: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var killed []string
			kill := func(name string, _ tmux.Options) error {
				killed = append(killed, name)
				return nil
			}
			swapCloseSessionSeams(t, tt.hasClients, kill)

			cmd := closeSessionIfUnattached("amux-session-abc", tmux.DefaultOptions())
			if cmd == nil {
				t.Fatal("expected a non-nil command")
			}
			if msg := cmd(); msg != nil {
				t.Fatalf("expected nil message, got %#v", msg)
			}

			if tt.wantKill {
				if len(killed) != 1 || killed[0] != "amux-session-abc" {
					t.Fatalf("expected exactly one KillSession(%q) call, got %v", "amux-session-abc", killed)
				}
			} else if len(killed) != 0 {
				t.Fatalf("expected the session to be kept alive, but KillSession was called: %v", killed)
			}
		})
	}
}

func TestCloseSessionIfUnattachedPollsUntilUnattached(t *testing.T) {
	// A session that starts attached and then loses its last client within the
	// deadline is killed once the poll observes it unattached.
	calls := 0
	hasClients := func(string, tmux.Options) (bool, error) {
		calls++
		return calls < 3, nil // attached for two polls, then unattached
	}
	var killed []string
	kill := func(name string, _ tmux.Options) error {
		killed = append(killed, name)
		return nil
	}
	swapCloseSessionSeams(t, hasClients, kill)
	// Generous deadline (the fake sleep is instant) so the loop reaches poll
	// #3 instead of bailing out on the deadline branch.
	closeSessionPollDeadline = time.Hour

	if msg := closeSessionIfUnattached("amux-session-poll", tmux.DefaultOptions())(); msg != nil {
		t.Fatalf("expected nil message, got %#v", msg)
	}
	if calls != 3 {
		t.Fatalf("expected 3 SessionHasClients polls, got %d", calls)
	}
	if len(killed) != 1 || killed[0] != "amux-session-poll" {
		t.Fatalf("expected exactly one KillSession(%q) call, got %v", "amux-session-poll", killed)
	}
}
