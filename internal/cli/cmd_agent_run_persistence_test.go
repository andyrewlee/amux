package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdAgentRunMetadataSaveFailureReturnsInternalErrorAndCleansSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	svc, err := NewServices("test-v1")
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	ws := data.NewWorkspace("ws-a", "main", "origin/main", workspaceRoot, workspaceRoot)
	if _, ok := svc.Config.Assistants[ws.Assistant]; !ok {
		replacement := ""
		for name := range svc.Config.Assistants {
			replacement = name
			break
		}
		if replacement == "" {
			t.Fatalf("expected at least one assistant in config")
		}
		ws.Assistant = replacement
	}
	if err := svc.Store.Save(ws); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}

	origStartSession := tmuxStartSession
	origTagSetter := tmuxSetSessionTag
	origKillSession := tmuxKillSession
	origStateFor := tmuxSessionStateFor
	origAppendTabMeta := appendWorkspaceOpenTabMeta
	defer func() {
		tmuxStartSession = origStartSession
		tmuxSetSessionTag = origTagSetter
		tmuxKillSession = origKillSession
		tmuxSessionStateFor = origStateFor
		appendWorkspaceOpenTabMeta = origAppendTabMeta
	}()

	tmuxStartSession = func(_ tmux.Options, _ ...string) (*exec.Cmd, context.CancelFunc) {
		return exec.Command("true"), func() {}
	}
	tmuxSetSessionTag = func(_, _, _ string, _ tmux.Options) error { return nil }
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	appendWorkspaceOpenTabMeta = func(_ *data.WorkspaceStore, _ data.WorkspaceID, _ data.TabInfo) error {
		return errors.New("metadata write failed")
	}

	killCalls := 0
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", string(ws.ID()), "--assistant", ws.Assistant},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitInternalError)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if killCalls != 1 {
		t.Fatalf("tmuxKillSession calls = %d, want 1", killCalls)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "metadata_save_failed" {
		t.Fatalf("expected metadata_save_failed, got %#v", env.Error)
	}

	loaded, err := svc.Store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Store.Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 0 {
		t.Fatalf("expected no open tabs persisted on metadata save failure, got %d", len(loaded.OpenTabs))
	}
}

func TestCmdAgentRunPromptSendFailureReturnsInternalErrorAndDoesNotPersistTab(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	svc, err := NewServices("test-v1")
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	ws := data.NewWorkspace("ws-a", "main", "origin/main", workspaceRoot, workspaceRoot)
	if _, ok := svc.Config.Assistants[ws.Assistant]; !ok {
		replacement := ""
		for name := range svc.Config.Assistants {
			replacement = name
			break
		}
		if replacement == "" {
			t.Fatalf("expected at least one assistant in config")
		}
		ws.Assistant = replacement
	}
	if err := svc.Store.Save(ws); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}

	origStartSession := tmuxStartSession
	origTagSetter := tmuxSetSessionTag
	origKillSession := tmuxKillSession
	origStateFor := tmuxSessionStateFor
	origSendKeys := tmuxSendKeys
	defer func() {
		tmuxStartSession = origStartSession
		tmuxSetSessionTag = origTagSetter
		tmuxKillSession = origKillSession
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSendKeys
	}()

	tmuxStartSession = func(_ tmux.Options, _ ...string) (*exec.Cmd, context.CancelFunc) {
		return exec.Command("true"), func() {}
	}
	tmuxSetSessionTag = func(_, _, _ string, _ tmux.Options) error { return nil }
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		return errors.New("send failed")
	}

	killCalls := 0
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		killCalls++
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", string(ws.ID()), "--assistant", ws.Assistant, "--prompt", "hello"},
		"test-v1",
	)
	if code != ExitInternalError {
		t.Fatalf("cmdAgentRun() code = %d, want %d", code, ExitInternalError)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if killCalls != 1 {
		t.Fatalf("tmuxKillSession calls = %d, want 1", killCalls)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "prompt_send_failed" {
		t.Fatalf("expected prompt_send_failed, got %#v", env.Error)
	}

	loaded, err := svc.Store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Store.Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 0 {
		t.Fatalf("expected no open tabs persisted when prompt send fails, got %d", len(loaded.OpenTabs))
	}
}
