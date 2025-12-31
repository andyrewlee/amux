package data

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// Worktree represents a git worktree entry with its associated metadata
type Worktree struct {
	Name    string    `json:"name"`
	Branch  string    `json:"branch"`
	Base    string    `json:"base"`    // Base ref (e.g., origin/main)
	Repo    string    `json:"repo"`    // Primary checkout path
	Root    string    `json:"root"`    // Worktree path
	Created time.Time `json:"created"`
}

// WorktreeID is a unique identifier based on repo+root hash
type WorktreeID string

// ID returns a unique identifier for the worktree based on its repo and root paths
func (w Worktree) ID() WorktreeID {
	sig := w.Repo + w.Root
	hash := sha1.Sum([]byte(sig))
	return WorktreeID(hex.EncodeToString(hash[:8]))
}

// IsPrimaryCheckout returns true if this is the main checkout (not a worktree)
func (w Worktree) IsPrimaryCheckout() bool {
	return w.Root == w.Repo
}

// NewWorktree creates a new Worktree with the current timestamp
func NewWorktree(name, branch, base, repo, root string) *Worktree {
	return &Worktree{
		Name:    name,
		Branch:  branch,
		Base:    base,
		Repo:    repo,
		Root:    root,
		Created: time.Now(),
	}
}
