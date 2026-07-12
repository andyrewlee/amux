package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/andyrewlee/amux/internal/config"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/process"
	"github.com/andyrewlee/amux/internal/supervisor"
	"github.com/andyrewlee/amux/internal/ui/center"
	"github.com/andyrewlee/amux/internal/ui/sidebar"
)

// Maintenance invariant: every new subsystem added to App that needs teardown
// must be nil-guarded in Shutdown and, where its stop is observable, added to
// TestShutdown_StopsEachSubsystemOnce below.

// TestShutdown_NilSubsystemsIsPanicFreeAndIdempotent pins the nil-guard
// skeleton: a zero-value App must survive Shutdown, and repeat calls must be
// no-ops. The sync.Once guard is proven by installing a live supervisor after
// the first call and checking a later call does not stop it.
func TestShutdown_NilSubsystemsIsPanicFreeAndIdempotent(t *testing.T) {
	a := &App{}

	a.Shutdown() // all subsystems nil: must not panic
	a.Shutdown() // second call: must not panic either

	// Idempotency: once the guard has fired, the teardown body must never run
	// again, even if a subsystem appears afterwards.
	sup := supervisor.New(context.Background())
	t.Cleanup(sup.Stop)
	a.supervisor = sup

	a.Shutdown()

	if err := sup.Context().Err(); err != nil {
		t.Fatalf("sync.Once guard broken: post-shutdown call stopped a later-installed supervisor (ctx err = %v)", err)
	}
}

// TestShutdown_StopsEachSubsystemOnce builds an App with real, cheaply
// constructed subsystems and asserts Shutdown stops each observable one
// exactly once.
//
// All six subsystem fields are concrete types, not interfaces, so recording
// fakes cannot be injected. Observable assertions:
//   - supervisor: Stop cancels its context.
//   - fileWatcher: Close closes the fsnotify watcher (Watch then fails with
//     fsnotify.ErrClosed).
//   - stateWatcher: Close sets its closed flag (same-package visibility).
//
// Exercised but not assertable (no observable stop state on an empty
// instance): center (*center.Model), sidebarTerminal (*sidebar.TerminalModel),
// workspaceService (delegates to an idle *process.ScriptRunner). Their real
// Close/CloseAll/StopAll bodies still run here, so a panic regression in any
// of them fails this test.
//
// "Exactly once" follows from the pair of checks: the stopped-assertions prove
// at-least-once, and the fresh-replacement checks after a second Shutdown
// prove the teardown body runs at most once.
func TestShutdown_StopsEachSubsystemOnce(t *testing.T) {
	sup := supervisor.New(context.Background())

	fw, err := git.NewFileWatcher(func(string) {})
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	t.Cleanup(func() { _ = fw.Close() })

	sw, err := newStateWatcher("", "", nil)
	if err != nil {
		t.Fatalf("newStateWatcher: %v", err)
	}
	t.Cleanup(func() { _ = sw.Close() })

	a := &App{
		supervisor:       sup,
		fileWatcher:      fw,
		stateWatcher:     sw,
		center:           center.New(&config.Config{}),
		sidebarTerminal:  sidebar.NewTerminalModel(),
		workspaceService: newWorkspaceService(nil, nil, process.NewScriptRunner(6400, 10), ""),
	}

	a.Shutdown()

	if err := sup.Context().Err(); !errors.Is(err, context.Canceled) {
		t.Errorf("supervisor not stopped: ctx err = %v, want context.Canceled", err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := fw.Watch(root); !errors.Is(err, fsnotify.ErrClosed) {
		t.Errorf("fileWatcher not closed: Watch after Shutdown = %v, want fsnotify.ErrClosed", err)
	}

	sw.mu.Lock()
	swClosed := sw.closed
	sw.mu.Unlock()
	if !swClosed {
		t.Error("stateWatcher not closed after Shutdown")
	}

	// Once-guard: a second Shutdown must not tear down freshly installed
	// subsystems, so no subsystem's stop can ever run twice via Shutdown.
	sup2 := supervisor.New(context.Background())
	t.Cleanup(sup2.Stop)
	sw2, err := newStateWatcher("", "", nil)
	if err != nil {
		t.Fatalf("newStateWatcher (replacement): %v", err)
	}
	t.Cleanup(func() { _ = sw2.Close() })
	a.supervisor = sup2
	a.stateWatcher = sw2

	a.Shutdown()

	if err := sup2.Context().Err(); err != nil {
		t.Errorf("second Shutdown stopped the replacement supervisor (ctx err = %v); sync.Once guard broken", err)
	}
	sw2.mu.Lock()
	sw2Closed := sw2.closed
	sw2.mu.Unlock()
	if sw2Closed {
		t.Error("second Shutdown closed the replacement state watcher; sync.Once guard broken")
	}
}
