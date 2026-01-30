package data

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// Runtime constants for workspace execution backends
const (
	RuntimeLocalWorktree = "local-worktree"
	RuntimeLocalCheckout = "local-checkout"
	RuntimeLocalDocker   = "local-docker"
	RuntimeCloudSandbox  = "cloud-sandbox"
)

// NormalizeRuntime returns a normalized runtime string
func NormalizeRuntime(runtime string) string {
	switch runtime {
	case RuntimeLocalWorktree, RuntimeLocalCheckout, RuntimeLocalDocker, RuntimeCloudSandbox:
		return runtime
	case "sandbox":
		return RuntimeCloudSandbox
	case "local", "":
		return RuntimeLocalWorktree
	default:
		return RuntimeLocalWorktree
	}
}

// TabInfo stores information about an open tab
type TabInfo struct {
	Assistant string `json:"assistant"`
	Name      string `json:"name"`
}

// ScriptsConfig holds the setup/run/archive script commands
type ScriptsConfig struct {
	Setup   string `json:"setup"`
	Run     string `json:"run"`
	Archive string `json:"archive"`
}

// Workspace represents a workspace with its associated metadata
type Workspace struct {
	// Identity
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	storeID WorkspaceID

	// Git info
	Branch string `json:"branch"`
	Base   string `json:"base"` // Base ref (e.g., origin/main)
	Repo   string `json:"repo"` // Primary checkout path
	Root   string `json:"root"` // Workspace path

	// Execution
	Runtime string `json:"runtime"` // local-worktree, local-checkout, cloud-sandbox

	// Agent config
	Assistant string `json:"assistant"` // claude, codex, gemini

	// Scripts
	Scripts    ScriptsConfig `json:"scripts"`
	ScriptMode string        `json:"script_mode"`

	// Environment
	Env map[string]string `json:"env"`

	// UI state
	OpenTabs       []TabInfo `json:"open_tabs,omitempty"`
	ActiveTabIndex int       `json:"active_tab_index"`

	// Lifecycle
	Archived   bool      `json:"archived"`
	ArchivedAt time.Time `json:"archived_at,omitempty"`
}

// WorkspaceID is a unique identifier based on repo+root hash
type WorkspaceID string

// ID returns a unique identifier for the workspace based on its repo and root paths
func (w Workspace) ID() WorkspaceID {
	return workspaceIDFromIdentity(workspaceIdentity(w.Repo, w.Root))
}

// IsPrimaryCheckout returns true if this is the primary checkout
func (w Workspace) IsPrimaryCheckout() bool {
	return w.Root == w.Repo
}

// IsMainBranch returns true if this workspace is on main or master branch
func (w Workspace) IsMainBranch() bool {
	return w.Branch == "main" || w.Branch == "master"
}

// NewWorkspace creates a new Workspace with the current timestamp and defaults
func NewWorkspace(name, branch, base, repo, root string) *Workspace {
	return &Workspace{
		Name:       name,
		Branch:     branch,
		Base:       base,
		Repo:       repo,
		Root:       root,
		Created:    time.Now(),
		Runtime:    RuntimeLocalWorktree,
		Assistant:  "claude",
		ScriptMode: "nonconcurrent",
		Env:        make(map[string]string),
	}
}

func legacyWorkspaceID(repo, root string) WorkspaceID {
	return workspaceIDFromIdentity(legacyWorkspaceIdentity(repo, root))
}

func workspaceIDFromIdentity(identity string) WorkspaceID {
	hash := sha1.Sum([]byte(identity))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}
