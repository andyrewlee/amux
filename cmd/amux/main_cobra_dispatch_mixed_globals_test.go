//go:build !windows

package main

import (
	"testing"

	"github.com/andyrewlee/amux/internal/cli"
)

func TestPrepareCobraDispatchArgsConsumesRunGlobalsAfterPreservedLeadingGlobals(t *testing.T) {
	tmp := t.TempDir()

	gotGF, gotArgs, err := prepareCobraDispatchArgs([]string{"--json", "--cwd", tmp, "sandbox", "ls"}, "sandbox")
	if err != nil {
		t.Fatalf("prepareCobraDispatchArgs() error = %v", err)
	}
	if gotGF.Cwd != tmp {
		t.Fatalf("cwd = %q, want %q", gotGF.Cwd, tmp)
	}
	if !gotGF.JSON {
		t.Fatal("expected JSON global to be consumed into GlobalFlags")
	}
	wantArgs := []string{"sandbox", "ls", "--json"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
		}
	}
}

func TestPrepareCobraDispatchArgsRemapsSandboxLsCommandLocalJSONFlag(t *testing.T) {
	gotGF, gotArgs, err := prepareCobraDispatchArgs([]string{"sandbox", "ls", "--json"}, "sandbox")
	if err != nil {
		t.Fatalf("prepareCobraDispatchArgs() error = %v", err)
	}
	if gotGF != (cli.GlobalFlags{JSON: true}) {
		t.Fatalf("GlobalFlags = %+v, want JSON global", gotGF)
	}
	wantArgs := []string{"sandbox", "ls", "--json"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
		}
	}
}

func TestPrepareCobraDispatchArgsConsumesPostCommandGlobalsBeforeDoubleDash(t *testing.T) {
	tmp := t.TempDir()

	gotGF, gotArgs, err := prepareCobraDispatchArgs([]string{"sandbox", "ls", "--cwd", tmp, "--request-id", "req-post", "--", "--quiet"}, "sandbox")
	if err != nil {
		t.Fatalf("prepareCobraDispatchArgs() error = %v", err)
	}
	if gotGF.Cwd != tmp {
		t.Fatalf("cwd = %q, want %q", gotGF.Cwd, tmp)
	}
	if gotGF.RequestID != "req-post" {
		t.Fatalf("request-id = %q, want %q", gotGF.RequestID, "req-post")
	}
	wantArgs := []string{"sandbox", "ls", "--", "--quiet"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
	}
	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("cobra args = %v, want %v", gotArgs, wantArgs)
		}
	}
}
