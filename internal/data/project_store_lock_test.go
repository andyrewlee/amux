//go:build !windows

package data

import (
	"os"
	"testing"
	"time"
)

// holdStoreRead parks the first read inside a store's transaction until
// release closes, after capturing the pre-interleave bytes. It returns the
// channel that reports the capture happened.
func holdStoreRead(setReadFile func(func(string) ([]byte, error))) (entered, release chan struct{}) {
	entered = make(chan struct{})
	release = make(chan struct{})
	setReadFile(func(p string) ([]byte, error) {
		raw, err := os.ReadFile(p)
		close(entered)
		<-release
		return raw, err
	})
	return entered, release
}

// awaitBlocked asserts ch does not fire within the window — the shape of "a
// second writer is parked on the sibling flock".
func awaitBlocked(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s completed while the sibling lock was held — serialization is broken", what)
	case <-time.After(200 * time.Millisecond):
	}
}

func awaitDone(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never completed after the lock was released", what)
	}
}

// TestProjectEnvStore_ConcurrentSetsKeepBothUpdates is the cross-instance
// lost-update proof: store A captures the base bytes and is held inside its
// transaction while store B commits a disjoint repo's edit. The sibling
// flock must park B until A's write lands, so the final file keeps both
// repos. Without the flock B commits early and A's stale read clobbers it.
func TestProjectEnvStore_ConcurrentSetsKeepBothUpdates(t *testing.T) {
	dir := t.TempDir()
	repoA, repoB := "/repo/a", "/repo/b"

	a := NewProjectEnvStore(dir)
	b := NewProjectEnvStore(dir)

	entered, release := holdStoreRead(func(f func(string) ([]byte, error)) { a.readFile = f })

	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		if err := a.Set(repoA, map[string]string{"K": "a"}); err != nil {
			t.Errorf("a.Set error = %v", err)
		}
	}()
	<-entered

	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		if err := b.Set(repoB, map[string]string{"K": "b"}); err != nil {
			t.Errorf("b.Set error = %v", err)
		}
	}()
	awaitBlocked(t, doneB, "b.Set")

	close(release)
	awaitDone(t, doneA, "a.Set")
	awaitDone(t, doneB, "b.Set")

	check := NewProjectEnvStore(dir)
	if got := check.ForRepo(repoA); got["K"] != "a" {
		t.Fatalf("repoA env = %v — a's update was lost", got)
	}
	if got := check.ForRepo(repoB); got["K"] != "b" {
		t.Fatalf("repoB env = %v — b's update was lost", got)
	}
}

// TestProjectScriptStore_ConcurrentSetsKeepBothUpdates mirrors the env-store
// proof for project-scripts.json.
func TestProjectScriptStore_ConcurrentSetsKeepBothUpdates(t *testing.T) {
	dir := t.TempDir()
	repoA, repoB := "/repo/a", "/repo/b"

	a := NewProjectScriptStore(dir)
	b := NewProjectScriptStore(dir)

	entered, release := holdStoreRead(func(f func(string) ([]byte, error)) { a.readFile = f })

	doneA := make(chan struct{})
	go func() {
		defer close(doneA)
		if err := a.Set(repoA, ScriptsConfig{Setup: "setup-a"}); err != nil {
			t.Errorf("a.Set error = %v", err)
		}
	}()
	<-entered

	doneB := make(chan struct{})
	go func() {
		defer close(doneB)
		if err := b.Set(repoB, ScriptsConfig{Archive: "archive-b"}); err != nil {
			t.Errorf("b.Set error = %v", err)
		}
	}()
	awaitBlocked(t, doneB, "b.Set")

	close(release)
	awaitDone(t, doneA, "a.Set")
	awaitDone(t, doneB, "b.Set")

	check := NewProjectScriptStore(dir)
	if got := check.ForRepo(repoA); got.Setup != "setup-a" {
		t.Fatalf("repoA scripts = %+v — a's update was lost", got)
	}
	if got := check.ForRepo(repoB); got.Archive != "archive-b" {
		t.Fatalf("repoB scripts = %+v — b's update was lost", got)
	}
}

// TestProjectEnvStore_SetBlocksWhileLockHeld proves the Set transaction
// contends on the sibling flock, not just the in-process mutex: an
// externally held exclusive lock (the shape a second process takes) must
// park the writer until released.
func TestProjectEnvStore_SetBlocksWhileLockHeld(t *testing.T) {
	dir := t.TempDir()
	store := NewProjectEnvStore(dir)

	held, err := lockRegistryFile(store.path+".lock", false)
	if err != nil {
		t.Fatalf("lockRegistryFile error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := store.Set("/repo/proj", map[string]string{"K": "v"}); err != nil {
			t.Errorf("Set error = %v", err)
		}
	}()

	awaitBlocked(t, done, "Set")
	unlockRegistryFile(held)
	awaitDone(t, done, "Set")
}
