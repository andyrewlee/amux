package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
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

func TestCmdAgentRunWaitCapturesBaselineBeforePromptSendAfterReadiness(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	svc, err := NewServices("test-v1")
	if err != nil {
		t.Fatalf("NewServices() error = %v", err)
	}

	workspaceRoot := t.TempDir()
	ws := data.NewWorkspace("ws-a", "main", "origin/main", workspaceRoot, workspaceRoot)
	assistantName := "claude"
	if _, ok := svc.Config.Assistants[assistantName]; !ok {
		replacement := ""
		for name := range svc.Config.Assistants {
			if name != "codex" {
				replacement = name
				break
			}
		}
		if replacement == "" {
			for name := range svc.Config.Assistants {
				replacement = name
				break
			}
		}
		if replacement == "" {
			t.Fatalf("expected at least one assistant in config")
		}
		assistantName = replacement
	}
	ws.Assistant = assistantName
	if err := svc.Store.Save(ws); err != nil {
		t.Fatalf("Store.Save() error = %v", err)
	}

	origStartSession := tmuxStartSession
	origTagSetter := tmuxSetSessionTag
	origStateFor := tmuxSessionStateFor
	origSendKeys := tmuxSendKeys
	origCapture := tmuxCapturePaneTail
	defer func() {
		tmuxStartSession = origStartSession
		tmuxSetSessionTag = origTagSetter
		tmuxSessionStateFor = origStateFor
		tmuxSendKeys = origSendKeys
		tmuxCapturePaneTail = origCapture
	}()

	tmuxStartSession = func(_ tmux.Options, _ ...string) (*exec.Cmd, context.CancelFunc) {
		return exec.Command("true"), func() {}
	}
	tmuxSetSessionTag = func(_, _, _ string, _ tmux.Options) error { return nil }
	tmuxSessionStateFor = func(_ string, _ tmux.Options) (tmux.SessionState, error) {
		return tmux.SessionState{Exists: true, HasLivePane: true}, nil
	}

	var events []string
	tmuxCapturePaneTail = func(_ string, lines int, _ tmux.Options) (string, bool) {
		events = append(events, "capture:"+strconv.Itoa(lines))
		switch lines {
		case 20:
			return "❯ ready", true
		case 80:
			return "before-send", true
		default:
			return "after-send", true
		}
	}
	tmuxSendKeys = func(_, _ string, _ bool, _ tmux.Options) error {
		events = append(events, "send")
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdAgentRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{
			"--workspace", string(ws.ID()),
			"--assistant", assistantName,
			"--prompt", "hello",
			"--wait",
			"--wait-timeout", "1ms",
			"--idle-threshold", "1ms",
		},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdAgentRun() code = %d, want %d; stderr=%q", code, ExitOK, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got error %#v", env.Error)
	}

	firstSend := -1
	firstCapture100 := -1
	for i, event := range events {
		if event == "send" && firstSend == -1 {
			firstSend = i
		}
		if event == "capture:100" && firstCapture100 == -1 {
			firstCapture100 = i
		}
	}
	if firstSend == -1 {
		t.Fatalf("expected send event, got events=%v", events)
	}
	if firstCapture100 == -1 {
		t.Fatalf("expected capture:100 event, got events=%v", events)
	}
	if firstCapture100 >= firstSend {
		t.Fatalf("capture:100 must happen before send (send=%d capture100=%d events=%v)", firstSend, firstCapture100, events)
	}
}
