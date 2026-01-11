package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/andyrewlee/amux/internal/daytona"
)

// SandboxConfig configures sandbox creation.
type SandboxConfig struct {
	Agent            Agent
	EnvVars          map[string]string
	Volumes          []VolumeSpec
	CredentialsMode  string
	AutoStopInterval int32
	Snapshot         string
}

func resolveVolumeMounts(client *daytona.Daytona, specs []VolumeSpec) ([]daytona.VolumeMount, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	mounts := make([]daytona.VolumeMount, 0, len(specs))
	for _, spec := range specs {
		volume, err := waitForVolumeReady(client, spec.Name)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, daytona.VolumeMount{VolumeID: volume.ID, MountPath: spec.MountPath})
	}
	return mounts, nil
}

// EnsureSandbox returns an existing sandbox or creates a new one.
func EnsureSandbox(cwd string, cfg SandboxConfig, recreate bool) (*daytona.Sandbox, *WorkspaceMeta, error) {
	client, err := GetDaytonaClient()
	if err != nil {
		return nil, nil, err
	}
	workspaceID := ComputeWorkspaceID(cwd)

	if cfg.Agent == "" {
		cfg.Agent = AgentShell
	}
	if cfg.AutoStopInterval == 0 {
		cfg.AutoStopInterval = 30
	}

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

	credentialsName := ""
	if cfg.CredentialsMode == "" {
		cfg.CredentialsMode = "sandbox"
	}
	if cfg.CredentialsMode != "none" {
		credentialsName = CredentialsVolumeName
	}

	configInputs := map[string]any{
		"volumes":          volumeEntries,
		"autoStopInterval": cfg.AutoStopInterval,
		"snapshot":         cfg.Snapshot,
		"credentialsVolume": func() any {
			if credentialsName == "" {
				return nil
			}
			return credentialsName
		}(),
	}
	configHash := ComputeConfigHash(configInputs)
	envVars := map[string]any{}
	for k, v := range cfg.EnvVars {
		envVars[k] = v
	}
	legacyConfigHash := ComputeConfigHash(map[string]any{
		"volumes":          volumeEntries,
		"autoStopInterval": cfg.AutoStopInterval,
		"snapshot":         cfg.Snapshot,
		"credentialsVolume": func() any {
			if credentialsName == "" {
				return nil
			}
			return credentialsName
		}(),
		"agent":           cfg.Agent,
		"envVars":         envVars,
		"credentialsMode": cfg.CredentialsMode,
	})

	meta, err := LoadWorkspaceMeta(cwd)
	if err != nil {
		return nil, nil, err
	}
	if meta != nil && meta.WorkspaceID == workspaceID {
		matchesCurrent := meta.ConfigHash == configHash
		matchesLegacy := meta.ConfigHash == legacyConfigHash
		if !recreate && (matchesCurrent || matchesLegacy) {
			sandbox, err := client.Get(meta.SandboxID)
			if err == nil {
				if err := sandbox.Start(60 * time.Second); err == nil {
					if !matchesCurrent && matchesLegacy {
						meta.ConfigHash = configHash
						meta.Agent = cfg.Agent
						_ = SaveWorkspaceMeta(cwd, *meta)
					} else if meta.Agent != cfg.Agent {
						meta.Agent = cfg.Agent
						_ = SaveWorkspaceMeta(cwd, *meta)
					}
					return sandbox, meta, nil
				}
			}
			fmt.Println("Existing sandbox not found, creating new one...")
		} else {
			if !recreate {
				fmt.Println("Sandbox settings changed; recreating sandbox to apply updates...")
			}
			if meta != nil {
				if oldSandbox, err := client.Get(meta.SandboxID); err == nil {
					_ = client.Delete(oldSandbox)
				}
			}
		}
	}

	labels := map[string]string{
		"amux.workspaceId":       workspaceID,
		"amux.workspacePathHash": workspaceID,
		"amux.agent":             cfg.Agent.String(),
	}

	mounts, err := resolveVolumeMounts(client, cfg.Volumes)
	if err != nil {
		return nil, nil, err
	}
	if cfg.CredentialsMode != "none" {
		credMount, err := GetCredentialsVolumeMount(client)
		if err != nil {
			return nil, nil, err
		}
		mounts = append(mounts, credMount)
	}

	params := &daytona.CreateSandboxParams{
		Language:         "typescript",
		Labels:           labels,
		EnvVars:          cfg.EnvVars,
		AutoStopInterval: cfg.AutoStopInterval,
		Volumes:          mounts,
		Snapshot:         cfg.Snapshot,
	}
	sandbox, err := client.Create(params, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := sandbox.WaitUntilStarted(60 * time.Second); err != nil {
		return nil, nil, err
	}

	meta = &WorkspaceMeta{
		WorkspaceID: workspaceID,
		SandboxID:   sandbox.ID,
		ConfigHash:  configHash,
		Agent:       cfg.Agent,
	}
	if err := SaveWorkspaceMeta(cwd, *meta); err != nil {
		return nil, nil, err
	}

	return sandbox, meta, nil
}

// ListAmuxSandboxes returns sandboxes created by AMUX.
func ListAmuxSandboxes() ([]*daytona.Sandbox, error) {
	client, err := GetDaytonaClient()
	if err != nil {
		return nil, err
	}
	sandboxes, err := client.List()
	if err != nil {
		return nil, err
	}
	filtered := make([]*daytona.Sandbox, 0, len(sandboxes))
	for _, sb := range sandboxes {
		if sb.Labels != nil {
			if _, ok := sb.Labels["amux.workspaceId"]; ok {
				filtered = append(filtered, sb)
			}
		}
	}
	return filtered, nil
}

// RemoveSandbox deletes a sandbox by ID or by current workspace meta.
func RemoveSandbox(cwd string, sandboxID string) error {
	client, err := GetDaytonaClient()
	if err != nil {
		return err
	}
	if sandboxID != "" {
		sandbox, err := client.Get(sandboxID)
		if err != nil {
			return err
		}
		return client.Delete(sandbox)
	}
	meta, err := LoadWorkspaceMeta(cwd)
	if err != nil {
		return err
	}
	if meta == nil {
		return fmt.Errorf("no workspace sandbox metadata found")
	}
	sandbox, err := client.Get(meta.SandboxID)
	if err != nil {
		return err
	}
	if err := client.Delete(sandbox); err != nil {
		return err
	}
	metaPath := filepath.Join(cwd, workspaceMetaPath)
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
