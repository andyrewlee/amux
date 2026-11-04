package tmux

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/agentstatus"
)

// ---------------------------------------------------------------------------
// Pure parse half (no tmux required)
// ---------------------------------------------------------------------------

func TestParsePaneReportRows(t *testing.T) {
	t.Parallel()
	lines := []string{
		// Full row.
		"$1|blocked|question|my-agent|needs approval|1700000000000|0|amux-ws-abc",
		// No state: not a report.
		"$2|||||1700000000001|0|other",
		// Malformed: wrong field count and bad session id dropped.
		"$3|blocked",
		"nope|blocked|x|y|1|0|name",
		// Higher severity on the same session name wins the multi-pane fold.
		"$4|working||b||1700000000002|0|amux-ws-abc",
		// A dead pane carries the process-exit annotation.
		"$5|error||late|it died|1700000000003|1|dead-ws",
		// A live pane's lower-severity report must not resurrect a dead
		// pane's higher-severity one (the winner keeps its own flag).
		"$6|blocked|question|x|needs you|1700000000004|1|mixed-ws",
		"$7|working||y||1700000000005|0|mixed-ws",
	}
	got := parsePaneReportRows(lines)
	if len(got) != 3 {
		t.Fatalf("got %d reports, want 3 (no-state row must be skipped): %#v", len(got), got)
	}
	want := agentstatus.Report{State: "blocked", Kind: "question", App: "my-agent", Message: "needs approval", AtMS: 1700000000000}
	if got["amux-ws-abc"] != want {
		t.Fatalf("fold mismatch: got %#v want %#v", got["amux-ws-abc"], want)
	}
	mixed := agentstatus.Report{State: "blocked", Kind: "question", App: "x", Message: "needs you", AtMS: 1700000000004, PaneDead: true}
	if got["mixed-ws"] != mixed {
		t.Fatalf("dead-pane fold mismatch: got %#v want %#v", got["mixed-ws"], mixed)
	}
	if !got["dead-ws"].PaneDead {
		t.Fatalf("pane_dead must be parsed: %#v", got["dead-ws"])
	}
}

// ---------------------------------------------------------------------------
// Real-server integration: the report option round trip
// ---------------------------------------------------------------------------

func TestPaneReportRoundTrip(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	createSession(t, opts, "rep-agent", "sleep 300")

	first := agentstatus.Report{State: "blocked", Kind: "question", App: "my-agent", Message: "needs approval"}
	if err := WritePaneReport("rep-agent", first, opts); err != nil {
		t.Fatalf("first write: %v", err)
	}
	reports, err := PaneReports(opts)
	if err != nil {
		t.Fatalf("PaneReports: %v", err)
	}
	got, ok := reports["rep-agent"]
	if !ok {
		t.Fatalf("no report for rep-agent: %#v", reports)
	}
	if got.State != "blocked" || got.Kind != "question" || got.App != "my-agent" || got.Message != "needs approval" {
		t.Fatalf("round trip mismatch: %#v", got)
	}
	if got.AtMS == 0 {
		t.Fatal("write must stamp AtMS")
	}

	// An empty state is the release form: clears every option.
	if err := WritePaneReport("rep-agent", agentstatus.Report{}, opts); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	reports, _ = PaneReports(opts)
	if _, still := reports["rep-agent"]; still {
		t.Fatalf("empty-state write left a report behind: %#v", reports["rep-agent"])
	}
	reblocked := agentstatus.Report{State: "blocked"}
	if err := WritePaneReport("rep-agent", reblocked, opts); err != nil {
		t.Fatalf("reblock write: %v", err)
	}

	// Clear unsets everything.
	if err := ClearPaneReport("rep-agent", opts); err != nil {
		t.Fatalf("clear: %v", err)
	}
	reports, _ = PaneReports(opts)
	if _, still := reports["rep-agent"]; still {
		t.Fatalf("cleared report still present: %#v", reports["rep-agent"])
	}
}

// TestEnsureDetachedSessionInstallsWorkingTap proves the automatic install
// end-to-end: EnsureDetachedSession's script pipes the pane to this test
// binary (TestMain dispatches the tap argv), a program inside the pane
// emits OSC 7501 as real output, and the report lands as pane options —
// the exact chain a hosted run script exercises in production, including
// the emitter script trick so the escape never rides through the tty's
// caret-notation input echo.
func TestEnsureDetachedSessionInstallsWorkingTap(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	emitter := t.TempDir() + "/emit.sh"
	// Spec-exact OSC 7501: colon-separated pairs, msg base64 ("needs review").
	script := "#!/bin/sh\nsleep 1\nprintf '\\033]7501;state=blocked:kind=question:app=e2e-tap:msg=bmVlZHMgcmV2aWV3\\007'\nsleep 30\n"
	if err := os.WriteFile(emitter, []byte(script), 0o755); err != nil {
		t.Fatalf("write emitter: %v", err)
	}
	tags := SessionTags{WorkspaceID: "ws-tap-e2e", WorkspaceName: "tap-e2e", Type: "run"}
	if err := EnsureDetachedSession("tap-auto", t.TempDir(), emitter, nil, opts, tags); err != nil {
		t.Fatalf("EnsureDetachedSession: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		reports, err := PaneReports(opts)
		if err == nil {
			if r, ok := reports["tap-auto"]; ok && r.State == "blocked" && r.Kind == "question" &&
				r.App == "e2e-tap" && r.Message == "needs review" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("auto-installed tap never landed the OSC 7501 report")
}

// TestTapInstallSeparatedInAttachScript pins the tap fragment's separation
// inside the attach-session script: sh parses '}{ cmd; }' but silently never
// executes it, so a regressed separator uninstalls the tap while every exit
// status stays green. The ensure and create scripts carry the same invariant
// structurally and are proven live by the tap integration tests; the attach
// script is the one rendered as an inspectable PreparedCommand, so it gets
// the shape pin too.
func TestTapInstallSeparatedInAttachScript(t *testing.T) {
	t.Parallel()
	prepared, err := NewClientCommand("sep-attach", ClientCommandParams{
		WorkDir: t.TempDir(),
		Command: "sleep 300",
		Options: Options{ServerName: "amux", ConfigPath: "/dev/null"},
		Tags:    SessionTags{WorkspaceID: "ws-sep", TabID: "tab-sep", Type: "agent", Assistant: "claude"},
	})
	if err != nil {
		t.Fatalf("NewClientCommand: %v", err)
	}
	t.Cleanup(prepared.AbortBeforeStart)
	script := prepared.Command
	if !strings.Contains(script, "pipe-pane -o -O -t 'sep-attach'") {
		t.Fatalf("attach script lost its tap install:\n%s", script)
	}
	if i := strings.Index(script, "}{ "); i >= 0 {
		t.Fatalf("attach script glues a brace group at offset %d — sh would parse but never run it:\n%s", i, script)
	}
}

// TestCreateDetachedSessionInstallsWorkingTap proves the create-allocation
// path's tap install live: the same pipe-pane chain as the ensure path, but
// through createSessionScript's collision-preserving epilogue.
func TestCreateDetachedSessionInstallsWorkingTap(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	emitter := t.TempDir() + "/emit.sh"
	script := "#!/bin/sh\nsleep 1\nprintf '\\033]7501;state=error:app=e2e-alloc\\007'\nsleep 30\n"
	if err := os.WriteFile(emitter, []byte(script), 0o755); err != nil {
		t.Fatalf("write emitter: %v", err)
	}
	tags := SessionTags{WorkspaceID: "ws-tap-alloc", WorkspaceName: "tap-alloc", Type: "run"}
	if err := CreateDetachedSession("tap-alloc", t.TempDir(), emitter, nil, opts, tags); err != nil {
		t.Fatalf("CreateDetachedSession: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		reports, err := PaneReports(opts)
		if err == nil {
			if r, ok := reports["tap-alloc"]; ok && r.State == "error" && r.App == "e2e-alloc" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("create-path tap never landed the OSC 7501 report")
}

// TestEnsureDetachedSessionReportsFailure pins that a totally failed ensure
// is an error, not a silent success: the tap epilogue must preserve the
// script's exit status. A trailing `; true` once masked every failure —
// the run-session caller treated a launch that never happened as hosted
// and leaked its payload. Real tmux exits 0 for some total failures
// (socket creation among them), so the failing tmux here is a PATH shim
// that exits 1 for everything: every arm of the ensure chain fails, and
// the epilogue must carry that status out of the script.
func TestEnsureDetachedSessionReportsFailure(t *testing.T) {
	shimDir := t.TempDir()
	if err := os.WriteFile(shimDir+"/tmux", []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write tmux shim: %v", err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	opts := Options{ServerName: "amux-shim", ConfigPath: "/dev/null", CommandTimeout: 5 * time.Second}
	err := EnsureDetachedSession("tap-fail", t.TempDir(), "sleep 300", nil, opts, SessionTags{})
	if err == nil {
		t.Fatal("a failed ensure must surface an error, not nil")
	}
	// The error must come from the script exec itself (the epilogue carried
	// the failure out), not from launch preparation.
	if !strings.Contains(err.Error(), "ensure detached session tap-fail") {
		t.Fatalf("error did not come from the script exec: %v", err)
	}
}

// TestCreateDetachedSessionCollisionSkipsTap pins the collision path: when
// the name belongs to a foreign session, the allocation fails with
// ErrSessionNameTaken and the epilogue's tap install is skipped — piping a
// foreign session's output through this instance's helper (or stamping
// options onto its panes) is not best-effort, it is touching a session
// this call does not own. The collision exit fires inside the race
// subshell, so the epilogue needs an explicit case guard to see it.
func TestCreateDetachedSessionCollisionSkipsTap(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)
	// A squatter session that looks taken to the create's race branch.
	createSession(t, opts, "tap-taken", "sleep 300")

	err := CreateDetachedSession("tap-taken", t.TempDir(), "sleep 300", nil, opts,
		SessionTags{WorkspaceID: "ws-tap-taken", WorkspaceName: "tap-taken", Type: "run"})
	if err == nil || !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("create on a taken name must report ErrSessionNameTaken, got %v", err)
	}
	// The foreign session must not have gained a pipe-pane tap.
	showCmd, cancelShow := tmuxCommand(opts, "show-options", "-p", "-t", "tap-taken", "pipe-pane")
	defer cancelShow()
	out, pipeErr := runTmuxCmd(showCmd)
	if pipeErr == nil && strings.TrimSpace(string(out)) != "" && strings.TrimSpace(string(out)) != "0" {
		t.Fatalf("collision path installed a tap on the foreign session: %q", string(out))
	}
}

// TestTapWritesToReportingPaneAfterSplit pins the tap's pane binding: the
// pipe reads the install-time pane, and the helper must write to that same
// pane — not the session's current active pane. After a split (or any
// active-pane switch), a session-targeted write would stamp the reporting
// pane's status onto a pane that never reported, and pane_dead attribution
// would drift. The install bakes '#{pane_id}' (format-expanded by
// pipe-pane), so read and write cannot diverge.
func TestTapWritesToReportingPaneAfterSplit(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	emitter := t.TempDir() + "/emit.sh"
	// Emit after the split below has landed, so the active pane is no
	// longer the reporting pane when the report arrives.
	script := "#!/bin/sh\nsleep 2\nprintf '\\033]7501;state=working:app=split-test\\007'\nsleep 30\n"
	if err := os.WriteFile(emitter, []byte(script), 0o755); err != nil {
		t.Fatalf("write emitter: %v", err)
	}
	tags := SessionTags{WorkspaceID: "ws-tap-split", WorkspaceName: "tap-split", Type: "run"}
	if err := EnsureDetachedSession("tap-split", t.TempDir(), emitter, nil, opts, tags); err != nil {
		t.Fatalf("EnsureDetachedSession: %v", err)
	}
	// Split: the new pane becomes the session's active pane.
	splitCmd, cancelSplit := tmuxCommand(opts, "split-window", "-t", "tap-split", "-h", "sleep", "300")
	defer cancelSplit()
	if out, err := runTmuxCmd(splitCmd); err != nil {
		t.Fatalf("split-window: %v (%s)", err, string(out))
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		listCmd, cancelList := tmuxCommand(opts, "list-panes", "-t", "tap-split", "-F",
			"#{pane_id}|#{@amux_reported_state}|#{pane_active}")
		out, err := runTmuxCmd(listCmd)
		cancelList()
		if err != nil {
			t.Fatalf("list-panes: %v", err)
		}
		rows := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(rows) != 2 {
			t.Fatalf("want 2 panes after split, got %d: %q", len(rows), string(out))
		}
		reported, active := 0, 0
		for _, row := range rows {
			fields := strings.Split(row, "|")
			if len(fields) != 3 {
				t.Fatalf("bad pane row %q", row)
			}
			if fields[1] != "" {
				reported++
			}
			if fields[2] == "1" {
				active++
			}
		}
		// Exactly one pane reported, and it cannot be the active one: the
		// split made the new pane active before the report arrived.
		if reported == 1 && active == 1 {
			for _, row := range rows {
				fields := strings.Split(row, "|")
				if fields[1] != "" && fields[2] == "1" {
					t.Fatalf("report landed on the ACTIVE pane %s — write target not pinned: %q", fields[0], string(out))
				}
			}
			return // exactly one report, on a non-active pane
		}
		if reported > 1 {
			t.Fatalf("report stamped onto multiple panes: %q", string(out))
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("tap never landed the report after the split")
}

// TestStatusTapCommandPinned pins the rendered installer: idempotence flags,
// write-only direction, the session target, and the helper argv — the
// contract the session-creation scripts and the e2e tap test both depend on.
// The helper argv is nested shell quoting (the tmux server runs it through
// sh -c), which is why it reads as quote soup.
func TestStatusTapCommandPinned(t *testing.T) {
	t.Parallel()
	got := StatusTapCommand("/usr/local/bin/amux", "amux", "amux-ws-x", Options{ServerName: "amux", ConfigPath: "/dev/null"})
	want := "tmux -L 'amux' -f '/dev/null' pipe-pane -o -O -t 'amux-ws-x' " +
		"''\\''/usr/local/bin/amux'\\'' '\\''--internal-status-tap'\\'' '\\''amux'\\'' '\\''amux-ws-x'\\'' '\\''#{pane_id}'\\'''"
	if got != want {
		t.Fatalf("tap command drifted:\n got %q\nwant %q", got, want)
	}
	if StatusTapCommand("", "amux", "s", Options{ServerName: "amux"}) != "" {
		t.Fatal("empty binary path must render no tap")
	}
}
