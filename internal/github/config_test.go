package github

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PreferredBaseBranch == "" {
		t.Fatalf("expected preferred base branch")
	}
}
