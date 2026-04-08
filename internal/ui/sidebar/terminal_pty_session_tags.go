package sidebar

import (
	"errors"
	"fmt"
	"strconv"
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

func terminalTagChecks(tags tmux.SessionTags) []struct {
	key  string
	want string
} {
	checks := []struct {
		key  string
		want string
	}{
		{key: "@amux", want: "1"},
	}
	if strings.TrimSpace(tags.WorkspaceID) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_workspace", want: strings.TrimSpace(tags.WorkspaceID)})
	}
	if strings.TrimSpace(tags.TabID) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_tab", want: strings.TrimSpace(tags.TabID)})
	}
	if strings.TrimSpace(tags.Type) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_type", want: strings.TrimSpace(tags.Type)})
	}
	if strings.TrimSpace(tags.Assistant) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_assistant", want: strings.TrimSpace(tags.Assistant)})
	}
	// CreatedAt is optional for reattach paths; SessionOwner/LeaseAtMS remain the
	// primary freshness/ownership tags for those sessions.
	if tags.CreatedAt > 0 {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_created_at", want: strconv.FormatInt(tags.CreatedAt, 10)})
	}
	if strings.TrimSpace(tags.InstanceID) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: "@amux_instance", want: strings.TrimSpace(tags.InstanceID)})
	}
	if strings.TrimSpace(tags.SessionOwner) != "" {
		checks = append(checks, struct {
			key  string
			want string
		}{key: tmux.TagSessionOwner, want: strings.TrimSpace(tags.SessionOwner)})
	}
	if tags.LeaseAtMS > 0 {
		checks = append(checks, struct {
			key  string
			want string
		}{key: tmux.TagSessionLeaseAt, want: strconv.FormatInt(tags.LeaseAtMS, 10)})
	}
	return checks
}
