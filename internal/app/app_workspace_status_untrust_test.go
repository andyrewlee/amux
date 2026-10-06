package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/ui/common"
)

// untrustStatusApp builds an app whose script runner owns an isolated trust
// registry (t.Setenv HOME), matching the status tests' construction.
func untrustStatusApp(t *testing.T) (*App, *process.ScriptRunner) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	scripts := process.NewScriptRunner(6200, 10)
	return &App{
		config:           &config.Config{PortRangeSize: 10},
		toast:            common.NewToastModel(),
		workspaceService: workspacesvc.New(nil, nil, scripts, ""),
		width:            120,
		height:           40,
	}, scripts
}

// trustedStatusRepo seeds a repo script config, optionally grants trust, and
// returns a workspace rooted on it.
func trustedStatusRepo(t *testing.T, scripts *process.ScriptRunner, trust bool) *data.Workspace {
	t.Helper()
	repo := t.TempDir()
	writeRepoConfig(t, repo, `{"run": "echo hi"}`)
	if trust {
		if err := scripts.TrustRepoScripts(repo); err != nil {
			t.Fatalf("TrustRepoScripts: %v", err)
		}
	}
	return &data.Workspace{Name: "ws", Repo: repo, Root: t.TempDir(), Branch: "feat"}
}

// TestWorkspaceStatus_UInterceptArming covers the affordance gate: the `U`
// intercept and its rendered hint exist only while the repo's script config
// is trusted — the state that actually has a grant to drop.
func TestWorkspaceStatus_UInterceptArming(t *testing.T) {
	t.Run("armed when trusted", func(t *testing.T) {
		app, scripts := untrustStatusApp(t)
		ws := trustedStatusRepo(t, scripts, true)
		statusInterval(t, app, ws)
		if !app.overlays.runOutputUntrustable {
			t.Fatal("trusted repo did not arm the U intercept")
		}
		if got := ansi.Strip(app.overlays.runOutput.View()); !strings.Contains(got, "press U to revoke") {
			t.Fatalf("status view missing the revoke affordance:\n%s", got)
		}
		var cmds []tea.Cmd
		if !app.handleRunOutputInput(tea.KeyPressMsg{Code: 'U', Text: "U"}, &cmds) {
			t.Fatal("U was not intercepted by the status viewer")
		}
		if app.dialog == nil || !app.dialog.Visible() {
			t.Fatal("typed-confirm dialog did not open")
		}
	})

	t.Run("disarmed when untrusted", func(t *testing.T) {
		app, scripts := untrustStatusApp(t)
		ws := trustedStatusRepo(t, scripts, false)
		statusInterval(t, app, ws)
		if app.overlays.runOutputUntrustable {
			t.Fatal("untrusted repo armed the U intercept")
		}
		if got := ansi.Strip(app.overlays.runOutput.View()); strings.Contains(got, "press U to revoke") {
			t.Fatalf("untrusted status renders the affordance:\n%s", got)
		}
		// The viewer itself consumes U (scroll), so the disarm signal is
		// that no confirm dialog opens.
		var cmds []tea.Cmd
		app.handleRunOutputInput(tea.KeyPressMsg{Code: 'U', Text: "U"}, &cmds)
		if app.dialog != nil && app.dialog.Visible() {
			t.Fatal("U opened a confirm dialog on an unarmed viewer")
		}
	})

	t.Run("disarmed with no repo config", func(t *testing.T) {
		app, _ := untrustStatusApp(t)
		ws := &data.Workspace{Name: "ws", Repo: t.TempDir(), Root: t.TempDir()}
		statusInterval(t, app, ws)
		if app.overlays.runOutputUntrustable {
			t.Fatal("config-less repo armed the U intercept")
		}
	})
}

// TestUntrustConfirmation_ImmediateEnterBlocked: Enter on the empty field is
// rejected as validation — no result, dialog stays open, grant survives.
func TestUntrustConfirmation_ImmediateEnterBlocked(t *testing.T) {
	app, scripts := untrustStatusApp(t)
	ws := trustedStatusRepo(t, scripts, true)
	statusInterval(t, app, ws)

	var cmds []tea.Cmd
	if !app.handleRunOutputInput(tea.KeyPressMsg{Code: 'U', Text: "U"}, &cmds) {
		t.Fatal("U was not intercepted")
	}
	cmds = cmds[:0]
	app.handleDialogInput(tea.KeyPressMsg{Code: tea.KeyEnter}, &cmds)
	for _, cmd := range cmds {
		if cmd != nil {
			if _, ok := cmd().(boundDialogResultMsg); ok {
				t.Fatal("empty Enter emitted a confirmed result")
			}
		}
	}
	if app.dialog == nil || !app.dialog.Visible() {
		t.Fatal("empty Enter closed the confirm dialog")
	}
	if trusted, _ := scripts.ScriptsTrusted(ws.Repo); !trusted {
		t.Fatal("grant dropped on a blocked Enter")
	}
}

// TestUntrustConfirmation_ConsumerValidation: the handler re-gates the typed
// word and the bound workspace — the field validator is interactive-only, so
// a result arriving otherwise must still prove intent.
func TestUntrustConfirmation_ConsumerValidation(t *testing.T) {
	app, scripts := untrustStatusApp(t)
	ws := trustedStatusRepo(t, scripts, true)
	snapTrusted := func() {
		t.Helper()
		if trusted, _ := scripts.ScriptsTrusted(ws.Repo); !trusted {
			t.Fatal("grant dropped on a rejected confirmation")
		}
	}

	rejects := []struct {
		name      string
		value     string
		workspace *data.Workspace
		confirmed bool
	}{
		{"empty", "", ws, true},
		{"whitespace", "   ", ws, true},
		{"wrong word", "yes", ws, true},
		{"missing workspace", untrustConfirmWord, nil, true},
		{"not confirmed", untrustConfirmWord, ws, false},
	}
	for _, tc := range rejects {
		t.Run("reject_"+tc.name, func(t *testing.T) {
			cmd := dialogResultUntrustScripts(app, common.DialogResult{
				ID: DialogUntrustScripts, Confirmed: tc.confirmed, Value: tc.value,
			}, dialogContext{workspace: tc.workspace})
			if cmd != nil {
				t.Fatal("rejected confirmation produced a revoke cmd")
			}
			snapTrusted()
		})
	}

	cmd := dialogResultUntrustScripts(app, common.DialogResult{
		ID: DialogUntrustScripts, Confirmed: true, Value: "  " + untrustConfirmWord + " ",
	}, dialogContext{workspace: ws})
	if cmd == nil {
		t.Fatal("valid confirmation produced no revoke cmd")
	}
	res, ok := cmd().(untrustResultMsg)
	if !ok || res.err != nil {
		t.Fatalf("revoke = %+v, want err nil", res)
	}
	if trusted, _ := scripts.ScriptsTrusted(ws.Repo); trusted {
		t.Fatal("grant survived a confirmed revoke")
	}
}

// TestUntrustConfirmation_FailedRevokeKeepsGrant covers the fail-closed
// direction end to end: a corrupt registry makes the store refuse the
// delete, the result surfaces an error, and the recorded grant is untouched.
func TestUntrustConfirmation_FailedRevokeKeepsGrant(t *testing.T) {
	app, scripts := untrustStatusApp(t)
	ws := trustedStatusRepo(t, scripts, true)

	// Corrupt the registry the runner's store reads — the revoke must refuse
	// rather than blindly rewrite bytes it cannot inspect.
	registry := filepath.Join(os.Getenv("HOME"), ".amux", "trusted-scripts.json")
	if err := os.WriteFile(registry, []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt registry seed: %v", err)
	}

	cmd := dialogResultUntrustScripts(app, common.DialogResult{
		ID: DialogUntrustScripts, Confirmed: true, Value: untrustConfirmWord,
	}, dialogContext{workspace: ws})
	if cmd == nil {
		t.Fatal("confirm produced no revoke cmd")
	}
	res, ok := cmd().(untrustResultMsg)
	if !ok {
		t.Fatalf("cmd emitted %T, want untrustResultMsg", cmd())
	}
	if res.err == nil {
		t.Fatal("expected revoke to fail on a corrupt registry")
	}
	// The result handler surfaces the failure to the user.
	if c := app.handleUntrustResult(res); c == nil {
		t.Fatal("failed revoke produced no user-facing toast")
	}
	if !app.toast.Visible() {
		t.Fatal("revoke failure did not surface a toast")
	}
	// Registry bytes preserved — the grant payload is still there to honor
	// once the file is repaired.
	after, err := os.ReadFile(registry)
	if err != nil || string(after) != "{corrupt" {
		t.Fatalf("failed revoke mutated the registry: %v", err)
	}
}

// TestUntrustResult_SuccessToasts: a clean revoke answers with a visible
// toast so the closed viewer's state change isn't silent.
func TestUntrustResult_SuccessToasts(t *testing.T) {
	app, _ := untrustStatusApp(t)
	if cmd := app.handleUntrustResult(untrustResultMsg{workspace: "ws"}); cmd == nil {
		t.Fatal("successful revoke produced no user-facing toast")
	}
	if !app.toast.Visible() {
		t.Fatal("successful revoke toast not visible")
	}
}
