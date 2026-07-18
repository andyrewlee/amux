package sidebar

import (
	"fmt"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// tagCheck mirrors the anonymous {key, want} struct returned by
// terminalTagChecks so the table-driven assertions can name expected entries.
type tagCheck struct {
	key  string
	want string
}

// TestTerminalTagChecks exhaustively covers the pure tag-derivation logic:
// which @amux_* options are emitted for a given SessionTags, in what order, and
// with what (trimmed) values. terminalTagChecks never touches tmux, so it is
// fully deterministic.
func TestTerminalTagChecks(t *testing.T) {
	tests := []struct {
		name string
		tags tmux.SessionTags
		want []tagCheck
	}{
		{
			name: "empty tags emit only the marker",
			tags: tmux.SessionTags{},
			want: []tagCheck{{key: "@amux", want: "1"}},
		},
		{
			name: "whitespace-only string fields are dropped",
			tags: tmux.SessionTags{
				WorkspaceID: "   ",
				TabID:       "\t",
				Type:        " \n ",
				Assistant:   "",
				InstanceID:  "  ",
			},
			want: []tagCheck{{key: "@amux", want: "1"}},
		},
		{
			name: "non-positive numeric fields are dropped",
			tags: tmux.SessionTags{
				CreatedAt: 0,
				LeaseAtMS: 0,
			},
			want: []tagCheck{{key: "@amux", want: "1"}},
		},
		{
			name: "negative numeric fields are dropped",
			tags: tmux.SessionTags{
				CreatedAt: -1,
				LeaseAtMS: -42,
			},
			want: []tagCheck{{key: "@amux", want: "1"}},
		},
		{
			name: "boundary numeric values of one are included",
			tags: tmux.SessionTags{
				CreatedAt: 1,
				LeaseAtMS: 1,
			},
			want: []tagCheck{
				{key: "@amux", want: "1"},
				{key: "@amux_created_at", want: "1"},
				{key: tmux.TagSessionLeaseAt, want: "1"},
				{key: tmux.TagSessionOwnerHeartbeatAt, want: "1"},
			},
		},
		{
			name: "string fields are trimmed",
			tags: tmux.SessionTags{
				WorkspaceID: "  ws-1  ",
				TabID:       "\ttab-2\t",
				Type:        " agent ",
				Assistant:   "  claude  ",
				InstanceID:  " inst-9 ",
			},
			want: []tagCheck{
				{key: "@amux", want: "1"},
				{key: "@amux_workspace", want: "ws-1"},
				{key: "@amux_tab", want: "tab-2"},
				{key: "@amux_type", want: "agent"},
				{key: "@amux_assistant", want: "claude"},
				{key: "@amux_instance", want: "inst-9"},
			},
		},
		{
			name: "session owner is trimmed and keyed by the tmux constant",
			tags: tmux.SessionTags{
				SessionOwner: "  owner-x  ",
			},
			want: []tagCheck{
				{key: "@amux", want: "1"},
				{key: tmux.TagSessionOwner, want: "owner-x"},
			},
		},
		{
			name: "all fields populated emit every check in declared order",
			tags: tmux.SessionTags{
				WorkspaceID:  "ws-1",
				TabID:        "tab-2",
				Type:         "agent",
				Assistant:    "claude",
				CreatedAt:    1700000000,
				InstanceID:   "inst-9",
				SessionOwner: "owner-x",
				LeaseAtMS:    1700000000123,
			},
			want: []tagCheck{
				{key: "@amux", want: "1"},
				{key: "@amux_workspace", want: "ws-1"},
				{key: "@amux_tab", want: "tab-2"},
				{key: "@amux_type", want: "agent"},
				{key: "@amux_assistant", want: "claude"},
				{key: "@amux_created_at", want: "1700000000"},
				{key: "@amux_instance", want: "inst-9"},
				{key: tmux.TagSessionOwner, want: "owner-x"},
				{key: tmux.TagSessionLeaseAt, want: "1700000000123"},
				{key: tmux.TagSessionOwnerHeartbeatAt, want: "1700000000123"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := terminalTagChecks(tt.tags)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d checks, got %d: %+v", len(tt.want), len(got), got)
			}
			for i, want := range tt.want {
				if got[i].key != want.key {
					t.Fatalf("check %d: expected key %q, got %q", i, want.key, got[i].key)
				}
				if got[i].want != want.want {
					t.Fatalf("check %d (%s): expected value %q, got %q", i, want.key, want.want, got[i].want)
				}
			}
		})
	}
}

// TestTerminalTagChecks_AlwaysLeadsWithMarker guarantees the @amux marker is the
// first emitted check regardless of which optional fields are set, since callers
// rely on it to identify amux-owned sessions.
func TestTerminalTagChecks_AlwaysLeadsWithMarker(t *testing.T) {
	cases := []tmux.SessionTags{
		{},
		{WorkspaceID: "ws"},
		{SessionOwner: "owner", LeaseAtMS: 5},
		{CreatedAt: 99, InstanceID: "inst"},
	}
	for i, tags := range cases {
		checks := terminalTagChecks(tags)
		if len(checks) == 0 {
			t.Fatalf("case %d: expected at least the marker check", i)
		}
		if checks[0].key != "@amux" || checks[0].want != "1" {
			t.Fatalf("case %d: expected leading @amux=1, got %q=%q", i, checks[0].key, checks[0].want)
		}
	}
}

// TestTerminalTagChecks_FormatsLeaseTimestamp confirms a large positive lease
// timestamp is rendered with the same base-10 formatting the verifier reads
// back, guarding against an accidental formatting drift.
func TestTerminalTagChecks_FormatsLeaseTimestamp(t *testing.T) {
	const lease int64 = 1_700_000_000_999
	checks := terminalTagChecks(tmux.SessionTags{LeaseAtMS: lease})
	var got string
	var found bool
	for _, c := range checks {
		if c.key == tmux.TagSessionLeaseAt {
			got = c.want
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a %s check, got %+v", tmux.TagSessionLeaseAt, checks)
	}
	if want := strconv.FormatInt(lease, 10); got != want {
		t.Fatalf("expected lease value %q, got %q", want, got)
	}
}

// TestVerifyTerminalSessionTagsOnce_EmptySessionName covers the pure guard that
// rejects a blank session name before any tmux command runs.
func TestVerifyTerminalSessionTagsOnce_EmptySessionName(t *testing.T) {
	for _, name := range []string{"", "   ", "\t\n"} {
		err := verifyTerminalSessionTagsOnce(name, tmux.SessionTags{}, tmux.Options{})
		if err == nil {
			t.Fatalf("session name %q: expected an error, got nil", name)
		}
		if err.Error() != "missing tmux session name" {
			t.Fatalf("session name %q: unexpected error %q", name, err.Error())
		}
	}
}

// TestVerifyTerminalSessionTagsOnce_NonexistentSessionMismatch verifies that a
// session which does not exist reads back empty tag values (tmux exit 1 -> "")
// and therefore fails verification with a mismatch rather than a tmux error.
func TestVerifyTerminalSessionTagsOnce_NonexistentSessionMismatch(t *testing.T) {
	skipIfNoTmuxSidebar(t)
	opts := tmuxTestServer(t)

	err := verifyTerminalSessionTagsOnce("no-such-session-xyz", tmux.SessionTags{}, opts)
	if err == nil {
		t.Fatal("expected a mismatch error for a session with no tags")
	}
	// The @amux marker is expected to be "1" but the missing session yields "".
	if want := `tmux tag mismatch for @amux: expected "1", got ""`; err.Error() != want {
		t.Fatalf("unexpected error: got %q, want %q", err.Error(), want)
	}
}

// TestApplyAndVerifyTerminalSessionTags_Roundtrip writes every tag through
// applyTerminalSessionTags against a live tmux session, then confirms
// verifyTerminalSessionTagsOnce reads them all back successfully.
func TestApplyAndVerifyTerminalSessionTags_Roundtrip(t *testing.T) {
	skipIfNoTmuxSidebar(t)
	opts := tmuxTestServer(t)

	const session = "tags-roundtrip"
	createTmuxSession(t, opts, session)

	tags := tmux.SessionTags{
		WorkspaceID:  "ws-1",
		TabID:        "tab-2",
		Type:         "agent",
		Assistant:    "claude",
		CreatedAt:    1700000000,
		InstanceID:   "inst-9",
		SessionOwner: "owner-x",
		LeaseAtMS:    1700000000123,
	}

	if err := applyTerminalSessionTags(session, tags, opts); err != nil {
		t.Fatalf("applyTerminalSessionTags: %v", err)
	}
	if err := verifyTerminalSessionTagsOnce(session, tags, opts); err != nil {
		t.Fatalf("expected verification to pass after apply, got %v", err)
	}

	// Spot-check one tag landed verbatim through the real tmux read path.
	got, err := tmux.SessionTagValue(session, "@amux_workspace", opts)
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "ws-1" {
		t.Fatalf("expected @amux_workspace=ws-1, got %q", got)
	}
}

// TestApplyTerminalSessionTags_MinimalSession applies only the marker (empty
// optional fields) and confirms the single @amux tag is written and verified.
func TestApplyTerminalSessionTags_MinimalSession(t *testing.T) {
	skipIfNoTmuxSidebar(t)
	opts := tmuxTestServer(t)

	const session = "tags-minimal"
	createTmuxSession(t, opts, session)

	if err := applyTerminalSessionTags(session, tmux.SessionTags{}, opts); err != nil {
		t.Fatalf("applyTerminalSessionTags: %v", err)
	}
	if err := verifyTerminalSessionTagsOnce(session, tmux.SessionTags{}, opts); err != nil {
		t.Fatalf("expected marker-only verification to pass, got %v", err)
	}
	got, err := tmux.SessionTagValue(session, "@amux", opts)
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "1" {
		t.Fatalf("expected @amux=1, got %q", got)
	}
}

// TestVerifyTerminalSessionTags_RetagsMissingTags drives the full
// verify-then-recover flow: a live session that starts untagged must fail the
// initial probes, get retagged by applyTerminalSessionTags, and then pass the
// final verification.
func TestVerifyTerminalSessionTags_RetagsMissingTags(t *testing.T) {
	skipIfNoTmuxSidebar(t)
	opts := tmuxTestServer(t)

	const session = "tags-retag"
	createTmuxSession(t, opts, session)

	tags := tmux.SessionTags{
		WorkspaceID: "ws-retag",
		TabID:       "tab-retag",
		Type:        "agent",
	}

	// The session has no @amux_* tags yet, so verifyTerminalSessionTags should
	// exhaust its polling window, retag the session, and succeed on the
	// post-retag verification.
	if err := verifyTerminalSessionTags(session, tags, opts); err != nil {
		t.Fatalf("expected retag recovery to succeed, got %v", err)
	}

	// Confirm the recovery path actually wrote the tags.
	got, err := tmux.SessionTagValue(session, "@amux_workspace", opts)
	if err != nil {
		t.Fatalf("SessionTagValue: %v", err)
	}
	if got != "ws-retag" {
		t.Fatalf("expected @amux_workspace=ws-retag after retag, got %q", got)
	}
}

// TestVerifyTerminalSessionTags_PassesWhenAlreadyTagged confirms the fast path:
// a session already carrying the expected tags verifies on the first probe
// without needing the retag fallback.
func TestVerifyTerminalSessionTags_PassesWhenAlreadyTagged(t *testing.T) {
	skipIfNoTmuxSidebar(t)
	opts := tmuxTestServer(t)

	const session = "tags-prepared"
	createTmuxSession(t, opts, session)

	tags := tmux.SessionTags{WorkspaceID: "ws-ready", TabID: "tab-ready"}
	if err := applyTerminalSessionTags(session, tags, opts); err != nil {
		t.Fatalf("seed applyTerminalSessionTags: %v", err)
	}

	if err := verifyTerminalSessionTags(session, tags, opts); err != nil {
		t.Fatalf("expected verification to pass for an already-tagged session, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Real-tmux test helpers, scoped to the sidebar package. These mirror the
// helpers in internal/tmux but live here so the sidebar tests can drive an
// isolated server without importing test-only internals.
// ---------------------------------------------------------------------------

func skipIfNoTmuxSidebar(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// tmuxTestServer returns Options pointing at an isolated tmux server and
// registers a cleanup that kills the server when the test finishes.
func tmuxTestServer(t *testing.T) tmux.Options {
	t.Helper()
	name := fmt.Sprintf("amux-sidebar-test-%d", time.Now().UnixNano())
	opts := tmux.Options{
		ServerName:     name,
		ConfigPath:     "/dev/null",
		CommandTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", name, "kill-server").Run()
	})
	// Fork the isolated server by creating a keepalive session. A bare
	// start-server can exit immediately on some sandboxes, so we boot the daemon
	// with a detached session (which reliably keeps it alive) and then confirm
	// the socket is reachable. Skip if the environment cannot host a server.
	if out, err := exec.Command(
		"tmux", "-L", name, "-f", "/dev/null",
		"new-session", "-d", "-s", "_keepalive", "sh", "-c", "sleep 300",
	).CombinedOutput(); err != nil {
		t.Skipf("tmux server socket unavailable: %v\n%s", err, out)
	}
	if out, err := exec.Command("tmux", "-L", name, "-f", "/dev/null", "show-options", "-g").CombinedOutput(); err != nil {
		t.Skipf("tmux server socket unreachable: %v\n%s", err, out)
	}
	return opts
}

// createTmuxSession creates a detached, long-lived tmux session on the isolated
// server and waits briefly for the pane to settle.
func createTmuxSession(t *testing.T, opts tmux.Options, name string) {
	t.Helper()
	out, err := exec.Command(
		"tmux", "-L", opts.ServerName, "-f", "/dev/null",
		"new-session", "-d", "-s", name, "sh", "-c", "sleep 300",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("create session %q: %v\n%s", name, err, out)
	}
	time.Sleep(50 * time.Millisecond)
}
