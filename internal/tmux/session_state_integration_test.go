package tmux

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// AllSessionStates integration tests
// ---------------------------------------------------------------------------

func TestAllSessionStates_EmptyServer(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	states, err := AllSessionStates(opts)
	if err != nil {
		t.Fatalf("AllSessionStates: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("expected empty map for empty server, got %v", states)
	}
}

func TestAllSessionStates_SingleLiveSession(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "live", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	states, err := AllSessionStates(opts)
	if err != nil {
		t.Fatalf("AllSessionStates: %v", err)
	}
	st, ok := states["live"]
	if !ok {
		t.Fatal("expected session 'live' in states map")
	}
	if !st.Exists {
		t.Fatal("expected Exists=true for live session")
	}
	if !st.HasLivePane {
		t.Fatal("expected HasLivePane=true for live session")
	}
}

func TestAllSessionStates_MultipleSessions(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	createSession(t, opts, "sess-a", "sleep 300")
	createSession(t, opts, "sess-b", "sleep 300")
	time.Sleep(50 * time.Millisecond)

	states, err := AllSessionStates(opts)
	if err != nil {
		t.Fatalf("AllSessionStates: %v", err)
	}
	for _, name := range []string{"sess-a", "sess-b"} {
		st, ok := states[name]
		if !ok {
			t.Fatalf("expected session %q in states map", name)
		}
		if !st.Exists || !st.HasLivePane {
			t.Fatalf("session %q: expected Exists=true, HasLivePane=true, got %+v", name, st)
		}
	}
}
