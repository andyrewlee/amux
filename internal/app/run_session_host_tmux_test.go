package app

import (
	"os/exec"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/testutil"
	"github.com/andyrewlee/amux/internal/tmux"
)

// Real-tmux coverage for the tmuxRunSessionHost batching surface — the fake
// host pins the seams elsewhere; only a live server proves StatusAll's
// batched enumeration, dead-pane exit-code recovery, and FindDetailed's
// tag/instance filtering against real tmux behavior.

// runHostSession mints a detached session on the isolated server carrying the
// run-session ownership shape FindDetailed filters on.
func runHostSession(t *testing.T, opts tmux.Options, name, wsID, instanceID, command string, extra map[string]string) {
	t.Helper()
	tags := map[string]string{
		"@amux":           "1",
		"@amux_workspace": wsID,
		"@amux_type":      "run",
	}
	if instanceID != "" {
		tags["@amux_instance"] = instanceID
	}
	for k, v := range extra {
		tags[k] = v
	}
	portReservationsSession(t, opts, name, command, tags)
}

func TestRunSessionHostStatusAllRealTmux(t *testing.T) {
	portReservationsTmuxAvailable(t)
	opts := portReservationsTestServer(t)
	host := newTmuxRunSessionHost(opts, "mynst.launch")

	live := "rrs-live"
	runHostSession(t, opts, live, "ws-1", "mynst.launch", "sleep 300", nil)

	// A live session resolves fully from the batch: Exists+Alive with the
	// no-exit-code sentinel — no per-session probe is needed to say so.
	testutil.Eventually(t, 10*time.Second, 50*time.Millisecond, func() bool {
		states, err := host.StatusAll([]string{live})
		if err != nil {
			return false
		}
		st := states[live]
		return st.Exists && st.Alive && st.ExitCode == -1
	}, "live session never reported Exists+Alive via StatusAll")

	// Missing sessions are simply absent from the map.
	states, err := host.StatusAll([]string{live, "rrs-never-created"})
	if err != nil {
		t.Fatalf("StatusAll: %v", err)
	}
	if _, ok := states["rrs-never-created"]; ok {
		t.Fatal("missing session reported by StatusAll")
	}

	// remain-on-exit keeps a dead pane (and its exit status) visible to the
	// batched scan + per-session fallback probe.
	if out, err := exec.Command("tmux", "-L", opts.ServerName,
		"set-option", "-g", "remain-on-exit", "on").CombinedOutput(); err != nil {
		t.Fatalf("set remain-on-exit: %v\n%s", err, out)
	}
	dead := "rrs-dead"
	runHostSession(t, opts, dead, "ws-1", "mynst.launch", "exit 7", nil)

	var st process.RunSessionState
	testutil.Eventually(t, 10*time.Second, 50*time.Millisecond, func() bool {
		states, err := host.StatusAll([]string{live, dead})
		if err != nil {
			return false
		}
		var ok bool
		st, ok = states[dead]
		return ok && st.Exists && !st.Alive
	}, "dead session never surfaced as Exists+dead via StatusAll")
	if st.ExitCode != 7 {
		t.Fatalf("dead session ExitCode = %d, want 7", st.ExitCode)
	}
	// The still-live sibling keeps resolving alongside the dead one.
	states, err = host.StatusAll([]string{live, dead})
	if err != nil {
		t.Fatalf("StatusAll mixed: %v", err)
	}
	if s := states[live]; !s.Exists || !s.Alive {
		t.Fatalf("live sibling lost beside dead session: %+v", s)
	}
}

func TestRunSessionHostFindDetailedRealTmux(t *testing.T) {
	portReservationsTmuxAvailable(t)
	opts := portReservationsTestServer(t)
	host := newTmuxRunSessionHost(opts, "mynst.launch")

	// Owned, correctly tagged session with a creation stamp.
	runHostSession(t, opts, "rrd-mine", "ws-1", "mynst.launch", "sleep 300",
		map[string]string{"@amux_created_at": "1700000042"})
	// Same workspace, same namespace, different launch suffix — a restarted
	// amux still owns it.
	runHostSession(t, opts, "rrd-relaunch", "ws-1", "mynst.other", "sleep 300", nil)
	// Pre-tagging session: no @amux_instance at all — still visible.
	runHostSession(t, opts, "rrd-pretag", "ws-1", "", "sleep 300", nil)
	// Foreign instance namespace — excluded even with the workspace tag.
	runHostSession(t, opts, "rrd-foreign", "ws-1", "otherns.abc", "sleep 300", nil)
	// Foreign workspace — excluded outright.
	runHostSession(t, opts, "rrd-otherws", "ws-2", "mynst.launch", "sleep 300", nil)
	// Garbage creation tag — malformed values must not error the scan; the
	// row parses to CreatedAt=0 (pre-tagging semantics).
	runHostSession(t, opts, "rrd-garbage", "ws-1", "mynst.launch", "sleep 300",
		map[string]string{"@amux_created_at": "notanumber"})

	var refs []process.RunSessionRef
	testutil.Eventually(t, 10*time.Second, 50*time.Millisecond, func() bool {
		var err error
		refs, err = host.FindDetailed("ws-1")
		return err == nil && len(refs) == 4
	}, "FindDetailed never returned exactly the 4 owned sessions")

	got := map[string]int64{}
	for _, ref := range refs {
		got[ref.Name] = ref.CreatedAt
	}
	if got["rrd-mine"] != 1700000042 {
		t.Fatalf("rrd-mine CreatedAt = %d, want 1700000042", got["rrd-mine"])
	}
	if got["rrd-relaunch"] != 0 {
		t.Fatalf("rrd-relaunch CreatedAt = %d, want 0 (no tag)", got["rrd-relaunch"])
	}
	if got["rrd-pretag"] != 0 {
		t.Fatalf("rrd-pretag CreatedAt = %d, want 0", got["rrd-pretag"])
	}
	if got["rrd-garbage"] != 0 {
		t.Fatalf("rrd-garbage CreatedAt = %d, want 0 (malformed tag)", got["rrd-garbage"])
	}
	for _, foreign := range []string{"rrd-foreign", "rrd-otherws"} {
		if _, ok := got[foreign]; ok {
			t.Fatalf("foreign session %s leaked into FindDetailed", foreign)
		}
	}
}
