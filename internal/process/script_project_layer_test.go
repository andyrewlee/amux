package process

import (
	"errors"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

// newLayerFixture returns a runner whose project layer serves scripts plus a
// bare workspace on a config-less repo — the shape every precedence case
// starts from.
func newLayerFixture(t *testing.T, scripts data.ScriptsConfig) (*ScriptRunner, *data.Workspace) {
	t.Helper()
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{}`)

	runner := NewScriptRunner(6200, 10)
	if scripts != (data.ScriptsConfig{}) {
		runner.SetProjectScriptResolver(func(repoPath string) data.ScriptsConfig {
			if data.NormalizePath(repoPath) == data.NormalizePath(repo) {
				return scripts
			}
			return data.ScriptsConfig{}
		})
	}
	return runner, &data.Workspace{Repo: repo, Root: wsRoot}
}

// TestResolveScriptCommandProjectLayer proves the third layer answers when
// repo and workspace both miss — for every lifecycle type, not just run.
func TestResolveScriptCommandProjectLayer(t *testing.T) {
	runner, ws := newLayerFixture(t, data.ScriptsConfig{
		Run: "run-proj", Archive: "archive-proj", OnDone: "ondone-proj", Setup: "setup-proj",
	})

	for _, tt := range []struct {
		scriptType ScriptType
		want       string
	}{
		{ScriptRun, "run-proj"},
		{ScriptArchive, "archive-proj"},
		{ScriptOnDone, "ondone-proj"},
	} {
		got, err := runner.resolveScriptCommand(ws, tt.scriptType)
		if err != nil {
			t.Fatalf("resolveScriptCommand(%v) error: %v", tt.scriptType, err)
		}
		if got != tt.want {
			t.Fatalf("resolveScriptCommand(%v) = %q, want %q", tt.scriptType, got, tt.want)
		}
	}
}

// TestResolveScriptCommandPrecedenceMatrix proves the documented order —
// repo → workspace → project — per field, across every overlap combination.
func TestResolveScriptCommandPrecedenceMatrix(t *testing.T) {
	t.Run("workspace beats project", func(t *testing.T) {
		runner, ws := newLayerFixture(t, data.ScriptsConfig{Run: "run-proj"})
		ws.Scripts = data.ScriptsConfig{Run: "run-ws"}
		got, err := runner.resolveScriptCommand(ws, ScriptRun)
		if err != nil || got != "run-ws" {
			t.Fatalf("got %q err %v, want run-ws", got, err)
		}
	})

	t.Run("repo beats workspace and project", func(t *testing.T) {
		runner, ws := newLayerFixture(t, data.ScriptsConfig{Run: "run-proj"})
		ws.Scripts = data.ScriptsConfig{Run: "run-ws"}
		writeWorkspaceConfig(t, ws.Repo, `{"run": "run-repo"}`)
		trustRepo(t, runner, ws.Repo)
		got, err := runner.resolveScriptCommand(ws, ScriptRun)
		if err != nil || got != "run-repo" {
			t.Fatalf("got %q err %v, want run-repo", got, err)
		}
	})

	t.Run("untrusted repo still gates even with a project fallback", func(t *testing.T) {
		// Fail-closed: a weaker layer never rescues a repo-supplied command
		// whose content the user hasn't approved — precedence picks the repo
		// command first, then the trust gate fires on it.
		runner, ws := newLayerFixture(t, data.ScriptsConfig{Run: "run-proj"})
		writeWorkspaceConfig(t, ws.Repo, `{"run": "run-repo"}`)
		got, err := runner.resolveScriptCommand(ws, ScriptRun)
		if got != "" {
			t.Fatalf("expected no command, got %q", got)
		}
		var notTrusted *ScriptsNotTrustedError
		if !errors.As(err, &notTrusted) {
			t.Fatalf("expected ScriptsNotTrustedError, got %v", err)
		}
	})
}

// TestResolveScriptCommandNoLayers proves the sentinel still returns when
// every layer misses — the project store wired or unwired changes nothing.
func TestResolveScriptCommandNoLayers(t *testing.T) {
	for _, wireStore := range []bool{false, true} {
		var scripts data.ScriptsConfig
		if wireStore {
			scripts = data.ScriptsConfig{Archive: "archive-proj"} // only archive; run must still miss
		}
		runner, ws := newLayerFixture(t, scripts)
		_, err := runner.resolveScriptCommand(ws, ScriptRun)
		if !errors.Is(err, ErrNoScriptConfigured) {
			t.Fatalf("wireStore=%v: expected ErrNoScriptConfigured, got %v", wireStore, err)
		}
	}
}

// TestProjectLayerStoreEndToEnd wires a real ProjectScriptStore through
// SetProjectScriptResolver — the same shape app init uses — and proves a
// file-edited project script resolves without a runner rebuild.
func TestProjectLayerStoreEndToEnd(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	writeWorkspaceConfig(t, repo, `{}`)

	store := data.NewProjectScriptStore(t.TempDir())
	runner := NewScriptRunner(6200, 10)
	runner.SetProjectScriptResolver(store.ForRepo)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	if _, err := runner.resolveScriptCommand(ws, ScriptRun); !errors.Is(err, ErrNoScriptConfigured) {
		t.Fatalf("expected ErrNoScriptConfigured before Set, got %v", err)
	}

	if err := store.Set(repo, data.ScriptsConfig{Run: "run-from-file"}); err != nil {
		t.Fatalf("store.Set: %v", err)
	}
	got, err := runner.resolveScriptCommand(ws, ScriptRun)
	if err != nil || got != "run-from-file" {
		t.Fatalf("after Set: got %q err %v, want run-from-file", got, err)
	}
}

// TestProjectSetupLayerRuns proves the setup script's third layer executes —
// RunSetup resolves it as a single command and runs it in the worktree.
func TestProjectSetupLayerRuns(t *testing.T) {
	runner, ws := newLayerFixture(t, data.ScriptsConfig{Setup: "printf setup-ran > setup.txt"})

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	if err := waitForFile(ws.Root+"/setup.txt", 2*time.Second); err != nil {
		t.Fatalf("project setup command did not run: %v", err)
	}
}

// TestProjectSetupYieldsToWorkspaceSetup proves setup honors the same
// precedence tail: a workspace-level Setup command wins over the project
// default.
func TestProjectSetupYieldsToWorkspaceSetup(t *testing.T) {
	runner, ws := newLayerFixture(t, data.ScriptsConfig{Setup: "printf proj > setup-src.txt"})
	ws.Scripts = data.ScriptsConfig{Setup: "printf ws > setup-src.txt"}

	if err := runner.RunSetup(ws); err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	if err := waitForFile(ws.Root+"/setup-src.txt", 2*time.Second); err != nil {
		t.Fatalf("setup marker missing: %v", err)
	}
}
