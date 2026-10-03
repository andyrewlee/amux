//go:build !windows

package panelaunch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errUnreachable = errors.New("unreachable")

// stubExec captures the exec call instead of replacing the test process.
func stubExec(t *testing.T, fn func(path string, argv, env []string) error) {
	t.Helper()
	old := execFn
	execFn = fn
	t.Cleanup(func() { execFn = old })
}

// TestPaneLaunchRunExecsMergedEnv drives the consumer in-process with the
// exec seam: raw env bytes at the seam (not filtered by any shell), merged
// precedence, payload consumed before exec, and cwd applied.
func TestPaneLaunchRunExecsMergedEnv(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	workDir := t.TempDir()
	marker := "amux-pl-invoke-marker-9c1b"

	// Prepare the command the way production does — with the tmux-var strip
	// prefix the tmux layer prepends before writing the payload.
	p, err := Prepare(workDir, "unset TMUX TMUX_PANE; probe-command", []string{
		"AMUX_PL=" + marker,
		"AMUX_PL_RAW=\xff\xfe",
		"AMUX_PL_DUP=first",
		"AMUX_PL_DUP=last",
		"AMUX_PL_EMPTY=",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	payloadPath := p.Path()

	var gotArgv, gotEnv []string
	var gotPath string
	var payloadGoneAtExec bool
	stubExec(t, func(path string, argv, env []string) error {
		gotPath = path
		gotArgv = argv
		gotEnv = env
		_, statErr := os.Lstat(payloadPath)
		payloadGoneAtExec = os.IsNotExist(statErr)
		cwd, _ := os.Getwd()
		// getcwd resolves symlinks; compare against the resolved workdir.
		if resolved, err := filepath.EvalSymlinks(workDir); err == nil && cwd != resolved {
			t.Errorf("exec cwd = %q, want %q", cwd, resolved)
		}
		return nil
	})
	cwdBefore, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwdBefore) })

	if code := runPayload(payloadPath); code != 0 {
		t.Fatalf("runPayload = %d", code)
	}
	if !payloadGoneAtExec {
		t.Fatal("payload still existed at exec")
	}
	if _, err := os.Lstat(p.Dir()); !os.IsNotExist(err) {
		t.Fatal("attempt dir survived consumption")
	}
	if len(gotArgv) != 3 || gotArgv[0] != "sh" || gotArgv[1] != "-lc" || gotArgv[2] != "unset TMUX TMUX_PANE; probe-command" {
		t.Fatalf("exec argv = %v", gotArgv)
	}
	env := envMap(gotEnv)
	if env["AMUX_PL"] != marker {
		t.Fatal("marker not delivered to exec env")
	}
	if env["AMUX_PL_RAW"] != "\xff\xfe" {
		t.Fatal("raw env bytes not preserved at exec")
	}
	if env["AMUX_PL_DUP"] != "last" {
		t.Fatal("duplicate assignment precedence broken")
	}
	if v, ok := env["AMUX_PL_EMPTY"]; !ok || v != "" {
		t.Fatal("empty value not delivered as empty")
	}
	if _, ok := env["TMUX"]; ok {
		t.Fatal("TMUX survived into exec env")
	}
	if gotPath == "" {
		t.Fatal("exec path empty")
	}
}

// TestPaneLaunchRunRemovesTMUX covers the tmux-injected pane vars: the
// consumer strips them from the exec env (the command's unset prefix is the
// in-shell belt to this suspender).
func TestPaneLaunchRunRemovesTMUX(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	t.Setenv("TMUX_PANE", "%4")
	p, err := Prepare(t.TempDir(), "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var gotEnv []string
	stubExec(t, func(path string, argv, env []string) error {
		gotEnv = env
		return nil
	})
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if code := runPayload(p.Path()); code != 0 {
		t.Fatalf("runPayload = %d", code)
	}
	env := envMap(gotEnv)
	if _, ok := env["TMUX"]; ok {
		t.Fatal("TMUX not stripped")
	}
	if _, ok := env["TMUX_PANE"]; ok {
		t.Fatal("TMUX_PANE not stripped")
	}
}

// TestPaneLaunchRunRejects covers unsafe payloads: wrong shape, symlinked
// payload, tampered permissions, expired, malformed — every failure is a
// generic stage error that removes the owned payload but never evals data.
// execFn is trapped for the whole test: a regression that lets a rejected
// payload reach exec would otherwise syscall.Exec over this test process
// and silently pass.
func TestPaneLaunchRunRejects(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	stubExec(t, func(string, []string, []string) error {
		t.Error("rejected payload reached exec")
		return errUnreachable
	})
	workDir := t.TempDir()

	t.Run("symlink payload refused", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), attemptPrefix+"sym")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(victim, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, payloadName)
		if err := os.Symlink(victim, link); err != nil {
			t.Fatal(err)
		}
		if code := runPayload(link); code == 0 {
			t.Fatal("symlinked payload consumed")
		}
		if _, err := os.Lstat(link); err != nil {
			t.Fatal("symlink was removed — only exact owned files may be unlinked")
		}
	})

	t.Run("wrong permissions refused", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), attemptPrefix+"perm")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, payloadName)
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code := runPayload(path); code == 0 {
			t.Fatal("payload with 0644 consumed")
		}
	})

	t.Run("dir permissions refused", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), attemptPrefix+"dperm")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, payloadName)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := runPayload(path); code == 0 {
			t.Fatal("payload in 0755 dir consumed")
		}
	})

	t.Run("expired payload removed", func(t *testing.T) {
		// Rewrite the payload's expiry into the past — a fake clock for
		// prepare AND consume would compare fake-now to fake-now+5m and
		// accept.
		p, err := Prepare(workDir, "echo hi", []string{"K=V"})
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		raw, err := os.ReadFile(p.Path())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		decoded, err := decodePayload(raw, time.Now())
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		decoded.ExpiresUnix = time.Now().Add(-time.Minute).Unix()
		raw, err = encodePayload(decoded)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if err := os.WriteFile(p.Path(), raw, 0o600); err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		if code := runPayload(p.Path()); code == 0 {
			t.Fatal("expired payload consumed")
		}
		if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
			t.Fatal("expired payload left behind")
		}
	})

	t.Run("malformed payload removed", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), attemptPrefix+"bad")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, payloadName)
		if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := runPayload(path); code == 0 {
			t.Fatal("malformed payload consumed")
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("malformed payload left behind")
		}
	})

	t.Run("unrelated directory refused", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), "some-other-dir")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, payloadName)
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := runPayload(path); code == 0 {
			t.Fatal("payload outside attempt dir consumed")
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("foreign payload removed")
		}
	})

	t.Run("missing workdir fails after unlink", func(t *testing.T) {
		p, err := Prepare(filepath.Join(workDir, "gone"), "echo hi", nil)
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if code := runPayload(p.Path()); code == 0 {
			t.Fatal("run succeeded with missing workdir")
		}
		if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
			t.Fatal("payload survived failed chdir")
		}
	})
}

// TestPaneLaunchDiscardRules covers the discard operation's exact-touch
// semantics.
func TestPaneLaunchDiscardRules(t *testing.T) {
	stubTempRoot(t, t.TempDir())

	t.Run("removes owned attempt", func(t *testing.T) {
		p, err := Prepare("/tmp", "echo hi", nil)
		if err != nil {
			t.Fatalf("Prepare: %v", err)
		}
		if code := discardPayload(p.Path()); code != 0 {
			t.Fatalf("discardPayload = %d", code)
		}
		if _, err := os.Lstat(p.Dir()); !os.IsNotExist(err) {
			t.Fatal("attempt survived discard")
		}
	})

	t.Run("already consumed is harmless", func(t *testing.T) {
		path := filepath.Join(tempRootFn(), attemptPrefix+"gone", payloadName)
		if code := discardPayload(path); code != 0 {
			t.Fatalf("discardPayload = %d", code)
		}
	})

	t.Run("symlink dir refused", func(t *testing.T) {
		target := filepath.Join(tempRootFn(), attemptPrefix+"target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(tempRootFn(), attemptPrefix+"link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if code := discardPayload(filepath.Join(link, payloadName)); code == 0 {
			t.Fatal("discard followed a symlinked attempt dir")
		}
	})

	t.Run("unrelated children keep dir", func(t *testing.T) {
		dir := filepath.Join(tempRootFn(), attemptPrefix+"kids")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, payloadName), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(dir, "other")
		if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code := discardPayload(filepath.Join(dir, payloadName)); code != 0 {
			t.Fatalf("discardPayload = %d", code)
		}
		if _, err := os.Lstat(other); err != nil {
			t.Fatal("unrelated child removed")
		}
	})
}

// TestPaneLaunchSubprocessRun drives the helper in a target child process:
// target chdir, target merged env, target exec, target exit status.
func TestPaneLaunchSubprocessRun(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	workDir := t.TempDir()
	outFile := filepath.Join(t.TempDir(), "probe.out")
	marker := "amux-pl-subproc-marker-42"

	// The probe writes env+cwd to a file via the final shell — the command is
	// the captured pane command, proof the consumer exec'd `sh -lc`.
	command := fmt.Sprintf("echo \"$AMUX_PL_SUB|$PWD|$AMUX_PL_DUP\" > %s; exit 3", shellQ(outFile))
	p, err := Prepare(workDir, command, []string{
		"AMUX_PL_SUB=" + marker,
		"AMUX_PL_DUP=old",
		"AMUX_PL_DUP=new",
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	_, stderr, code := runHelperArgv(t, invocationFlag, "run", p.Path())
	if code != 3 {
		t.Fatalf("helper exit = %d, want 3 (stderr=%s)", code, stderr)
	}
	if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
		t.Fatal("payload survived subprocess consumption")
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("probe output: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(raw)), "|")
	if len(parts) != 3 || parts[0] != marker || parts[2] != "new" {
		t.Fatalf("probe output missing delivered env")
	}
	wd, err := filepath.EvalSymlinks(strings.TrimSpace(parts[1]))
	if err != nil {
		t.Fatalf("resolve probe cwd: %v", err)
	}
	want, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	if wd != want {
		t.Fatalf("probe cwd = %q, want %q", wd, want)
	}
}

// TestPaneLaunchSubprocessStderrSanitized asserts helper failures emit no
// marker, payload content, or path detail.
func TestPaneLaunchSubprocessStderrSanitized(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	marker := "amux-pl-stderr-marker-bad"
	p, err := Prepare("/definitely/missing/workdir", "echo "+marker, []string{"AMUX_PL=" + marker})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	_, stderr, code := runHelperArgv(t, invocationFlag, "run", p.Path())
	if code == 0 {
		t.Fatal("helper succeeded on missing workdir")
	}
	for _, leak := range []string{marker, p.Path(), "AMUX_PL"} {
		if strings.Contains(stderr, leak) {
			t.Fatalf("helper stderr leaked %q: %s", leak, stderr)
		}
	}
}

// TestPaneLaunchSubprocessDiscard runs the discard op in a target child.
func TestPaneLaunchSubprocessDiscard(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, _, code := runHelperArgv(t, invocationFlag, "discard", p.Path()); code != 0 {
		t.Fatalf("discard exit = %d", code)
	}
	if _, err := os.Lstat(p.Dir()); !os.IsNotExist(err) {
		t.Fatal("attempt survived subprocess discard")
	}
}

// TestPaneLaunchSubprocessExpiry uses the fake clock to expire a prepared
// payload, then proves the consumer refuses it — no target five-minute wait.
func TestPaneLaunchSubprocessExpiry(t *testing.T) {
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// Back-date the payload file's expiry by rewriting it with an elapsed
	// timestamp, then invoke the target consumer.
	raw, err := os.ReadFile(p.Path())
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}
	decoded, err := decodePayload(raw, time.Now())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	decoded.ExpiresUnix = time.Now().Add(-time.Minute).Unix()
	raw, err = encodePayload(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if err := os.WriteFile(p.Path(), raw, 0o600); err != nil {
		t.Fatalf("rewrite payload: %v", err)
	}
	if _, _, code := runHelperArgv(t, invocationFlag, "run", p.Path()); code == 0 {
		t.Fatal("expired payload consumed")
	}
	if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
		t.Fatal("expired payload left behind")
	}
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func shellQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
