package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andyrewlee/amux/internal/sandbox"
)

const missingSandboxErrMsg = "no sandbox for this project - run `amux sandbox run <agent>` first"

type resolveCurrentSandboxTestProvider struct {
	getErr error
	sb     sandbox.RemoteSandbox
}

func (p *resolveCurrentSandboxTestProvider) Name() string { return "test-provider" }

func (p *resolveCurrentSandboxTestProvider) CreateSandbox(context.Context, sandbox.SandboxCreateConfig) (sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *resolveCurrentSandboxTestProvider) GetSandbox(context.Context, string) (sandbox.RemoteSandbox, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	return p.sb, nil
}

func (p *resolveCurrentSandboxTestProvider) ListSandboxes(context.Context) ([]sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *resolveCurrentSandboxTestProvider) DeleteSandbox(context.Context, string) error {
	return errors.New("not implemented")
}

func (p *resolveCurrentSandboxTestProvider) Volumes() sandbox.VolumeManager { return nil }

func (p *resolveCurrentSandboxTestProvider) Snapshots() sandbox.SnapshotManager { return nil }

func (p *resolveCurrentSandboxTestProvider) SupportsFeature(sandbox.ProviderFeature) bool {
	return false
}

type resolveCurrentSandboxTestSandbox struct {
	id           string
	startErr     error
	waitReadyErr error
}

func (s *resolveCurrentSandboxTestSandbox) ID() string                  { return s.id }
func (s *resolveCurrentSandboxTestSandbox) State() sandbox.SandboxState { return sandbox.StateStarted }
func (s *resolveCurrentSandboxTestSandbox) Labels() map[string]string   { return nil }
func (s *resolveCurrentSandboxTestSandbox) Start(context.Context) error { return s.startErr }
func (s *resolveCurrentSandboxTestSandbox) Stop(context.Context) error  { return nil }
func (s *resolveCurrentSandboxTestSandbox) WaitReady(context.Context, time.Duration) error {
	return s.waitReadyErr
}

func (s *resolveCurrentSandboxTestSandbox) Exec(context.Context, string, *sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return nil, nil
}

func (s *resolveCurrentSandboxTestSandbox) ExecInteractive(context.Context, string, io.Reader, io.Writer, io.Writer, *sandbox.ExecOptions) (int, error) {
	return 0, nil
}

func (s *resolveCurrentSandboxTestSandbox) UploadFile(context.Context, string, string) error {
	return nil
}

func (s *resolveCurrentSandboxTestSandbox) DownloadFile(context.Context, string, string) error {
	return nil
}

func (s *resolveCurrentSandboxTestSandbox) GetPreviewURL(context.Context, int) (string, error) {
	return "", nil
}
func (s *resolveCurrentSandboxTestSandbox) Refresh(context.Context) error { return nil }

func prepareSandboxMeta(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := sandbox.SaveSandboxMeta(cwd, "test-provider", sandbox.SandboxMeta{
		SandboxID: "sb-123",
		Agent:     sandbox.AgentClaude,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveSandboxMeta() error = %v", err)
	}
	return cwd
}

func TestResolveCurrentSandboxPropagatesGetSandboxErrors(t *testing.T) {
	cwd := prepareSandboxMeta(t)
	getErr := errors.New("api unavailable")
	provider := &resolveCurrentSandboxTestProvider{getErr: getErr}

	_, _, err := resolveCurrentSandbox(provider, cwd)
	if !errors.Is(err, getErr) {
		t.Fatalf("resolveCurrentSandbox() error = %v, want %v", err, getErr)
	}
}

func TestResolveCurrentSandboxMapsGetSandboxNotFound(t *testing.T) {
	cwd := prepareSandboxMeta(t)
	provider := &resolveCurrentSandboxTestProvider{getErr: sandbox.ErrNotFound}

	_, _, err := resolveCurrentSandbox(provider, cwd)
	if err == nil || err.Error() != missingSandboxErrMsg {
		t.Fatalf("resolveCurrentSandbox() error = %v, want %q", err, missingSandboxErrMsg)
	}
}

func TestResolveCurrentSandboxPropagatesStartErrors(t *testing.T) {
	cwd := prepareSandboxMeta(t)
	startErr := errors.New("auth denied")
	provider := &resolveCurrentSandboxTestProvider{
		sb: &resolveCurrentSandboxTestSandbox{
			id:       "sb-123",
			startErr: startErr,
		},
	}

	_, _, err := resolveCurrentSandbox(provider, cwd)
	if !errors.Is(err, startErr) {
		t.Fatalf("resolveCurrentSandbox() error = %v, want %v", err, startErr)
	}
}

func TestResolveCurrentSandboxMapsStartNotFound(t *testing.T) {
	cwd := prepareSandboxMeta(t)
	provider := &resolveCurrentSandboxTestProvider{
		sb: &resolveCurrentSandboxTestSandbox{
			id:       "sb-123",
			startErr: sandbox.ErrNotFound,
		},
	}

	_, _, err := resolveCurrentSandbox(provider, cwd)
	if err == nil || err.Error() != missingSandboxErrMsg {
		t.Fatalf("resolveCurrentSandbox() error = %v, want %q", err, missingSandboxErrMsg)
	}
}
