package cli

import "testing"

func TestParseGlobalFlagsRejectsEmptyCwdEqualsValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--cwd=", "status"})
	if err == nil {
		t.Fatalf("expected error for empty --cwd= value")
	}
}

func TestParseGlobalFlagsRejectsEmptyCwdSpaceValue(t *testing.T) {
	_, _, err := ParseGlobalFlags([]string{"--cwd", "", "status"})
	if err == nil {
		t.Fatalf("expected error for empty --cwd value")
	}
}
