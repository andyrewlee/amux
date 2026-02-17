package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestResolveTerminalSessionForWorkspacePrefersAttachedThenNewest(t *testing.T) {
	origQuery := sessionQueryRows
	t.Cleanup(func() { sessionQueryRows = origQuery })

	sessionQueryRows = func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{name: "amux-ws-a-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "terminal"}, attached: false, createdAt: 100},
			{name: "amux-ws-a-term-tab-2", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "terminal"}, attached: false, createdAt: 200},
			{name: "amux-ws-a-term-tab-3", tags: map[string]string{"@amux_workspace": "ws-a", "@amux_type": "terminal"}, attached: true, createdAt: 50},
			{name: "amux-ws-b-term-tab-1", tags: map[string]string{"@amux_workspace": "ws-b", "@amux_type": "terminal"}, attached: true, createdAt: 999},
		}, nil
	}

	got, ok, err := resolveTerminalSessionForWorkspace(data.WorkspaceID("ws-a"), tmux.Options{})
	if err != nil {
		t.Fatalf("resolveTerminalSessionForWorkspace() error = %v", err)
	}
	if !ok {
		t.Fatal("expected session to be found")
	}
	if got != "amux-ws-a-term-tab-3" {
		t.Fatalf("session = %q, want %q", got, "amux-ws-a-term-tab-3")
	}
}

func TestResolveTerminalSessionForWorkspaceReturnsQueryError(t *testing.T) {
	origQuery := sessionQueryRows
	t.Cleanup(func() { sessionQueryRows = origQuery })

	wantErr := errors.New("query failed")
	sessionQueryRows = func(_ tmux.Options) ([]sessionRow, error) {
		return nil, wantErr
	}

	_, ok, err := resolveTerminalSessionForWorkspace(data.WorkspaceID("ws-a"), tmux.Options{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if ok {
		t.Fatal("expected ok=false on query error")
	}
}

func TestCmdTerminalRunRejectsUnexpectedPositionalArgs(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdTerminalRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", "0123456789abcdef", "--text", "npm", "run", "dev"},
		"test-v1",
	)
	if code != ExitUsage {
		t.Fatalf("cmdTerminalRun() code = %d, want %d", code, ExitUsage)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
}

func TestCmdTerminalRunPreservesWhitespaceInTextPayload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origQuery := sessionQueryRows
	origSend := tmuxSendKeys
	t.Cleanup(func() {
		sessionQueryRows = origQuery
		tmuxSendKeys = origSend
	})

	const workspaceID = "0123456789abcdef"
	sessionQueryRows = func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-test-term-tab-1",
				tags:      map[string]string{"@amux_workspace": workspaceID, "@amux_type": "terminal"},
				attached:  true,
				createdAt: 1,
			},
		}, nil
	}

	var gotSession string
	var gotText string
	var gotEnter bool
	tmuxSendKeys = func(name, text string, enter bool, _ tmux.Options) error {
		gotSession = name
		gotText = text
		gotEnter = enter
		return nil
	}

	raw := "  npm run dev  "
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdTerminalRun(
		&out,
		&errOut,
		GlobalFlags{JSON: true},
		[]string{"--workspace", workspaceID, "--text", raw, "--enter=false"},
		"test-v1",
	)
	if code != ExitOK {
		t.Fatalf("cmdTerminalRun() code = %d, want %d", code, ExitOK)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", errOut.String())
	}
	if gotSession != "amux-test-term-tab-1" {
		t.Fatalf("session = %q, want %q", gotSession, "amux-test-term-tab-1")
	}
	if gotText != raw {
		t.Fatalf("text = %q, want %q", gotText, raw)
	}
	if gotEnter {
		t.Fatalf("enter = true, want false")
	}
}
