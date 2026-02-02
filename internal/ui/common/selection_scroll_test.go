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

	need, gen := s.NeedsTick()
	if !need {
		t.Fatal("expected NeedsTick to return true")
	}
	if gen != 1 {
		t.Fatalf("expected gen=1, got %d", gen)
	}
	if !s.Active {
		t.Fatal("expected Active=true after NeedsTick")
	}
	if s.Gen != 1 {
		t.Fatalf("expected Gen=1, got %d", s.Gen)
	}
}

func TestNeedsTick_NoOpWhenActive(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)

	need1, gen1 := s.NeedsTick()
	if !need1 {
		t.Fatal("first NeedsTick should return true")
	}

	// Second call while Active — should be no-op
	need2, gen2 := s.NeedsTick()
	if need2 {
		t.Fatal("second NeedsTick should return false (already active)")
	}
	if gen2 != 0 {
		t.Fatalf("expected gen=0 when not needed, got %d", gen2)
	}
	if s.Gen != gen1 {
		t.Fatalf("Gen should not change, was %d now %d", gen1, s.Gen)
	}
}

func TestNeedsTick_NoOpWhenNoScroll(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(12, 24) // in bounds

	need, gen := s.NeedsTick()
	if need {
		t.Fatal("NeedsTick should return false when ScrollDir=0")
	}
	if gen != 0 {
		t.Fatalf("expected gen=0, got %d", gen)
	}
	if s.Active {
		t.Fatal("Active should remain false")
	}
}

func TestHandleTick_ValidTick(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen := s.NeedsTick()

	ok := s.HandleTick(gen)
	if !ok {
		t.Fatal("HandleTick should return true for matching gen")
	}
	if !s.Active {
		t.Fatal("Active should still be true after valid tick")
	}
}

func TestHandleTick_StaleTick(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen := s.NeedsTick()

	// Simulate stale tick (wrong gen)
	ok := s.HandleTick(gen + 1)
	if ok {
		t.Fatal("HandleTick should return false for mismatched gen")
	}
	if s.Active {
		t.Fatal("Active should be false after stale tick")
	}
}

func TestHandleTick_AfterReset(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen := s.NeedsTick()

	s.Reset()

	ok := s.HandleTick(gen)
	if ok {
		t.Fatal("HandleTick should return false after Reset (gen mismatch)")
	}
}

func TestHandleTick_DirectionCleared(t *testing.T) {
	var s SelectionScrollState
	s.SetDirection(-1, 24)
	_, gen := s.NeedsTick()

	// Mouse moved back into viewport
	s.SetDirection(12, 24)

	ok := s.HandleTick(gen)
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
	_, gen := s.NeedsTick()

	// Direction changed without going through center
	s.SetDirection(30, 24) // below → -1

	// Tick should still be valid (gen matches, dir != 0, active)
	ok := s.HandleTick(gen)
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
	_, gen1 := s.NeedsTick()

	s.Reset()

	// Should be able to start a new tick loop
	s.SetDirection(-1, 24)
	need, gen2 := s.NeedsTick()
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
	need, gen := s.NeedsTick()
	if !need {
		t.Fatal("step 2: expected NeedsTick=true")
	}

	// 3. Multiple ticks while mouse is held still
	for i := 0; i < 5; i++ {
		ok := s.HandleTick(gen)
		if !ok {
			t.Fatalf("step 3 tick %d: expected HandleTick=true", i)
		}
	}

	// 4. Mouse released → Reset
	s.Reset()
	ok := s.HandleTick(gen)
	if ok {
		t.Fatal("step 4: HandleTick should fail after Reset")
	}

	// 5. New selection starts and drags below viewport
	s.SetDirection(30, 24)
	need2, gen2 := s.NeedsTick()
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
	ok = s.HandleTick(gen)
	if ok {
		t.Fatal("step 6: old gen should be rejected")
	}

	// 7. New gen tick should work (need to restart since HandleTick
	//    with wrong gen set Active=false)
	s.SetDirection(30, 24) // re-set direction
	need3, gen3 := s.NeedsTick()
	if !need3 {
		t.Fatal("step 7: expected NeedsTick=true after re-activation")
	}
	ok = s.HandleTick(gen3)
	if !ok {
		t.Fatal("step 7: HandleTick with correct gen should succeed")
	}
}
