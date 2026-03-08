package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type computerTestProvider struct {
	counter int
	volumes *computerTestVolumeManager
}

func (p *computerTestProvider) Name() string { return "test-provider" }

func (p *computerTestProvider) CreateSandbox(_ context.Context, config SandboxCreateConfig) (RemoteSandbox, error) {
	p.counter++
	return NewMockRemoteSandbox(fmt.Sprintf("sb-%d", p.counter)), nil
}

func (p *computerTestProvider) GetSandbox(context.Context, string) (RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *computerTestProvider) ListSandboxes(context.Context) ([]RemoteSandbox, error) {
	return nil, errors.New("not implemented")
}

func (p *computerTestProvider) DeleteSandbox(context.Context, string) error {
	return errors.New("not implemented")
}

func (p *computerTestProvider) Volumes() VolumeManager { return p.volumes }

func (p *computerTestProvider) Snapshots() SnapshotManager { return nil }

func (p *computerTestProvider) SupportsFeature(feature ProviderFeature) bool {
	return feature == FeatureVolumes
}

type computerTestVolumeManager struct{}

func (v *computerTestVolumeManager) Create(context.Context, string) (*VolumeInfo, error) {
	return nil, errors.New("not implemented")
}

func (v *computerTestVolumeManager) Get(context.Context, string) (*VolumeInfo, error) {
	return nil, errors.New("not implemented")
}

func (v *computerTestVolumeManager) GetOrCreate(_ context.Context, name string) (*VolumeInfo, error) {
	return &VolumeInfo{ID: "vol-" + name, Name: name, State: "ready"}, nil
}

func (v *computerTestVolumeManager) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func (v *computerTestVolumeManager) List(context.Context) ([]*VolumeInfo, error) {
	return nil, errors.New("not implemented")
}

func (v *computerTestVolumeManager) WaitReady(_ context.Context, name string, _ time.Duration) (*VolumeInfo, error) {
	return &VolumeInfo{ID: "vol-" + name, Name: name, State: "ready"}, nil
}

func TestCreateSandboxSessionNoMetaDoesNotPersistMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := filepath.Join(home, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	provider := &computerTestProvider{volumes: &computerTestVolumeManager{}}
	_, meta, err := CreateSandboxSessionNoMeta(provider, cwd, SandboxConfig{
		Agent:                 AgentClaude,
		Ephemeral:             true,
		PersistenceVolumeName: "persist-test",
	})
	if err != nil {
		t.Fatalf("CreateSandboxSessionNoMeta() error = %v", err)
	}
	if meta == nil {
		t.Fatal("CreateSandboxSessionNoMeta() meta = nil, want non-nil")
	}

	stored, err := LoadSandboxMeta(cwd, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if stored != nil {
		t.Fatalf("LoadSandboxMeta() = %+v, want nil", *stored)
	}
}

func TestCreateSandboxSessionPersistsMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwd := filepath.Join(home, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	provider := &computerTestProvider{volumes: &computerTestVolumeManager{}}
	sb, meta, err := CreateSandboxSession(provider, cwd, SandboxConfig{
		Agent:                 AgentClaude,
		Ephemeral:             true,
		PersistenceVolumeName: "persist-test",
	})
	if err != nil {
		t.Fatalf("CreateSandboxSession() error = %v", err)
	}
	if meta == nil || meta.SandboxID != sb.ID() {
		t.Fatalf("CreateSandboxSession() meta = %+v, want sandbox ID %q", meta, sb.ID())
	}

	stored, err := LoadSandboxMeta(cwd, provider.Name())
	if err != nil {
		t.Fatalf("LoadSandboxMeta() error = %v", err)
	}
	if stored == nil || stored.SandboxID != sb.ID() {
		t.Fatalf("LoadSandboxMeta() = %#v, want sandbox ID %q", stored, sb.ID())
	}
}
