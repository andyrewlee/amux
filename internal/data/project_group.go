package data

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

// ProjectGroup bundles multiple repos into a coordinated group.
type ProjectGroup struct {
	Name       string           `json:"name"`
	Repos      []GroupRepo      `json:"repos"`   // ordered; first = primary
	Profile    string           `json:"profile"`
	Workspaces []GroupWorkspace `json:"-"` // loaded from store
}

// GroupRepo identifies a git repository within a group.
type GroupRepo struct {
	Path string `json:"path"` // absolute path to git repo
	Name string `json:"name"` // basename of path
}

// GroupWorkspace represents a multi-root workspace spanning multiple repos.
type GroupWorkspace struct {
	Name         string            `json:"name"`
	Created      time.Time         `json:"created"`
	GroupName    string            `json:"group_name"`
	Primary      Workspace         `json:"primary"`              // cwd for agent
	Secondary    []Workspace       `json:"secondary"`            // additional repos
	Archived     bool              `json:"archived"`
	ArchivedAt   time.Time         `json:"archived_at,omitempty"`
	AllowEdits      bool              `json:"allow_edits,omitempty"`
	Isolated        bool              `json:"isolated,omitempty"`
	SkipPermissions bool              `json:"skip_permissions,omitempty"`
	LoadClaudeMD bool              `json:"load_claude_md,omitempty"`
	Assistant    string            `json:"assistant"`
	Profile      string            `json:"-"`
	OpenTabs     []TabInfo         `json:"open_tabs,omitempty"`
	ActiveTabIndex int             `json:"active_tab_index"`
	Scripts      ScriptsConfig     `json:"scripts"`
	ScriptMode   string            `json:"script_mode"`
	Env          map[string]string `json:"env"`
}

// ID returns a unique identifier for the group workspace.
func (gw GroupWorkspace) ID() WorkspaceID {
	identity := "group:" + gw.GroupName + "\n" + NormalizePath(gw.Primary.Repo) + "\n" + NormalizePath(gw.Primary.Root)
	hash := sha1.Sum([]byte(identity))
	return WorkspaceID(hex.EncodeToString(hash[:8]))
}

// AllRoots returns all individual repo worktree root paths (from Secondary).
func (gw *GroupWorkspace) AllRoots() []string {
	roots := make([]string, len(gw.Secondary))
	for i, ws := range gw.Secondary {
		roots[i] = ws.Root
	}
	return roots
}

// SecondaryRoots returns non-primary worktree root paths.
// Deprecated: with group root as Primary.Root, all repos are in Secondary.
// Use AllRoots() instead.
func (gw *GroupWorkspace) SecondaryRoots() []string {
	return gw.AllRoots()
}

// FindWorkspaceByName finds a group workspace by name.
func (g *ProjectGroup) FindWorkspaceByName(name string) *GroupWorkspace {
	for i := range g.Workspaces {
		if g.Workspaces[i].Name == name {
			return &g.Workspaces[i]
		}
	}
	return nil
}

// RepoPaths returns all repo paths in the group.
func (g *ProjectGroup) RepoPaths() []string {
	paths := make([]string, len(g.Repos))
	for i, r := range g.Repos {
		paths[i] = r.Path
	}
	return paths
}
