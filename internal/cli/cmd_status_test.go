package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
)

func TestCmdStatusJSON(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode JSON: %v\nraw: %s", err, w.String())
	}
	if !env.OK {
		t.Error("expected ok=true")
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", env.Data)
	}
	if _, exists := data["version"]; !exists {
		t.Error("expected 'version' in data")
	}
	if _, exists := data["tmux_available"]; !exists {
		t.Error("expected 'tmux_available' in data")
	}
	if _, exists := data["stack_child_count"]; !exists {
		t.Error("expected 'stack_child_count' in data")
	}
	if _, exists := data["stack_root_count"]; !exists {
		t.Error("expected 'stack_root_count' in data")
	}
}

func TestCmdStatusHuman(t *testing.T) {
	var w, wErr bytes.Buffer
	gf := GlobalFlags{JSON: false}
	code := cmdStatus(&w, &wErr, gf, nil, "test-v1")

	if code != ExitOK {
		t.Fatalf("expected exit 0, got %d; stderr: %s", code, wErr.String())
	}

	output := w.String()
	if output == "" {
		t.Error("expected non-empty human output")
	}
}

func TestCmdStatusUnexpectedArgsReturnsUsageError(t *testing.T) {
	var w bytes.Buffer
	var wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, []string{"garbage"}, "test-v1")
	if code != ExitUsage {
		t.Fatalf("cmdStatus() code = %d, want %d", code, ExitUsage)
	}
	if wErr.Len() != 0 {
		t.Fatalf("expected no stderr output in JSON mode, got %q", wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	if env.OK {
		t.Fatalf("expected ok=false")
	}
	if env.Error == nil || env.Error.Code != "usage_error" {
		t.Fatalf("expected usage_error, got %#v", env.Error)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "unexpected arguments") {
		t.Fatalf("unexpected usage_error message: %#v", env.Error)
	}
}

func TestCmdStatusCountsActiveStackRootWhenOnlyRootWorkspaceIsActive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	rootPath := filepath.Join(repoRoot, "root")
	childPath := filepath.Join(repoRoot, "child")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(rootPath) error = %v", err)
	}
	if err := os.MkdirAll(childPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(childPath) error = %v", err)
	}

	root := data.NewWorkspace("feature", "feature", "main", repoRoot, rootPath)
	child := data.NewWorkspace("feature.refactor", "feature.refactor", "feature", repoRoot, childPath)
	data.ApplyStackParent(child, root, root.Branch)

	store := data.NewWorkspaceStore(filepath.Join(home, ".amux", "workspaces-metadata"))
	if err := store.Save(root); err != nil {
		t.Fatalf("store.Save(root) error = %v", err)
	}
	if err := store.Save(child); err != nil {
		t.Fatalf("store.Save(child) error = %v", err)
	}

	origEnsure := statusTmuxEnsureAvailable
	origActiveByWorkspace := statusTmuxAmuxSessionsByWorkspace
	origListSessions := statusTmuxListSessions
	defer func() {
		statusTmuxEnsureAvailable = origEnsure
		statusTmuxAmuxSessionsByWorkspace = origActiveByWorkspace
		statusTmuxListSessions = origListSessions
	}()
	statusTmuxEnsureAvailable = func() error { return nil }
	statusTmuxAmuxSessionsByWorkspace = func(_ tmux.Options) (map[string][]string, error) {
		return map[string][]string{string(root.ID()): {"sess-root"}}, nil
	}
	statusTmuxListSessions = func(_ tmux.Options) ([]string, error) {
		return []string{"sess-root"}, nil
	}

	var w, wErr bytes.Buffer
	code := cmdStatus(&w, &wErr, GlobalFlags{JSON: true}, nil, "test-v1")
	if code != ExitOK {
		t.Fatalf("cmdStatus() code = %d; stderr: %s", code, wErr.String())
	}

	var env Envelope
	if err := json.Unmarshal(w.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, w.String())
	}
	dataMap, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env.Data)
	}
	if got, _ := dataMap["stack_root_count"].(float64); int(got) != 1 {
		t.Fatalf("stack_root_count = %v, want 1", got)
	}
	if got, _ := dataMap["active_stack_root_count"].(float64); int(got) != 1 {
		t.Fatalf("active_stack_root_count = %v, want 1", got)
	}
}
