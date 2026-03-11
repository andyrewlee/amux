package cli

import "testing"

func TestCobraStatusRejectsUnexpectedPositionalArgs(t *testing.T) {
	cmd := buildStatusCommand()
	cmd.SetArgs([]string{"typo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected status command to reject unexpected positional args")
	}
}

func TestCobraDoctorRejectsUnexpectedPositionalArgs(t *testing.T) {
	cmd := buildEnhancedDoctorCommand()
	cmd.SetArgs([]string{"typo"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected doctor command to reject unexpected positional args")
	}
}
