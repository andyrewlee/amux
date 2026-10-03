package panelaunch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain is the subprocess seam: every package test binary can be invoked
// as the private launcher helper, so dispatch must run before testing's
// flag parsing — exactly like the production main.
func TestMain(m *testing.M) {
	if handled, code := HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestPaneLaunchProtocolRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	p := &payload{
		Version:     protocolVersion,
		ExpiresUnix: time.Now().Add(time.Minute).Unix(),
		WorkDir:     []byte("/tmp/work dir"),
		Command:     []byte("echo hi\nwith newline"),
		Environment: [][]byte{
			[]byte("K=V"),
			// Invalid UTF-8 and arbitrary bytes must survive verbatim — the
			// wire keeps env entries as raw bytes, not JSON strings.
			{'R', 'A', 'W', '=', 0xff, 0xfe, 'x'},
			[]byte("EMPTY="),
			[]byte("SPACE NAME=value with = signs"),
			[]byte("UNICODE=héllo→🙂"),
		},
	}
	raw, err := encodePayload(p)
	if err != nil {
		t.Fatalf("encodePayload: %v", err)
	}
	got, err := decodePayload(raw, time.Now())
	if err != nil {
		t.Fatalf("decodePayload: %v", err)
	}
	if string(got.WorkDir) != string(p.WorkDir) || string(got.Command) != string(p.Command) {
		t.Fatal("workdir/command did not round-trip")
	}
	if len(got.Environment) != len(p.Environment) {
		t.Fatalf("env len = %d, want %d", len(got.Environment), len(p.Environment))
	}
	for i := range p.Environment {
		if string(got.Environment[i]) != string(p.Environment[i]) {
			t.Fatalf("env[%d] did not round-trip raw bytes", i)
		}
	}
}

func TestPaneLaunchProtocolRejections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	valid := func() *payload {
		return &payload{
			Version:     protocolVersion,
			ExpiresUnix: time.Now().Add(time.Minute).Unix(),
			WorkDir:     []byte("/tmp/w"),
			Command:     []byte("echo hi"),
			Environment: [][]byte{[]byte("K=V")},
		}
	}
	mustEncode := func(p *payload) []byte {
		raw, err := encodePayload(p)
		if err != nil {
			t.Fatalf("encodePayload: %v", err)
		}
		return raw
	}
	now := time.Now()

	t.Run("unsupported version", func(t *testing.T) {
		p := valid()
		p.Version = 99
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted unsupported version")
		}
	})
	t.Run("expired", func(t *testing.T) {
		p := valid()
		p.ExpiresUnix = now.Add(-time.Second).Unix()
		if _, err := decodePayload(mustEncode(p), now); err != stageExpired {
			t.Fatalf("err = %v, want stageExpired", err)
		}
	})
	t.Run("boundary instant is expired", func(t *testing.T) {
		p := valid()
		p.ExpiresUnix = now.Unix()
		if _, err := decodePayload(mustEncode(p), now); err != stageExpired {
			t.Fatalf("err = %v, want stageExpired at boundary", err)
		}
	})
	t.Run("malformed json", func(t *testing.T) {
		if _, err := decodePayload([]byte("{nope"), now); err == nil {
			t.Fatal("accepted malformed payload")
		}
	})
	t.Run("empty workdir", func(t *testing.T) {
		p := valid()
		p.WorkDir = nil
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted empty workdir")
		}
	})
	t.Run("NUL in command", func(t *testing.T) {
		p := valid()
		p.Command = []byte{'a', 0, 'b'}
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted NUL command")
		}
	})
	t.Run("NUL in env", func(t *testing.T) {
		p := valid()
		p.Environment = [][]byte{{'K', 0, '=', 'v'}}
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted NUL env")
		}
	})
	t.Run("env missing equals", func(t *testing.T) {
		p := valid()
		p.Environment = [][]byte{[]byte("NOEQUALS")}
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted assignment without '='")
		}
	})
	t.Run("env empty name", func(t *testing.T) {
		p := valid()
		p.Environment = [][]byte{[]byte("=v")}
		if _, err := decodePayload(mustEncode(p), now); err == nil {
			t.Fatal("accepted empty env name")
		}
	})
	t.Run("error text carries no content", func(t *testing.T) {
		p := valid()
		p.Command = []byte("echo S3CR3T-MARKER")
		p.Version = 2
		_, err := decodePayload(mustEncode(p), now)
		if err == nil || strings.Contains(err.Error(), "S3CR3T-MARKER") {
			t.Fatalf("decode error leaked payload content: %v", err)
		}
	})
}

func TestPaneLaunchMergeEnv(t *testing.T) {
	got := mergeEnv(
		[]string{"A=base", "SERVER_ONLY=keep", "DUP=first", "EMPTYBASE="},
		[][]byte{
			[]byte("DUP=winner"),
			[]byte("DUP=last"),
			[]byte("NEW="),
			[]byte("NAME WITH SPACE=v v"),
		},
	)
	env := map[string]string{}
	for _, e := range got {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	if env["A"] != "base" || env["SERVER_ONLY"] != "keep" {
		t.Fatal("server-only keys must survive the merge")
	}
	if env["DUP"] != "last" {
		t.Fatal("last duplicate assignment must win")
	}
	if v, ok := env["NEW"]; !ok || v != "" {
		t.Fatal("empty assignment must be preserved as empty")
	}
	if env["NAME WITH SPACE"] != "v v" {
		t.Fatal("non-POSIX env name must survive")
	}
	if env["EMPTYBASE"] != "" {
		t.Fatal("inherited empty value must survive")
	}
}

func TestPaneLaunchDropEnvKeys(t *testing.T) {
	got := dropEnvKeys([]string{"TMUX=1", "KEEP=v", "TMUX_PANE=%3", "TMUXOTHER=x"}, "TMUX", "TMUX_PANE")
	if len(got) != 2 || got[0] != "KEEP=v" || got[1] != "TMUXOTHER=x" {
		t.Fatalf("dropEnvKeys = %v", got)
	}
}

func TestPaneLaunchInvocationDispatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	if handled, _ := HandleInvocation([]string{"--version"}); handled {
		t.Fatal("unrelated argv handled as pane-launch")
	}
	if handled, _ := HandleInvocation(nil); handled {
		t.Fatal("empty argv handled as pane-launch")
	}
	for _, args := range [][]string{
		{invocationFlag},
		{invocationFlag, "run"},
		{invocationFlag, "run", "a", "b"},
		{invocationFlag, "bogus", "/tmp/x/payload"},
		{invocationFlag, "run", "relative/payload"},
		{invocationFlag, "run", "/tmp/not-an-attempt/payload"},
		{invocationFlag, "run", "/tmp/amux-pane-launch-x/wrongname"},
	} {
		handled, code := HandleInvocation(args)
		if !handled {
			t.Fatalf("reserved argv %v not handled", args)
		}
		if code == 0 {
			t.Fatalf("malformed argv %v succeeded", args)
		}
	}
}

// TestPaneLaunchPrivateInvocation exercises HandleInvocation's discard path
// end-to-end in-process: a real prepared attempt is discarded and the
// already-consumed case is a no-op.
func TestPaneLaunchPrivateInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())

	p, err := Prepare(t.TempDir(), "echo hi", []string{"K=V"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	handled, code := HandleInvocation([]string{invocationFlag, "discard", p.Path()})
	if !handled || code != 0 {
		t.Fatalf("discard = (handled=%v, code=%d)", handled, code)
	}
	if _, err := os.Lstat(p.Path()); !os.IsNotExist(err) {
		t.Fatal("payload survived discard")
	}
	if _, err := os.Lstat(p.Dir()); !os.IsNotExist(err) {
		t.Fatal("attempt dir survived discard")
	}
	// Already-consumed: repeat discard is harmless.
	handled, code = HandleInvocation([]string{invocationFlag, "discard", p.Path()})
	if !handled || code != 0 {
		t.Fatalf("second discard = (handled=%v, code=%d)", handled, code)
	}
}

// stubTempRoot redirects payload attempts into a test-owned temp root.
func stubTempRoot(t *testing.T, root string) {
	t.Helper()
	old := tempRootFn
	tempRootFn = func() string { return root }
	t.Cleanup(func() { tempRootFn = old })
}

// runHelperArgv invokes this test binary as the launcher helper in a real
// subprocess — the same shape a pane spawn uses.
func runHelperArgv(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var outBuf, errBuf strings.Builder
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("helper spawn: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func TestPaneLaunchInvocationArgv(t *testing.T) {
	argv := InvocationArgv("/opt/amux", "run", "/tmp/x/payload")
	if len(argv) != 4 || argv[0] != "/opt/amux" || argv[1] != invocationFlag || argv[2] != "run" || argv[3] != "/tmp/x/payload" {
		t.Fatalf("InvocationArgv = %v", argv)
	}
}

func TestPaneLaunchSpecFileLayout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pane launch is unix-only")
	}
	stubTempRoot(t, t.TempDir())
	p, err := Prepare("/tmp", "echo hi", nil)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer func() { _ = p.Discard() }()
	if filepath.Base(p.Path()) != payloadName {
		t.Fatalf("payload name = %q", filepath.Base(p.Path()))
	}
	if !strings.HasPrefix(filepath.Base(p.Dir()), attemptPrefix) {
		t.Fatalf("attempt dir = %q", filepath.Base(p.Dir()))
	}
	if !filepath.IsAbs(p.Path()) {
		t.Fatal("payload path must be absolute")
	}
}
