package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Shared wait/poll helpers for the e2e suite, built on testutil.Eventually /
// testutil.Consistently so deadline handling and failure messaging stay
// uniform across tests.

// agentSessionTags matches the tmux sessions amux creates for agent tabs.
var agentSessionTags = map[string]string{
	"@amux":      "1",
	"@amux_type": "agent",
}

// lazyString defers rendering of debug output until a wait helper actually
// fails, so timeout messages show the state observed at the deadline rather
// than at call time.
type lazyString func() string

func (f lazyString) String() string { return f() }

func waitForUIContains(t *testing.T, session *PTYSession, needle string, timeout time.Duration) {
	t.Helper()
	if err := session.WaitForContains(needle, timeout); err != nil {
		t.Fatalf("waiting for %q: %v", needle, err)
	}
}

// waitForUIConsistentlyAbsent waits until needle has remained absent for the
// full stableFor window. This is for async UI paths where a transient blank or
// reload frame should not be mistaken for the settled state.
func waitForUIConsistentlyAbsent(t *testing.T, session *PTYSession, needle string, timeout, stableFor time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var stableSince time.Time
	lastScreen := session.ScreenASCII()
	for time.Now().Before(deadline) {
		lastScreen = session.ScreenASCII()
		if stringsContains(lastScreen, needle) {
			stableSince = time.Time{}
		} else {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= stableFor {
				return
			}
		}
		time.Sleep(screenPollInterval)
	}
	t.Fatalf("timeout waiting for %q to stay absent for %s\n\nScreen:\n%s", needle, stableFor, lastScreen)
}

// waitForCond polls cond until it returns true, failing the test with the
// formatted message on timeout.
func waitForCond(t *testing.T, cond func() bool, timeout time.Duration, msgf string, args ...any) {
	t.Helper()
	testutil.Eventually(t, timeout, condPollInterval, cond, msgf, args...)
}

// waitForFileBytes polls path until it contains want, returning the last-read
// contents and whether want was found.
func waitForFileBytes(path string, want []byte, timeout time.Duration) ([]byte, bool) {
	var last []byte
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := readFileBytes(path); err == nil {
			last = b
			if bytes.Contains(b, want) {
				return b, true
			}
		}
		time.Sleep(condPollInterval)
	}
	return last, false
}

func readFileBytes(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.Base(path))
}

func waitForAgentSessions(t *testing.T, opts tmux.Options, timeout time.Duration) []string {
	t.Helper()
	var sessions []string
	testutil.Eventually(t, timeout, sessionPollInterval, func() bool {
		got, err := tmux.ListSessionsMatchingTags(agentSessionTags, opts)
		if err != nil || len(got) == 0 {
			return false
		}
		sessions = got
		return true
	}, "timeout waiting for agent sessions\n%s", lazyString(func() string { return tmuxSessionDebug(opts) }))
	return sessions
}

func waitForNoAgentSessionsForWorkspace(t *testing.T, opts tmux.Options, wsID string, timeout time.Duration) {
	t.Helper()
	tags := map[string]string{
		"@amux":           "1",
		"@amux_type":      "agent",
		"@amux_workspace": wsID,
	}
	testutil.Eventually(t, timeout, sessionPollInterval, func() bool {
		sessions, err := tmux.ListSessionsMatchingTags(tags, opts)
		return err == nil && len(sessions) == 0
	}, "timeout waiting for agent sessions for workspace %s to be torn down\n%s", wsID, lazyString(func() string { return tmuxSessionDebug(opts) }))
}

func waitForTerminalSessionCount(t *testing.T, opts tmux.Options, wsID string, count int, timeout time.Duration) {
	t.Helper()
	testutil.Eventually(t, timeout, sessionPollInterval, func() bool {
		sessions, err := tmux.ListSessionsMatchingTags(map[string]string{
			"@amux":           "1",
			"@amux_type":      "terminal",
			"@amux_workspace": wsID,
		}, opts)
		return err == nil && len(sessions) == count
	}, "timeout waiting for %d terminal sessions for workspace %s", count, wsID)
}

func waitForAssistantSessions(t *testing.T, opts tmux.Options, want map[string]bool, timeout time.Duration) map[string][]string {
	t.Helper()
	var result map[string][]string
	testutil.Eventually(t, timeout, sessionPollInterval, func() bool {
		rows, err := tmux.SessionsWithTags(agentSessionTags, []string{"@amux_assistant"}, opts)
		if err != nil {
			return false
		}
		byAssistant := make(map[string][]string)
		for _, row := range rows {
			assistant := strings.TrimSpace(row.Tags["@amux_assistant"])
			if assistant == "" {
				continue
			}
			byAssistant[assistant] = append(byAssistant[assistant], row.Name)
		}
		for assistant := range want {
			if len(byAssistant[assistant]) == 0 {
				return false
			}
		}
		result = byAssistant
		return true
	}, "timeout waiting for assistant sessions: %v\n%s", want, lazyString(func() string { return tmuxSessionDebug(opts) }))
	return result
}

func waitForSessionTypes(t *testing.T, opts tmux.Options, want map[string]bool, timeout time.Duration) {
	t.Helper()
	prefix := tmux.SessionName("amux") + "-"
	testutil.Eventually(t, timeout, sessionPollInterval, func() bool {
		sessions, err := tmux.ListSessionsMatchingTags(map[string]string{"@amux": "1"}, opts)
		if err != nil {
			// Tag listing can fail transiently on older tmux; fall back to a
			// session-name prefix count so the wait still terminates.
			return hasSessionsWithPrefix(t, opts, prefix, len(want))
		}
		if len(sessions) == 0 {
			return false
		}
		types := map[string]bool{}
		for _, session := range sessions {
			value, err := tmux.SessionTagValue(session, "@amux_type", opts)
			if err != nil {
				continue
			}
			types[strings.TrimSpace(value)] = true
		}
		for typ := range want {
			if !types[typ] {
				return false
			}
		}
		return true
	}, "timeout waiting for tmux session types %v", want)
}

func assertAgentSessionsStayLive(t *testing.T, opts tmux.Options, duration time.Duration) {
	t.Helper()
	testutil.Consistently(t, duration, sessionPollInterval, func() string {
		sessions, err := tmux.ListSessionsMatchingTags(agentSessionTags, opts)
		if err != nil {
			return "" // transient tmux error: keep watching
		}
		if len(sessions) == 0 {
			return "expected at least one agent session to stay alive"
		}
		for _, name := range sessions {
			state, err := tmux.SessionStateFor(name, opts)
			if err != nil {
				continue
			}
			if state.Exists && state.HasLivePane {
				return ""
			}
		}
		return fmt.Sprintf("agent sessions not live: %v", sessions)
	})
}

func assertScreenNeverContains(t *testing.T, session *PTYSession, needles []string, duration time.Duration) {
	t.Helper()
	testutil.Consistently(t, duration, screenPollInterval, func() string {
		screen := session.ScreenASCII()
		for _, needle := range needles {
			if stringsContains(screen, needle) {
				return fmt.Sprintf("unexpected screen text %q\n\nScreen:\n%s", needle, screen)
			}
		}
		return ""
	})
}

func hasSessionsWithPrefix(t *testing.T, opts tmux.Options, prefix string, minCount int) bool {
	t.Helper()
	sessions, err := tmux.ListSessions(opts)
	if err != nil {
		return false
	}
	count := 0
	for _, name := range sessions {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count >= minCount
}

func tmuxSessionDebug(opts tmux.Options) string {
	rows, err := tmux.SessionsWithTags(map[string]string{}, []string{
		"@amux",
		"@amux_type",
		"@amux_assistant",
		"@amux_workspace",
		"@amux_tab",
	}, opts)
	if err != nil {
		return fmt.Sprintf("tmux sessions: error=%v", err)
	}
	if len(rows) == 0 {
		return "tmux sessions: none"
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf(
			"%s amux=%q type=%q assistant=%q workspace=%q tab=%q",
			row.Name,
			row.Tags["@amux"],
			row.Tags["@amux_type"],
			row.Tags["@amux_assistant"],
			row.Tags["@amux_workspace"],
			row.Tags["@amux_tab"],
		))
	}
	return "tmux sessions:\n" + strings.Join(lines, "\n")
}
