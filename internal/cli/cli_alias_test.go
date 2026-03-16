package cli

import "testing"

func TestBuildLsAliasRegistersSandboxLsFlags(t *testing.T) {
	cmd := buildLsAlias()

	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("buildLsAlias() did not register the --json flag")
	}
	if err := cmd.ParseFlags([]string{"--json"}); err != nil {
		t.Fatalf("ParseFlags(--json) error = %v", err)
	}
}
