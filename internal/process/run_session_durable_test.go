package process

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestRunScriptHostedUsesDurableReservation proves the hosted run path draws
// its env from the durable registry: the session's captured env carries the
// committed interval, a second runner on the same state home sees the same
// interval rather than allocating a second range, and an unsaved workspace
// degrades to transient allocation instead of failing closed.
func TestRunScriptHostedUsesDurableReservation(t *testing.T) {
	home, meta := t.TempDir(), t.TempDir()
	newRunner := func() (*ScriptRunner, *fakeRunSessionHost) {
		r := NewScriptRunner(6200, 10)
		store := data.NewPortReservationStore(home)
		if err := store.Initialize(nil); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		r.SetPortReservationStore(store)
		host := newFakeRunSessionHost()
		r.SetRunHost(host)
		return r, host
	}

	runnerA, hostA := newRunner()
	ws := savedWorkspace(t, meta, "ws")
	ws.Scripts.Run = "make dev"
	ws.ScriptMode = "concurrent"
	if _, err := runnerA.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() error = %v", err)
	}
	sess := hostA.sessions[hostA.ensured[0]]
	envJoined := "\n" + strings.Join(sess.env, "\n") + "\n"
	if !strings.Contains(envJoined, "\nAMUX_PORT=6200\n") || !strings.Contains(envJoined, "\nAMUX_PORT_RANGE=6200-6209\n") {
		t.Fatalf("session env lacks the committed interval: %v", sess.env)
	}

	// The "restarted" runner mints no new range for the same stored ID.
	runnerB, hostB := newRunner()
	if _, err := runnerB.RunScript(ws, ScriptRun); err != nil {
		t.Fatalf("RunScript() on second runner error = %v", err)
	}
	sessB := hostB.sessions[hostB.ensured[0]]
	envJoinedB := "\n" + strings.Join(sessB.env, "\n") + "\n"
	if !strings.Contains(envJoinedB, "\nAMUX_PORT=6200\n") {
		t.Fatalf("second instance session env = %v, want alpha's retained interval", sessB.env)
	}

	// Unsaved workspace: degrades to transient allocation, session still mints.
	unsaved := newHostedWorkspace(t, "concurrent")
	if _, err := runnerB.RunScript(unsaved, ScriptRun); err != nil {
		t.Fatalf("RunScript(unsaved) = %v, want transient fallback", err)
	}
	if len(hostB.ensured) != 2 {
		t.Fatalf("ensured sessions = %d, want 2 (saved ws + degraded unsaved ws)", len(hostB.ensured))
	}
	sessU := hostB.sessions[hostB.ensured[1]]
	envJoinedU := "\n" + strings.Join(sessU.env, "\n") + "\n"
	if !strings.Contains(envJoinedU, "\nAMUX_PORT=") {
		t.Fatalf("degraded session env lacks AMUX_PORT: %v", sessU.env)
	}
}
