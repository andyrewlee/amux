package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAssistantDXSelfScriptRef_UsesAbsoluteFallbackOutsideRepoCWD(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	t.Setenv("AMUX_ASSISTANT_DX_CMD_REF", "")

	got := assistantDXSelfScriptRef()
	if !filepath.IsAbs(got) {
		t.Fatalf("assistantDXSelfScriptRef() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "assistant-dx.sh" {
		t.Fatalf("assistantDXSelfScriptRef() basename = %q, want %q", filepath.Base(got), "assistant-dx.sh")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("assistantDXSelfScriptRef() path is not usable: %v", err)
	}
}

func TestAssistantTurnStepScriptPath_UsesAbsoluteFallbackOutsideRepoCWD(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	t.Setenv("AMUX_ASSISTANT_TURN_STEP_SCRIPT", "")
	t.Setenv("AMUX_ASSISTANT_TURN_SCRIPT_DIR", "")

	got := assistantTurnStepScriptPath()
	if !filepath.IsAbs(got) {
		t.Fatalf("assistantTurnStepScriptPath() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "assistant-step.sh" {
		t.Fatalf("assistantTurnStepScriptPath() basename = %q, want %q", filepath.Base(got), "assistant-step.sh")
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("assistantTurnStepScriptPath() path is not usable: %v", err)
	}
}

func TestAssistantCompatDefaultScriptRef_FallsBackToNativeCommandRefWithoutScripts(t *testing.T) {
	oldStat := assistantCompatStat
	oldRepoLookup := assistantCompatRepoScriptPathFunc
	t.Cleanup(func() {
		assistantCompatStat = oldStat
		assistantCompatRepoScriptPathFunc = oldRepoLookup
	})

	assistantCompatStat = func(string) (os.FileInfo, error) {
		return nil, errors.New("missing")
	}
	assistantCompatRepoScriptPathFunc = func(string) string {
		return ""
	}

	if got := assistantCompatDefaultScriptRef("assistant-dx.sh"); got != "amux assistant dx" {
		t.Fatalf("assistantCompatDefaultScriptRef(assistant-dx.sh) = %q, want %q", got, "amux assistant dx")
	}
	if got := assistantCompatDefaultScriptRef("assistant-step.sh"); got != "amux assistant step" {
		t.Fatalf("assistantCompatDefaultScriptRef(assistant-step.sh) = %q, want %q", got, "amux assistant step")
	}
}

func TestAssistantCompatShellCommandRef_PreservesNativeAssistantCommand(t *testing.T) {
	if got := assistantCompatShellCommandRef("amux assistant dx"); got != "amux assistant dx" {
		t.Fatalf("assistantCompatShellCommandRef() = %q, want native command ref preserved", got)
	}
}
