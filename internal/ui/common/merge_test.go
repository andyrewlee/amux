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
