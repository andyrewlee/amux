package sidebar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func verifyTerminalSessionTags(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
	const (
		verifyTimeout  = 2 * time.Second
		verifyInterval = 40 * time.Millisecond
	)
	deadline := time.Now().Add(verifyTimeout)
	var lastErr error
	for {
		lastErr = verifyTerminalSessionTagsOnce(sessionName, tags, opts)
		if lastErr == nil {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(verifyInterval)
	}
	if err := applyTerminalSessionTags(sessionName, tags, opts); err != nil {
		return fmt.Errorf("tmux tag verification failed (%w), retag failed: %w", lastErr, err)
	}
	if err := verifyTerminalSessionTagsOnce(sessionName, tags, opts); err != nil {
		return fmt.Errorf("tmux tag verification failed after retag: %w", err)
	}
	return nil
}

func verifyTerminalSessionTagsOnce(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
	if strings.TrimSpace(sessionName) == "" {
		return errors.New("missing tmux session name")
	}
	checks := terminalTagChecks(tags)
	for _, check := range checks {
		got, err := tmux.SessionTagValue(sessionName, check.key, opts)
		if err != nil {
			return fmt.Errorf("failed to verify tmux tag %s: %w", check.key, err)
		}
		got = strings.TrimSpace(got)
		if got != check.want {
			return fmt.Errorf("tmux tag mismatch for %s: expected %q, got %q", check.key, check.want, got)
		}
	}
	return nil
}

func applyTerminalSessionTags(sessionName string, tags tmux.SessionTags, opts tmux.Options) error {
	checks := terminalTagChecks(tags)
	for _, check := range checks {
		if err := tmux.SetSessionTagValue(sessionName, check.key, check.want, opts); err != nil {
			return err
		}
	}
	return nil
}

// terminalTagChecks resolves the verify/retag expectations from the single
// SessionTags→option mapping in internal/tmux — the same pairs session
// creation emits, so a new tag field cannot drift between emit and verify.
// Display tags (@amux_workspace_name/@amux_project) ride along, which is
// what lets a reattach retag self-heal stale names.
func terminalTagChecks(tags tmux.SessionTags) []struct {
	key  string
	want string
} {
	pairs := tmux.SessionTagPairs(tags)
	checks := make([]struct {
		key  string
		want string
	}, 0, len(pairs))
	for _, p := range pairs {
		checks = append(checks, struct {
			key  string
			want string
		}{key: p.Key, want: strings.TrimSpace(p.Value)})
	}
	return checks
}
