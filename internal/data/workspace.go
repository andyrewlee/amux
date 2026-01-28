package data

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// Runtime constants for workspace execution backends
const (
	RuntimeLocal        = "local"
	RuntimeLocalDocker  = "local-docker"
	RuntimeCloudSandbox = "cloud-sandbox"
)

// NormalizeRuntime returns a normalized runtime string
func NormalizeRuntime(runtime string) string {
	switch runtime {
	case RuntimeLocal, RuntimeLocalDocker, RuntimeCloudSandbox:
		return runtime
	case "local-worktree", "local-checkout", "": // backward compatibility
		return RuntimeLocal
	default:
		return RuntimeLocal
	}
}

// Workspace represents a workspace with its associated metadata
type Workspace struct {
	Name    string    `json:"name"`
	Branch  string    `json:"branch"`
	Base    string    `json:"base"` // Base ref (e.g., origin/main)
	Repo    string    `json:"repo"` // Primary checkout path
	Root    string    `json:"root"` // Workspace path
	Created time.Time `json:"created"`
}

// WorkspaceID is a unique identifier based on repo+root hash
type WorkspaceID string

// ID returns a unique identifier for the workspace based on its repo and root paths
func (w Workspace) ID() WorkspaceID {
	sig := w.Repo + w.Root
	hash := sha1.Sum([]byte(sig))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}

// IsPrimaryCheckout returns true if this is the primary checkout
func (w Workspace) IsPrimaryCheckout() bool {
	return w.Root == w.Repo
}

// IsMainBranch returns true if this workspace is on main or master branch
func (w Workspace) IsMainBranch() bool {
	return w.Branch == "main" || w.Branch == "master"
}

// NewWorkspace creates a new Workspace with the current timestamp
func NewWorkspace(name, branch, base, repo, root string) *Workspace {
	return &Workspace{
		Name:    name,
		Branch:  branch,
		Base:    base,
		Repo:    repo,
		Root:    root,
		Created: time.Now(),
	}
}
