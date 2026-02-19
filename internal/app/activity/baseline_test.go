package activity

import (
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

func TestFreshTagVisibleActivity_SeedsWhenUninitialized(t *testing.T) {
	const sessionName = "sess-seed"
	seedHash := [16]byte{1}
	states := map[string]*SessionState{}
	updated := map[string]*SessionState{}
	captureCalls := 0
	captureFn := func(string, int, tmux.Options) (string, bool) {
		captureCalls++
		return "seed", true
	}
	hashFn := func(string) [16]byte { return seedHash }

	if !FreshTagVisibleActivity(sessionName, states, updated, time.Now(), tmux.Options{}, captureFn, hashFn) {
		t.Fatal("expected fresh tag to be active when state is uninitialized")
	}
	if captureCalls != 1 {
		t.Fatalf("expected one capture call, got %d", captureCalls)
	}
	state := states[sessionName]
	if state == nil {
		t.Fatal("expected state to be created")
	}
	if !state.Initialized {
		t.Fatal("expected state to be initialized")
	}
	if state.Score != 0 {
		t.Fatalf("expected seeded score 0, got %d", state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected seeded state to have no hold timer")
	}
	if state.LastHash != seedHash {
		t.Fatalf("expected seeded hash %v, got %v", seedHash, state.LastHash)
	}
	if updated[sessionName] == nil {
		t.Fatal("expected seeded state to be marked updated")
	}
}

func TestFreshTagVisibleActivity_InitializedCaptureFailurePassesThrough(t *testing.T) {
	const sessionName = "sess-capture-fail"
	originalHash := [16]byte{3}
	state := &SessionState{
		LastHash:    originalHash,
		Score:       1,
		Initialized: true,
	}
	states := map[string]*SessionState{sessionName: state}
	updated := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "", false }
	hashFn := func(string) [16]byte { return [16]byte{9} }

	if !FreshTagVisibleActivity(sessionName, states, updated, time.Now(), tmux.Options{}, captureFn, hashFn) {
		t.Fatal("expected capture failure path to preserve fresh-tag activity")
	}
	if state.Score != 1 {
		t.Fatalf("expected score unchanged, got %d", state.Score)
	}
	if state.LastHash != originalHash {
		t.Fatalf("expected hash unchanged, got %v", state.LastHash)
	}
	if updated[sessionName] != nil {
		t.Fatal("did not expect updated state entry on capture failure pass-through")
	}
}

func TestFreshTagVisibleActivity_ChangedHashMarksActive(t *testing.T) {
	const sessionName = "sess-changed"
	oldHash := [16]byte{4}
	newHash := [16]byte{5}
	state := &SessionState{
		LastHash:    oldHash,
		Score:       0,
		Initialized: true,
	}
	states := map[string]*SessionState{sessionName: state}
	updated := map[string]*SessionState{}
	now := time.Now()
	captureFn := func(string, int, tmux.Options) (string, bool) { return "changed", true }
	hashFn := func(string) [16]byte { return newHash }

	if !FreshTagVisibleActivity(sessionName, states, updated, now, tmux.Options{}, captureFn, hashFn) {
		t.Fatal("expected changed visible content to count as active")
	}
	if state.LastHash != newHash {
		t.Fatalf("expected hash to update to %v, got %v", newHash, state.LastHash)
	}
	if state.Score != ScoreThreshold {
		t.Fatalf("expected score to rise to threshold %d, got %d", ScoreThreshold, state.Score)
	}
	if state.LastActiveAt.IsZero() {
		t.Fatal("expected hold timer to be set on visible delta")
	}
	if updated[sessionName] == nil {
		t.Fatal("expected updated state entry for changed hash")
	}
}

func TestFreshTagVisibleActivity_UnchangedHashDecaysAndClearsHold(t *testing.T) {
	const sessionName = "sess-unchanged"
	hashValue := [16]byte{7}
	state := &SessionState{
		LastHash:     hashValue,
		Score:        ScoreMax,
		LastActiveAt: time.Now(),
		Initialized:  true,
	}
	states := map[string]*SessionState{sessionName: state}
	updated := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	if FreshTagVisibleActivity(sessionName, states, updated, time.Now(), tmux.Options{}, captureFn, hashFn) {
		t.Fatal("expected unchanged visible content to not count as active")
	}
	if state.Score != ScoreThreshold-1 {
		t.Fatalf("expected score to decay to %d, got %d", ScoreThreshold-1, state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected hold timer to be cleared")
	}
	if updated[sessionName] == nil {
		t.Fatal("expected updated state entry for unchanged hash path")
	}
}

func TestFreshTagVisibleActivity_UnchangedHashAtZeroStaysZero(t *testing.T) {
	const sessionName = "sess-zero"
	hashValue := [16]byte{8}
	state := &SessionState{
		LastHash:     hashValue,
		Score:        0,
		LastActiveAt: time.Now(),
		Initialized:  true,
	}
	states := map[string]*SessionState{sessionName: state}
	updated := map[string]*SessionState{}
	captureFn := func(string, int, tmux.Options) (string, bool) { return "same", true }
	hashFn := func(string) [16]byte { return hashValue }

	if FreshTagVisibleActivity(sessionName, states, updated, time.Now(), tmux.Options{}, captureFn, hashFn) {
		t.Fatal("expected unchanged zero-score session to stay inactive")
	}
	if state.Score != 0 {
		t.Fatalf("expected score to stay at 0, got %d", state.Score)
	}
	if !state.LastActiveAt.IsZero() {
		t.Fatal("expected hold timer to be cleared")
	}
}
