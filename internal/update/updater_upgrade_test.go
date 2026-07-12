package update

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// recordingDeps returns an upgradeDeps wired to fakes that succeed by default
// and append each invoked step's name to *calls in execution order. Individual
// fields can be overridden per-test to inject a guard failure or error.
func recordingDeps(calls *[]string) upgradeDeps {
	return upgradeDeps{
		isHomebrewBuild: func() bool { *calls = append(*calls, "isHomebrewBuild"); return false },
		isGoInstall:     func() bool { *calls = append(*calls, "isGoInstall"); return false },
		findAsset: func(*Release) *Asset {
			*calls = append(*calls, "findAsset")
			return &Asset{Name: "amux_1.0.0_test.tar.gz", BrowserDownloadURL: "https://example.com/a.tar.gz"}
		},
		currentBinary: func() (string, error) { *calls = append(*calls, "currentBinary"); return "/usr/local/bin/amux", nil },
		canWrite:      func(string) bool { *calls = append(*calls, "canWrite"); return true },
		fetchChecksumsRaw: func(*Release) ([]byte, error) {
			*calls = append(*calls, "fetchChecksumsRaw")
			return []byte("deadbeef  amux_1.0.0_test.tar.gz\n"), nil
		},
		fetchSignature: func(*Release) ([]byte, error) {
			*calls = append(*calls, "fetchSignature")
			return []byte("untrusted comment: fake\nc2ln\n"), nil
		},
		verifySignature: func([]byte, []byte) error { *calls = append(*calls, "verifySignature"); return nil },
		download:        func(string, io.Writer) error { *calls = append(*calls, "download"); return nil },
		verify:          func(string, string) error { *calls = append(*calls, "verify"); return nil },
		extract:         func(string, string) (string, error) { *calls = append(*calls, "extract"); return "/tmp/amux", nil },
		install:         func(string, string) error { *calls = append(*calls, "install"); return nil },
	}
}

// upgraderWith builds an Updater whose Upgrade path is fully driven by deps.
func upgraderWith(deps upgradeDeps) *Updater {
	return &Updater{version: "v1.0.0", github: NewGitHubClient(), deps: deps}
}

func TestUpgradeNilRelease(t *testing.T) {
	var calls []string
	u := upgraderWith(recordingDeps(&calls))
	err := u.Upgrade(nil)
	if err == nil || !strings.Contains(err.Error(), "no release") {
		t.Fatalf("expected nil-release error, got: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("nil release should short-circuit before any dep call, got: %v", calls)
	}
}

func TestUpgradeGuardsAndErrors(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(d *upgradeDeps)
		wantErr    string
		wantNoCall string // a step that must never run once the guard trips
	}{
		{
			name:       "homebrew early return",
			mutate:     func(d *upgradeDeps) { d.isHomebrewBuild = func() bool { return true } },
			wantErr:    "brew upgrade amux",
			wantNoCall: "findAsset",
		},
		{
			name:       "go install early return",
			mutate:     func(d *upgradeDeps) { d.isGoInstall = func() bool { return true } },
			wantErr:    "go install",
			wantNoCall: "findAsset",
		},
		{
			name:       "no platform asset",
			mutate:     func(d *upgradeDeps) { d.findAsset = func(*Release) *Asset { return nil } },
			wantErr:    "no binary available",
			wantNoCall: "currentBinary",
		},
		{
			name: "current binary path error",
			mutate: func(d *upgradeDeps) {
				d.currentBinary = func() (string, error) { return "", errors.New("boom") }
			},
			wantErr:    "getting current binary path",
			wantNoCall: "canWrite",
		},
		{
			name:       "no write permission",
			mutate:     func(d *upgradeDeps) { d.canWrite = func(string) bool { return false } },
			wantErr:    "no write permission",
			wantNoCall: "fetchChecksumsRaw",
		},
		{
			name: "fetch checksums error",
			mutate: func(d *upgradeDeps) {
				d.fetchChecksumsRaw = func(*Release) ([]byte, error) { return nil, errors.New("net down") }
			},
			wantErr:    "fetching checksums",
			wantNoCall: "download",
		},
		{
			name: "missing signature asset",
			mutate: func(d *upgradeDeps) {
				d.fetchSignature = func(*Release) ([]byte, error) {
					return nil, errors.New("checksums.txt.minisig not found in release")
				}
			},
			wantErr:    "fetching release signature",
			wantNoCall: "download",
		},
		{
			name: "signature verification fails",
			mutate: func(d *upgradeDeps) {
				d.verifySignature = func([]byte, []byte) error { return errors.New("invalid signature") }
			},
			wantErr:    "release signature verification failed",
			wantNoCall: "download",
		},
		{
			name: "checksum not found for asset",
			mutate: func(d *upgradeDeps) {
				d.fetchChecksumsRaw = func(*Release) ([]byte, error) {
					return []byte("abc  some-other-asset.tar.gz\n"), nil
				}
			},
			wantErr:    "checksum not found",
			wantNoCall: "download",
		},
		{
			name:       "download error",
			mutate:     func(d *upgradeDeps) { d.download = func(string, io.Writer) error { return errors.New("404") } },
			wantErr:    "downloading",
			wantNoCall: "verify",
		},
		{
			name: "extract error",
			mutate: func(d *upgradeDeps) {
				d.extract = func(string, string) (string, error) { return "", errors.New("bad tar") }
			},
			wantErr:    "extracting binary",
			wantNoCall: "install",
		},
		{
			name:       "install error surfaces",
			mutate:     func(d *upgradeDeps) { d.install = func(string, string) error { return errors.New("eperm") } },
			wantErr:    "installing binary",
			wantNoCall: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			deps := recordingDeps(&calls)
			tt.mutate(&deps)
			u := upgraderWith(deps)

			err := u.Upgrade(&Release{TagName: "v1.0.0"})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
			if tt.wantNoCall != "" {
				for _, c := range calls {
					if c == tt.wantNoCall {
						t.Errorf("step %q must not run after guard %q tripped; calls=%v", tt.wantNoCall, tt.name, calls)
					}
				}
			}
		})
	}
}

// TestUpgradeChecksumMismatchNeverInstalls is the load-bearing safety test: if
// verification fails the install step must never be reached. A regression that
// installed before verifying would let this test catch it.
func TestUpgradeChecksumMismatchNeverInstalls(t *testing.T) {
	var calls []string
	deps := recordingDeps(&calls)
	deps.verify = func(string, string) error {
		calls = append(calls, "verify")
		return errors.New("checksum mismatch")
	}
	installed := false
	deps.install = func(string, string) error {
		installed = true
		calls = append(calls, "install")
		return nil
	}
	u := upgraderWith(deps)

	err := u.Upgrade(&Release{TagName: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("expected checksum verification error, got: %v", err)
	}
	if installed {
		t.Fatal("install must never run when checksum verification fails")
	}
	for _, c := range calls {
		if c == "install" {
			t.Fatalf("install appears in call order after failed verify: %v", calls)
		}
	}
}

func TestUpgradeArchiveCloseFailureNeverVerifies(t *testing.T) {
	var calls []string
	deps := recordingDeps(&calls)
	deps.download = func(_ string, w io.Writer) error {
		calls = append(calls, "download")
		f, ok := w.(*os.File)
		if !ok {
			t.Fatalf("download writer = %T, want *os.File", w)
		}
		if _, err := f.Write([]byte("partial archive")); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return nil
	}
	verified := false
	deps.verify = func(string, string) error {
		verified = true
		calls = append(calls, "verify")
		return nil
	}
	u := upgraderWith(deps)

	err := u.Upgrade(&Release{TagName: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "closing archive file") {
		t.Fatalf("expected archive close error, got: %v", err)
	}
	if verified {
		t.Fatal("verify must never run when archive close fails")
	}
	for _, c := range calls {
		if c == "verify" {
			t.Fatalf("verify appears in call order after failed close: %v", calls)
		}
	}
}

func TestUpgradeDownloadFailureReportsArchiveCloseFailure(t *testing.T) {
	var calls []string
	deps := recordingDeps(&calls)
	deps.download = func(_ string, w io.Writer) error {
		calls = append(calls, "download")
		f, ok := w.(*os.File)
		if !ok {
			t.Fatalf("download writer = %T, want *os.File", w)
		}
		if err := f.Close(); err != nil {
			return err
		}
		return errors.New("download failed")
	}
	verified := false
	deps.verify = func(string, string) error {
		verified = true
		calls = append(calls, "verify")
		return nil
	}
	u := upgraderWith(deps)

	err := u.Upgrade(&Release{TagName: "v1.0.0"})
	if err == nil {
		t.Fatal("expected download/close error, got nil")
	}
	for _, want := range []string{"downloading", "download failed", "closing archive file after failed download"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got: %v", want, err)
		}
	}
	if verified {
		t.Fatal("verify must never run when download fails")
	}
	for _, c := range calls {
		if c == "verify" {
			t.Fatalf("verify appears in call order after failed download: %v", calls)
		}
	}
}

// TestUpgradeHappyPathOrder pins the orchestration order, proving in particular
// that verify runs strictly before extract and install (verify -> extract ->
// install), so an install-before-verify regression cannot pass.
func TestUpgradeHappyPathOrder(t *testing.T) {
	var calls []string
	u := upgraderWith(recordingDeps(&calls))

	if err := u.Upgrade(&Release{TagName: "v1.0.0"}); err != nil {
		t.Fatalf("happy-path Upgrade() error = %v", err)
	}

	want := []string{
		"isHomebrewBuild", "isGoInstall", "findAsset", "currentBinary", "canWrite",
		"fetchChecksumsRaw", "fetchSignature", "verifySignature",
		"download", "verify", "extract", "install",
	}
	if len(calls) != len(want) {
		t.Fatalf("call sequence length mismatch: got %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call[%d] = %q, want %q (full=%v)", i, calls[i], want[i], calls)
		}
	}

	verifyIdx, installIdx := indexOf(calls, "verify"), indexOf(calls, "install")
	extractIdx := indexOf(calls, "extract")
	if !(verifyIdx < extractIdx && extractIdx < installIdx) {
		t.Errorf("expected verify(%d) < extract(%d) < install(%d)", verifyIdx, extractIdx, installIdx)
	}

	// The release signature must be verified before anything is downloaded,
	// i.e. before any checksum from the (signed) checksums file is trusted.
	sigIdx, downloadIdx := indexOf(calls, "verifySignature"), indexOf(calls, "download")
	if !(sigIdx < downloadIdx) {
		t.Errorf("expected verifySignature(%d) < download(%d)", sigIdx, downloadIdx)
	}
}

// TestUpgradeSignatureRejectedNeverInstalls is the signature-gate counterpart
// to the checksum-mismatch safety test: a rejected release signature must stop
// the upgrade before download, extract, or install run.
func TestUpgradeSignatureRejectedNeverInstalls(t *testing.T) {
	var calls []string
	deps := recordingDeps(&calls)
	deps.verifySignature = func([]byte, []byte) error {
		calls = append(calls, "verifySignature")
		return errors.New("invalid signature")
	}
	installed := false
	deps.install = func(string, string) error {
		installed = true
		calls = append(calls, "install")
		return nil
	}
	u := upgraderWith(deps)

	err := u.Upgrade(&Release{TagName: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "release signature verification failed") {
		t.Fatalf("expected signature verification error, got: %v", err)
	}
	if installed {
		t.Fatal("install must never run when signature verification fails")
	}
	for _, c := range calls {
		if c == "download" || c == "extract" || c == "install" {
			t.Fatalf("step %q ran after failed signature verification: %v", c, calls)
		}
	}
}

// TestUpgradeVersionMonotonicity covers the downgrade guard: equal and higher
// targets proceed (reinstall stays legitimate), a strictly-lower target is
// refused before download/extract/install run (same assertion style as
// TestUpgradeSignatureRejectedNeverInstalls) unless ForceDowngrade is set,
// and dev/unparseable current versions skip the check entirely.
func TestUpgradeVersionMonotonicity(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		targetTag      string
		force          bool
		wantErr        string // "" means Upgrade must succeed and install
	}{
		{
			name:           "higher target proceeds",
			currentVersion: "v1.0.0",
			targetTag:      "v1.1.0",
		},
		{
			name:           "equal target proceeds (reinstall allowed)",
			currentVersion: "v1.0.0",
			targetTag:      "v1.0.0",
		},
		{
			name:           "strictly lower target blocked before download",
			currentVersion: "v1.0.0",
			targetTag:      "v0.9.0",
			wantErr:        "refusing to downgrade from v1.0.0 to v0.9.0",
		},
		{
			name:           "strictly lower target proceeds when forced",
			currentVersion: "v1.0.0",
			targetTag:      "v0.9.0",
			force:          true,
		},
		{
			name:           "dev build skips downgrade check",
			currentVersion: "dev",
			targetTag:      "v0.9.0",
		},
		{
			name:           "unparseable current version skips downgrade check",
			currentVersion: "1.0",
			targetTag:      "v0.9.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			u := upgraderWith(recordingDeps(&calls))
			u.version = tt.currentVersion
			u.ForceDowngrade = tt.force

			err := u.Upgrade(&Release{TagName: tt.targetTag})

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Upgrade() error = %v, want success", err)
				}
				if indexOf(calls, "install") == -1 {
					t.Fatalf("expected install to run, calls=%v", calls)
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
			for _, c := range calls {
				if c == "download" || c == "extract" || c == "install" {
					t.Fatalf("step %q ran after downgrade guard tripped: %v", c, calls)
				}
			}
		})
	}
}

func indexOf(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}

// TestNewUpdaterWiresRealDeps guards against a future edit that forgets to
// default a deps field, which would nil-panic in production but be invisible to
// the fake-driven tests above.
func TestNewUpdaterWiresRealDeps(t *testing.T) {
	u := NewUpdater("v1.0.0", "none", "unknown")
	d := u.deps
	checks := []struct {
		name string
		nil  bool
	}{
		{"isHomebrewBuild", d.isHomebrewBuild == nil},
		{"isGoInstall", d.isGoInstall == nil},
		{"findAsset", d.findAsset == nil},
		{"currentBinary", d.currentBinary == nil},
		{"canWrite", d.canWrite == nil},
		{"fetchChecksumsRaw", d.fetchChecksumsRaw == nil},
		{"fetchSignature", d.fetchSignature == nil},
		{"verifySignature", d.verifySignature == nil},
		{"download", d.download == nil},
		{"verify", d.verify == nil},
		{"extract", d.extract == nil},
		{"install", d.install == nil},
	}
	for _, c := range checks {
		if c.nil {
			t.Errorf("NewUpdater left deps.%s nil; production Upgrade would panic", c.name)
		}
	}
}
