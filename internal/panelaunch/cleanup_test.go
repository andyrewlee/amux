//go:build !windows

package panelaunch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeAttempt(t *testing.T, root, suffix string, payloadAge, dirAge time.Duration) (dir, path string) {
	t.Helper()
	dir = filepath.Join(root, attemptPrefix+suffix)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("mkdir attempt: %v", err)
	}
	path = filepath.Join(dir, payloadName)
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	old := time.Now().Add(-payloadAge)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("chtimes payload: %v", err)
	}
	dirOld := time.Now().Add(-dirAge)
	if err := os.Chtimes(dir, dirOld, dirOld); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}
	return dir, path
}

func TestPaneLaunchCleanupSweep(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	t.Run("expired attempt removed", func(t *testing.T) {
		dir, path := makeAttempt(t, root, "old", launchBudget+time.Minute, launchBudget+time.Minute)
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("expired payload survived sweep")
		}
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Fatal("expired dir survived sweep")
		}
	})

	t.Run("fresh attempt survives", func(t *testing.T) {
		_, path := makeAttempt(t, root, "fresh", 0, 0)
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("fresh payload swept")
		}
	})

	t.Run("empty expired dir removed", func(t *testing.T) {
		dir := filepath.Join(root, attemptPrefix+"empty")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		old := now.Add(-launchBudget - time.Minute)
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Fatal("empty expired attempt survived sweep")
		}
	})

	t.Run("partial attempt uses latest mtime", func(t *testing.T) {
		// Payload fresh, dir old: the payload's mtime keeps the whole
		// attempt alive.
		dir, _ := makeAttempt(t, root, "mixed", 0, launchBudget+time.Hour)
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(dir); err != nil {
			t.Fatal("attempt with fresh payload swept by dir mtime")
		}
	})

	t.Run("symlinked attempt dir untouched", func(t *testing.T) {
		target := filepath.Join(root, "target-dir")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, attemptPrefix+"link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(link); err != nil {
			t.Fatal("symlink removed")
		}
		if _, err := os.Lstat(target); err != nil {
			t.Fatal("symlink target removed")
		}
	})

	t.Run("symlinked payload refuses whole attempt", func(t *testing.T) {
		dir := filepath.Join(root, attemptPrefix+"symlink")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		victim := filepath.Join(root, "victim")
		if err := os.WriteFile(victim, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, filepath.Join(dir, payloadName)); err != nil {
			t.Fatal(err)
		}
		old := now.Add(-launchBudget - time.Minute)
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(victim); err != nil {
			t.Fatal("symlink target removed")
		}
	})

	t.Run("unrelated children keep dir", func(t *testing.T) {
		dir, _ := makeAttempt(t, root, "kids", launchBudget+time.Minute, launchBudget+time.Minute)
		other := filepath.Join(dir, "other")
		if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Creating the child refreshed the dir mtime; age it again so the
		// attempt is expired.
		old := now.Add(-launchBudget - time.Minute)
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
		var warned []string
		SweepExpired(root, now, func(format string, args ...any) {
			warned = append(warned, format)
		})
		if _, err := os.Lstat(other); err != nil {
			t.Fatal("unrelated child removed")
		}
		if _, err := os.Lstat(dir); err != nil {
			t.Fatal("non-empty dir force-removed")
		}
		if len(warned) == 0 {
			t.Fatal("degradation not reported")
		}
	})

	t.Run("non-attempt entries ignored", func(t *testing.T) {
		fresh := filepath.Join(root, "unrelated")
		if err := os.WriteFile(fresh, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := now.Add(-launchBudget - time.Minute)
		if err := os.Chtimes(fresh, old, old); err != nil {
			t.Fatal(err)
		}
		SweepExpired(root, now, nil)
		if _, err := os.Lstat(fresh); err != nil {
			t.Fatal("unrelated file swept")
		}
	})
}

// TestPaneLaunchCleanupPrepared attempts through the target Prepare/nowFn
// path: a fresh prepared attempt survives the sweep, an expired one goes.
func TestPaneLaunchCleanupPrepared(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	root := tempRootFn()

	fresh, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	SweepExpired(root, time.Now(), nil)
	if _, err := os.Lstat(fresh.Path()); err != nil {
		t.Fatal("fresh prepared attempt swept")
	}
	_ = fresh.Discard()

	// Expire an attempt by back-dating both mtimes.
	stale, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	old := time.Now().Add(-launchBudget - time.Minute)
	for _, p := range []string{stale.Path(), stale.Dir()} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	SweepExpired(root, time.Now(), nil)
	if _, err := os.Lstat(stale.Dir()); !os.IsNotExist(err) {
		t.Fatal("expired prepared attempt survived sweep")
	}
}
