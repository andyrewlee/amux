package common

import "testing"

func TestMergeByID(t *testing.T) {
	t.Parallel()
	id := func(s string) string { return s }
	isNil := func(s string) bool { return s == "" }

	t.Run("dedupes preserving order, existing first", func(t *testing.T) {
		t.Parallel()
		merged, active := MergeByID([]string{"a", "b"}, []string{"b", "c"}, 1, id, isNil)
		if got, want := merged, []string{"a", "b", "c"}; !eq(got, want) {
			t.Fatalf("merged = %v, want %v", got, want)
		}
		// incoming[1] == "c" is appended at index 2.
		if active != 2 {
			t.Fatalf("active = %d, want 2", active)
		}
	})

	t.Run("active maps to existing index when duplicate", func(t *testing.T) {
		t.Parallel()
		merged, active := MergeByID([]string{"a", "b"}, []string{"b"}, 0, id, isNil)
		if !eq(merged, []string{"a", "b"}) {
			t.Fatalf("merged = %v", merged)
		}
		if active != 1 { // incoming "b" maps to existing index 1
			t.Fatalf("active = %d, want 1", active)
		}
	})

	t.Run("nil entries skipped, active -1 when none", func(t *testing.T) {
		t.Parallel()
		merged, active := MergeByID([]string{"", "a"}, []string{""}, 0, id, isNil)
		if !eq(merged, []string{"a"}) {
			t.Fatalf("merged = %v, want [a]", merged)
		}
		if active != -1 {
			t.Fatalf("active = %d, want -1", active)
		}
	})
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRebindTabMaps_ClampsActiveIndex(t *testing.T) {
	t.Parallel()
	id := func(s string) string { return s }
	isNil := func(s string) bool { return s == "" }

	t.Run("migrates old active when new has none", func(t *testing.T) {
		tabs := map[string][]string{"old": {"a", "b"}}
		active := map[string]int{"old": 1}
		merged := RebindTabMaps(tabs, active, "old", "new", id, isNil)
		if len(merged) != 2 {
			t.Fatalf("merged = %v", merged)
		}
		if active["new"] != 1 {
			t.Fatalf("active[new] = %d, want 1", active["new"])
		}
		if _, ok := tabs["old"]; ok {
			t.Fatal("old tab key not deleted")
		}
		if _, ok := active["old"]; ok {
			t.Fatal("old active key not deleted")
		}
	})

	t.Run("clamps new active out of range after merge", func(t *testing.T) {
		tabs := map[string][]string{"old": {"x", "y"}, "new": {"x"}}
		active := map[string]int{"old": 0, "new": 5}
		merged := RebindTabMaps(tabs, active, "old", "new", id, isNil)
		if got, want := active["new"], len(merged)-1; got != want {
			t.Fatalf("active[new] = %d, want clamp to %d (merged=%v)", got, want, merged)
		}
	})

	t.Run("empty merged yields active 0", func(t *testing.T) {
		tabs := map[string][]string{"old": {}, "new": {}}
		active := map[string]int{"old": 3, "new": 2}
		merged := RebindTabMaps(tabs, active, "old", "new", id, isNil)
		if len(merged) != 0 {
			t.Fatalf("merged = %v", merged)
		}
		if active["new"] != 0 {
			t.Fatalf("active[new] = %d, want 0", active["new"])
		}
	})
}
