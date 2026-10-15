package common

import "testing"

// TestNextIdx exercises circular forward navigation, wraparound, and no-tabs.
func TestNextIdx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tabs      []string
		active    int
		wsID      string
		wantIdx   int
		wantMoved bool
	}{
		{
			name:      "advances within range",
			tabs:      []string{"a", "b", "c"},
			active:    0,
			wsID:      "ws",
			wantIdx:   1,
			wantMoved: true,
		},
		{
			name:      "wraps from last to first",
			tabs:      []string{"a", "b", "c"},
			active:    2,
			wsID:      "ws",
			wantIdx:   0,
			wantMoved: true,
		},
		{
			name:      "single tab wraps to itself",
			tabs:      []string{"only"},
			active:    0,
			wsID:      "ws",
			wantIdx:   0,
			wantMoved: true,
		},
		{
			name:      "no tabs reports no move",
			tabs:      []string{},
			active:    0,
			wsID:      "ws",
			wantIdx:   0,
			wantMoved: false,
		},
		{
			name:      "unknown workspace reports no move",
			tabs:      []string{"a"},
			active:    0,
			wsID:      "missing",
			wantIdx:   0,
			wantMoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ByWorkspace["ws"] = tt.tabs
			s.ActiveByWorkspace["ws"] = tt.active

			gotIdx, gotMoved := s.NextIdx(tt.wsID)
			if gotIdx != tt.wantIdx || gotMoved != tt.wantMoved {
				t.Errorf("NextIdx(%q) = (%d, %v), want (%d, %v)",
					tt.wsID, gotIdx, gotMoved, tt.wantIdx, tt.wantMoved)
			}
			// On a move the stored index updates; on a no-op for a known ws the
			// original index is left untouched.
			if tt.wantMoved {
				if got := s.ActiveByWorkspace["ws"]; got != tt.wantIdx {
					t.Errorf("stored active = %d, want %d after move", got, tt.wantIdx)
				}
			} else if tt.wsID == "missing" {
				if got := s.ActiveByWorkspace["ws"]; got != tt.active {
					t.Errorf("stored active mutated to %d on no-op, want %d", got, tt.active)
				}
			}
		})
	}
}

// TestPrevIdx exercises circular backward navigation and negative-modulo wrap.
func TestPrevIdx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tabs      []string
		active    int
		wsID      string
		wantIdx   int
		wantMoved bool
	}{
		{
			name:      "moves back within range",
			tabs:      []string{"a", "b", "c"},
			active:    2,
			wsID:      "ws",
			wantIdx:   1,
			wantMoved: true,
		},
		{
			name:      "wraps from first to last",
			tabs:      []string{"a", "b", "c"},
			active:    0,
			wsID:      "ws",
			wantIdx:   2,
			wantMoved: true,
		},
		{
			name:      "single tab wraps to itself",
			tabs:      []string{"only"},
			active:    0,
			wsID:      "ws",
			wantIdx:   0,
			wantMoved: true,
		},
		{
			name:      "no tabs reports no move",
			tabs:      []string{},
			active:    0,
			wsID:      "ws",
			wantIdx:   0,
			wantMoved: false,
		},
		{
			name:      "unknown workspace reports no move",
			tabs:      []string{"a"},
			active:    0,
			wsID:      "missing",
			wantIdx:   0,
			wantMoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ByWorkspace["ws"] = tt.tabs
			s.ActiveByWorkspace["ws"] = tt.active

			gotIdx, gotMoved := s.PrevIdx(tt.wsID)
			if gotIdx != tt.wantIdx || gotMoved != tt.wantMoved {
				t.Errorf("PrevIdx(%q) = (%d, %v), want (%d, %v)",
					tt.wsID, gotIdx, gotMoved, tt.wantIdx, tt.wantMoved)
			}
			if tt.wantMoved {
				if got := s.ActiveByWorkspace["ws"]; got != tt.wantIdx {
					t.Errorf("stored active = %d, want %d after move", got, tt.wantIdx)
				}
			} else if tt.wsID == "missing" {
				if got := s.ActiveByWorkspace["ws"]; got != tt.active {
					t.Errorf("stored active mutated to %d on no-op, want %d", got, tt.active)
				}
			}
		})
	}
}

// TestIdxSequences walks full forward and backward cycles, wrapping once per lap.
func TestIdxSequences(t *testing.T) {
	t.Parallel()

	seqs := []struct {
		name string
		step func(*TabSet[string]) (int, bool)
		want []int
	}{
		{"next", func(s *TabSet[string]) (int, bool) { return s.NextIdx("ws") }, []int{1, 2, 0, 1, 2, 0}},
		{"prev", func(s *TabSet[string]) (int, bool) { return s.PrevIdx("ws") }, []int{2, 1, 0, 2, 1, 0}},
	}

	for _, sq := range seqs {
		t.Run(sq.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ByWorkspace["ws"] = []string{"a", "b", "c"}
			for i, w := range sq.want {
				gotIdx, moved := sq.step(&s)
				if !moved {
					t.Fatalf("step %d: %s reported no move", i, sq.name)
				}
				if gotIdx != w {
					t.Fatalf("step %d: %s = %d, want %d", i, sq.name, gotIdx, w)
				}
			}
		})
	}
}

// TestNextPrevRoundTrip confirms Next then Prev returns to the original index.
func TestNextPrevRoundTrip(t *testing.T) {
	t.Parallel()

	for start := 0; start < 3; start++ {
		s := NewTabSet[string]()
		s.ByWorkspace["ws"] = []string{"a", "b", "c"}
		s.ActiveByWorkspace["ws"] = start

		s.NextIdx("ws")
		got, _ := s.PrevIdx("ws")
		if got != start {
			t.Errorf("start=%d: Next then Prev = %d, want %d", start, got, start)
		}
	}
}

// TestSelectIdx covers in-range success, out-of-range boundaries, and rejections.
func TestSelectIdx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tabs     []string
		idx      int
		wsID     string
		wantOK   bool
		wantIdx  int // expected stored active index after the call
		startIdx int // initial active index
	}{
		{
			name:     "selects first index from a non-zero start",
			tabs:     []string{"a", "b", "c"},
			idx:      0,
			wsID:     "ws",
			wantOK:   true,
			wantIdx:  0,
			startIdx: 2,
		},
		{
			name:     "selects last index",
			tabs:     []string{"a", "b", "c"},
			idx:      2,
			wsID:     "ws",
			wantOK:   true,
			wantIdx:  2,
			startIdx: 0,
		},
		{
			name:     "negative index rejected, active unchanged",
			tabs:     []string{"a", "b"},
			idx:      -1,
			wsID:     "ws",
			wantOK:   false,
			wantIdx:  1,
			startIdx: 1,
		},
		{
			name:     "index equal to length rejected (off by one upper bound)",
			tabs:     []string{"a", "b"},
			idx:      2,
			wsID:     "ws",
			wantOK:   false,
			wantIdx:  0,
			startIdx: 0,
		},
		{
			name:     "no tabs rejects index zero",
			tabs:     []string{},
			idx:      0,
			wsID:     "ws",
			wantOK:   false,
			wantIdx:  0,
			startIdx: 0,
		},
		{
			name:     "unknown workspace rejects any index",
			tabs:     []string{"a"},
			idx:      0,
			wsID:     "missing",
			wantOK:   false,
			wantIdx:  0,
			startIdx: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ByWorkspace["ws"] = tt.tabs
			s.ActiveByWorkspace["ws"] = tt.startIdx

			gotOK := s.SelectIdx(tt.wsID, tt.idx)
			if gotOK != tt.wantOK {
				t.Errorf("SelectIdx(%q, %d) = %v, want %v", tt.wsID, tt.idx, gotOK, tt.wantOK)
			}
			if got := s.ActiveByWorkspace["ws"]; got != tt.wantIdx {
				t.Errorf("stored active = %d, want %d", got, tt.wantIdx)
			}
		})
	}
}
