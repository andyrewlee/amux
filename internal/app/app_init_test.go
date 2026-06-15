package app

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
)

// swapWatcherConstructors installs failing/stub watcher constructors for the
// duration of a test and restores the originals on cleanup. The seams default
// to the real constructors in production; only tests reassign them.
func swapWatcherConstructors(
	t *testing.T,
	fileFn func(func(string)) (*git.FileWatcher, error),
	stateFn func(string, string, func(string, []string)) (*stateWatcher, error),
) {
	t.Helper()
	origFile, origState := newFileWatcherFn, newStateWatcherFn
	t.Cleanup(func() {
		newFileWatcherFn = origFile
		newStateWatcherFn = origState
	})
	if fileFn != nil {
		newFileWatcherFn = fileFn
	}
	if stateFn != nil {
		newStateWatcherFn = stateFn
	}
}

// newAppWithFailingWatchers builds a real App via New() with both watcher
// constructors forced to fail, isolating the home directory to a temp dir so
// config/path creation stays hermetic. The supervisor's tab-actor goroutine is
// stopped on cleanup so the test does not leak it.
func newAppWithFailingWatchers(t *testing.T) (*App, error) {
	t.Helper()
	// Isolate ~/.amux so DefaultConfig()/EnsureDirectories() touch only a temp dir.
	t.Setenv("HOME", t.TempDir())

	swapWatcherConstructors(t,
		func(func(string)) (*git.FileWatcher, error) {
			return nil, errors.New("forced file watcher failure")
		},
		func(string, string, func(string, []string)) (*stateWatcher, error) {
			return nil, errors.New("forced state watcher failure")
		},
	)

	app, err := New("test-version", "test-commit", "test-date")
	if app != nil && app.supervisor != nil {
		t.Cleanup(app.supervisor.Stop)
	}
	return app, err
}

// TestNew_WatcherConstructionFailure_ReturnsUsableApp pins that when both the
// file and state watcher constructors fail, New() still returns a non-nil App
// with the watchers left nil and the disabled flags set, rather than erroring or
// leaving a half-built struct.
func TestNew_WatcherConstructionFailure_ReturnsUsableApp(t *testing.T) {
	app, err := newAppWithFailingWatchers(t)
	if err != nil {
		t.Fatalf("New() returned error despite watcher construction failing: %v", err)
	}
	if app == nil {
		t.Fatal("New() returned nil app despite watcher construction failing")
	}
	if app.fileWatcher != nil {
		t.Error("expected fileWatcher to be nil after construction failure")
	}
	if app.stateWatcher != nil {
		t.Error("expected stateWatcher to be nil after construction failure")
	}
	if app.fileWatcherErr == nil {
		t.Error("expected fileWatcherErr to be set after construction failure")
	}
	if app.stateWatcherErr == nil {
		t.Error("expected stateWatcherErr to be set after construction failure")
	}
}

// TestStartWatchers_NilWatchersReturnNilCmd guards the nil-watcher fast paths:
// with no watchers wired, the start commands must short-circuit to nil instead
// of dereferencing a nil watcher or reading from a nil channel.
func TestStartWatchers_NilWatchersReturnNilCmd(t *testing.T) {
	app := &App{}
	if cmd := app.startFileWatcher(); cmd != nil {
		t.Error("startFileWatcher should return nil when fileWatcher is nil")
	}
	if cmd := app.startStateWatcher(); cmd != nil {
		t.Error("startStateWatcher should return nil when stateWatcher is nil")
	}
}

// TestInit_WithFailingWatchers_NoPanic drives the real Init() on an app whose
// watchers failed to construct and asserts it produces a non-nil batch command
// without panicking. It deliberately does not claim anything about how many
// warning toasts the batch holds: Init folds the toast commands into a
// tea.Batch, which returns an opaque tea.Cmd whose contents/length cannot be
// inspected. The exact warning-queuing behavior is pinned separately against the
// watcherWarningCmds helper in TestWatcherWarningCmds.
func TestInit_WithFailingWatchers_NoPanic(t *testing.T) {
	app, err := newAppWithFailingWatchers(t)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	var cmd tea.Cmd
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Init() panicked with nil watchers: %v", r)
			}
		}()
		cmd = app.Init()
	}()
	if cmd == nil {
		t.Fatal("Init() returned nil command")
	}
}

// TestWatcherWarningCmds pins the warning-queuing behavior that Init relies on
// but cannot itself assert (Init batches the commands into an opaque tea.Cmd).
// The helper must emit exactly one warning toast per set watcher-err flag, in a
// fixed file-then-state order, and nothing when both watchers came up cleanly.
func TestWatcherWarningCmds(t *testing.T) {
	tests := []struct {
		name      string
		fileErr   bool
		stateErr  bool
		wantCount int
	}{
		{name: "no failures", fileErr: false, stateErr: false, wantCount: 0},
		{name: "file watcher only", fileErr: true, stateErr: false, wantCount: 1},
		{name: "state watcher only", fileErr: false, stateErr: true, wantCount: 1},
		{name: "both watchers", fileErr: true, stateErr: true, wantCount: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// newAppShell wires a real toast model without touching any watcher
			// services, so the helper's toast.ShowWarning calls are exercised in
			// isolation from New().
			app := newAppShell(nil)
			if tt.fileErr {
				app.fileWatcherErr = errors.New("forced file watcher failure")
			}
			if tt.stateErr {
				app.stateWatcherErr = errors.New("forced state watcher failure")
			}

			cmds := app.watcherWarningCmds()
			if len(cmds) != tt.wantCount {
				t.Fatalf("watcherWarningCmds() returned %d commands, want %d", len(cmds), tt.wantCount)
			}
			for i, cmd := range cmds {
				if cmd == nil {
					t.Errorf("watcherWarningCmds()[%d] is nil; warning toast command must be non-nil", i)
				}
			}
		})
	}
}

// TestUpdateAndView_WithFailingWatchers_NoPanic confirms the app remains usable
// after a watcher construction failure: a representative Update and a View must
// not panic with nil watchers in place.
func TestUpdateAndView_WithFailingWatchers_NoPanic(t *testing.T) {
	app, err := newAppWithFailingWatchers(t)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Update/View panicked with nil watchers: %v", r)
		}
	}()

	// A window-size message exercises layout/render wiring without needing a PTY.
	model, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if model == nil {
		t.Fatal("Update returned nil model")
	}
	// Drive the watcher-event handlers directly to prove the nil-watcher paths
	// are guarded end to end, not just at start.
	if m, _ := app.Update(messages.FileWatcherEvent{Root: "/tmp/does-not-matter"}); m == nil {
		t.Fatal("Update(FileWatcherEvent) returned nil model")
	}
	if m, _ := app.Update(messages.StateWatcherEvent{Reason: "workspaces"}); m == nil {
		t.Fatal("Update(StateWatcherEvent) returned nil model")
	}

	_ = app.View()

	// Update and View recover any panic into app.err and still return a non-nil
	// model/view, so the model != nil checks above cannot actually observe a
	// nil-watcher panic. Assert app.err stayed nil to make this a real no-panic proof.
	if app.err != nil {
		t.Fatalf("app recorded an error from Update/View with nil watchers: %v", app.err)
	}
}
