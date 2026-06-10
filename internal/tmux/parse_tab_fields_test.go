package tmux

import "testing"

func TestParseTabFields(t *testing.T) {
	parts, err := parseTabFields("%1\t 12 \t34", 3)
	if err != nil {
		t.Fatalf("parseTabFields error = %v", err)
	}
	if parts[0] != "%1" || parts[1] != "12" || parts[2] != "34" {
		t.Fatalf("expected trimmed fields, got %q", parts)
	}

	if _, err := parseTabFields("%1\t12", 3); err == nil {
		t.Fatal("expected error for line with too few fields")
	}
	if _, err := parseTabFields("", 2); err == nil {
		t.Fatal("expected error for empty line")
	}
	// More fields than required is fine (forward compatible).
	if _, err := parseTabFields("a\tb\tc\td", 3); err != nil {
		t.Fatalf("unexpected error for extra fields: %v", err)
	}
}
