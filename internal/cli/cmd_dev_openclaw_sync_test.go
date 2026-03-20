package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCmdDevOpenClawSyncJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics vary on windows")
	}

	oldLookPath := openClawLookPath
	oldCommand := openClawExecCommand
	t.Cleanup(func() {
		openClawLookPath = oldLookPath
		openClawExecCommand = oldCommand
	})

	openClawLookPath = func(file string) (string, error) {
		return "/fake/openclaw", nil
	}
	openClawExecCommand = fakeOpenClawCommand

	dir := t.TempDir()
	skillSrc := filepath.Join(dir, "skills", "amux")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir skill src: %v", err)
	}
	mainWS := filepath.Join(dir, "main")
	devWS := filepath.Join(dir, "dev")
	verifyLog := filepath.Join(dir, "verify.log")
	t.Setenv("OPENCLAW_HELPER_VERIFY_LOG", verifyLog)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevOpenClawSync(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--skill-src", skillSrc,
		"--main-workspace", mainWS,
		"--dev-workspace", devWS,
	}, "test")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr=%s\nstdout=%s", code, ExitOK, errOut.String(), out.String())
	}

	for _, ws := range []string{mainWS, devWS} {
		linkPath := filepath.Join(ws, "skills", "amux")
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink %s: %v", linkPath, err)
		}
		if target != skillSrc {
			t.Fatalf("link target = %q, want %q", target, skillSrc)
		}
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got false: %s", out.String())
	}

	rawVerify, err := os.ReadFile(verifyLog)
	if err != nil {
		t.Fatalf("read verify log: %v", err)
	}
	verifyText := string(rawVerify)
	if !strings.Contains(verifyText, "--workspace "+mainWS+" skills info amux") {
		t.Fatalf("main verification args = %q, want explicit workspace", verifyText)
	}
	if !strings.Contains(verifyText, "--dev --workspace "+devWS+" skills info amux") {
		t.Fatalf("dev verification args = %q, want explicit dev workspace", verifyText)
	}
}

func TestCmdDevOpenClawSyncMissingDependency(t *testing.T) {
	oldLookPath := openClawLookPath
	t.Cleanup(func() {
		openClawLookPath = oldLookPath
	})
	openClawLookPath = func(file string) (string, error) {
		return "", os.ErrNotExist
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevOpenClawSync(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--skill-src", t.TempDir(),
	}, "test")
	if code != ExitDependency {
		t.Fatalf("exit code = %d, want %d", code, ExitDependency)
	}
}

func TestCmdDevOpenClawSyncSkipVerifyExplicitWorkspacesDoesNotRequireDependency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics vary on windows")
	}

	oldLookPath := openClawLookPath
	t.Cleanup(func() {
		openClawLookPath = oldLookPath
	})
	openClawLookPath = func(file string) (string, error) {
		return "", os.ErrNotExist
	}

	dir := t.TempDir()
	skillSrc := filepath.Join(dir, "skills", "amux")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir skill src: %v", err)
	}
	mainWS := filepath.Join(dir, "main-explicit")
	devWS := filepath.Join(dir, "dev-explicit")

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevOpenClawSync(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--skill-src", skillSrc,
		"--main-workspace", mainWS,
		"--dev-workspace", devWS,
		"--skip-verify",
	}, "test")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr=%s\nstdout=%s", code, ExitOK, errOut.String(), out.String())
	}

	for _, ws := range []string{mainWS, devWS} {
		linkPath := filepath.Join(ws, "skills", "amux")
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink %s: %v", linkPath, err)
		}
		if target != skillSrc {
			t.Fatalf("link target = %q, want %q", target, skillSrc)
		}
	}

	var env Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got false: %s", out.String())
	}
}

func TestCmdDevOpenClawSyncExplicitWorkspaceOverridesConfiguredDefaults(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics vary on windows")
	}

	oldLookPath := openClawLookPath
	oldCommand := openClawExecCommand
	t.Cleanup(func() {
		openClawLookPath = oldLookPath
		openClawExecCommand = oldCommand
	})

	openClawLookPath = func(file string) (string, error) {
		return "/fake/openclaw", nil
	}
	openClawExecCommand = fakeOpenClawCommand

	dir := t.TempDir()
	skillSrc := filepath.Join(dir, "skills", "amux")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir skill src: %v", err)
	}
	configMainWS := filepath.Join(dir, "configured-main")
	configDevWS := filepath.Join(dir, "configured-dev")
	mainWS := filepath.Join(dir, "main-explicit")
	devWS := filepath.Join(dir, "dev-explicit")
	verifyLog := filepath.Join(dir, "verify.log")
	t.Setenv("OPENCLAW_HELPER_CONFIG_MAIN", configMainWS)
	t.Setenv("OPENCLAW_HELPER_CONFIG_DEV", configDevWS)
	t.Setenv("OPENCLAW_HELPER_VERIFY_LOG", verifyLog)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevOpenClawSync(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--skill-src", skillSrc,
		"--main-workspace", mainWS,
		"--dev-workspace", devWS,
	}, "test")
	if code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr=%s\nstdout=%s", code, ExitOK, errOut.String(), out.String())
	}

	for _, ws := range []string{mainWS, devWS} {
		linkPath := filepath.Join(ws, "skills", "amux")
		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink %s: %v", linkPath, err)
		}
		if target != skillSrc {
			t.Fatalf("link target = %q, want %q", target, skillSrc)
		}
	}
	for _, ws := range []string{configMainWS, configDevWS} {
		if _, err := os.Stat(filepath.Join(ws, "skills", "amux")); !os.IsNotExist(err) {
			t.Fatalf("configured fallback workspace %q should not have been modified; err=%v", ws, err)
		}
	}

	rawVerify, err := os.ReadFile(verifyLog)
	if err != nil {
		t.Fatalf("read verify log: %v", err)
	}
	verifyText := string(rawVerify)
	if strings.Contains(verifyText, configMainWS) || strings.Contains(verifyText, configDevWS) {
		t.Fatalf("verification used configured defaults instead of explicit workspaces: %q", verifyText)
	}
}

func TestCmdDevOpenClawSyncRefusesExistingNonSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics vary on windows")
	}

	oldLookPath := openClawLookPath
	oldCommand := openClawExecCommand
	t.Cleanup(func() {
		openClawLookPath = oldLookPath
		openClawExecCommand = oldCommand
	})

	openClawLookPath = func(file string) (string, error) {
		return "/fake/openclaw", nil
	}
	openClawExecCommand = fakeOpenClawCommand

	dir := t.TempDir()
	skillSrc := filepath.Join(dir, "skills", "amux")
	if err := os.MkdirAll(skillSrc, 0o755); err != nil {
		t.Fatalf("mkdir skill src: %v", err)
	}
	mainWS := filepath.Join(dir, "main")
	devWS := filepath.Join(dir, "dev")
	existingSkillPath := filepath.Join(mainWS, "skills", "amux")
	if err := os.MkdirAll(existingSkillPath, 0o755); err != nil {
		t.Fatalf("mkdir existing skill path: %v", err)
	}
	existingFile := filepath.Join(existingSkillPath, "local.txt")
	if err := os.WriteFile(existingFile, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := cmdDevOpenClawSync(&out, &errOut, GlobalFlags{JSON: true}, []string{
		"--skill-src", skillSrc,
		"--main-workspace", mainWS,
		"--dev-workspace", devWS,
	}, "test")
	if code != ExitInternalError {
		t.Fatalf("exit code = %d, want %d\nstderr=%s\nstdout=%s", code, ExitInternalError, errOut.String(), out.String())
	}

	if _, err := os.Stat(existingFile); err != nil {
		t.Fatalf("existing file should be preserved, stat error = %v", err)
	}
	linkInfo, err := os.Lstat(existingSkillPath)
	if err != nil {
		t.Fatalf("lstat existing skill path: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("existing non-symlink target should not have been replaced with symlink")
	}
}

func fakeOpenClawCommand(name string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", name}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	sep := 0
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	cmdArgs := args[sep+1:]
	configIdx := -1
	for i := 1; i < len(cmdArgs)-2; i++ {
		if cmdArgs[i] == "config" && cmdArgs[i+1] == "get" && cmdArgs[i+2] == "agents.defaults.workspace" {
			configIdx = i
			break
		}
	}
	if configIdx >= 0 {
		isDev := false
		for _, arg := range cmdArgs[1:configIdx] {
			if arg == "--dev" {
				isDev = true
				break
			}
		}
		if isDev {
			if value := os.Getenv("OPENCLAW_HELPER_CONFIG_DEV"); value != "" {
				_, _ = os.Stdout.WriteString(value + "\n")
			}
			os.Exit(0)
		}
		if value := os.Getenv("OPENCLAW_HELPER_CONFIG_MAIN"); value != "" {
			_, _ = os.Stdout.WriteString(value + "\n")
		}
		os.Exit(0)
	}
	if len(cmdArgs) >= 3 {
		skillsIdx := -1
		for i := 0; i < len(cmdArgs)-2; i++ {
			if cmdArgs[i] == "skills" && cmdArgs[i+1] == "info" && cmdArgs[i+2] == "amux" {
				skillsIdx = i
				break
			}
		}
		if skillsIdx >= 0 {
			if logPath := os.Getenv("OPENCLAW_HELPER_VERIFY_LOG"); logPath != "" {
				f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
				if err == nil {
					_, _ = f.WriteString(strings.Join(cmdArgs, " ") + "\n")
					_ = f.Close()
				}
			}
			_, _ = os.Stdout.WriteString("ok\n")
			os.Exit(0)
		}
	}
	os.Exit(0)
}
