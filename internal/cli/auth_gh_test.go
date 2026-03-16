package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/sandbox"
)

type ghAuthTestProvider struct {
	sb        sandbox.RemoteSandbox
	getErr    error
	deletedID []string
}

func (p *ghAuthTestProvider) Name() string { return "test-provider" }

func (p *ghAuthTestProvider) CreateSandbox(context.Context, sandbox.SandboxCreateConfig) (sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *ghAuthTestProvider) GetSandbox(context.Context, string) (sandbox.RemoteSandbox, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	return p.sb, nil
}

func (p *ghAuthTestProvider) ListSandboxes(context.Context) ([]sandbox.RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *ghAuthTestProvider) DeleteSandbox(_ context.Context, id string) error {
	p.deletedID = append(p.deletedID, id)
	return nil
}

func (p *ghAuthTestProvider) Volumes() sandbox.VolumeManager     { return nil }
func (p *ghAuthTestProvider) Snapshots() sandbox.SnapshotManager { return nil }
func (p *ghAuthTestProvider) SupportsFeature(sandbox.ProviderFeature) bool {
	return false
}

func TestAcquireSandboxForGhAuthCreatesTemporarySandboxWhenNoSandboxExists(t *testing.T) {
	cwd := t.TempDir()
	provider := &ghAuthTestProvider{}
	tempSandbox := sandbox.NewMockRemoteSandbox("temp-123")
	var createdCfg sandbox.SandboxConfig

	prevResolve := resolveProviderForGhAuth
	prevLoadMeta := loadSandboxMetaForGhAuth
	prevCreate := createSandboxSessionNoMetaForGhAuth
	prevStdout := cliStdout
	defer func() {
		resolveProviderForGhAuth = prevResolve
		loadSandboxMetaForGhAuth = prevLoadMeta
		createSandboxSessionNoMetaForGhAuth = prevCreate
		cliStdout = prevStdout
	}()

	resolveProviderForGhAuth = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		return provider, provider.Name(), nil
	}
	loadSandboxMetaForGhAuth = func(cwd, provider string) (*sandbox.SandboxMeta, error) {
		return nil, nil
	}
	createSandboxSessionNoMetaForGhAuth = func(provider sandbox.Provider, cwd string, cfg sandbox.SandboxConfig) (sandbox.RemoteSandbox, *sandbox.SandboxMeta, error) {
		createdCfg = cfg
		return tempSandbox, nil, nil
	}

	var output bytes.Buffer
	cliStdout = &output

	sb, cleanup, err := acquireSandboxForGhAuth(cwd)
	if err != nil {
		t.Fatalf("acquireSandboxForGhAuth() error = %v", err)
	}
	if sb != tempSandbox {
		t.Fatalf("acquireSandboxForGhAuth() sandbox = %v, want temporary sandbox", sb)
	}
	if cleanup == nil {
		t.Fatal("acquireSandboxForGhAuth() cleanup = nil, want cleanup for temporary sandbox")
	}
	if createdCfg.Agent != sandbox.AgentShell {
		t.Fatalf("temporary sandbox agent = %q, want %q", createdCfg.Agent, sandbox.AgentShell)
	}
	if createdCfg.CredentialsMode != "sandbox" {
		t.Fatalf("temporary sandbox credentials mode = %q, want sandbox", createdCfg.CredentialsMode)
	}
	if !strings.Contains(output.String(), "temporary sandbox") {
		t.Fatalf("output = %q, want temporary sandbox message", output.String())
	}

	cleanup()

	if tempSandbox.State() != sandbox.StateStopped {
		t.Fatalf("temporary sandbox state = %q, want %q after cleanup", tempSandbox.State(), sandbox.StateStopped)
	}
	if len(provider.deletedID) != 1 || provider.deletedID[0] != tempSandbox.ID() {
		t.Fatalf("deleted sandboxes = %v, want [%q]", provider.deletedID, tempSandbox.ID())
	}
}

func TestAcquireSandboxForGhAuthReusesExistingSandbox(t *testing.T) {
	cwd := t.TempDir()
	existingSandbox := &resolveCurrentSandboxTestSandbox{id: "sb-123"}
	provider := &ghAuthTestProvider{sb: existingSandbox}
	createCalled := false

	prevResolve := resolveProviderForGhAuth
	prevLoadMeta := loadSandboxMetaForGhAuth
	prevCreate := createSandboxSessionNoMetaForGhAuth
	defer func() {
		resolveProviderForGhAuth = prevResolve
		loadSandboxMetaForGhAuth = prevLoadMeta
		createSandboxSessionNoMetaForGhAuth = prevCreate
	}()

	resolveProviderForGhAuth = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		return provider, provider.Name(), nil
	}
	loadSandboxMetaForGhAuth = func(cwd, provider string) (*sandbox.SandboxMeta, error) {
		return &sandbox.SandboxMeta{SandboxID: existingSandbox.id}, nil
	}
	createSandboxSessionNoMetaForGhAuth = func(provider sandbox.Provider, cwd string, cfg sandbox.SandboxConfig) (sandbox.RemoteSandbox, *sandbox.SandboxMeta, error) {
		createCalled = true
		return nil, nil, errors.New("unexpected create")
	}

	sb, cleanup, err := acquireSandboxForGhAuth(cwd)
	if err != nil {
		t.Fatalf("acquireSandboxForGhAuth() error = %v", err)
	}
	if sb != existingSandbox {
		t.Fatalf("acquireSandboxForGhAuth() sandbox = %v, want existing sandbox", sb)
	}
	if cleanup != nil {
		t.Fatal("acquireSandboxForGhAuth() returned cleanup for existing sandbox")
	}
	if createCalled {
		t.Fatal("acquireSandboxForGhAuth() created a temporary sandbox unexpectedly")
	}
}

var (
	_ sandbox.RemoteSandbox = (*resolveCurrentSandboxTestSandbox)(nil)
	_ io.Writer             = (*bytes.Buffer)(nil)
)
