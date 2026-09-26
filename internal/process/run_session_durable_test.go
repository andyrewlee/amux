package process

import (
	"errors"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestRunScriptHostedUsesDurableReservation proves the hosted run path draws
// its env from the durable registry: the session's captured env carries the
// committed interval, a second runner on the same state home sees the same
// interval rather than allocating a second range, and an unsaved workspace
// fails closed with ErrWorkspaceMetadataNotPersisted before any session is
// created.
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

	// Unsaved workspace: fail closed, no session minted.
	unsaved := newHostedWorkspace(t, "concurrent")
	if _, err := runnerB.RunScript(unsaved, ScriptRun); !errors.Is(err, ErrWorkspaceMetadataNotPersisted) {
		t.Fatalf("RunScript(unsaved) = %v, want ErrWorkspaceMetadataNotPersisted", err)
	}
	if len(hostB.ensured) != 1 {
		t.Fatalf("unsaved workspace created %d sessions, want 1 (only the saved ws's)", len(hostB.ensured))
	}
}
