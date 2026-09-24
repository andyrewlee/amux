package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
)

// TestTrustDialog_ShowsApprovedCommands proves the dialog renders every repo
// command the approval covers — including on-done, which fires unattended on a
// lifecycle edge and previously produced no review surface at all.
func TestTrustDialog_ShowsApprovedCommands(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{
		"setup-workspace": ["npm install", "make proto"],
		"run": "make dev",
		"archive": "tar -czf a.tgz .",
		"on-done": "./hooks/finish.sh",
		"env": {"SECRET_TOKEN": "hunter2", "PATH": "./bin:$PATH"}
	}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	for _, want := range []string{
		"setup: npm install",
		"setup: make proto",
		"run: make dev",
		"archive: tar -czf a.tgz .",
		"on-done: ./hooks/finish.sh",
		"env: PATH, SECRET_TOKEN",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("trust dialog missing %q, got:\n%s", want, view)
		}
	}
	// Env VALUES must never render — they can carry secrets.
	if strings.Contains(view, "hunter2") || strings.Contains(view, "./bin:$PATH") {
		t.Fatalf("trust dialog leaked an env value, got:\n%s", view)
	}
}

// TestTrustDialog_NoManifestWithoutConfig proves the dialog degrades to its
// one-line form when there is no repo config to review.
func TestTrustDialog_NoManifestWithoutConfig(t *testing.T) {
	repo := t.TempDir() // no .amux/workspaces.json
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if strings.Contains(view, "setup:") || strings.Contains(view, "env:") {
		t.Fatalf("expected no manifest without repo config, got:\n%s", view)
	}
}

// TestTrustDialog_ManifestSanitizesCommands proves repo command text can't
// inject terminal escapes into the dialog frame.
func TestTrustDialog_ManifestSanitizesCommands(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"run":"echo \u001b[2Jhi"}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if strings.Contains(view, "\x1b") {
		t.Fatalf("trust dialog rendered a raw escape, got:\n%q", view)
	}
	if !strings.Contains(view, "echo hi") {
		t.Fatalf("sanitized command text missing, got:\n%s", view)
	}
}

// TestTrustDialog_ManifestSanitizesEnvKeys proves repo env key names can't
// inject terminal escapes or fabricate dialog lines into the consent surface.
func TestTrustDialog_ManifestSanitizesEnvKeys(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{
		"run": "make dev",
		"env": {
			"SAFE_KEY": "1",
			"BAD\u001b[8mKEY": "2",
			"EVIL\n  env: Trust me, press Yes": "3"
		}
	}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if strings.Contains(view, "\x1b") {
		t.Fatalf("trust dialog rendered a raw escape, got:\n%q", view)
	}
	// The newline-injected key must stay flattened inside the single env
	// line — it cannot fabricate a second "env:" row.
	envLines := 0
	for line := range strings.Lines(view) {
		if strings.Contains(line, "  env: ") {
			envLines++
			if !strings.Contains(line, "SAFE_KEY") {
				t.Fatalf("sanitized env key missing from env line %q", line)
			}
		}
	}
	if envLines != 1 {
		t.Fatalf("expected exactly one env line, got %d in:\n%s", envLines, view)
	}
}

// TestRepoScriptCommandsForTrust_IncludesOnDone pins the coverage fix: on-done
// joins the indirection-scan command set.
func TestRepoScriptCommandsForTrust_IncludesOnDone(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"on-done":"./hook.sh"}`)
	app := newTrustDialogApp()
	cmds := app.repoScriptCommandsForTrust(repo)
	if len(cmds) != 1 || cmds[0] != "./hook.sh" {
		t.Fatalf("repoScriptCommandsForTrust = %v, want [./hook.sh]", cmds)
	}
}
