package data

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// Runtime constants for workspace execution backends
const (
	RuntimeLocalWorktree = "local-worktree"
	RuntimeLocalCheckout = "local-checkout"
	RuntimeLocalDocker   = "local-docker"
	RuntimeCloudSandbox  = "cloud-sandbox"
	DefaultAssistant     = "claude"
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
	Assistant   string `json:"assistant"`
	Name        string `json:"name"`
	SessionName string `json:"session_name,omitempty"`
	Status      string `json:"status,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
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
	Branch           string `json:"branch"`
	Base             string `json:"base"`                  // Base ref (e.g., origin/main)
	BaseCommit       string `json:"base_commit,omitempty"` // Exact base commit used for restacks/reparents
	PendingForcePush bool   `json:"pending_force_push,omitempty"`
	Repo             string `json:"repo"` // Primary checkout path
	Root             string `json:"root"` // Workspace path
	// Stack metadata persists parent/child workspace relationships.
	ParentWorkspaceID    WorkspaceID `json:"parent_workspace_id,omitempty"`
	ParentBranch         string      `json:"parent_branch,omitempty"`
	StackRootWorkspaceID WorkspaceID `json:"stack_root_workspace_id,omitempty"`
	StackDepth           int         `json:"stack_depth,omitempty"`

	// Execution
	Runtime string `json:"runtime"` // local-worktree, local-checkout, cloud-sandbox

	// Agent config
	Assistant string `json:"assistant"` // Assistant profile ID (e.g. claude, codex, openclaw)

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

var workspaceIDRegex = regexp.MustCompile("^[a-f0-9]{16}$")

// ID returns a unique identifier for the workspace based on its repo and root paths
func (w Workspace) ID() WorkspaceID {
	return workspaceIDFromIdentity(workspaceIdentity(w.Repo, w.Root))
}

// IsValidWorkspaceID reports whether the id matches the canonical workspace ID format.
func IsValidWorkspaceID(id WorkspaceID) bool {
	return workspaceIDRegex.MatchString(string(id))
}

// IsPrimaryCheckout returns true if this is the primary checkout
func (w Workspace) IsPrimaryCheckout() bool {
	repo := NormalizePath(w.Repo)
	root := NormalizePath(w.Root)
	if repo == "" || root == "" {
		return false
	}
	return root == repo
}

// IsMainBranch returns true if this workspace is on main or master branch
func (w Workspace) IsMainBranch() bool {
	return w.Branch == "main" || w.Branch == "master"
}

// HasStackParent reports whether this workspace is attached to a parent workspace.
func (w Workspace) HasStackParent() bool {
	return strings.TrimSpace(string(w.ParentWorkspaceID)) != ""
}

// EffectiveStackRootWorkspaceID returns the stored stack root when available,
// otherwise it derives one from the parent or the workspace itself.
func (w Workspace) EffectiveStackRootWorkspaceID() WorkspaceID {
	if strings.TrimSpace(string(w.StackRootWorkspaceID)) != "" {
		return w.StackRootWorkspaceID
	}
	if strings.TrimSpace(string(w.ParentWorkspaceID)) != "" {
		return w.ParentWorkspaceID
	}
	return w.ID()
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
		Assistant:  DefaultAssistant,
		ScriptMode: "nonconcurrent",
		Env:        make(map[string]string),
	}
}

func workspaceIDFromIdentity(identity string) WorkspaceID {
	hash := sha1.Sum([]byte(identity))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}
