package process

import (
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
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
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := runner.LastScriptOutputs(ws)[ScriptOnDone]; ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("success transcript never recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
