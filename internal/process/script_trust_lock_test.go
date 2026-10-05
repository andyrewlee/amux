//go:build !windows

package process

import (
	"os"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"
)

func awaitTrustDone(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never completed after the lock was released", what)
	}
}

// TestScriptTrust_ConcurrentTrustsKeepBothApprovals is the cross-instance
// lost-update proof: trust A reads the base registry and is held inside its
// transaction while trust B commits a disjoint repo approval. The sibling
// flock must serialize them so the final registry keeps both repos —
// otherwise A's stale read silently revokes B's approval.
func TestScriptTrust_ConcurrentTrustsKeepBothApprovals(t *testing.T) {
	dir := t.TempDir()
	repoA, repoB := "/repo/a", "/repo/b"
	contentA, contentB := []byte("config-a"), []byte("config-b")

	a := NewScriptTrust(dir)
	b := NewScriptTrust(dir)

	release := make(chan struct{})
	entered := make(chan struct{})
	a.readFile = func(p string) ([]byte, error) {
		raw, err := os.ReadFile(p)
		close(entered)
		<-release
		return raw, err
	}

	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		if err := a.Trust(repoA, contentA); err != nil {
			t.Errorf("a.Trust error = %v", err)
		}
	}()
	<-entered

	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		if err := b.Trust(repoB, contentB); err != nil {
			t.Errorf("b.Trust error = %v", err)
		}
	}()
	select {
	case <-doneB:
		t.Fatal("b.Trust completed while a held the lock mid-transaction — serialization is broken")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	awaitTrustDone(t, doneA, "a.Trust")
	awaitTrustDone(t, doneB, "b.Trust")

	check := NewScriptTrust(dir)
	if !check.IsTrusted(repoA, contentA) || !check.IsTrusted(repoB, contentB) {
		t.Fatal("merged registry lost an approval — concurrent Trust calls were not serialized")
	}
}

// TestScriptTrust_TrustBlocksWhileLockHeld proves Trust contends on the
// sibling flock: an externally held exclusive lock (a second process's
// shape) must park the writer until released.
func TestScriptTrust_TrustBlocksWhileLockHeld(t *testing.T) {
	dir := t.TempDir()
	trust := NewScriptTrust(dir)

	held, err := fsatomic.LockFile(trust.path+".lock", false)
	if err != nil {
		t.Fatalf("LockFile error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := trust.Trust("/repo/a", []byte("config")); err != nil {
			t.Errorf("Trust error = %v", err)
		}
	}()

	select {
	case <-done:
		fsatomic.UnlockFile(held)
		t.Fatal("Trust completed while the sibling lock was held externally")
	case <-time.After(200 * time.Millisecond):
	}

	fsatomic.UnlockFile(held)
	awaitTrustDone(t, done, "Trust")
}
