package data

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Runtime constants for workspace execution backends.
//
// RESERVED / forward-compat: no execution backend selection exists — every
// workspace runs as a local worktree and nothing branches on this value.
// The field and constants are kept only so persisted records carrying
// "runtime" decode stably and legacy values normalize on load.
const (
	RuntimeLocalWorktree = "local-worktree"
	RuntimeLocalCheckout = "local-checkout"
	RuntimeLocalDocker   = "local-docker"
	RuntimeCloudSandbox  = "cloud-sandbox"
	DefaultAssistant     = "claude"
)

// normalizeRuntime normalizes a persisted runtime value on load. It exists
// solely for schema/legacy-value compatibility — it is not an execution
// backend selector, and callers should not grow any. Unknown or empty
// values coerce to RuntimeLocalWorktree (the only real runtime).
func normalizeRuntime(runtime string) string {
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
	// OnDone is a fire-once hook run when an agent session in this workspace
	// crosses the working→done edge (the same transition that rings the
	// notify_on_done bell). Runs in the workspace root with AMUX_SESSION set.
	OnDone string `json:"on-done,omitempty"`
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
	// Runtime is reserved/forward-compat — always RuntimeLocalWorktree in
	// practice; see the constants block above. Nothing reads it to select a
	// backend.
	Runtime string `json:"runtime"`

	// Agent config
	Assistant string `json:"assistant"` // Assistant profile ID (e.g. claude, codex, antigravity)

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
	// Shelved distinguishes an intentional archive ("shelve": worktree removed,
	// branch + metadata kept for restore) from the accidental kind (worktree
	// vanished under us). Only accidental archives are GC'd after the retention
	// window; a shelf persists until the user restores or purges it.
	Shelved bool `json:"shelved,omitempty"`

	// Version is the on-disk schema stamp the store's save path writes.
	// Records saved before versioning carry no key (= v0); the read path
	// stays field-compatible either way.
	Version int `json:"version,omitempty"`
}

// WorkspaceID is a unique identifier based on repo+root hash
type WorkspaceID string

// ID returns the workspace's stable identifier. Once the record has been
// persisted, the store key (minted at first save) is the identity — it does
// not drift when NormalizePath's symlink resolution flips with worktree
// existence. Unsaved workspaces fall back to the path-derived hash.
// Decision: a persisted stable ID beats recomputing the path hash
// because every durable consumer — session tags, session names, agent
// buckets, lifecycle marks — keys on whatever ID() returned at stamp time;
// letting it move invalidates all of them at once. The computed form remains
// available via ComputedID for matching artifacts stamped under a legacy
// drifted identity.
func (w Workspace) ID() WorkspaceID {
	if w.storeID != "" {
		return w.storeID
	}
	return w.ComputedID()
}

// ComputedID returns the identifier derived from the workspace's current
// repo+root paths, ignoring any persisted store key. Unlike ID it always
// reflects the live path identity — which means it drifts when path
// existence changes NormalizePath's resolution. Use it only for matching
// pre-existing artifacts (tmux session names/tags) that may carry a form
// minted before the stable-ID machinery existed.
func (w Workspace) ComputedID() WorkspaceID {
	return workspaceIDFromIdentity(workspaceIdentity(w.Repo, w.Root))
}

// MetadataID returns the store key this workspace was loaded from. Legacy
// records can live under an older key even though ID now computes a normalized
// identity. New or unsaved workspaces fall back to their canonical ID.
func (w Workspace) MetadataID() WorkspaceID {
	if w.storeID != "" {
		return w.storeID
	}
	return w.ID()
}

// WorkspaceIdentitySet returns the deduped identity forms under which a
// workspace's durable artifacts (tmux session names, tags, marks) may have
// been stamped: the persisted store key, the live ID, and the computed
// path-hash. Ordering is stable-forms-first — MetadataID/ID precede
// ComputedID, which exists to match artifacts minted under a legacy drifted
// identity (pre-042 workspaces, post-stamp path drift). This is THE set for
// artifact matching: hand-rolled subsets like {ID, MetadataID} silently drop
// ComputedID and make such artifacts unreachable.
//
// Narrower contract note: callers that deliberately want a subset (e.g. a
// two-tier lookup that defers the EvalSymlinks-costing ComputedID walk)
// should keep their explicit forms rather than truncating this set.
func WorkspaceIdentitySet(w *Workspace) []WorkspaceID {
	if w == nil {
		return nil
	}
	ids := make([]WorkspaceID, 0, 3)
	seen := make(map[WorkspaceID]struct{}, 3)
	for _, id := range []WorkspaceID{w.MetadataID(), w.ID(), w.ComputedID()} {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// WorkspaceIdentityStrings is the string form of WorkspaceIdentitySet, for
// callers comparing against string-typed artifact tags.
func WorkspaceIdentityStrings(w *Workspace) []string {
	ids := WorkspaceIdentitySet(w)
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
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
	hash := sha256.Sum256([]byte(identity))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}

// Clone returns a deep copy of the workspace: value fields copy verbatim
// (storeID included — a clone is the same record), and the reference fields
// Env and OpenTabs are rebuilt so the clone shares no mutable state with the
// source. Async Cmd closures that capture a workspace must hold a Clone —
// the live model may mutate Env/OpenTabs while the closure is in flight.
//
// When a new reference-typed field is added to Workspace it must be copied
// here; the field-coverage test in workspace_clone_test.go fails otherwise.
func (w Workspace) Clone() Workspace {
	out := w
	if w.Env != nil {
		out.Env = make(map[string]string, len(w.Env))
		for key, value := range w.Env {
			out.Env[key] = value
		}
	}
	if w.OpenTabs != nil {
		out.OpenTabs = make([]TabInfo, len(w.OpenTabs))
		copy(out.OpenTabs, w.OpenTabs)
	}
	return out
}
