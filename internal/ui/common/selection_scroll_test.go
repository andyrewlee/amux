package common

import (
	"testing"
)

func TestSetDirection(t *testing.T) {
	tests := []struct {
		name    string
		termY   int
		height  int
		wantDir int
	}{
		{"above viewport", -1, 24, 1},
		{"far above viewport", -10, 24, 1},
		{"top edge (in bounds)", 0, 24, 0},
		{"middle", 12, 24, 0},
		{"bottom edge (in bounds)", 23, 24, 0},
		{"below viewport", 24, 24, -1},
		{"far below viewport", 100, 24, -1},
		{"single row - above", -1, 1, 1},
		{"single row - at row", 0, 1, 0},
		{"single row - below", 1, 1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SelectionScrollState
			s.SetDirection(tt.termY, tt.height)
			if s.ScrollDir != tt.wantDir {
				t.Errorf("SetDirection(%d, %d) ScrollDir = %d, want %d",
					tt.termY, tt.height, s.ScrollDir, tt.wantDir)
			}
		})
	}
}

func TestSetDirectionOverwritesPrevious(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-5, 24) // above → +1
	if s.ScrollDir != 1 {
		t.Fatalf("expected ScrollDir=1, got %d", s.ScrollDir)
	}
	s.SetDirection(12, 24) // back to center → 0
	if s.ScrollDir != 0 {
		t.Fatalf("expected ScrollDir=0 after re-entering viewport, got %d", s.ScrollDir)
	}
	s.SetDirection(30, 24) // below → -1
	if s.ScrollDir != -1 {
		t.Fatalf("expected ScrollDir=-1, got %d", s.ScrollDir)
	}
}

func TestNeedsTick_StartsLoop(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24) // above viewport

	need, gen, seq := s.NeedsTick()
	if !need {
		t.Fatal("expected NeedsTick to return true")
	}
	if gen != 1 {
		t.Fatalf("expected gen=1, got %d", gen)
	}
	if seq != 1 {
		t.Fatalf("expected seq=1, got %d", seq)
	}
	if !s.Active {
		t.Fatal("expected Active=true after NeedsTick")
	}
	if s.Gen != 1 {
		t.Fatalf("expected Gen=1, got %d", s.Gen)
	}
}

func TestNeedsTick_ReRequestsCurrentSequenceWhileActive(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)

	need1, gen1, seq1 := s.NeedsTick()
	if !need1 {
		t.Fatal("first NeedsTick should return true")
	}

	// A second drag motion re-requests the same tick. If the first request was
	// dropped, this replaces it; if both arrive, the sequence check rejects the
	// duplicate after the first one advances the chain.
	need2, gen2, seq2 := s.NeedsTick()
	if !need2 {
		t.Fatal("second NeedsTick should re-request the chain")
	}
	if gen2 != gen1 {
		t.Fatalf("active chain should keep generation %d, got %d", gen1, gen2)
	}
	if seq2 != seq1 {
		t.Fatalf("active chain should re-request sequence %d, got %d", seq1, seq2)
	}
	if !s.HandleTick(gen2, seq2) {
		t.Fatal("current chain generation should be accepted")
	}
	if s.HandleTick(gen1, seq1) {
		t.Fatal("duplicate tick sequence should be rejected")
	}
	if !s.Active {
		t.Fatal("duplicate tick should not stop the current chain")
	}
}

func TestNeedsTick_NoOpWhenNoScroll(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(12, 24) // in bounds

	need, gen, seq := s.NeedsTick()
	if need {
		t.Fatal("NeedsTick should return false when ScrollDir=0")
	}
	if gen != 0 {
		t.Fatalf("expected gen=0, got %d", gen)
	}
	if seq != 0 {
		t.Fatalf("expected seq=0, got %d", seq)
	}
	if s.Active {
		t.Fatal("Active should remain false")
	}
}

func TestHandleTick_ValidTick(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen, seq := s.NeedsTick()

	ok := s.HandleTick(gen, seq)
	if !ok {
		t.Fatal("HandleTick should return true for matching gen")
	}
	if !s.Active {
		t.Fatal("Active should still be true after valid tick")
	}
}

func TestHandleTick_StaleTickDoesNotStopCurrentChain(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen, seq := s.NeedsTick()

	// Simulate stale tick (wrong gen)
	ok := s.HandleTick(gen+1, seq)
	if ok {
		t.Fatal("HandleTick should return false for mismatched gen")
	}
	if !s.Active {
		t.Fatal("Active should remain true after stale tick")
	}
	if !s.HandleTick(gen, seq) {
		t.Fatal("current generation should still be accepted after stale tick")
	}
}

func TestHandleTick_DuplicateSequenceDoesNotStopCurrentChain(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen, seq := s.NeedsTick()

	if !s.HandleTick(gen, seq) {
		t.Fatal("first tick should be accepted")
	}
	if s.HandleTick(gen, seq) {
		t.Fatal("duplicate tick sequence should be rejected")
	}
	if !s.Active {
		t.Fatal("duplicate tick should not stop current chain")
	}
	if !s.HandleTick(gen, seq+1) {
		t.Fatal("next tick sequence should be accepted")
	}
}

func TestHandleTick_AfterReset(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen, seq := s.NeedsTick()

	s.Reset()

	ok := s.HandleTick(gen, seq)
	if ok {
		t.Fatal("HandleTick should return false after Reset (gen mismatch)")
	}
}

func TestHandleTick_DirectionCleared(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen, seq := s.NeedsTick()

	// Mouse moved back into viewport
	s.SetDirection(12, 24)

	ok := s.HandleTick(gen, seq)
	if ok {
		t.Fatal("HandleTick should return false when ScrollDir=0")
	}
	if s.Active {
		t.Fatal("Active should be cleared")
	}
}

func TestHandleTick_DirectionChanged(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24) // above → +1
	_, gen, seq := s.NeedsTick()

	// Direction changed without going through center
	s.SetDirection(30, 24) // below → -1

	// Tick should still be valid (gen matches, dir != 0, active)
	ok := s.HandleTick(gen, seq)
	if !ok {
		t.Fatal("HandleTick should still be valid when direction changed but gen matches")
	}
	if s.ScrollDir != -1 {
		t.Fatalf("ScrollDir should be -1, got %d", s.ScrollDir)
	}
}

func TestReset(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	s.NeedsTick()

	oldGen := s.Gen
	s.Reset()

	if s.ScrollDir != 0 {
		t.Fatalf("ScrollDir should be 0, got %d", s.ScrollDir)
	}
	if s.Active {
		t.Fatal("Active should be false")
	}
	if s.Gen != oldGen+1 {
		t.Fatalf("Gen should be bumped from %d to %d, got %d", oldGen, oldGen+1, s.Gen)
	}
}

func TestReset_AllowsNewTickLoop(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen1, _ := s.NeedsTick()

	s.Reset()

	// Should be able to start a new tick loop
	s.SetDirection(-1, 24)
	need, gen2, _ := s.NeedsTick()
	if !need {
		t.Fatal("should be able to start new tick loop after Reset")
	}
	if gen2 == gen1 {
		t.Fatalf("new gen should differ from old gen: got %d", gen2)
	}
}

func TestFullLifecycle(t *testing.T) {
	var s SelectionScrollState

	// 1. Mouse drags above viewport
	s.SetDirection(-5, 24)
	if s.ScrollDir != 1 {
		t.Fatalf("step 1: ScrollDir = %d, want 1", s.ScrollDir)
	}

	// 2. First motion triggers tick loop
	need, gen, seq := s.NeedsTick()
	if !need {
		t.Fatal("step 2: expected NeedsTick=true")
	}

	// 3. Multiple ticks while mouse is held still
	for i := 0; i < 5; i++ {
		ok := s.HandleTick(gen, seq)
		if !ok {
			t.Fatalf("step 3 tick %d: expected HandleTick=true", i)
		}
		seq++
	}

	// 4. Mouse released → Reset
	s.Reset()
	ok := s.HandleTick(gen, seq)
	if ok {
		t.Fatal("step 4: HandleTick should fail after Reset")
	}

	// 5. New selection starts and drags below viewport
	s.SetDirection(30, 24)
	need2, gen2, seq2 := s.NeedsTick()
	if !need2 {
		t.Fatal("step 5: expected new tick loop")
	}
	if gen2 == gen {
		t.Fatal("step 5: new gen should differ")
	}
	if s.ScrollDir != -1 {
		t.Fatalf("step 5: ScrollDir = %d, want -1", s.ScrollDir)
	}

	// 6. Old gen tick should be rejected
	ok = s.HandleTick(gen, seq)
	if ok {
		t.Fatal("step 6: old gen should be rejected")
	}

	// 7. New gen tick should still work after the old gen was ignored.
	ok = s.HandleTick(gen2, seq2)
	if !ok {
		t.Fatal("step 7: HandleTick with correct gen should succeed")
	}
}
