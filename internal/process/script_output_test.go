package process

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"

	"github.com/andyrewlee/amux/internal/testutil"
)

func TestTailWriter_Bounded(t *testing.T) {
	w := &tailWriter{max: 10}
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("ABCDEFGHIJ")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got := w.String()
	if !strings.Contains(got, "10 bytes of earlier output dropped") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if !strings.HasSuffix(got, "ABCDEFGHIJ") {
		t.Fatalf("tail must keep the most recent bytes, got %q", got)
	}
}

func TestTailWriter_UnderCapNoMarker(t *testing.T) {
	w := &tailWriter{max: 10}
	_, _ = w.Write([]byte("short"))
	if got := w.String(); got != "short" {
		t.Fatalf("String() = %q, want verbatim %q", got, "short")
	}
}

// TestRunSetup_RecordsCombinedOutput proves stdout (previously dropped) and
// stderr both land in the recorded transcript — the transcript is the whole
// setup's output, not just the failing command's stderr.
func TestRunSetup_RecordsCombinedOutput(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo setup-stdout", "echo setup-stderr 1>&2"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptSetup]
	if !ok {
		t.Fatal("no setup output recorded")
	}
	if !strings.Contains(entry.Text, "setup-stdout") || !strings.Contains(entry.Text, "setup-stderr") {
		t.Fatalf("transcript missing a stream: %q", entry.Text)
	}
	if entry.Err != "" {
		t.Fatalf("successful run recorded Err %q", entry.Err)
	}
}

func TestRunSetup_FailureRecordsTranscriptAndError(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo before-fail; exit 3"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	err := runner.RunSetup(ws)
	if err == nil {
		t.Fatal("RunSetup() succeeded, want failure")
	}
	if !strings.Contains(err.Error(), "before-fail") {
		t.Fatalf("error must carry the captured output, got %v", err)
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptSetup]
	if !ok || !strings.Contains(entry.Text, "before-fail") || entry.Err == "" {
		t.Fatalf("failure transcript = %+v (ok=%v), want output + Err", entry, ok)
	}
}

// TestRunSetup_WorkspaceScriptFallback proves the scripts editor's `setup`
// field actually runs: with no repo setup-workspace config, ws.Scripts.Setup
// is the fallback command — and as user input it runs without a trust prompt.
func TestRunSetup_WorkspaceScriptFallback(t *testing.T) {
	repo := t.TempDir() // deliberately no .amux/workspaces.json
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Scripts.Setup = "echo user-setup-out"

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptSetup]
	if !ok || !strings.Contains(entry.Text, "user-setup-out") {
		t.Fatalf("user setup transcript = %+v (ok=%v), want the echoed marker", entry, ok)
	}
}

// TestRunSetup_NoScriptConfiguredSentinel makes "nothing to run" distinguishable
// from failure: neither the repo config nor the workspace's own field defines a
// setup, so the caller can stay silent on create yet still answer a re-run key.
func TestRunSetup_NoScriptConfiguredSentinel(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	err := runner.RunSetup(ws)
	if !errors.Is(err, ErrNoScriptConfigured) {
		t.Fatalf("RunSetup() error = %v, want ErrNoScriptConfigured", err)
	}
	if _, ok := runner.LastScriptOutputs(ws)[ScriptSetup]; ok {
		t.Fatal("a no-op setup must not leave a transcript entry")
	}
}

// TestRunSetup_RepoConfigWinsOverWorkspaceScript pins the resolution order: a
// repo setup-workspace list shadows the workspace's own field, matching how
// run/archive resolve in resolveScriptCommand.
func TestRunSetup_RepoConfigWinsOverWorkspaceScript(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo repo-setup-out"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Scripts.Setup = "echo user-setup-out"

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	entry := runner.LastScriptOutputs(ws)[ScriptSetup]
	if !strings.Contains(entry.Text, "repo-setup-out") || strings.Contains(entry.Text, "user-setup-out") {
		t.Fatalf("transcript = %q, want repo command only", entry.Text)
	}
}

func TestRunArchive_RecordsCombinedOutput(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": "echo archive-out; echo archive-err 1>&2"}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	if err := runner.RunArchive(ws); err != nil {
		t.Fatalf("RunArchive() error = %v", err)
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptArchive]
	if !ok {
		t.Fatal("no archive output recorded")
	}
	if !strings.Contains(entry.Text, "archive-out") || !strings.Contains(entry.Text, "archive-err") {
		t.Fatalf("transcript missing a stream: %q", entry.Text)
	}
}

// TestRunOnDone_FailureNotifiesAndRecords is the visibility contract: a
// detached on-done hook that exits non-zero must notify the exit listener
// (the app's toast path) and leave a recorded transcript — previously it
// died in a Debug log with no output at all.
func TestRunOnDone_FailureNotifiesAndRecords(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Scripts.OnDone = "echo hook-out; echo hook-err 1>&2; exit 7" // user-entered: no trust needed

	type call struct {
		ws *data.Workspace
		st ScriptType
	}
	calls := make(chan call, 4)
	runner.SetScriptExitListener(func(w *data.Workspace, st ScriptType, runErr error) {
		calls <- call{w, st}
		if runErr == nil {
			t.Error("listener runErr = nil on a non-zero exit")
		}
	})

	if err := runner.RunOnDone(ws, "session-x"); err != nil {
		t.Fatalf("RunOnDone() spawn error = %v", err)
	}
	select {
	case got := <-calls:
		if got.st != ScriptOnDone || got.ws != ws {
			t.Fatalf("listener got %+v, want on-done for ws", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exit listener never fired")
	}
	entry, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]
	if !ok {
		t.Fatal("no on-done output recorded")
	}
	if !strings.Contains(entry.Text, "hook-out") || !strings.Contains(entry.Text, "hook-err") {
		t.Fatalf("transcript missing a stream: %q", entry.Text)
	}
	if !strings.Contains(entry.Err, "exit status 7") {
		t.Fatalf("Err = %q, want the exit status", entry.Err)
	}
}

// TestRunOnDone_SuccessNoNotification proves the listener fires only on
// failure — a healthy hook records output but doesn't alarm.
func TestRunOnDone_SuccessNoNotification(t *testing.T) {
	repo := t.TempDir()
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo
	ws.Scripts.OnDone = "echo fine"

	calls := make(chan struct{}, 1)
	runner.SetScriptExitListener(func(*data.Workspace, ScriptType, error) {
		calls <- struct{}{}
	})
	if err := runner.RunOnDone(ws, "session-x"); err != nil {
		t.Fatalf("RunOnDone() error = %v", err)
	}
	// Wait until the transcript lands (the Wait goroutine records before it
	// would notify), then give the (absent) notify a beat.
	testutil.Eventually(t, 3*time.Second, 10*time.Millisecond, func() bool {
		_, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]
		return ok
	}, "success transcript never recorded")
	select {
	case <-calls:
		t.Fatal("listener fired on a clean exit")
	case <-time.After(50 * time.Millisecond):
	}
}

// TestLastScriptOutputs_ScopedPerWorkspace proves transcripts don't cross
// workspaces.
func TestLastScriptOutputs_ScopedPerWorkspace(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"archive": "echo marker"}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	wsA := newHostedWorkspace(t, "nonconcurrent")
	wsA.Repo = repo
	wsB := newHostedWorkspace(t, "nonconcurrent")
	wsB.Repo = repo
	wsB.Root = t.TempDir()

	if err := runner.RunArchive(wsA); err != nil {
		t.Fatalf("RunArchive() error = %v", err)
	}
	if _, ok := runner.LastScriptOutputs(wsB)[ScriptArchive]; ok {
		t.Fatal("wsB sees wsA's archive transcript")
	}
	if _, ok := runner.LastScriptOutputs(wsA)[ScriptArchive]; !ok {
		t.Fatal("wsA's transcript missing")
	}
}

// TestForgetWorkspacePrunesRetention: deleting a workspace must drop its
// lastOutput transcripts and run-session identity forms while leaving other
// workspaces' retention intact — the maps otherwise grow with the lifetime
// workspace count.
func TestForgetWorkspacePrunesRetention(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	wsA := newHostedWorkspace(t, "nonconcurrent")
	wsB := newHostedWorkspace(t, "nonconcurrent")

	runner.recordScriptOutput(wsA, ScriptSetup, "setup-A", nil)
	runner.recordScriptOutput(wsA, ScriptArchive, "arch-A", nil)
	runner.recordScriptOutput(wsB, ScriptSetup, "setup-B", nil)
	runner.markRunSessionSeen(wsA)
	runner.markRunSessionSeen(wsB)
	runner.markRunSessionSwept(wsA)

	runner.ForgetWorkspace(wsA)

	if got := runner.LastScriptOutputs(wsA); len(got) != 0 {
		t.Fatalf("deleted workspace A still has %d retained transcripts", len(got))
	}
	if runner.runSessionSeen(wsA) || runner.runSessionSwept(wsA) {
		t.Fatal("deleted workspace A still marked seen/swept")
	}
	if got := runner.LastScriptOutputs(wsB); len(got) != 1 || got[ScriptSetup].Text != "setup-B" {
		t.Fatalf("workspace B retention must survive pruning A: %+v", got)
	}
	if !runner.runSessionSeen(wsB) {
		t.Fatal("workspace B's seen marker must survive pruning A")
	}
}

// TestForgetWorkspaceIdempotentAndNilSafe: pruning a workspace twice, or a
// nil workspace, is a no-op rather than a panic.
func TestForgetWorkspaceIdempotentAndNilSafe(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	runner.ForgetWorkspace(ws)
	runner.ForgetWorkspace(ws)
	runner.ForgetWorkspace(nil)
	var nilRunner *ScriptRunner
	nilRunner.ForgetWorkspace(ws)
}

// TestForgetWorkspaceDropsParkedReleaseAndLifecycleState: a confirmed delete
// must not leave the parked port release or the coordinator record behind —
// the sweep that drains pendingRelease only runs inside IsRunning, which a
// deleted workspace never reaches again.
func TestForgetWorkspaceDropsParkedReleaseAndLifecycleState(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	key := scriptWorkspaceKey(ws)

	// A released-while-running workspace parks its port; a quiescent
	// coordinator record persists from a completed lifecycle admission.
	runner.pendingRelease[key] = pendingPortRelease{root: ws.Root}
	runner.lifecycle.stateFor(key)
	if _, err := runner.portAllocator.AllocatePort(ws.Root); err != nil {
		t.Fatalf("seed AllocatePort: %v", err)
	}

	runner.ForgetWorkspace(ws)

	if _, ok := runner.pendingRelease[key]; ok {
		t.Fatal("parked pendingRelease survived ForgetWorkspace")
	}
	if _, ok := runner.portAllocator.GetPort(ws.Root); ok {
		t.Fatal("parked release's port was never returned")
	}
	runner.lifecycle.mu.Lock()
	_, stillThere := runner.lifecycle.states[key]
	runner.lifecycle.mu.Unlock()
	if stillThere {
		t.Fatal("quiescent lifecycle state survived ForgetWorkspace")
	}
}

// TestForgetWorkspaceKeepsLiveReleaseAndState: a pending release pinned to a
// still-running entry and a state with live work must survive — the finish
// path drains them; deleting early would strand the port or drop tracking.
func TestForgetWorkspaceKeepsLiveReleaseAndState(t *testing.T) {
	runner := NewScriptRunner(6200, 10)
	ws := newHostedWorkspace(t, "nonconcurrent")
	key := scriptWorkspaceKey(ws)

	entry := &runningScript{}
	runner.pendingRelease[key] = pendingPortRelease{root: ws.Root, running: entry}
	st := runner.lifecycle.stateFor(key)
	st.teardown = true

	runner.ForgetWorkspace(ws)

	if _, ok := runner.pendingRelease[key]; !ok {
		t.Fatal("live running-pinned pendingRelease was dropped — its port release is now stranded")
	}
	runner.lifecycle.mu.Lock()
	_, stillThere := runner.lifecycle.states[key]
	runner.lifecycle.mu.Unlock()
	if !stillThere {
		t.Fatal("teardown-gated lifecycle state was dropped mid-operation")
	}
}

// TestLiveSetupOutput_StreamsWhileRunningAndClearsAfter pins the live-tail
// contract the script-output viewer polls: output is visible while the
// setup is in flight (before the transcript records), and the live flag
// drops once RunSetup returns — by which point the recorded transcript
// already exists.
func TestLiveSetupOutput_StreamsWhileRunningAndClearsAfter(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"setup-workspace": ["echo live-line; sleep 0.4"]}`)
	runner := NewScriptRunner(6200, 10)
	trustRepo(t, runner, repo)
	ws := newHostedWorkspace(t, "nonconcurrent")
	ws.Repo = repo

	done := make(chan error, 1)
	go func() { done <- runner.RunSetup(ws) }()

	// Poll until the live tail carries the first command's output — bounded
	// so a broken publish fails the test rather than hanging it.
	deadline := time.Now().Add(5 * time.Second)
	var text string
	var live bool
	for time.Now().Before(deadline) {
		text, live = runner.LiveSetupOutput(ws)
		if live && strings.Contains(text, "live-line") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !live || !strings.Contains(text, "live-line") {
		t.Fatalf("LiveSetupOutput mid-run = (%q, %v), want live tail containing live-line", text, live)
	}

	if err := <-done; err != nil {
		t.Fatalf("RunSetup() error = %v", err)
	}
	if text, live := runner.LiveSetupOutput(ws); live || text != "" {
		t.Fatalf("LiveSetupOutput after finish = (%q, %v), want (\"\", false)", text, live)
	}
	// The recorded transcript must already hold the finished run — the
	// viewer's final fetch composes it once live drops.
	if entry, ok := runner.LastScriptOutputs(ws)[ScriptSetup]; !ok || !strings.Contains(entry.Text, "live-line") {
		t.Fatalf("recorded transcript missing after live drop: %+v", runner.LastScriptOutputs(ws))
	}
}
