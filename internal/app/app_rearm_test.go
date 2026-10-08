package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/git"
	"github.com/andyrewlee/amux/internal/messages"
	"github.com/andyrewlee/amux/internal/ui/dashboard"
)

// panicGitStatus is a GitStatusService whose every method panics, giving a
// deterministic in-handler panic seam for Update-recovery tests.
type panicGitStatus struct{}

func (panicGitStatus) GetCached(string) *git.StatusResult            { panic("injected panic") }
func (panicGitStatus) UpdateCache(string, *git.StatusResult)         { panic("injected panic") }
func (panicGitStatus) Invalidate(string)                             { panic("injected panic") }
func (panicGitStatus) Refresh(string) (*git.StatusResult, error)     { panic("injected panic") }
func (panicGitStatus) RefreshFast(string) (*git.StatusResult, error) { panic("injected panic") }

// TestUpdate_RearmsFileWatcherChainAfterHandlerPanic is the end-to-end proof
// for plan 057: a panic inside a watcher event handler previously made
// Update's recover drop the next channel-read cmd, permanently killing the
// watcher chain. The re-armed cmd must yield the next queued event.
func TestUpdate_RearmsFileWatcherChainAfterHandlerPanic(t *testing.T) {
	ch := make(chan messages.FileWatcherEvent, 1)
	a := &App{
		gitStatus:     panicGitStatus{}, // panics at Invalidate inside the handler
		fileWatcher:   &git.FileWatcher{},
		fileWatcherCh: ch,
	}
	ch <- messages.FileWatcherEvent{Root: "/tmp/repo/ws-next"}

	model, cmd := a.Update(messages.FileWatcherEvent{Root: "/tmp/repo/ws"})

	if model != a {
		t.Fatalf("Update returned model %T, want the same *App after panic recovery", model)
	}
	if a.err == nil {
		t.Fatal("expected a.err to record the recovered panic")
	}
	if cmd == nil {
		t.Fatal("expected the watcher chain to re-arm after the handler panic")
	}
	got := cmd()
	event, ok := got.(messages.FileWatcherEvent)
	if !ok {
		t.Fatalf("re-armed watcher cmd produced %T, want messages.FileWatcherEvent", got)
	}
	if event.Root != "/tmp/repo/ws-next" {
		t.Fatalf("re-armed watcher cmd produced root %q, want the queued event", event.Root)
	}
}

// TestUpdate_RearmsGitStatusTickAfterHandlerPanic covers a SafeTick chain:
// the re-armed cmd cannot be executed synchronously (it sleeps), so a
// non-nil cmd plus the recorded panic prove the chain was restarted.
func TestUpdate_RearmsGitStatusTickAfterHandlerPanic(t *testing.T) {
	a := &App{
		activeWorkspace: &data.Workspace{Root: "/tmp/repo/ws"},
		gitStatus:       panicGitStatus{}, // panics at GetCached inside the handler
	}

	model, cmd := a.Update(messages.GitStatusTick{})

	if model != a {
		t.Fatalf("Update returned model %T, want the same *App after panic recovery", model)
	}
	if a.err == nil {
		t.Fatal("expected a.err to record the recovered panic")
	}
	if cmd == nil {
		t.Fatal("expected the git-status ticker to re-arm after the handler panic")
	}
}

// TestRearmCmdForPanic_MapsPeriodicChains pins the msg→starter mapping: every
// self-rearming chain must map to a non-nil cmd on a configured App, and every
// other message must map to nil so non-periodic panic behavior is unchanged.
func TestRearmCmdForPanic_MapsPeriodicChains(t *testing.T) {
	a := &App{
		fileWatcher:    &git.FileWatcher{},
		fileWatcherCh:  make(chan messages.FileWatcherEvent),
		stateWatcher:   &stateWatcher{},
		stateWatcherCh: make(chan messages.StateWatcherEvent),
	}

	cases := []struct {
		name string
		msg  tea.Msg
		want bool // want a non-nil re-arm cmd
	}{
		{"git status tick", messages.GitStatusTick{}, true},
		{"orphan gc tick", messages.OrphanGCTick{}, true},
		{"pty watchdog tick", messages.PTYWatchdogTick{}, true},
		{"tmux sync tick", messages.TmuxSyncTick{Token: a.tmuxActivity.syncToken}, true},
		{"tmux sync stale tick", messages.TmuxSyncTick{Token: -1}, false},
		{"tmux activity tick", tmuxActivityTick{}, true},
		{"run output tick", runOutputTickMsg{token: 1}, true},
		{"run output refreshed", runOutputRefreshedMsg{token: 1}, true},
		{"file watcher event", messages.FileWatcherEvent{}, true},
		{"state watcher event", messages.StateWatcherEvent{}, true},
		{"key press", tea.KeyPressMsg{}, false},
		{"window size", tea.WindowSizeMsg{}, false},
		{"error", messages.Error{}, false},
		{"nil msg", nil, false},
	}
	for _, tc := range cases {
		got := a.rearmCmdForPanic(tc.msg)
		if tc.want && got == nil {
			t.Errorf("%s: expected a re-arm cmd, got nil", tc.name)
		}
		if !tc.want && got != nil {
			t.Errorf("%s: expected nil re-arm cmd, got non-nil", tc.name)
		}
	}
}

// TestRearmCmdForPanic_NilSafeOnUnconfiguredApp proves the mapping itself
// cannot panic on a bare App — it runs inside the recover path, where a second
// panic would escape Update entirely. Watchers/spinner have no starter until
// their fields exist, so they map to nil there.
func TestRearmCmdForPanic_NilSafeOnUnconfiguredApp(t *testing.T) {
	a := &App{}
	for _, msg := range []tea.Msg{
		messages.GitStatusTick{},
		messages.OrphanGCTick{},
		messages.PTYWatchdogTick{},
		messages.TmuxSyncTick{},
		tmuxActivityTick{},
		runOutputTickMsg{token: 1},
		runOutputRefreshedMsg{token: 1},
		messages.FileWatcherEvent{},
		messages.StateWatcherEvent{},
		dashboard.SpinnerTickMsg{},
		tea.KeyPressMsg{},
		nil,
	} {
		_ = a.rearmCmdForPanic(msg)
	}
}
