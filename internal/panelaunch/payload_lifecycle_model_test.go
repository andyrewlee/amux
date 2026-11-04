//go:build !windows

package panelaunch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// factsOf derives the classification inputs from a real attempt directory,
// mirroring sweepAttempt's probes exactly: Lstat (no symlink following),
// ownership via euidOwns, and the latest of the dir/payload mtimes against
// the budget.
func factsOf(t *testing.T, dir string, now time.Time) attemptFacts {
	t.Helper()
	f := attemptFacts{withinBudget: true}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return f
	}
	if err != nil {
		t.Fatalf("lstat attempt dir: %v", err)
	}
	f.dirPresent = true
	// The sweep's ownership gate, not the in-band 0700 gate: the model must
	// see exactly what sweepAttempt sees or the sweep proves the wrong policy.
	f.dirOwned = info.IsDir() && info.Mode()&os.ModeSymlink == 0 && euidOwns(info)
	latest := info.ModTime()
	payloadPath := filepath.Join(dir, payloadName)
	pi, err := os.Lstat(payloadPath)
	switch {
	case err == nil:
		f.payloadPresent = true
		f.payloadOwned = pi.Mode().IsRegular() && euidOwns(pi)
		if pi.ModTime().After(latest) {
			latest = pi.ModTime()
		}
	case os.IsNotExist(err):
	default:
		t.Fatalf("lstat payload: %v", err)
	}
	f.withinBudget = now.Before(latest.Add(launchBudget))
	return f
}

// assertClass classifies a real attempt and checks it maps onto the
// machine's expected position — the lockstep between disk and model.
func assertClass(t *testing.T, dir string, now time.Time, want attemptClass) {
	t.Helper()
	got := classifyAttempt(factsOf(t, dir, now))
	if got != want {
		t.Fatalf("attempt %s classifies %v, want %v", dir, got, want)
	}
}

// assertDirGone asserts the attempt directory is fully removed.
func assertDirGone(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("attempt dir %s still on disk (%v)", dir, err)
	}
}

// TestPaneLaunchLifecycleCheck runs the formal verification of the payload
// lifecycle machine and the classification sweep.
func TestPaneLaunchLifecycleCheck(t *testing.T) {
	if err := PaneLaunchLifecycleCheck(); err != nil {
		t.Fatal(err)
	}
}

// TestPaneLaunchLifecyclePrepareConsume drives the real prepare → consume
// path and asserts both the filesystem end-state and the machine's
// prediction: absent --prepare--> prepared --consume--> consumed, with the
// payload already unlinked when exec fires.
func TestPaneLaunchLifecyclePrepareConsume(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	p, err := Prepare(t.TempDir(), "echo hi", []string{"K=V"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	now := time.Now()
	assertClass(t, p.Dir(), now, classPreparedFresh)

	var payloadGoneAtExec bool
	stubExec(t, func(_ string, _, _ []string) error {
		_, statErr := os.Lstat(p.Path())
		payloadGoneAtExec = os.IsNotExist(statErr)
		return nil
	})
	if code := runPayload(p.Path()); code != 0 {
		t.Fatalf("runPayload exit = %d, want 0", code)
	}
	if !payloadGoneAtExec {
		t.Fatal("payload still present at exec: unlink-before-decode violated")
	}
	assertDirGone(t, p.Dir())
	assertClass(t, p.Dir(), now, classAbsent)
	// One-shot: a second consume finds nothing and fails closed.
	if code := runPayload(p.Path()); code != exitFailure {
		t.Fatalf("second runPayload exit = %d, want %d", code, exitFailure)
	}
}

// TestPaneLaunchLifecyclePrepareDiscard drives the in-band discard path:
// absent --crash-partial/partial or prepare--> ... --discard-unused-->
// discarded-in-band, end-state gone.
func TestPaneLaunchLifecyclePrepareDiscard(t *testing.T) {
	stubTempRoot(t, t.TempDir())

	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	now := time.Now()
	assertClass(t, p.Dir(), now, classPreparedFresh)
	if code := discardPayload(p.Path()); code != 0 {
		t.Fatalf("discardPayload exit = %d, want 0", code)
	}
	assertDirGone(t, p.Dir())
	assertClass(t, p.Dir(), now, classAbsent)

	// A partial attempt (crashed writer) is equally discardable.
	partial, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := os.Remove(partial.Path()); err != nil {
		t.Fatalf("remove payload to simulate crashed writer: %v", err)
	}
	assertClass(t, partial.Dir(), now, classPartialFresh)
	if code := discardPayload(partial.Path()); code != 0 {
		t.Fatalf("discardPayload(partial) exit = %d, want 0", code)
	}
	assertDirGone(t, partial.Dir())
}

// TestPaneLaunchLifecycleCrashPartialSweep drives prepare → crash-partial
// → budget-elapse → sweep-expire against the sweep's `now` seam.
func TestPaneLaunchLifecycleCrashPartialSweep(t *testing.T) {
	root := t.TempDir()
	stubTempRoot(t, root)
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if err := os.Remove(p.Path()); err != nil {
		t.Fatalf("remove payload to simulate crashed writer: %v", err)
	}

	now := time.Now()
	assertClass(t, p.Dir(), now, classPartialFresh)
	SweepExpired(root, now, nil) // fresh: untouchable
	if _, err := os.Lstat(p.Dir()); err != nil {
		t.Fatalf("fresh partial attempt swept: %v", err)
	}

	past := now.Add(launchBudget + time.Minute)
	assertClass(t, p.Dir(), past, classPartialExpired)
	SweepExpired(root, past, nil)
	assertDirGone(t, p.Dir())
	assertClass(t, p.Dir(), past, classAbsent)
}

// TestPaneLaunchLifecycleBudgetSweep drives prepare → budget-elapse →
// sweep-expire: a fresh prepared attempt survives the sweep, the same
// attempt past the budget goes.
func TestPaneLaunchLifecycleBudgetSweep(t *testing.T) {
	root := t.TempDir()
	stubTempRoot(t, root)
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	now := time.Now()
	assertClass(t, p.Dir(), now, classPreparedFresh)
	SweepExpired(root, now, nil)
	if _, err := os.Lstat(p.Path()); err != nil {
		t.Fatalf("fresh prepared attempt swept: %v", err)
	}

	past := now.Add(launchBudget + time.Minute)
	assertClass(t, p.Dir(), past, classPreparedExpired)
	SweepExpired(root, past, nil)
	assertDirGone(t, p.Dir())
}

// TestPaneLaunchLifecycleExpiredConsume pins the modeled quirk that expiry
// bounds usability, not consumption: a consumer running past the budget
// still unlinks the payload before decode rejects it, so the attempt is
// destroyed (expired --consume--> consumed) and reports stage-expired.
func TestPaneLaunchLifecycleExpiredConsume(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	execCalled := false
	stubExec(t, func(_ string, _, _ []string) error {
		execCalled = true
		return nil
	})
	oldNow := nowFn
	nowFn = func() time.Time { return time.Now().Add(launchBudget + time.Minute) }
	t.Cleanup(func() { nowFn = oldNow })

	if code := runPayload(p.Path()); code != exitFailure {
		t.Fatalf("expired runPayload exit = %d, want %d", code, exitFailure)
	}
	if execCalled {
		t.Fatal("expired payload reached exec")
	}
	assertDirGone(t, p.Dir()) // unlinked before the decode ever ran
}

// TestPaneLaunchLifecycleForeignRefuses pins the fail-closed boundary: an
// attempt carrying an unrelated child has its owned expired payload swept,
// but the directory (now foreign litter) is never recursively removed and
// reclassifies off-limits.
func TestPaneLaunchLifecycleForeignRefuses(t *testing.T) {
	root := t.TempDir()
	stubTempRoot(t, root)
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	other := filepath.Join(p.Dir(), "other")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatalf("write unrelated child: %v", err)
	}

	past := time.Now().Add(launchBudget + time.Minute)
	// The child's mtime refreshed the dir; age it back past the budget.
	old := past.Add(-launchBudget - 2*time.Minute)
	if err := os.Chtimes(p.Dir(), old, old); err != nil {
		t.Fatalf("chtimes dir: %v", err)
	}
	SweepExpired(root, past, nil)
	if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
		t.Fatal("owned expired payload survived sweep behind foreign child")
	}
	if _, err := os.Lstat(other); err != nil {
		t.Fatal("unrelated child removed: sweep must never recurse")
	}
	if _, err := os.Lstat(p.Dir()); err != nil {
		t.Fatal("non-empty attempt dir force-removed")
	}
	// Payload gone, dir still owned and empty-hosted: past budget with an
	// unrelated child — the dir-only shape classifies partial-expired, but
	// the rmdir keeps failing, so the dir is eternal litter by design.
	assertClass(t, p.Dir(), past, classPartialExpired)
}
