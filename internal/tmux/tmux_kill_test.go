//go:build !windows

package tmux

import (
	"syscall"
	"testing"
	"time"
)

func TestKillSession_KillsProcessTree(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Spawn a session whose pane runs a parent with two children.
	createSession(t, opts, "tree", "sleep 300 & sleep 300 & wait")
	time.Sleep(100 * time.Millisecond)

	pids, err := PanePIDs("tree", opts)
	if err != nil {
		t.Fatalf("PanePIDs: %v", err)
	}
	if len(pids) != 1 {
		t.Fatalf("expected 1 pane PID, got %v", pids)
	}

	pid := pids[0]
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		t.Fatalf("Getpgid: %v", err)
	}

	if err := KillSession("tree", opts); err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	// Verify the process group is dead.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = syscall.Kill(-pgid, 0)
		if err == syscall.ESRCH {
			break
		}
		if err == syscall.EPERM {
			t.Skip("signal permissions restricted in this environment")
		}
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still alive after KillSession", pgid)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestKillSession_ProcessTreeAcrossWindows(t *testing.T) {
	skipIfNoTmux(t)
	opts := testServer(t)

	// Session with 2 windows, each spawning children.
	createSession(t, opts, "multi-tree", "sleep 300 & sleep 300 & wait")
	addWindow(t, opts, "multi-tree", "sleep 300 & sleep 300 & wait")
	time.Sleep(100 * time.Millisecond)

	pids, err := PanePIDs("multi-tree", opts)
	if err != nil {
		t.Fatalf("PanePIDs: %v", err)
	}
	if len(pids) != 2 {
		t.Fatalf("expected 2 pane PIDs (-s flag regression), got %v", pids)
	}

	// Collect pgids for both panes.
	pgids := make([]int, len(pids))
	for i, pid := range pids {
		pgid, err := syscall.Getpgid(pid)
		if err != nil {
			if err == syscall.EPERM {
				t.Skip("signal permissions restricted in this environment")
			}
			t.Fatalf("Getpgid(%d): %v", pid, err)
		}
		pgids[i] = pgid
	}

	if err := KillSession("multi-tree", opts); err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	// Verify both process groups are dead.
	for _, pgid := range pgids {
		deadline := time.Now().Add(2 * time.Second)
		for {
			err = syscall.Kill(-pgid, 0)
			if err == syscall.ESRCH {
				break
			}
			if err == syscall.EPERM {
				t.Skip("signal permissions restricted in this environment")
			}
			if time.Now().After(deadline) {
				t.Fatalf("process group %d still alive after KillSession", pgid)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
