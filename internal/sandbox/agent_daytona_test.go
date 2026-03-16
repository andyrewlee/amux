package sandbox

import "testing"

func TestBuildSSHInvocationArgsIncludesRemoteCommand(t *testing.T) {
	target := "token@example.com"
	remoteCommand := "bash -lc 'cd /repo && exec bash -i'"

	got := buildSSHInvocationArgs(target, remoteCommand, false)

	if len(got) == 0 || got[len(got)-1] != remoteCommand {
		t.Fatalf("buildSSHInvocationArgs() = %v, want remote command %q appended", got, remoteCommand)
	}
}

func TestBuildSSHInvocationArgsOmitsRemoteCommand(t *testing.T) {
	target := "token@example.com"
	remoteCommand := "bash -lc 'cd /repo && exec bash -i'"

	got := buildSSHInvocationArgs(target, remoteCommand, true)

	for _, arg := range got {
		if arg == remoteCommand {
			t.Fatalf("buildSSHInvocationArgs() = %v, did not expect remote command when omitRemoteCommand is true", got)
		}
	}
}

func TestBuildSSHDebugSummaryOmitsRemoteCommand(t *testing.T) {
	target := "token@example.com"
	remoteCommand := "env FOO=secret /bin/bash -lc 'cd /repo && exec bash -i'"

	got := buildSSHDebugSummary(target, remoteCommand, true)

	if got != "ssh "+target {
		t.Fatalf("buildSSHDebugSummary() = %q, want %q", got, "ssh "+target)
	}
}

func TestBuildSSHDebugSummaryIncludesRedactedRemoteCommand(t *testing.T) {
	target := "token@example.com"
	remoteCommand := "env FOO=secret /bin/bash -lc 'cd /repo && exec bash -i'"

	got := buildSSHDebugSummary(target, remoteCommand, false)

	if got != redactExports(remoteCommand) {
		t.Fatalf("buildSSHDebugSummary() = %q, want %q", got, redactExports(remoteCommand))
	}
}
