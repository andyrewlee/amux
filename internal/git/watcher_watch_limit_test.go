package git

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/andyrewlee/amux/internal/perf"
)

func TestIsWatchLimitError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unrelated", err: errors.New("boom"), want: false},
		{name: "ENOSPC", err: syscall.ENOSPC, want: true},
		{name: "EMFILE", err: syscall.EMFILE, want: true},
		{name: "ENFILE", err: syscall.ENFILE, want: true},
		{name: "wrapped ENOSPC", err: fmt.Errorf("add watch: %w", syscall.ENOSPC), want: true},
		{name: "string too many open files", err: errors.New("too many open files"), want: true},
		{name: "string no space left on device", err: errors.New("no space left on device"), want: true},
		{name: "string inotify watch limit", err: errors.New("inotify watch limit reached"), want: true},
		{name: "string case-insensitive", err: errors.New("TOO MANY OPEN FILES"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWatchLimitError(tt.err); got != tt.want {
				t.Fatalf("isWatchLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestAddWatchPathDisablesOnLimit(t *testing.T) {
	restore := perf.EnableForTest()
	defer restore()
	// Clear any pre-existing counters so the assertion below sees only the
	// increments produced by this test.
	perf.Snapshot()

	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}
	defer func() {
		_ = fw.Close()
	}()

	// Inject the inotify-exhaustion failure over the real watcher.Add call.
	fw.addWatch = func(string) error { return syscall.ENOSPC }

	path := t.TempDir() // os.Stat must succeed before addWatch is reached.

	err1 := func() error {
		_, err := fw.addWatchPath(path)
		return err
	}()
	if err1 == nil {
		t.Fatalf("addWatchPath() returned nil error on watch limit")
	}
	if !errors.Is(err1, ErrWatchLimit) {
		t.Fatalf("addWatchPath() error = %v, want errors.Is(_, ErrWatchLimit)", err1)
	}
	if !fw.disabled {
		t.Fatalf("expected fw.disabled = true after watch limit")
	}

	// Second call (still over-limit) must not re-enter the !fw.disabled block:
	// it returns the same wrapped fw.disabledErr and does not re-increment the
	// perf counter.
	_, err2 := fw.addWatchPath(path)
	if err2 != fw.disabledErr {
		t.Fatalf("second addWatchPath() error = %v, want identical to fw.disabledErr %v", err2, fw.disabledErr)
	}
	if err1 != err2 {
		t.Fatalf("expected idempotent error identity; err1 = %v, err2 = %v", err1, err2)
	}

	_, counters := perf.Snapshot()
	if got, ok := counterValue(counters, "git_watcher_watch_limit"); !ok || got != 1 {
		t.Fatalf("git_watcher_watch_limit counter = %d (present=%v), want exactly 1 (no double-increment)", got, ok)
	}
}

func TestAddWatchPathNonLimitError(t *testing.T) {
	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}
	defer func() {
		_ = fw.Close()
	}()

	sentinel := errors.New("permission denied")
	fw.addWatch = func(string) error { return sentinel }

	path := t.TempDir()

	_, gotErr := fw.addWatchPath(path)
	if !errors.Is(gotErr, sentinel) {
		t.Fatalf("addWatchPath() error = %v, want the injected non-limit error", gotErr)
	}
	if fw.disabled {
		t.Fatalf("expected fw.disabled to stay false on a non-limit error")
	}
	if errors.Is(gotErr, ErrWatchLimit) {
		t.Fatalf("non-limit error unexpectedly matched ErrWatchLimit: %v", gotErr)
	}
}

func counterValue(counters []perf.CounterSnapshot, name string) (int64, bool) {
	for _, c := range counters {
		if c.Name == name {
			return c.Value, true
		}
	}
	return 0, false
}
