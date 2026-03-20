package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
	"github.com/andyrewlee/amux/internal/sandbox"
)

type attachProviderStub struct {
	getCalls int
}

func (p *attachProviderStub) Name() string { return "provider-b" }

func (p *attachProviderStub) CreateSandbox(context.Context, sandbox.SandboxCreateConfig) (sandbox.RemoteSandbox, error) {
	return nil, nil
}

func (p *attachProviderStub) GetSandbox(context.Context, string) (sandbox.RemoteSandbox, error) {
	p.getCalls++
	return &rollbackSandbox{id: "sb-provider-b"}, nil
}

func (p *attachProviderStub) ListSandboxes(context.Context) ([]sandbox.RemoteSandbox, error) {
	return nil, nil
}

func (p *attachProviderStub) DeleteSandbox(context.Context, string) error { return nil }

func (p *attachProviderStub) Volumes() sandbox.VolumeManager { return nil }

func (p *attachProviderStub) Snapshots() sandbox.SnapshotManager { return nil }

func (p *attachProviderStub) SupportsFeature(sandbox.ProviderFeature) bool { return false }

func TestAttachSessionUsesSelectedProviderForMetadataLookup(t *testing.T) {
	t.Setenv("AMUX_PROVIDER", "provider-b")

	origLoadConfig := loadSandboxConfig
	origResolveProvider := resolveSandboxProvider
	origLoadMeta := loadSandboxMeta
	origSetupCredentials := setupSandboxCredentials
	t.Cleanup(func() {
		loadSandboxConfig = origLoadConfig
		resolveSandboxProvider = origResolveProvider
		loadSandboxMeta = origLoadMeta
		setupSandboxCredentials = origSetupCredentials
	})

	loadSandboxConfig = func() (sandbox.Config, error) {
		return sandbox.Config{DaytonaAPIKey: "test-key"}, nil
	}
	provider := &attachProviderStub{}
	resolveSandboxProvider = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		if override != "" {
			t.Fatalf("provider override = %q, want empty selected-provider lookup", override)
		}
		return provider, provider.Name(), nil
	}
	loadSandboxMeta = func(cwd, providerName string) (*sandbox.SandboxMeta, error) {
		if providerName != provider.Name() {
			t.Fatalf("metadata provider filter = %q, want %q", providerName, provider.Name())
		}
		return nil, nil
	}
	setupSandboxCredentials = func(sb sandbox.RemoteSandbox, cfg sandbox.CredentialsConfig, verbose bool) error {
		t.Fatal("SetupCredentials should not run when no matching metadata exists for the selected provider")
		return nil
	}

	manager := NewSandboxManager(nil)
	ws := &data.Workspace{Root: t.TempDir()}

	session, err := manager.attachSession(ws)
	if err != nil {
		t.Fatalf("attachSession() error = %v", err)
	}
	if session != nil {
		t.Fatalf("attachSession() = %#v, want nil without matching metadata for selected provider", session)
	}
	if provider.getCalls != 0 {
		t.Fatalf("GetSandbox() calls = %d, want 0 without matching metadata", provider.getCalls)
	}
}

func TestAttachSessionIgnoresCanonicalLiveSessionFromDifferentProvider(t *testing.T) {
	t.Setenv("AMUX_PROVIDER", "provider-b")

	origLoadConfig := loadSandboxConfig
	origResolveProvider := resolveSandboxProvider
	origLoadMeta := loadSandboxMeta
	origSetupCredentials := setupSandboxCredentials
	t.Cleanup(func() {
		loadSandboxConfig = origLoadConfig
		resolveSandboxProvider = origResolveProvider
		loadSandboxMeta = origLoadMeta
		setupSandboxCredentials = origSetupCredentials
	})

	loadSandboxConfig = func() (sandbox.Config, error) {
		return sandbox.Config{DaytonaAPIKey: "test-key"}, nil
	}
	provider := &attachProviderStub{}
	resolveSandboxProvider = func(cfg sandbox.Config, cwd, override string) (sandbox.Provider, string, error) {
		return provider, provider.Name(), nil
	}
	loadSandboxMeta = func(cwd, providerName string) (*sandbox.SandboxMeta, error) {
		if providerName != provider.Name() {
			t.Fatalf("metadata provider filter = %q, want %q", providerName, provider.Name())
		}
		return &sandbox.SandboxMeta{
			SandboxID:    "sb-provider-b",
			Provider:     provider.Name(),
			WorktreeID:   sandbox.ComputeWorktreeID(cwd),
			WorkspaceIDs: []string{"ws-provider-b"},
		}, nil
	}
	setupSandboxCredentials = func(sb sandbox.RemoteSandbox, cfg sandbox.CredentialsConfig, verbose bool) error {
		return nil
	}

	base := t.TempDir()
	realRepo := filepath.Join(base, "repo-real")
	realRoot := filepath.Join(realRepo, "feature")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", realRoot, err)
	}

	linkRepoA := filepath.Join(base, "repo-link-a")
	if err := os.Symlink(realRepo, linkRepoA); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoA, realRepo, err)
	}
	linkRootA := filepath.Join(linkRepoA, "feature")

	linkRepoB := filepath.Join(base, "repo-link-b")
	if err := os.Symlink(realRepo, linkRepoB); err != nil {
		t.Fatalf("Symlink(%q -> %q) error = %v", linkRepoB, realRepo, err)
	}
	linkRootB := filepath.Join(linkRepoB, "feature")

	manager := NewSandboxManager(nil)
	manager.sessionsWithTags = nil
	existing := &sandboxSession{
		sandbox:       &rollbackSandbox{id: "sb-provider-a"},
		providerName:  "provider-a",
		worktreeID:    sandbox.ComputeWorktreeID(linkRootA),
		workspaceRepo: linkRepoA,
		workspaceRoot: linkRootA,
		workspacePath: "/remote/provider-a",
	}
	manager.storeSession(existing)

	ws := data.NewWorkspace("feature", "main", "main", linkRepoB, linkRootB)
	session, err := manager.attachSession(ws)
	if err != nil {
		t.Fatalf("attachSession() error = %v", err)
	}
	if session == nil {
		t.Fatal("attachSession() = nil, want provider-b session")
	}
	if session == existing {
		t.Fatal("expected attachSession() to ignore canonical live session from provider-a")
	}
	if session.providerName != provider.Name() {
		t.Fatalf("providerName = %q, want %q", session.providerName, provider.Name())
	}
	if session.sandbox == nil || session.sandbox.ID() != "sb-provider-b" {
		t.Fatalf("sandbox = %#v, want attached provider-b sandbox", session.sandbox)
	}
	if provider.getCalls != 1 {
		t.Fatalf("GetSandbox() calls = %d, want 1 for provider-b attach", provider.getCalls)
	}
}

func TestSelectedSandboxMetadataProviderFallsBackToDefaultProvider(t *testing.T) {
	origLoadConfig := loadSandboxConfig
	t.Cleanup(func() {
		loadSandboxConfig = origLoadConfig
	})

	loadSandboxConfig = func() (sandbox.Config, error) {
		return sandbox.Config{}, nil
	}

	if got := selectedSandboxMetadataProvider(); got != sandbox.DefaultProviderName {
		t.Fatalf("selectedSandboxMetadataProvider() = %q, want %q", got, sandbox.DefaultProviderName)
	}
}
