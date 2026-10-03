package app

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/app/workspacesvc"
	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/ui/center"
)

// interceptUpdateStore wraps a real WorkspaceStore and pauses inside the
// Update transaction — immediately before the callback runs — so the test
// can queue a lifecycle writer behind whatever locks the save still holds.
type interceptUpdateStore struct {
	workspacesvc.WorkspaceStore
	beforeCallback func()
}

func (s *interceptUpdateStore) Update(id data.WorkspaceID, fn func(ws *data.Workspace) (bool, error)) error {
	return s.WorkspaceStore.Update(id, func(fresh *data.Workspace) (bool, error) {
		if s.beforeCallback != nil {
			s.beforeCallback()
		}
		return fn(fresh)
	})
}

// TestTabPersistLifecycleGuardDoesNotReenter is the lock-order regression:
// while a tab save is paused inside the store transaction still holding the
// service's lifecycle guard, a lifecycle mutation writer queues behind it.
// Releasing the barrier must let the save complete — a save that re-acquired
// the lifecycle read lock inside Update (the old code path) would deadlock:
// the writer waits on the save's first read hold while the save's second
// read acquisition waits on the writer.
func TestTabPersistLifecycleGuardDoesNotReenter(t *testing.T) {
	ws := data.NewWorkspace("feature", "feature", "main", "/repo", "/repo/feature")
	wsID := string(ws.ID())
	store := data.NewWorkspaceStore(t.TempDir())
	if err := store.Save(ws); err != nil {
		t.Fatalf("seed Save() error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	fake := &interceptUpdateStore{WorkspaceStore: store}
	fake.beforeCallback = func() {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	}
	svc := workspacesvc.New(nil, fake, nil, "")

	c := center.New(nil)
	c.SetWorkspace(ws)
	c.AddTab(&center.Tab{Name: "tab-a", Assistant: "claude", Workspace: ws})
	app := &App{
		center:           c,
		workspaceService: svc,
		lifecycle:        newWorkspaceLifecycleState(),
	}
	svc.Configure(workspacesvc.Deps{
		MutationInFlight:      app.isWorkspaceMutationInFlightWS,
		MutationInFlightGuard: app.runUnlessWorkspaceMutationInFlightWS,
	})

	// Every exit path releases the barrier so a failing test never leaves
	// the save goroutine parked inside the store.
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	snap := tabPersistSnapshot{
		wsID:      wsID,
		seq:       1,
		tabs:      []data.TabInfo{{Name: "tab-a"}},
		activeIdx: 0,
		fallback:  snapshotWorkspaceForSave(ws),
	}
	saveDone := make(chan error, 1)
	go func() {
		_, err := app.persistOneTabSnapshot(snap)
		saveDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save never entered the store Update")
	}

	// Queue a lifecycle writer behind the save's held guard.
	writerDone := make(chan struct{})
	go func() {
		app.lifecycle.markMutatingWorkspaceIDs(ws, true)
		close(writerDone)
	}()

	// A failed TryRLock while the save is paused holding a read lock proves
	// the writer has queued: a lone reader would still acquire it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if !app.lifecycle.phaseMu.TryRLock() {
			break
		}
		app.lifecycle.phaseMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("lifecycle writer never queued behind the paused save")
		}
		runtime.Gosched()
	}

	// Now let the save continue. If anything inside the transaction tried
	// to take the lifecycle read lock again — the old Update-callback
	// predicate — it would wait on the queued writer, which in turn waits
	// on the read lock the save still holds.
	releaseOnce.Do(func() { close(release) })

	select {
	case err := <-saveDone:
		if err != nil {
			t.Fatalf("persistOneTabSnapshot() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tab save deadlocked: re-acquiring the lifecycle read lock inside the guarded save waits on the queued writer")
	}
	select {
	case <-writerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle writer never completed after the save released the guard")
	}

	// The writer marking means the save finished first — and because the
	// mark landed after the write committed, the tabs are on disk.
	loaded, err := store.Load(ws.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.OpenTabs) != 1 || loaded.OpenTabs[0].Name != "tab-a" {
		t.Fatalf("tabs not persisted through the lock handoff: %v", loaded.OpenTabs)
	}
}
