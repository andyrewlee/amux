package computer

import (
	"context"
	"io"
	"time"
)

// Provider defines the interface for computer providers.
// This abstraction allows amux to support multiple backends
// (Sprites, Daytona, Docker, etc.) while maintaining a consistent API.
type Provider interface {
	// Name returns the provider identifier (e.g., "daytona", "e2b", "modal")
	Name() string

	// CreateComputer creates a new computer with the given configuration
	CreateComputer(ctx context.Context, config ComputerCreateConfig) (RemoteComputer, error)

	// GetComputer retrieves an existing computer by ID
	GetComputer(ctx context.Context, id string) (RemoteComputer, error)

	// ListComputers returns all computers for this provider
	ListComputers(ctx context.Context) ([]RemoteComputer, error)

	// DeleteComputer removes a computer
	DeleteComputer(ctx context.Context, id string) error

	// Volumes returns the volume manager for persistent storage
	Volumes() VolumeManager

	// Snapshots returns the snapshot manager for pre-built images
	Snapshots() SnapshotManager

	// SupportsFeature checks if provider supports a specific feature
	SupportsFeature(feature ProviderFeature) bool
}

// ProviderFeature represents optional provider capabilities
type ProviderFeature string

const (
	// DefaultProviderName is a placeholder; user must choose provider explicitly.
	DefaultProviderName = ""
	ProviderDaytona     = "daytona"
	ProviderSprites     = "sprites"
	ProviderDocker      = "docker"
	// FeatureVolumes indicates persistent volume support
	FeatureVolumes ProviderFeature = "volumes"
	// FeatureSnapshots indicates snapshot/image support
	FeatureSnapshots ProviderFeature = "snapshots"
	// FeatureDesktop indicates remote desktop (VNC) support
	FeatureDesktop ProviderFeature = "desktop"
	// FeaturePreviewURLs indicates public URL preview support
	FeaturePreviewURLs ProviderFeature = "preview_urls"
	// FeatureSSHAccess indicates SSH access support
	FeatureSSHAccess ProviderFeature = "ssh_access"
	// FeatureExecSessions indicates exec session listing/attach support
	FeatureExecSessions ProviderFeature = "exec_sessions"
	// FeatureCheckpoints indicates checkpoint/restore support
	FeatureCheckpoints ProviderFeature = "checkpoints"
	// FeatureNetworkPolicy indicates network policy support
	FeatureNetworkPolicy ProviderFeature = "network_policy"
	// FeatureTCPProxy indicates raw TCP proxy support
	FeatureTCPProxy ProviderFeature = "tcp_proxy"
)

// ComputerCreateConfig defines configuration for creating a computer
type ComputerCreateConfig struct {
	// Name is an optional provider-specific identifier (e.g., sprite name, container name).
	Name string
	// Agent is the coding agent to run (claude, codex, etc.)
	Agent Agent
	// Snapshot is the pre-built image to use (optional)
	Snapshot string
	// EnvVars are environment variables to inject
	EnvVars map[string]string
	// Labels are metadata labels for the computer
	Labels map[string]string
	// Volumes are persistent volumes to mount
	Volumes []VolumeMount
	// AutoStopMinutes is the idle timeout before auto-stop (0 = disabled)
	AutoStopMinutes int32
	// Resources specifies CPU/memory requirements (provider-specific)
	Resources *ResourceConfig
}

// ResourceConfig specifies compute resources
type ResourceConfig struct {
	CPUCores float32
	MemoryGB float32
}

// VolumeMount defines how a volume is mounted in a computer
type VolumeMount struct {
	VolumeID  string
	MountPath string
	Subpath   string
	ReadOnly  bool
}

// RemoteComputer represents a running or stopped computer instance
type RemoteComputer interface {
	// ID returns the unique computer identifier
	ID() string

	// State returns current state (pending, started, stopped, error)
	State() ComputerState

	// Labels returns the computer metadata labels
	Labels() map[string]string

	// Start starts a stopped computer
	Start(ctx context.Context) error

	// Stop stops a running computer
	Stop(ctx context.Context) error

	// WaitReady waits until computer is ready for commands
	WaitReady(ctx context.Context, timeout time.Duration) error

	// Exec executes a command and returns the result
	Exec(ctx context.Context, cmd string, opts *ExecOptions) (*ExecResult, error)

	// ExecInteractive runs an interactive session with PTY
	ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error)

	// UploadFile uploads a file to the computer
	UploadFile(ctx context.Context, localPath, remotePath string) error

	// DownloadFile downloads a file from the computer
	DownloadFile(ctx context.Context, remotePath, localPath string) error

	// GetPreviewURL returns a public URL for a port (if supported)
	GetPreviewURL(ctx context.Context, port int) (string, error)

	// Refresh updates computer state from the provider
	Refresh(ctx context.Context) error
}

// ComputerState represents the lifecycle state of a computer
type ComputerState string

const (
	StatePending ComputerState = "pending"
	StateStarted ComputerState = "started"
	StateStopped ComputerState = "stopped"
	StateError   ComputerState = "error"
)

// ExecOptions configures command execution
type ExecOptions struct {
	Cwd     string
	Env     map[string]string
	Timeout time.Duration
	User    string
}

// ExecResult contains command execution results
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// ExecSession describes a running or historical exec session.
type ExecSession struct {
	ID           string
	Command      string
	TTY          bool
	Active       bool
	Workdir      string
	CreatedAt    time.Time
	LastActivity time.Time
}

// ExecSessionManager provides optional exec session management.
type ExecSessionManager interface {
	ListExecSessions(ctx context.Context) ([]ExecSession, error)
	AttachExecSession(ctx context.Context, id string, stdin io.Reader, stdout, stderr io.Writer, opts *ExecOptions) (int, error)
	KillExecSession(ctx context.Context, id string, signal string, timeout time.Duration) error
}

// CheckpointInfo describes a computer checkpoint.
type CheckpointInfo struct {
	ID        string
	CreatedAt time.Time
	SourceID  string
	Comment   string
}

// CheckpointEvent represents a streaming checkpoint event.
type CheckpointEvent struct {
	Type      string
	Message   string
	Timestamp time.Time
}

// CheckpointManager provides optional checkpoint/restore operations.
type CheckpointManager interface {
	CreateCheckpoint(ctx context.Context, comment string, onEvent func(CheckpointEvent)) (*CheckpointInfo, error)
	ListCheckpoints(ctx context.Context) ([]CheckpointInfo, error)
	GetCheckpoint(ctx context.Context, id string) (*CheckpointInfo, error)
	RestoreCheckpoint(ctx context.Context, id string, onEvent func(CheckpointEvent)) error
}

// NetworkPolicyRule describes a network policy rule.
type NetworkPolicyRule struct {
	Domain  string
	Action  string // allow|deny
	Include string // optional preset include
}

// NetworkPolicy describes outbound network policy rules.
type NetworkPolicy struct {
	Rules []NetworkPolicyRule
}

// NetworkPolicyManager provides optional network policy support.
type NetworkPolicyManager interface {
	GetNetworkPolicy(ctx context.Context) (*NetworkPolicy, error)
	SetNetworkPolicy(ctx context.Context, policy NetworkPolicy) error
}

// TCPProxyManager provides optional raw TCP proxy support.
type TCPProxyManager interface {
	OpenTCPProxy(ctx context.Context, host string, port int) (io.ReadWriteCloser, error)
}

// VolumeManager manages persistent volumes
type VolumeManager interface {
	// Create creates a new volume
	Create(ctx context.Context, name string) (*VolumeInfo, error)

	// Get retrieves a volume by name
	Get(ctx context.Context, name string) (*VolumeInfo, error)

	// GetOrCreate gets an existing volume or creates it
	GetOrCreate(ctx context.Context, name string) (*VolumeInfo, error)

	// Delete removes a volume
	Delete(ctx context.Context, name string) error

	// List returns all volumes
	List(ctx context.Context) ([]*VolumeInfo, error)

	// WaitReady waits for volume to be ready
	WaitReady(ctx context.Context, name string, timeout time.Duration) (*VolumeInfo, error)
}

// VolumeInfo contains volume metadata
type VolumeInfo struct {
	ID    string
	Name  string
	State string
	Size  int64
}

// SnapshotManager manages pre-built computer images
type SnapshotManager interface {
	// Create creates a new snapshot
	Create(ctx context.Context, name string, baseImage string, onLogs func(string)) (*SnapshotInfo, error)

	// Get retrieves a snapshot by name
	Get(ctx context.Context, name string) (*SnapshotInfo, error)

	// Delete removes a snapshot
	Delete(ctx context.Context, name string) error

	// List returns all snapshots
	List(ctx context.Context) ([]*SnapshotInfo, error)
}

// SnapshotInfo contains snapshot metadata
type SnapshotInfo struct {
	ID    string
	Name  string
	State string
}

// ProviderRegistry manages available computer providers
type ProviderRegistry struct {
	providers map[string]Provider
	defaultID string
}

// NewProviderRegistry creates a new provider registry
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry
func (r *ProviderRegistry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// SetDefault sets the default provider
func (r *ProviderRegistry) SetDefault(name string) {
	r.defaultID = name
}

// Get returns a provider by name
func (r *ProviderRegistry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Default returns the default provider
func (r *ProviderRegistry) Default() (Provider, bool) {
	if r.defaultID == "" {
		return nil, false
	}
	return r.Get(r.defaultID)
}

// List returns all registered provider names
func (r *ProviderRegistry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Optional provider interfaces

// DesktopStatus reports desktop/VNC availability.
type DesktopStatus struct {
	Status string
}

// DesktopAccess is an optional interface for providers that support remote desktops.
type DesktopAccess interface {
	DesktopStatus(ctx context.Context) (*DesktopStatus, error)
	StartDesktop(ctx context.Context) error
	StopDesktop(ctx context.Context) error
}

// ComputerResources is an optional interface for providers that expose resource details.
type ComputerResources interface {
	CPUCores() float32
	MemoryGB() float32
}
