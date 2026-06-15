package process

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

// TestScriptsNotTrustedErrorError covers the Error() formatter and the
// errors.Is/Unwrap contract that callers rely on to distinguish a trust skip
// from a genuine setup failure.
func TestScriptsNotTrustedErrorError(t *testing.T) {
	tests := []struct {
		name string
		err  *ScriptsNotTrustedError
		want string
	}{
		{
			name: "all fields populated",
			err:  &ScriptsNotTrustedError{Repo: "/repo/a", Command: "npm install", ConfigHash: "abc123"},
			want: `/repo/a ("npm install"): project scripts not trusted`,
		},
		{
			name: "empty command quotes to empty string",
			err:  &ScriptsNotTrustedError{Repo: "/repo/b", Command: "", ConfigHash: ""},
			want: `/repo/b (""): project scripts not trusted`,
		},
		{
			name: "command with quotes is escaped via %q",
			err:  &ScriptsNotTrustedError{Repo: "/repo/c", Command: `echo "hi"`, ConfigHash: "deadbeef"},
			want: `/repo/c ("echo \"hi\""): project scripts not trusted`,
		},
		{
			name: "empty repo",
			err:  &ScriptsNotTrustedError{Repo: "", Command: "make build"},
			want: ` ("make build"): project scripts not trusted`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
			// The sentinel must be reachable through both Unwrap and errors.Is so
			// callers can branch on a trust skip regardless of the carried fields.
			if !errors.Is(tt.err, ErrScriptsNotTrusted) {
				t.Fatalf("errors.Is(err, ErrScriptsNotTrusted) = false, want true")
			}
			if unwrapped := tt.err.Unwrap(); !errors.Is(unwrapped, ErrScriptsNotTrusted) {
				t.Fatalf("Unwrap() = %v, want ErrScriptsNotTrusted", unwrapped)
			}
		})
	}
}

// TestScriptRunnerTrustRepoScriptsIfHash exercises the hash-bound trust path:
// matching hash and empty-hash both trust; a mismatch returns
// ErrScriptsChangedSincePrompt without trusting; a missing config file is a
// no-op; a malformed/unreadable config surfaces the load error.
func TestScriptRunnerTrustRepoScriptsIfHash(t *testing.T) {
	const config = `{"setup-workspace": ["echo hi"]}`
	wantHash := hashConfig([]byte(config))

	tests := []struct {
		name         string
		writeConfig  bool
		configBody   string
		expectedHash string
		wantErr      error
		wantTrusted  bool
	}{
		{
			name:         "matching hash records trust",
			writeConfig:  true,
			configBody:   config,
			expectedHash: wantHash,
			wantErr:      nil,
			wantTrusted:  true,
		},
		{
			name:         "empty expected hash skips the check and trusts",
			writeConfig:  true,
			configBody:   config,
			expectedHash: "",
			wantErr:      nil,
			wantTrusted:  true,
		},
		{
			name:         "mismatched hash returns changed-since-prompt and does not trust",
			writeConfig:  true,
			configBody:   config,
			expectedHash: "0000000000000000000000000000000000000000000000000000000000000000",
			wantErr:      ErrScriptsChangedSincePrompt,
			wantTrusted:  false,
		},
		{
			name:         "missing config file is a no-op",
			writeConfig:  false,
			expectedHash: "anything",
			wantErr:      nil,
			wantTrusted:  false, // no config to trust; IsTrusted on empty content stays false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			if tt.writeConfig {
				writeWorkspaceConfig(t, repo, tt.configBody)
			}

			runner := NewScriptRunner(6200, 10)
			useTempTrust(t, runner)

			err := runner.TrustRepoScriptsIfHash(repo, tt.expectedHash)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("TrustRepoScriptsIfHash() error = %v, want %v", err, tt.wantErr)
			}

			// Verify the trust state actually changed (or not) as a behavioral
			// consequence, not just the returned error.
			_, raw, loadErr := runner.loadConfigRaw(repo)
			if loadErr != nil {
				t.Fatalf("loadConfigRaw() error = %v", loadErr)
			}
			if got := runner.trust.IsTrusted(repo, raw); got != tt.wantTrusted {
				t.Fatalf("IsTrusted() = %v, want %v", got, tt.wantTrusted)
			}
		})
	}
}

// TestScriptRunnerTrustRepoScriptsIfHashLoadError proves the load error is
// surfaced (not swallowed) and trust is not recorded when the config cannot be
// read.
func TestScriptRunnerTrustRepoScriptsIfHashLoadError(t *testing.T) {
	repo := t.TempDir()
	writeWorkspaceConfig(t, repo, `{"run":"echo hi"}`)
	configPath := filepath.Join(repo, ".amux", configFilename)
	if err := os.Chmod(configPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o644) })

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)

	err := runner.TrustRepoScriptsIfHash(repo, "")
	if err == nil {
		t.Fatal("expected a read error, got nil")
	}
	if os.IsNotExist(err) {
		t.Fatalf("expected permission error, got IsNotExist: %v", err)
	}
}

// TestScriptRunnerTrustRepoScriptsIfHashEnablesExecution is an end-to-end
// behavioral check: after TrustRepoScriptsIfHash succeeds with the matching
// hash, RunScript no longer returns the not-trusted sentinel.
func TestScriptRunnerTrustRepoScriptsIfHashEnablesExecution(t *testing.T) {
	repo := t.TempDir()
	wsRoot := t.TempDir()
	const config = `{"run": "true"}`
	writeWorkspaceConfig(t, repo, config)

	runner := NewScriptRunner(6200, 10)
	useTempTrust(t, runner)
	ws := &data.Workspace{Repo: repo, Root: wsRoot}

	// Before trusting, the repo-supplied run script is gated.
	if _, err := runner.RunScript(ws, ScriptRun); !errors.Is(err, ErrScriptsNotTrusted) {
		t.Fatalf("expected ErrScriptsNotTrusted before trust, got %v", err)
	}

	if err := runner.TrustRepoScriptsIfHash(repo, hashConfig([]byte(config))); err != nil {
		t.Fatalf("TrustRepoScriptsIfHash() error = %v", err)
	}

	cmd, err := runner.RunScript(ws, ScriptRun)
	if err != nil {
		t.Fatalf("RunScript() after trust error = %v", err)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd after trusting repo")
	}
	_ = runner.Stop(ws)
}

// TestScriptRunnerPortAllocated covers the validation guards and the allocator
// passthrough of PortAllocated.
func TestScriptRunnerPortAllocated(t *testing.T) {
	const portStart = 6200

	tests := []struct {
		name      string
		ws        *data.Workspace
		allocate  bool
		nilAlloc  bool
		wantPort  int
		wantFound bool
	}{
		{
			name:      "nil workspace",
			ws:        nil,
			wantPort:  0,
			wantFound: false,
		},
		{
			name:      "empty repo fails validation",
			ws:        &data.Workspace{Repo: "", Root: "/some/root"},
			wantPort:  0,
			wantFound: false,
		},
		{
			name:      "empty root fails validation",
			ws:        &data.Workspace{Repo: "/some/repo", Root: ""},
			wantPort:  0,
			wantFound: false,
		},
		{
			name:      "nil allocator reports no allocation",
			ws:        &data.Workspace{Repo: "/some/repo", Root: "/some/root"},
			nilAlloc:  true,
			wantPort:  0,
			wantFound: false,
		},
		{
			name:      "valid workspace with no allocation",
			ws:        &data.Workspace{Repo: "/some/repo", Root: "/some/root"},
			allocate:  false,
			wantPort:  0,
			wantFound: false,
		},
		{
			name:      "valid workspace returns allocated base",
			ws:        &data.Workspace{Repo: "/some/repo", Root: "/some/root"},
			allocate:  true,
			wantPort:  portStart,
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewScriptRunner(portStart, 10)
			if tt.nilAlloc {
				runner.portAllocator = nil
			}
			if tt.allocate && runner.portAllocator != nil {
				runner.portAllocator.AllocatePort(tt.ws.Root)
			}

			port, found := runner.PortAllocated(tt.ws)
			if port != tt.wantPort || found != tt.wantFound {
				t.Fatalf("PortAllocated() = (%d, %v), want (%d, %v)", port, found, tt.wantPort, tt.wantFound)
			}
		})
	}
}

// TestScriptRunnerPortAllocatedReflectsRelease proves PortAllocated mirrors the
// allocator so callers observe a release: a freshly allocated workspace is
// reported held, and after ReleaseWorkspace it is reported free.
func TestScriptRunnerPortAllocatedReflectsRelease(t *testing.T) {
	ws := &data.Workspace{Repo: t.TempDir(), Root: t.TempDir()}

	runner := NewScriptRunner(6200, 10)
	runner.portAllocator.AllocatePort(ws.Root)

	port, ok := runner.PortAllocated(ws)
	if !ok || port != 6200 {
		t.Fatalf("PortAllocated() = (%d, %v), want (6200, true) after allocate", port, ok)
	}

	runner.ReleaseWorkspace(ws)

	if _, ok := runner.PortAllocated(ws); ok {
		t.Fatal("PortAllocated() reported held after release, want released")
	}
}
