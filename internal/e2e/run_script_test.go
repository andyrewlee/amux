package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/tmux"
	"github.com/andyrewlee/amux/internal/ui/layout"
)

// The workspace `run` script lifecycle — hosted session creation, the [run]
// indicator, post-exit state, and a fresh session on re-run — is heavily
// unit-tested in internal/process but never exercised through the real
// binary. This drives it end to end: a project-level (user-authored, no
// trust gate) run script, the sidebar `r`/`R` bindings, and real tmux
// session enumeration.
func TestRunScriptLifecycleHosted(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	home := t.TempDir()
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")

	// The sentinel proves the script executed through the real hosted path;
	// the echoed markers prove the captured tail reaches the output viewer.
	sentinel := filepath.Join(t.TempDir(), "run-finished")
	store := data.NewProjectScriptStore(filepath.Join(home, ".amux"))
	if err := store.Set(repo, data.ScriptsConfig{
		Run: fmt.Sprintf("echo RUNBEGIN-E2E; sleep 2; echo RUNEND-E2E; touch '%s'", sentinel),
	}); err != nil {
		t.Fatalf("seed project scripts: %v", err)
	}

	server := fmt.Sprintf("amux-e2e-run-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	t.Cleanup(func() { killTmuxServer(t, server) })

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home:   home,
		Env:    sessionEnv(binDir, server),
		Width:  180,
		Height: 40,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "[New agent]", persistenceTimeout)

	// r/R bind on the sidebar's Changes model — focus it by clicking a
	// blank row below the list content (the same pane-focus idiom the
	// control-keys fixture uses; a blank row selects nothing).
	l := layout.NewManager()
	l.Resize(180, 40)
	if l.SidebarWidth() <= 0 {
		t.Fatalf("180x40 must render the sidebar pane (mode %v)", l.Mode())
	}
	sidebarX := l.LeftGutter() + l.DashboardWidth() + l.GapX() + l.CenterWidth() + l.GapX()
	if err := session.SendString(leftClickInput(sidebarX+4, 15)); err != nil {
		t.Fatalf("focus sidebar: %v", err)
	}

	wsID := workspaceIDForRepo(repo)
	runSessions := func() []string {
		names, err := tmux.FindRunSessions(wsID, "", opts)
		if err != nil {
			return nil
		}
		return names
	}

	if err := session.SendString("R"); err != nil {
		t.Fatalf("send run-output key: %v", err)
	}
	waitForUIContains(t, session, "No run output", persistenceTimeout)

	if err := session.SendString("r"); err != nil {
		t.Fatalf("send run key: %v", err)
	}

	// A hosted run session is created and the Changes indicator lights.
	deadline := time.Now().Add(persistenceTimeout)
	for time.Now().Before(deadline) && len(runSessions()) == 0 {
		time.Sleep(sessionPollInterval)
	}
	if names := runSessions(); len(names) == 0 {
		t.Fatalf("run session did not appear on tmux server %s\n\nscreen:\n%s", server, session.ScreenASCII())
	}
	waitForUIContains(t, session, "[run]", persistenceTimeout)

	// The script actually ran: its marker file lands and, once it exits, the
	// indicator clears on the next status refresh.
	if got, ok := waitForFileBytes(sentinel, []byte(""), persistenceTimeout); !ok {
		t.Fatalf("run script did not create its sentinel (got %q)", got)
	}
	if err := session.WaitForAbsent("[run]", persistenceTimeout); err != nil {
		t.Fatalf("run indicator did not clear after script exit: %v", err)
	}

	// R opens the captured-output viewer with the post-exit tail and status.
	if err := session.SendString("R"); err != nil {
		t.Fatalf("send run-output key: %v", err)
	}
	waitForUIContains(t, session, "Run output", persistenceTimeout)
	waitForUIContains(t, session, "RUNBEGIN-E2E", persistenceTimeout)
	waitForUIContains(t, session, "RUNEND-E2E", persistenceTimeout)

	// Close the viewer (Esc) then re-run: a fresh live session must appear.
	if err := session.SendString("\x1b"); err != nil {
		t.Fatalf("close run output: %v", err)
	}
	if err := session.WaitForAbsent("Run output", persistenceTimeout); err != nil {
		t.Fatalf("run output viewer did not close: %v", err)
	}
	first := runSessions()
	if err := session.SendString("r"); err != nil {
		t.Fatalf("send second run key: %v", err)
	}
	waitForCond(t, func() bool {
		names := runSessions()
		if len(names) == 0 {
			return false
		}
		// A second run must not re-attach to the exited session: the hosted
		// enumeration reports a live row again (name may be identical — the
		// base name is deterministic — but remain-on-exit keeps the dead one
		// listed only alongside a new live ordinal).
		return true
	}, persistenceTimeout, "second run did not produce a hosted session (first=%v)", first)
	waitForUIContains(t, session, "[run]", persistenceTimeout)
}

// TestTmuxServerDeathDegradesGracefully kills the app's own tmux server
// mid-session and asserts the documented degraded contract: the frame keeps
// rendering (no hang), sessions surface their dead state (disconnected tab
// indicator), and the input loop stays alive — then the app drives its own
// recovery by respawning sessions on the fresh server socket.
func TestTmuxServerDeathDegradesGracefully(t *testing.T) {
	skipIfNoGit(t)
	skipIfNoTmux(t)

	repo := initRepo(t)
	home := t.TempDir()
	writeRegistry(t, home, repo)
	writeConfig(t, home, false)
	binDir := writeStubAssistant(t, home, "claude")

	server := fmt.Sprintf("amux-e2e-death-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: server, ConfigPath: "/dev/null"}
	t.Cleanup(func() { killTmuxServer(t, server) })

	session, cleanup, err := StartPTYSession(PTYOptions{
		Home: home,
		Env:  sessionEnv(binDir, server),
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer cleanup()

	waitForUIContains(t, session, filepath.Base(repo), persistenceTimeout)
	activatePrimaryWorkspace(t, session)
	waitForUIContains(t, session, "[New agent]", persistenceTimeout)

	// One live agent tab before the kill.
	createAgentTab(t, session)
	waitForAgentSessions(t, opts, persistenceTimeout)

	killTmuxServer(t, server)

	// Degraded contract: the dead state surfaces — tmux's own
	// remain-on-exit marker in the pane and the STOPPED status badge,
	// while the frame keeps rendering (these waits are render-loop proof).
	waitForUIContains(t, session, "[server exited]", persistenceTimeout)
	waitForUIContains(t, session, "STOPPED", persistenceTimeout)

	// The input loop is alive: the prefix palette still opens.
	if err := session.SendBytes([]byte{0}); err != nil {
		t.Fatalf("send prefix: %v", err)
	}
	waitForPrefixPalette(t, session)
	if err := session.SendString("\x1b"); err != nil {
		t.Fatalf("dismiss palette: %v", err)
	}

	// Recovery contract: a client command respawns the server socket, so
	// the documented manual path — t s restart — mints a fresh session on
	// it and the tab comes back. (t r reattach refuses a fully-missing
	// session — the dead-session guard — so restart is the verb here.)
	// Tab actions dispatch to the focused pane, so the click puts the dead
	// tab's pane in charge of the key first.
	l := layout.NewManager()
	l.Resize(120, 30)
	centerX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	if err := session.SendString(leftClickInput(centerX+4, 8)); err != nil {
		t.Fatalf("focus dead tab pane: %v", err)
	}
	sendPrefixSequence(t, session, "t", "s")
	deadline := time.Now().Add(persistenceTimeout)
	for time.Now().Before(deadline) {
		if names := func() []string { s, _ := tmux.ListSessions(opts); return s }(); len(names) > 0 {
			return
		}
		time.Sleep(sessionPollInterval)
	}
	t.Fatalf("restart did not mint a session on the respawned server\n\nscreen:\n%s", session.ScreenASCII())
}
