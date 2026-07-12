package git

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestBuildRemoveWorkspaceCommandWindows(t *testing.T) {
	orig := removeWorkspacePathGOOS
	defer func() { removeWorkspacePathGOOS = orig }()
	removeWorkspacePathGOOS = "windows"

	const workspacePath = "/target/workspace path"
	cmd := buildRemoveWorkspaceCommand(context.Background(), workspacePath)

	wantArgs := []string{
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Remove-Item -LiteralPath $env:AMUX_REMOVE_PATH -Recurse -Force -ErrorAction Stop",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Errorf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}

	const envPrefix = "AMUX_REMOVE_PATH="
	wantEnv := envPrefix + workspacePath
	got := ""
	matches := 0
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, envPrefix) {
			// Later entries win for exec.Cmd, so keep the last match; this
			// tolerates a host that already exports AMUX_REMOVE_PATH.
			got = entry
			matches++
		}
	}
	if matches == 0 {
		t.Fatalf("cmd.Env has no %s entry; env = %v", envPrefix, cmd.Env)
	}
	if got != wantEnv {
		t.Errorf("effective %s entry = %q, want %q", envPrefix, got, wantEnv)
	}
}

func TestBuildRemoveWorkspaceCommandUnix(t *testing.T) {
	orig := removeWorkspacePathGOOS
	defer func() { removeWorkspacePathGOOS = orig }()
	removeWorkspacePathGOOS = "linux"

	cmd := buildRemoveWorkspaceCommand(context.Background(), "/target/ws")

	wantArgs := []string{"rm", "-rf", "--", "/target/ws"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Errorf("cmd.Args = %#v, want %#v", cmd.Args, wantArgs)
	}
	if cmd.Env != nil {
		t.Errorf("cmd.Env = %v, want nil", cmd.Env)
	}
}
