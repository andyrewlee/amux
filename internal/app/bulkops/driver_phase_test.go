package bulkops

import "testing"

// TestDriverPhaseFormallyChecked verifies the bulk driver's control protocol
// by exhaustive model checking: draining is reachable, Start is the only way
// in, and only finishing or abandonment end a drain.
func TestDriverPhaseFormallyChecked(t *testing.T) {
	if err := PhaseCheck(); err != nil {
		t.Fatalf("bulk driver phase machine failed model checking: %v", err)
	}
}
