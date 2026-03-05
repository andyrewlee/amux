package sandbox

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	persistMountPath = "/amux"
)

// SandboxConfig configures sandbox creation.
type SandboxConfig struct {
	Agent                 Agent
	EnvVars               map[string]string
	Volumes               []VolumeSpec
	CredentialsMode       string
	AutoStopInterval      int32
	AutoDeleteInterval    int32
	Snapshot              string
	Ephemeral             bool
	PersistenceVolumeName string
}

func resolveVolumeMounts(manager VolumeManager, specs []VolumeSpec) ([]VolumeMount, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if manager == nil {
		return nil, errors.New("volume manager is not available")
	}
	mounts := make([]VolumeMount, 0, len(specs))
	for _, spec := range specs {
		volume, err := manager.WaitReady(context.Background(), spec.Name, 0)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, VolumeMount{VolumeID: volume.ID, MountPath: spec.MountPath})
	}
	return mounts, nil
}

func resolvePersistentMount(provider Provider, userSpecs []VolumeSpec, volumeName string) (*VolumeMount, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	for _, spec := range userSpecs {
		if spec.MountPath == persistMountPath || strings.HasPrefix(spec.MountPath, persistMountPath+"/") {
			return nil, fmt.Errorf("volume mount path %q conflicts with amux persistence mount", spec.MountPath)
		}
	}
	if !provider.SupportsFeature(FeatureVolumes) {
		return nil, fmt.Errorf("provider %q does not support volumes", provider.Name())
	}
	manager := provider.Volumes()
	if manager == nil {
		return nil, errors.New("volume manager is not available")
	}
	if strings.TrimSpace(volumeName) == "" {
		volumeName = defaultPersistenceVolumeName
	}
	volume, err := manager.GetOrCreate(context.Background(), volumeName)
	if err != nil {
		return nil, err
	}
	if _, err := manager.WaitReady(context.Background(), volumeName, 0); err != nil {
		return nil, err
	}
	return &VolumeMount{VolumeID: volume.ID, MountPath: persistMountPath}, nil
}

// CreateSandboxSession always creates a new sandbox for this run.
func CreateSandboxSession(provider Provider, cwd string, cfg SandboxConfig) (RemoteSandbox, *SandboxMeta, error) {
	if provider == nil {
		return nil, nil, errors.New("provider is required")
	}
	providerName := provider.Name()
	worktreeID := ComputeWorktreeID(cwd)
	project := filepath.Base(cwd)
	if project == "" || project == "." || project == "/" {
		project = "unknown"
	}

	if cfg.Agent == "" {
		cfg.Agent = AgentShell
	}
	if len(cfg.Volumes) > 0 && !provider.SupportsFeature(FeatureVolumes) {
		return nil, nil, fmt.Errorf("provider %q does not support volumes", providerName)
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	labels := map[string]string{
		"amux.worktreeId": worktreeID,
		"amux.agent":      cfg.Agent.String(),
		"amux.provider":   providerName,
		"amux.project":    project,
		"amux.createdAt":  createdAt,
	}

	userMounts, err := resolveVolumeMounts(provider.Volumes(), cfg.Volumes)
	if err != nil {
		return nil, nil, err
	}
	persistMount, err := resolvePersistentMount(provider, cfg.Volumes, cfg.PersistenceVolumeName)
	if err != nil {
		return nil, nil, err
	}
	mounts := make([]VolumeMount, 0, len(userMounts)+1)
	if persistMount != nil {
		mounts = append(mounts, *persistMount)
	}
	mounts = append(mounts, userMounts...)

	sb, err := provider.CreateSandbox(context.Background(), SandboxCreateConfig{
		Agent:             cfg.Agent,
		Snapshot:          cfg.Snapshot,
		EnvVars:           cfg.EnvVars,
		Labels:            labels,
		Volumes:           mounts,
		AutoStopMinutes:   cfg.AutoStopInterval,
		AutoDeleteMinutes: cfg.AutoDeleteInterval,
		Ephemeral:         cfg.Ephemeral,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := sb.WaitReady(context.Background(), 60*time.Second); err != nil {
		return nil, nil, err
	}
	applyEnvVars(sb, cfg.EnvVars)

	meta := &SandboxMeta{
		SandboxID:  sb.ID(),
		CreatedAt:  createdAt,
		Agent:      cfg.Agent,
		Provider:   providerName,
		WorktreeID: worktreeID,
		Project:    project,
	}
	if err := SaveSandboxMeta(cwd, providerName, *meta); err != nil {
		return nil, nil, err
	}

	return sb, meta, nil
}

type envConfigurator interface {
	setDefaultEnv(env map[string]string)
}

func applyEnvVars(handle RemoteSandbox, env map[string]string) {
	if handle == nil {
		return
	}
	if len(env) == 0 {
		return
	}
	if configurator, ok := handle.(envConfigurator); ok {
		configurator.setDefaultEnv(env)
	}
}

// ListAmuxSandboxes returns sandboxes created by amux.
func ListAmuxSandboxes(provider Provider) ([]RemoteSandbox, error) {
	if provider == nil {
		return nil, errors.New("provider is required")
	}
	sandboxes, err := provider.ListSandboxes(context.Background())
	if err != nil {
		return nil, err
	}
	filtered := make([]RemoteSandbox, 0, len(sandboxes))
	for _, sb := range sandboxes {
		labels := sb.Labels()
		if labels != nil {
			if _, ok := labels["amux.provider"]; ok {
				filtered = append(filtered, sb)
			}
		}
	}
	return filtered, nil
}

// RemoveSandbox deletes a sandbox by ID or by current worktree meta.
func RemoveSandbox(provider Provider, cwd, sandboxID string) error {
	if provider == nil {
		return errors.New("provider is required")
	}
	if sandboxID != "" {
		if err := provider.DeleteSandbox(context.Background(), sandboxID); err != nil {
			return err
		}
		return RemoveSandboxMetaByID(sandboxID)
	}
	meta, err := LoadSandboxMeta(cwd, provider.Name())
	if err != nil {
		return err
	}
	if meta == nil {
		return errors.New("no sandbox metadata found for this project")
	}
	if err := provider.DeleteSandbox(context.Background(), meta.SandboxID); err != nil {
		return err
	}
	return RemoveSandboxMeta(cwd, provider.Name())
}

// Agent identifies the CLI agents supported by AMUX sandboxes.
type Agent string

const (
	AgentClaude   Agent = "claude"
	AgentCodex    Agent = "codex"
	AgentOpenCode Agent = "opencode"
	AgentAmp      Agent = "amp"
	AgentGemini   Agent = "gemini"
	AgentDroid    Agent = "droid"
	AgentShell    Agent = "shell"
)

func (a Agent) String() string { return string(a) }

func IsValidAgent(value string) bool {
	switch value {
	case string(AgentClaude), string(AgentCodex), string(AgentOpenCode), string(AgentAmp), string(AgentGemini), string(AgentDroid), string(AgentShell):
		return true
	default:
		return false
	}
}

// AgentAutoUpdates indicates which agents handle their own updates automatically.
// Agents that auto-update don't need TTL-based version checking - we just verify
// they're installed. Agents that don't auto-update (only Codex currently) use
// TTL-based reinstalls to stay current.
var AgentAutoUpdates = map[Agent]bool{
	AgentClaude:   true,  // Native installer auto-updates on startup
	AgentOpenCode: true,  // Auto-downloads updates on startup
	AgentAmp:      true,  // Bun-based installer supports auto-update
	AgentGemini:   true,  // npm-triggered auto-update
	AgentDroid:    true,  // Bun-based auto-update
	AgentCodex:    false, // Only shows TUI notification, requires manual update
	AgentShell:    true,  // N/A - no installation needed
}

// VolumeSpec defines a named volume mount.
type VolumeSpec struct {
	Name      string
	MountPath string
}
