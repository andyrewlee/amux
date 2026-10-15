package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
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

// TestTrustDialog_ManifestStripsBidiControls pins the Trojan-Source fix: a
// repo command carrying Unicode bidi/format controls (which make the rendered
// dialog text differ from the bytes that would run) must not reach the frame.
func TestTrustDialog_ManifestStripsBidiControls(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"run":"echo \u202e hello", "env":{"K\u202eY":"v"}}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	for _, r := range []rune{0x202a, 0x202e, 0x2066, 0x2069, 0x200e, 0x200f, 0x061c} {
		if strings.ContainsRune(view, r) {
			t.Fatalf("trust dialog rendered bidi/format rune U+%04X, got:\n%s", r, view)
		}
	}
	if !strings.Contains(view, "run: echo  hello") {
		t.Fatalf("sanitized command text missing, got:\n%s", view)
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

func TestHandleDialogResultTrustScriptsTrustsAndRetriesSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	marker := filepath.Join(wsRoot, "setup-ran")
	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+marker+`"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	scripts := process.NewScriptRunner(6200, 10)
	err := scripts.RunSetup(ws)
	var trustErr *process.ScriptsNotTrustedError
	if !errors.As(err, &trustErr) {
		t.Fatalf("expected initial setup to be blocked by trust gate, got %v", err)
	}
	app := &App{
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		dialog:           common.NewConfirmDialog(DialogTrustScripts, "Trust", "Trust scripts?"),
		dlg:              dialogContext{workspace: ws, trustScriptsHash: trustErr.ConfigHash},
	}

	cmd := app.handleDialogResult(common.DialogResult{ID: DialogTrustScripts, Confirmed: true}, app.dlg)
	if cmd == nil {
		t.Fatal("expected trust confirmation to return a setup retry command")
	}
	msg, ok := cmd().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	if msg.Err != nil {
		t.Fatalf("trust-and-retry setup failed: %v", msg.Err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected trusted setup command to run: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if err := scripts.RunSetup(ws); err != nil {
		t.Fatalf("expected repo config to remain trusted after dialog confirmation: %v", err)
	}
}

func TestTrustScriptsRetryRejectsChangedConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	wsRoot := t.TempDir()
	originalMarker := filepath.Join(wsRoot, "original")
	changedMarker := filepath.Join(wsRoot, "changed")
	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+originalMarker+`"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, wsRoot)

	scripts := process.NewScriptRunner(6200, 10)
	err := scripts.RunSetup(ws)
	var trustErr *process.ScriptsNotTrustedError
	if !errors.As(err, &trustErr) {
		t.Fatalf("expected initial setup to be blocked by trust gate, got %v", err)
	}

	workspaceSetupConfig(t, repo, `{"setup-workspace":["touch `+changedMarker+`"]}`)
	service := workspacesvc.New(nil, nil, scripts, "")
	msg, ok := service.TrustRepoScriptsAndRunSetupAsync(ws, trustErr.ConfigHash)().(messages.WorkspaceSetupComplete)
	if !ok {
		t.Fatalf("expected WorkspaceSetupComplete, got %T", msg)
	}
	var changedTrustErr *process.ScriptsNotTrustedError
	if !errors.As(msg.Err, &changedTrustErr) {
		t.Fatalf("expected changed config to be re-gated with a trust error, got %v", msg.Err)
	}
	if changedTrustErr.ConfigHash == trustErr.ConfigHash {
		t.Fatal("expected changed config trust prompt to carry a new hash")
	}
	if _, err := os.Stat(originalMarker); !os.IsNotExist(err) {
		t.Fatalf("original setup command should not run after stale approval, stat err=%v", err)
	}
	if _, err := os.Stat(changedMarker); !os.IsNotExist(err) {
		t.Fatalf("changed setup command should not run under stale approval, stat err=%v", err)
	}
}

// newTrustDialogApp builds a minimal App whose workspaceService can load a
// repo's .amux/workspaces.json, so the trust-dialog show handler can compute its
// indirection warning. config is non-nil because presentDialog reads UI hints.
func newTrustDialogApp() *App {
	return &App{
		config:           &config.Config{},
		width:            120,
		height:           40,
		workspaceService: workspacesvc.New(nil, nil, process.NewScriptRunner(6200, 10), ""),
	}
}

// TestHandleShowTrustScriptsDialog_WarnsOnInRepoScriptReference proves the trust
// dialog surfaces in-repo files an approved command reaches into (via the
// already-shipped process.ReferencesInRepoFiles detector), so the user knows the
// manifest hash they approve does not cover those scripts' later contents. This
// is informational only and changes nothing the trust gate blocks.
func TestHandleShowTrustScriptsDialog_WarnsOnInRepoScriptReference(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "dev.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write dev.sh: %v", err)
	}
	workspaceSetupConfig(t, repo, `{"setup-workspace":["bash ./scripts/dev.sh"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if !strings.Contains(view, "scripts/dev.sh") {
		t.Fatalf("expected trust dialog to name the referenced in-repo script, got:\n%s", view)
	}
	if !strings.Contains(view, "re-verify") {
		t.Fatalf("expected the indirection warning wording, got:\n%s", view)
	}
}

// TestHandleShowTrustScriptsDialog_WarnsOnUnresolvableCommand proves a command
// with expansion/glob constructs (which hide references from the static
// detector) raises the "amux can't list every file" warning.
func TestHandleShowTrustScriptsDialog_WarnsOnUnresolvableCommand(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"run":"$MYVAR/foo"}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if !strings.Contains(view, "variables/globs") {
		t.Fatalf("expected unresolvable-command warning, got:\n%s", view)
	}
}

// TestHandleShowTrustScriptsDialog_NoIndirectionMakesNoSafetyClaim proves that
// when the detector finds neither a referenced file nor an unresolvable
// construct, the dialog renders no extra warning AND no reassuring "safe" text:
// an empty detector result is explicitly NOT a safety guarantee.
func TestHandleShowTrustScriptsDialog_NoIndirectionMakesNoSafetyClaim(t *testing.T) {
	repo := t.TempDir()
	workspaceSetupConfig(t, repo, `{"setup-workspace":["echo hello"]}`)
	ws := data.NewWorkspace("feature", "feature", "main", repo, filepath.Join(repo, "ws"))

	app := newTrustDialogApp()
	app.handleShowTrustScriptsDialog(messages.ShowTrustScriptsDialog{Workspace: ws, ConfigHash: "hash"})

	view := dialogView(t, app.dialog)
	if strings.Contains(view, "re-verify") || strings.Contains(view, "variables/globs") {
		t.Fatalf("expected no indirection warning for a plain command, got:\n%s", view)
	}
	for _, banned := range []string{"safe", "no in-repo", "nothing to run", "verified"} {
		if strings.Contains(strings.ToLower(view), banned) {
			t.Fatalf("dialog must not imply safety (%q), got:\n%s", banned, view)
		}
	}
}
