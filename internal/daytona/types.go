package daytona

import "time"

// DaytonaConfig defines configuration for the API client.
type DaytonaConfig struct {
	APIKey string
	APIURL string
	Target string
}

// VolumeMount defines a volume mount for a sandbox.
type VolumeMount struct {
	VolumeID  string `json:"volumeId"`
	MountPath string `json:"mountPath"`
	Subpath   string `json:"subpath,omitempty"`
}

// CreateSandboxParams defines params for sandbox creation.
type CreateSandboxParams struct {
	Name                string
	Language            string
	Snapshot            string
	EnvVars             map[string]string
	Labels              map[string]string
	AutoStopInterval    int32
	AutoDeleteInterval  int32
	AutoArchiveInterval int32
	Ephemeral           bool
	Volumes             []VolumeMount
}

// CreateOptions defines create options.
type CreateOptions struct {
	Timeout time.Duration
}

// ExecuteCommandOptions defines optional params for command execution.
type ExecuteCommandOptions struct {
	Cwd     string
	Env     map[string]string
	Timeout time.Duration
}

// ExecutionArtifacts contains parsed artifacts from execution output.
type ExecutionArtifacts struct {
	Stdout string
}

// ExecuteResponse is the result of command execution.
type ExecuteResponse struct {
	ExitCode  int32
	Result    string
	Artifacts *ExecutionArtifacts
}

// CreateSnapshotParams defines snapshot creation parameters.
type CreateSnapshotParams struct {
	Name  string
	Image any // string or *Image
}

// SnapshotCreateOptions defines create options.
type SnapshotCreateOptions struct {
	Timeout time.Duration
	OnLogs  func(string)
}

// Snapshot represents a snapshot object.
type Snapshot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	ErrorReason string `json:"errorReason"`
}

// Volume represents a volume object.
type Volume struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	ErrorReason string `json:"errorReason"`
}

// SshAccess represents SSH access data.
type SshAccess struct {
	ID        string    `json:"id"`
	SandboxID string    `json:"sandboxId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// SshAccessValidation represents SSH access validation results.
type SshAccessValidation struct {
	Valid        bool   `json:"valid"`
	SandboxID    string `json:"sandboxId"`
	RunnerDomain string `json:"runnerDomain"`
}

// PortPreview contains preview URL data for a sandbox port.
type PortPreview struct {
	URL   string
	Token string
}

// ComputerUseStatus reports desktop status.
type ComputerUseStatus struct {
	Status string `json:"status"`
}

// ComputerUseStartResponse reports start results.
type ComputerUseStartResponse struct {
	Message string                 `json:"message"`
	Status  map[string]interface{} `json:"status"`
}

// ComputerUseStopResponse reports stop results.
type ComputerUseStopResponse struct {
	Message string                 `json:"message"`
	Status  map[string]interface{} `json:"status"`
}
