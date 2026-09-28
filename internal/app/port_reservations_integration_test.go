package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Real-tmux persistence proof: a workspace's port reservation is committed to
// the shared registry BEFORE its run session exists, so an app restart (a new
// runner + host against the same state home) cannot hand that range to a
// different workspace while the old session is still alive. The oracles are
// the registry contents and the pane output of the actual sessions — never
// listening-port probing.

func portReservationsTmuxAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
}

// portReservationsTestServer returns tmux options pointed at an isolated
// server socket and kills that server when the test ends — scoped strictly to
// the test-created server.
func portReservationsTestServer(t *testing.T) tmux.Options {
	t.Helper()
	name := fmt.Sprintf("amux-portres-%d", time.Now().UnixNano())
	opts := tmux.Options{ServerName: name, ConfigPath: "/dev/null", CommandTimeout: 10 * time.Second}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", name, "kill-server").Run()
	})
	// exit-empty=off keeps the isolated server alive between session churn.
	out, err := exec.Command("tmux", "-L", name, "-f", "/dev/null",
		"new-session", "-d", "-s", "_keepalive", "sleep 300").CombinedOutput()
	if err != nil {
		t.Skipf("isolated tmux server unavailable: %v\n%s", err, out)
	}
	out, err = exec.Command("tmux", "-L", name, "set-option", "-s", "exit-empty", "off").CombinedOutput()
	if err != nil {
		t.Skipf("tmux server option unavailable: %v\n%s", err, out)
	}
	if out, err := exec.Command("tmux", "-L", name, "kill-session", "-t", "_keepalive").CombinedOutput(); err != nil {
		t.Skipf("tmux keepalive teardown failed: %v\n%s", err, out)
	}
	return opts
}

// portReservationsSavedWorkspace writes a workspace record so it carries the
// persisted store ID durable reservations key on.
func portReservationsSavedWorkspace(t *testing.T, store *data.WorkspaceStore, base, name, runCmd string) *data.Workspace {
	t.Helper()
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := data.NewWorkspace(name, name, "main", filepath.Join(base, "repo-"+name), root)
	ws.Scripts.Run = runCmd
	if err := store.Save(ws); err != nil {
		t.Fatalf("Save(%s): %v", name, err)
	}
	if _, ok := ws.StoredID(); !ok {
		t.Fatalf("saved workspace %s has no StoredID — fixture broken", name)
	}
	return ws
}

// mustStoredID unwraps the persisted store key the durable registry keys on.
func mustStoredID(t *testing.T, ws *data.Workspace) data.WorkspaceID {
	t.Helper()
	id, ok := ws.StoredID()
	if !ok {
		t.Fatalf("workspace %s lacks StoredID", ws.Name)
	}
	return id
}

// portReservationsRunEnv waits for the session's pane to print its injected
// port env and returns the "AMUX_PORT=N AMUX_PORT_RANGE=B-E" line.
func portReservationsRunEnv(t *testing.T, host *tmuxRunSessionHost, sessionName string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out := host.Tail(sessionName, 20)
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "AMUX_PORT=") {
				return strings.TrimSpace(line)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	list, _ := exec.Command("tmux", "-L", host.opts.ServerName, "list-sessions", "-F", "#{session_name} inst=#{@amux_instance} ws=#{@amux_workspace}").CombinedOutput()
	t.Fatalf("session %s never printed its AMUX_PORT env line; tail: %q; sessions: %s", sessionName, host.Tail(sessionName, 20), list)
	return ""
}

// portReservationsSession creates a detached session on the isolated server.
func portReservationsSession(t *testing.T, opts tmux.Options, name, command string, tags map[string]string) {
	t.Helper()
	out, err := exec.Command("tmux", "-L", opts.ServerName, "-f", "/dev/null",
		"new-session", "-d", "-s", name, "sh", "-c", command).CombinedOutput()
	if err != nil {
		t.Fatalf("create session %q: %v\n%s", name, err, out)
	}
	for k, v := range tags {
		if out, err := exec.Command("tmux", "-L", opts.ServerName,
			"set-option", "-t", name, k, v).CombinedOutput(); err != nil {
			t.Fatalf("set %s=%s on %s: %v\n%s", k, v, name, err, out)
		}
	}
}

func portReservationsKillSession(t *testing.T, opts tmux.Options, name string) {
	t.Helper()
	out, err := exec.Command("tmux", "-L", opts.ServerName, "kill-session", "-t", name).CombinedOutput()
	if err != nil {
		t.Fatalf("kill session %q: %v\n%s", name, err, out)
	}
}

// TestPortReservationAdoptionGuardRealTmux exercises the first-upgrade guard
// against a live (isolated) tmux server: a surviving amux session blocks
// adoption and leaves no registry behind; a foreign-namespace or non-amux
// session does not.
func TestPortReservationAdoptionGuardRealTmux(t *testing.T) {
	portReservationsTmuxAvailable(t)
	opts := portReservationsTestServer(t)

	home := t.TempDir()
	store := data.NewPortReservationStore(home)
	guard := portReservationGuard(opts, newInstanceID(home))

	// A legacy-looking amux session (amux- name, no usable instance tag)
	// blocks first adoption — the registry must NOT be created.
	portReservationsSession(t, opts, "amux-legacy-run", "sleep 300", nil)
	err := store.Initialize(guard)
	if !errors.Is(err, errPortReservationAdoptionBlocked) {
		t.Fatalf("expected adoption-blocked error, got %v", err)
	}
	if _, statErr := os.Stat(store.Path()); !os.IsNotExist(statErr) {
		t.Fatal("blocked adoption must not leave a registry file behind")
	}

	// Same home, still missing file: a foreign-namespace amux session and a
	// plain non-amux session do not block — they cannot be holding this state
	// home's ranges.
	portReservationsKillSession(t, opts, "amux-legacy-run")
	foreign := otherNamespace() + ".00112233445566aa"
	portReservationsSession(t, opts, "amux-foreign-run", "sleep 300", map[string]string{
		"@amux":          "1",
		"@amux_instance": foreign,
	})
	portReservationsSession(t, opts, "plain-scratch", "sleep 300", nil)
	if err := store.Initialize(guard); err != nil {
		t.Fatalf("adoption must succeed once only foreign/non-amux sessions remain: %v", err)
	}
	if _, statErr := os.Stat(store.Path()); statErr != nil {
		t.Fatalf("registry must exist after successful adoption: %v", statErr)
	}
}

func TestPortReservationsSurviveAppRestart(t *testing.T) {
	portReservationsTmuxAvailable(t)
	opts := portReservationsTestServer(t)

	home := t.TempDir()
	metaStore := data.NewWorkspaceStore(filepath.Join(home, "workspaces"))
	reservations := data.NewPortReservationStore(home)

	// Two instance IDs under ONE state home — the two real launches share the
	// namespace, like a quit+restart or a concurrent second window.
	instanceA := newInstanceID(home)
	instanceB := newInstanceID(home)

	// First adoption against the real (empty) tmux server creates the
	// registry; a second Initialize must not re-run the guard.
	if err := reservations.Initialize(portReservationGuard(opts, instanceA)); err != nil {
		t.Fatalf("first Initialize: %v", err)
	}
	if err := reservations.Initialize(func() error { return errors.New("guard re-ran on an existing registry") }); err != nil {
		t.Fatalf("second Initialize on existing registry: %v", err)
	}

	// The run command prints its injected env, then stays alive — the pane
	// text is proof of what the session was launched with.
	runCmd := `echo "AMUX_PORT=$AMUX_PORT AMUX_PORT_RANGE=$AMUX_PORT_RANGE"; sleep 300`
	wsA := portReservationsSavedWorkspace(t, metaStore, t.TempDir(), "alpha", runCmd)
	wsA.ScriptMode = "concurrent" // the restarted instance must NOT stop the survivor
	wsB := portReservationsSavedWorkspace(t, metaStore, t.TempDir(), "beta", runCmd)
	wsC := portReservationsSavedWorkspace(t, metaStore, t.TempDir(), "gamma", runCmd)

	const start, size = 6200, 10
	newRunner := func(instanceID string) (*process.ScriptRunner, *tmuxRunSessionHost) {
		host := newTmuxRunSessionHost(opts, instanceID)
		r := process.NewScriptRunner(start, size)
		r.SetPortReservationStore(reservations)
		r.SetRunHost(host)
		return r, host
	}
	sessA := "amux-ws-" + string(wsA.ID()) + "-run"
	sessB := "amux-ws-" + string(wsB.ID()) + "-run"

	// Instance A launches alpha's run session; the reservation commits first.
	runnerA, hostA := newRunner(instanceA)
	if _, err := runnerA.RunScript(wsA, process.ScriptRun); err != nil {
		t.Fatalf("RunScript(alpha): %v", err)
	}
	envA := portReservationsRunEnv(t, hostA, sessA)
	if envA != "AMUX_PORT=6200 AMUX_PORT_RANGE=6200-6209" {
		t.Fatalf("alpha session env = %q, want first interval", envA)
	}
	base, end, found, err := reservations.Lookup(string(mustStoredID(t, wsA)))
	if err != nil || !found || base != 6200 || end != 6209 {
		t.Fatalf("registry alpha interval = %d-%d found=%v err=%v, want 6200-6209", base, end, found, err)
	}

	// "Restart": runnerA is dropped with its tmux session still alive. The
	// second instance shares the registry but has no memory of instance A's
	// allocations.
	runnerA = nil
	runnerB, hostB := newRunner(instanceB)

	// Same-ID re-allocation on the restarted instance returns alpha's
	// committed interval verbatim — no new range, no drift.
	envA2, err := runnerB.BuildSessionEnv(wsA)
	if err != nil {
		t.Fatalf("BuildSessionEnv(alpha) after restart: %v", err)
	}
	joined := strings.Join(envA2, "\n")
	if !strings.Contains(joined, "AMUX_PORT=6200\n") && !strings.HasSuffix(joined, "AMUX_PORT=6200") {
		t.Fatalf("alpha env after restart lost its reservation: %q", envA2)
	}

	// Launching a second session for alpha (concurrent default) keeps the same
	// interval — the registry, not the process, owns the range.
	if _, err := runnerB.RunScript(wsA, process.ScriptRun); err != nil {
		t.Fatalf("RunScript(alpha) after restart: %v", err)
	}
	sessA2 := sessA + "-2"
	if got := portReservationsRunEnv(t, hostB, sessA2); !strings.Contains(got, "AMUX_PORT=6200 ") {
		t.Fatalf("alpha's second session env = %q, want interval 6200-6209", got)
	}
	// The original instance-A session is still alive alongside it — the
	// restart neither killed nor re-keyed it.
	exists, alive, _, serr := hostB.Status(sessA)
	if serr != nil || !exists || !alive {
		t.Fatalf("alpha's original session after restart: exists=%v alive=%v err=%v", exists, alive, serr)
	}

	// Beta receives a disjoint interval — never alpha's retained range.
	if _, err := runnerB.RunScript(wsB, process.ScriptRun); err != nil {
		t.Fatalf("RunScript(beta): %v", err)
	}
	envB := portReservationsRunEnv(t, hostB, sessB)
	if envB != "AMUX_PORT=6210 AMUX_PORT_RANGE=6210-6219" {
		t.Fatalf("beta session env = %q, want the disjoint second interval", envB)
	}

	// Release with alpha's sessions still live is retention-preserving: the
	// delayed-release path can never hand a surviving session's range away.
	runnerB.ReleaseWorkspace(wsA)
	if base, end, found, err := runnerB.PortInterval(wsA); err != nil || !found || base != 6200 || end != 6209 {
		t.Fatalf("alpha interval after release = %d-%d found=%v err=%v, want retained 6200-6209", base, end, found, err)
	}

	// Gamma still cannot steal alpha's retained range.
	envC, err := runnerB.BuildSessionEnv(wsC)
	if err != nil {
		t.Fatalf("BuildSessionEnv(gamma): %v", err)
	}
	joined = strings.Join(envC, "\n")
	if !strings.Contains(joined, "AMUX_PORT=6220\n") && !strings.HasSuffix(joined, "AMUX_PORT=6220") {
		t.Fatalf("gamma env = %q, want the third interval — alpha's stayed reserved", envC)
	}
}
