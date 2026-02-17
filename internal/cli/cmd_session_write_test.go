package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func testSessionServices(t *testing.T, queryFn func(tmux.Options) ([]sessionRow, error)) *Services {
	t.Helper()
	return &Services{
		Store:            data.NewWorkspaceStore(t.TempDir()),
		TmuxOpts:         tmux.Options{},
		Version:          "test-v1",
		QuerySessionRows: queryFn,
	}
}

// --- routeSession tests ---

func TestRouteSessionNoSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeSession(&out, &errOut, GlobalFlags{}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

func TestRouteSessionNoSubcommandJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeSession(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
}

func TestRouteSessionUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeSession(&out, &errOut, GlobalFlags{}, []string{"bogus"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

func TestRouteSessionUnknownSubcommandJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := routeSession(&out, &errOut, GlobalFlags{JSON: true}, []string{"bogus"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
}

// --- cmdSessionList tests ---

func TestCmdSessionListJSON(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-ws1-tab-1",
				tags:      map[string]string{"@amux_workspace": "ws1", "@amux_type": "agent"},
				attached:  true,
				createdAt: 1000,
			},
		}, nil
	})

	var out, errOut bytes.Buffer
	code := cmdSessionListWith(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d; stderr: %s", code, ExitOK, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}
}

func TestCmdSessionListRejectsArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdSessionListWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"extra"}, "test-v1", nil)
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

// --- cmdSessionPrune tests ---

func TestCmdSessionPruneDryRunJSON(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-gone-tab-1",
				tags:      map[string]string{"@amux_workspace": "gone", "@amux_type": "agent"},
				createdAt: 100,
			},
		}, nil
	})

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d; stderr: %s", code, ExitOK, errOut.String())
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true for dry run")
	}

	raw, _ := json.Marshal(env.Data)
	var result pruneResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal pruneResult: %v", err)
	}
	if !result.DryRun {
		t.Fatalf("expected dry_run=true")
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
}

func TestCmdSessionPruneYesJSON(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-gone-tab-1",
				tags:      map[string]string{"@amux_workspace": "gone", "@amux_type": "agent"},
				createdAt: 100,
			},
		}, nil
	})

	killed := []string{}
	origKill := tmuxKillSession
	defer func() { tmuxKillSession = origKill }()
	tmuxKillSession = func(name string, _ tmux.Options) error {
		killed = append(killed, name)
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--yes"}, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d; stderr: %s", code, ExitOK, errOut.String())
	}

	if len(killed) != 1 || killed[0] != "amux-gone-tab-1" {
		t.Fatalf("killed = %v, want [amux-gone-tab-1]", killed)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}
}

func TestCmdSessionPruneOlderThanInvalid(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--older-than", "abc"}, "test-v1", nil)
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

func TestCmdSessionPruneOlderThanNonPositive(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--older-than", "-5m"}, "test-v1", nil)
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

func TestCmdSessionPruneRejectsExtraArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"extra"}, "test-v1", nil)
	if code != ExitUsage {
		t.Fatalf("code = %d, want %d", code, ExitUsage)
	}
}

func TestCmdSessionPruneDryRunHuman(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-ws-a-term-tab-1",
				tags:      map[string]string{"@amux_workspace": "ws-a", "@amux_type": "term-tab"},
				createdAt: 100,
			},
		}, nil
	})

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d; stderr: %s", code, ExitOK, errOut.String())
	}

	output := out.String()
	if !bytes.Contains(out.Bytes(), []byte("Would prune")) {
		t.Fatalf("expected dry-run message, got %q", output)
	}
}

func TestCmdSessionPruneNothingToPrune(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return nil, nil
	})

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--yes"}, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true")
	}
}

func TestCmdSessionPrunePartialFailureReturnsError(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-gone-tab-1",
				tags:      map[string]string{"@amux_workspace": "gone", "@amux_type": "agent"},
				createdAt: 100,
			},
			{
				name:      "amux-gone-tab-2",
				tags:      map[string]string{"@amux_workspace": "gone", "@amux_type": "agent"},
				createdAt: 100,
			},
		}, nil
	})

	origKill := tmuxKillSession
	defer func() { tmuxKillSession = origKill }()
	tmuxKillSession = func(name string, _ tmux.Options) error {
		if name == "amux-gone-tab-2" {
			return errors.New("kill failed")
		}
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--yes"}, "test-v1", svc)
	if code != ExitInternalError {
		t.Fatalf("code = %d, want %d", code, ExitInternalError)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if env.OK {
		t.Fatalf("expected ok=false for partial failure")
	}
	if env.Error == nil || env.Error.Code != "prune_partial_failed" {
		t.Fatalf("expected error code prune_partial_failed, got %#v", env.Error)
	}
}

func TestCmdSessionPruneFullSuccessReturnsOK(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return []sessionRow{
			{
				name:      "amux-gone-tab-1",
				tags:      map[string]string{"@amux_workspace": "gone", "@amux_type": "agent"},
				createdAt: 100,
			},
		}, nil
	})

	origKill := tmuxKillSession
	defer func() { tmuxKillSession = origKill }()
	tmuxKillSession = func(_ string, _ tmux.Options) error {
		return nil
	}

	var out, errOut bytes.Buffer
	code := cmdSessionPruneWith(&out, &errOut, GlobalFlags{JSON: true}, []string{"--yes"}, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true for full success")
	}
}

func TestCmdSessionListHumanEmpty(t *testing.T) {
	svc := testSessionServices(t, func(_ tmux.Options) ([]sessionRow, error) {
		return nil, nil
	})

	var out, errOut bytes.Buffer
	code := cmdSessionListWith(&out, &errOut, GlobalFlags{}, nil, "test-v1", svc)
	if code != ExitOK {
		t.Fatalf("code = %d, want %d", code, ExitOK)
	}
	if !bytes.Contains(out.Bytes(), []byte("No sessions")) {
		t.Fatalf("expected 'No sessions' message, got %q", out.String())
	}
}
