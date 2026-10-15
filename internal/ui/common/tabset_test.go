package common

import "testing"

// TestNewTabSet verifies the constructor returns initialized, independent maps.
func TestNewTabSet(t *testing.T) {
	t.Parallel()

	s := NewTabSet[string]()
	if s.ByWorkspace == nil {
		t.Fatal("ByWorkspace map should be initialized, got nil")
	}
	if s.ActiveByWorkspace == nil {
		t.Fatal("ActiveByWorkspace map should be initialized, got nil")
	}
	if len(s.ByWorkspace) != 0 || len(s.ActiveByWorkspace) != 0 {
		t.Errorf("fresh TabSet not empty: tabs=%d active=%d",
			len(s.ByWorkspace), len(s.ActiveByWorkspace))
	}

	// The maps must be writable without panicking and must be distinct from a
	// second TabSet's maps (no shared backing store).
	s.ByWorkspace["ws"] = []string{"a"}
	s.ActiveByWorkspace["ws"] = 0

	other := NewTabSet[string]()
	if len(other.ByWorkspace) != 0 || len(other.ActiveByWorkspace) != 0 {
		t.Error("two TabSets share map state; constructor must allocate fresh maps")
	}
}

// TestTabs covers reads against populated, empty, and entirely-unknown
// workspaces.
func TestTabs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state map[string][]string
		wsID  string
		want  []string
	}{
		{
			name:  "populated workspace",
			state: map[string][]string{"ws": {"a", "b", "c"}},
			wsID:  "ws",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "unknown workspace returns nil",
			state: map[string][]string{"ws": {"a"}},
			wsID:  "missing",
			want:  nil,
		},
		{
			name:  "empty slice stored",
			state: map[string][]string{"ws": {}},
			wsID:  "ws",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ByWorkspace = tt.state
			got := s.Tabs(tt.wsID)
			if !eq(got, tt.want) {
				t.Errorf("Tabs(%q) = %v, want %v", tt.wsID, got, tt.want)
			}
			// eq compares by length only; assert nil-ness explicitly.
			switch tt.name {
			case "unknown workspace returns nil":
				if got != nil {
					t.Errorf("Tabs(%q) = %v, want nil slice", tt.wsID, got)
				}
			case "empty slice stored":
				if got == nil {
					t.Errorf("Tabs(%q) returned nil, want non-nil empty slice", tt.wsID)
				}
			}
		})
	}
}

// TestActiveIdx confirms unset workspaces default to 0 and stored values round trip.
func TestActiveIdx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state map[string]int
		wsID  string
		want  int
	}{
		{
			name:  "unset workspace defaults to zero",
			state: map[string]int{},
			wsID:  "ws",
			want:  0,
		},
		{
			name:  "stored index returned, unknown ws stays zero",
			state: map[string]int{"ws": 3, "other": 2},
			wsID:  "ws",
			want:  3,
		},
		{
			name:  "unknown workspace defaults to zero when others set",
			state: map[string]int{"other": 2},
			wsID:  "ws",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := NewTabSet[string]()
			s.ActiveByWorkspace = tt.state
			if got := s.ActiveIdx(tt.wsID); got != tt.want {
				t.Errorf("ActiveIdx(%q) = %d, want %d", tt.wsID, got, tt.want)
			}
		})
	}
}

// TestSetActiveIdx checks the setter records arbitrary values verbatim,
// including out-of-range ones (the type performs no clamping here).
func TestSetActiveIdx(t *testing.T) {
	t.Parallel()

	s := NewTabSet[string]()
	s.SetActiveIdx("ws", 2)
	if got := s.ActiveByWorkspace["ws"]; got != 2 {
		t.Errorf("after SetActiveIdx(2) stored = %d, want 2", got)
	}

	// Overwrites replace the prior value; negative values are stored verbatim
	// since SetActiveIdx does no range guarding (callers/SelectIdx do).
	s.SetActiveIdx("ws", 5)
	if got := s.ActiveIdx("ws"); got != 5 {
		t.Errorf("ActiveIdx after overwrite = %d, want 5", got)
	}
	s.SetActiveIdx("ws", -1)
	if got := s.ActiveIdx("ws"); got != -1 {
		t.Errorf("ActiveIdx after set(-1) = %d, want -1", got)
	}
}

// TestDeleteWorkspace clears both maps for the target while siblings stay intact.
func TestDeleteWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("removes both tab and active entries", func(t *testing.T) {
		t.Parallel()
		s := NewTabSet[string]()
		s.ByWorkspace["ws"] = []string{"a", "b"}
		s.ActiveByWorkspace["ws"] = 1

		s.DeleteWorkspace("ws")

		if _, ok := s.ByWorkspace["ws"]; ok {
			t.Error("ByWorkspace entry not deleted")
		}
		if _, ok := s.ActiveByWorkspace["ws"]; ok {
			t.Error("ActiveByWorkspace entry not deleted")
		}
		if got := s.Tabs("ws"); got != nil {
			t.Errorf("Tabs after delete = %v, want nil", got)
		}
	})

	t.Run("leaves sibling workspaces intact", func(t *testing.T) {
		t.Parallel()
		s := NewTabSet[string]()
		s.ByWorkspace["keep"] = []string{"x"}
		s.ActiveByWorkspace["keep"] = 0
		s.ByWorkspace["drop"] = []string{"y"}
		s.ActiveByWorkspace["drop"] = 0

		s.DeleteWorkspace("drop")

		if got := s.Tabs("keep"); !eq(got, []string{"x"}) {
			t.Errorf("sibling Tabs = %v, want [x]", got)
		}
		if _, ok := s.ByWorkspace["drop"]; ok {
			t.Error("dropped workspace still present")
		}
	})

	t.Run("deleting unknown workspace is a no-op", func(t *testing.T) {
		t.Parallel()
		s := NewTabSet[string]()
		s.ByWorkspace["ws"] = []string{"a"}
		s.ActiveByWorkspace["ws"] = 0

		s.DeleteWorkspace("missing")

		if got := s.Tabs("ws"); !eq(got, []string{"a"}) {
			t.Errorf("existing Tabs mutated by no-op delete = %v, want [a]", got)
		}
	})
}
