package common

import "testing"

// TestSearchMachineCheck runs the exhaustive verifier over the shared
// `/`-field machine — wrap-marker one-shotness, edit-returns-to-field,
// no-match-never-selects, and the idle/drop boundaries.
func TestSearchMachineCheck(t *testing.T) {
	if err := SearchCheck(); err != nil {
		t.Fatalf("search machine failed verification: %v", err)
	}
}

// TestSearchCtlWrapMarkerIsOneShot pins the runtime behavior the checker
// proves over the table: the marker is armed only by a wrapping jump and
// disarmed by the next non-wrapping jump or scroll.
func TestSearchCtlWrapMarkerIsOneShot(t *testing.T) {
	var c SearchCtl
	c.Apply(SearchOpenField)
	c.Apply(SearchCloseMatched)
	if c.WrapMarked() {
		t.Fatal("wrap marker armed without a wrapping jump")
	}
	c.Apply(SearchJumpWrap)
	if !c.WrapMarked() {
		t.Fatal("wrapping jump did not arm the marker")
	}
	c.Apply(SearchJumpStay)
	if c.WrapMarked() {
		t.Fatal("marker survived a non-wrapping jump (must be one-shot)")
	}
	c.Apply(SearchJumpWrap)
	c.Apply(SearchScroll)
	if c.WrapMarked() {
		t.Fatal("marker survived a manual scroll")
	}
}

// TestSearchCtlRejectedStepsAreNoOps pins the fail-closed contract the
// dialogs rely on: keys that make no sense in the current mode (n with no
// query, scrolling while idle, jumping from cleared with the wrong event)
// leave the state untouched.
func TestSearchCtlRejectedStepsAreNoOps(t *testing.T) {
	var c SearchCtl
	c.Apply(SearchJumpWrap)    // no query: rejected
	c.Apply(SearchJumpRecover) // nothing to recover: rejected
	c.Apply(SearchScroll)      // self-loop, still idle
	if c.Editing() || c.WrapMarked() {
		t.Fatalf("idle state moved without opening the field: %v", c)
	}
}
