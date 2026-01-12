package computer

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// ComputerConfig configures computer creation.
type ComputerConfig struct {
	Agent            Agent
	EnvVars          map[string]string
	Volumes          []VolumeSpec
	CredentialsMode  string
	AutoStopInterval int32
	Snapshot         string
}

func resolveVolumeMounts(manager VolumeManager, specs []VolumeSpec) ([]VolumeMount, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	if manager == nil {
		return nil, fmt.Errorf("volume manager is not available")
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

// EnsureComputer returns an existing computer or creates a new one.
// There is ONE shared computer per provider, used by all projects.
// Workspaces are isolated by worktreeID within the shared computer.
func EnsureComputer(provider Provider, cwd string, cfg ComputerConfig, recreate bool) (RemoteComputer, *ComputerMeta, error) {
	if provider == nil {
		return nil, nil, fmt.Errorf("provider is required")
	}
	providerName := provider.Name()
	worktreeID := ComputeWorktreeID(cwd)
	computerName := "amux" // One shared computer for all projects

	if cfg.Agent == "" {
		cfg.Agent = AgentShell
	}
	if cfg.AutoStopInterval == 0 {
		cfg.AutoStopInterval = 30
	}
	if len(cfg.Volumes) > 0 && !provider.SupportsFeature(FeatureVolumes) {
		return nil, nil, fmt.Errorf("provider %q does not support volumes", providerName)
	}

	// Simplified config hash - no credentials volume, no projectId
	volumeSpecs := append([]VolumeSpec{}, cfg.Volumes...)
	sort.Slice(volumeSpecs, func(i, j int) bool {
		return fmt.Sprintf("%s:%s", volumeSpecs[i].Name, volumeSpecs[i].MountPath) < fmt.Sprintf("%s:%s", volumeSpecs[j].Name, volumeSpecs[j].MountPath)
	})
	volumeEntries := make([]any, 0, len(volumeSpecs))
	for _, spec := range volumeSpecs {
		volumeEntries = append(volumeEntries, map[string]any{
			"name":      spec.Name,
			"mountPath": spec.MountPath,
		})
	}

	configInputs := map[string]any{
		"volumes":          volumeEntries,
		"autoStopInterval": cfg.AutoStopInterval,
		"snapshot":         cfg.Snapshot,
	}
	configHash := ComputeConfigHash(configInputs)

	// Try to reuse existing shared computer
	meta, err := LoadComputerMeta(cwd, providerName)
	if err != nil {
		return nil, nil, err
	}
	if meta != nil {
		matchesConfig := meta.ConfigHash == configHash
		if !recreate && matchesConfig {
			computerHandle, err := provider.GetComputer(context.Background(), meta.ComputerID)
			if err == nil {
				_ = computerHandle.Refresh(context.Background())
				if computerHandle.State() != StateStarted {
					if err := computerHandle.Start(context.Background()); err == nil {
						_ = computerHandle.WaitReady(context.Background(), 60*time.Second)
					}
				}
				if computerHandle.State() == StateStarted || computerHandle.State() == StatePending {
					if meta.Agent != cfg.Agent {
						meta.Agent = cfg.Agent
						_ = SaveComputerMeta(cwd, providerName, *meta)
					}
					applyEnvVars(computerHandle, cfg.EnvVars)
					return computerHandle, meta, nil
				}
			}
			fmt.Println("Existing computer not found, creating new one...")
		} else if !recreate {
			fmt.Println("Computer settings changed; recreating computer to apply updates...")
		}
		// Delete existing computer before creating new one
		if meta != nil {
			_ = provider.DeleteComputer(context.Background(), meta.ComputerID)
		}
	}

	// Labels for the shared computer (worktreeId is informational only)
	labels := map[string]string{
		"amux.worktreeId": worktreeID,
		"amux.agent":      cfg.Agent.String(),
		"amux.provider":   providerName,
	}

	// Only mount user-specified volumes, not credentials volume
	mounts, err := resolveVolumeMounts(provider.Volumes(), cfg.Volumes)
	if err != nil {
		return nil, nil, err
	}

	computerHandle, err := provider.CreateComputer(context.Background(), ComputerCreateConfig{
		Name:            computerName,
		Agent:           cfg.Agent,
		Snapshot:        cfg.Snapshot,
		EnvVars:         cfg.EnvVars,
		Labels:          labels,
		Volumes:         mounts,
		AutoStopMinutes: cfg.AutoStopInterval,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := computerHandle.WaitReady(context.Background(), 60*time.Second); err != nil {
		return nil, nil, err
	}
	applyEnvVars(computerHandle, cfg.EnvVars)

	meta = &ComputerMeta{
		ComputerID: computerHandle.ID(),
		ConfigHash: configHash,
		Agent:      cfg.Agent,
	}
	if err := SaveComputerMeta(cwd, providerName, *meta); err != nil {
		return nil, nil, err
	}

	return computerHandle, meta, nil
}

type envConfigurator interface {
	setDefaultEnv(env map[string]string)
}

func applyEnvVars(handle RemoteComputer, env map[string]string) {
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

// ListAmuxComputers returns computers created by AMUX.
func ListAmuxComputers(provider Provider) ([]RemoteComputer, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	sandboxes, err := provider.ListComputers(context.Background())
	if err != nil {
		return nil, err
	}
	filtered := make([]RemoteComputer, 0, len(sandboxes))
	for _, sb := range sandboxes {
		labels := sb.Labels()
		if labels != nil {
			// Check for amux.provider label (new) or amux.projectId label (legacy)
			if _, ok := labels["amux.provider"]; ok {
				filtered = append(filtered, sb)
			} else if _, ok := labels["amux.projectId"]; ok {
				filtered = append(filtered, sb)
			}
		}
	}
	return filtered, nil
}

// RemoveComputer deletes a computer by ID or by current project meta.
func RemoveComputer(provider Provider, cwd string, computerID string) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	providerName := provider.Name()
	if computerID != "" {
		return provider.DeleteComputer(context.Background(), computerID)
	}
	meta, err := LoadComputerMeta(cwd, providerName)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("no project computer metadata found")
	}
	if err := provider.DeleteComputer(context.Background(), meta.ComputerID); err != nil {
		return err
	}
	return RemoveComputerMeta(cwd, providerName)
}

// Agent identifies the CLI agents supported by AMUX computers.
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

// VolumeSpec defines a named volume mount.
type VolumeSpec struct {
	Name      string
	MountPath string
}
