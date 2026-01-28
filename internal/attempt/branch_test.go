package attempt

import "testing"

func TestBranchName(t *testing.T) {
	name := BranchName("lin", "ENG-123", "abcdef")
	if name != "lin/ENG-123/abcd" {
		t.Fatalf("unexpected branch name: %s", name)
	}
}
