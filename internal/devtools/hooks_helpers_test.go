package devtools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// hookFixture is an isolated environment for executing the repository's real
// git hook scripts. External commands (git, make, go, tmux) are stub shell
// scripts in stubDir that record one record per invocation to logFile —
// fields separated by \x1f so arguments containing spaces stay unambiguous.
// Nothing in the fixture may invoke a real git mutation, make target, go run,
// or tmux server.
type hookFixture struct {
	stubDir string
	logFile string
	repoDir string // what the git stub reports for `rev-parse --show-toplevel`
	staged  string // newline-delimited staged Go filenames for the `diff` stub
	cwd     string // unrelated working directory the hook runs from
}

// newHookFixture builds the stub bin directory and scratch dirs. tmux is only
// stubbed when tmuxUsable is true; without it the hook's `command -v tmux`
// check fails exactly as on a tmux-less host.
func newHookFixture(t *testing.T, tmuxUsable bool) *hookFixture {
	t.Helper()
	base := t.TempDir()
	fx := &hookFixture{
		stubDir: filepath.Join(base, "bin"),
		logFile: filepath.Join(base, "invocations.log"),
		repoDir: filepath.Join(base, "repo"),
		staged:  filepath.Join(base, "staged.txt"),
		cwd:     filepath.Join(base, "cwd"),
	}
	for _, dir := range []string{fx.stubDir, fx.repoDir, fx.cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
	if err := os.WriteFile(fx.logFile, nil, 0o600); err != nil {
		t.Fatalf("create log: %v", err)
	}
	if err := os.WriteFile(fx.staged, nil, 0o600); err != nil {
		t.Fatalf("create staged list: %v", err)
	}

	writeStub(t, fx.stubDir, "git", `#!/bin/sh
{ printf 'git'; printf '\x1f%s' "$@"; printf '\n'; } >> "$STUB_LOG"
if [ "$1" = "rev-parse" ]; then
  printf '%s\n' "$STUB_REPO"
  exit 0
fi
if [ "$1" = "diff" ]; then
  case " $* " in
    *" -z "*) tr '\n' '\0' < "$STUB_STAGED";;
    *) cat "$STUB_STAGED";;
  esac
fi
exit 0
`)
	writeStub(t, fx.stubDir, "make", `#!/bin/sh
{ printf 'make'; printf '\x1f%s' "$@"; printf '\x1fBASE_REF=%s' "${BASE_REF:-}"; printf '\n'; } >> "$STUB_LOG"
if [ -n "${STUB_FAIL_MAKE:-}" ]; then
  case " $* " in *" $STUB_FAIL_MAKE "*) exit 1;; esac
fi
exit 0
`)
	writeStub(t, fx.stubDir, "go", `#!/bin/sh
{ printf 'go'; printf '\x1f%s' "$@"; printf '\n'; } >> "$STUB_LOG"
if [ -n "${STUB_FAIL_GO:-}" ]; then
  case " $* " in *" $STUB_FAIL_GO "*) exit 1;; esac
fi
exit 0
`)
	if tmuxUsable {
		writeStub(t, fx.stubDir, "tmux", `#!/bin/sh
{ printf 'tmux'; printf '\x1f%s' "$@"; printf '\n'; } >> "$STUB_LOG"
exit "${STUB_TMUX_RC:-0}"
`)
	}
	return fx
}

func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub %q: %v", name, err)
	}
}

// stageFiles sets the staged-file list the git stub reports (newline
// delimited; the stub converts for the -z query) and materializes each file
// under the fake repo root so the hook's real wc -l can measure it.
func (fx *hookFixture) stageFiles(t *testing.T, relPaths ...string) {
	t.Helper()
	var b strings.Builder
	for _, rel := range relPaths {
		target := filepath.Join(fx.repoDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("mkdir for %q: %v", rel, err)
		}
		if err := os.WriteFile(target, []byte("package x\n"), 0o644); err != nil {
			t.Fatalf("write %q: %v", rel, err)
		}
		b.WriteString(rel)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(fx.staged, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write staged list: %v", err)
	}
}

// runHook executes the repository hook script via bash from the fixture's
// unrelated working directory. The environment is constructed explicitly so
// ambient AMUX_* variables can never leak into a case; extraEnv carries
// per-case flags (AMUX_SKIP_*, STUB_*).
func (fx *hookFixture) runHook(t *testing.T, hook string, extraEnv map[string]string) (string, int) {
	t.Helper()
	hookPath := filepath.Join(repoRoot(t), ".githooks", hook)
	env := []string{
		"PATH=" + fx.stubDir + string(os.PathListSeparator) + "/usr/bin" + string(os.PathListSeparator) + "/bin",
		"HOME=" + t.TempDir(),
		"STUB_LOG=" + fx.logFile,
		"STUB_REPO=" + fx.repoDir,
		"STUB_STAGED=" + fx.staged,
	}
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}

	cmd := exec.Command("bash", hookPath)
	cmd.Dir = fx.cwd
	cmd.Env = env
	out, runErr := cmd.CombinedOutput()

	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("run %s: %v\n%s", hook, runErr, out)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

// invocations returns the recorded stub calls; each record is
// argv[0] followed by \x1f-separated fields as logged by the stubs.
func (fx *hookFixture) invocations(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(fx.logFile)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	var records [][]string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		records = append(records, strings.Split(line, "\x1f"))
	}
	return records
}

// sawPrefix reports whether any recorded invocation begins with want.
func (fx *hookFixture) sawPrefix(t *testing.T, want ...string) bool {
	t.Helper()
	for _, rec := range fx.invocations(t) {
		if len(rec) < len(want) {
			continue
		}
		match := true
		for i, field := range want {
			if rec[i] != field {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// sawAnyField reports whether any recorded invocation contains a field equal
// to want (used for BASE_REF forwarding, which the make stub appends last).
func (fx *hookFixture) sawAnyField(t *testing.T, want string) bool {
	t.Helper()
	for _, rec := range fx.invocations(t) {
		for _, field := range rec {
			if field == want {
				return true
			}
		}
	}
	return false
}

// logDump renders the invocation log for actionable test failures.
func (fx *hookFixture) logDump(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(fx.logFile)
	if err != nil {
		return "<unreadable: " + err.Error() + ">"
	}
	return string(data)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".githooks")); err != nil {
		t.Fatalf("repo root %q missing .githooks: %v", root, err)
	}
	return root
}
