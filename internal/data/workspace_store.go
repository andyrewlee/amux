package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/fsatomic"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/validation"
)

const workspaceFilename = "workspace.json"

// WorkspaceStore manages workspace persistence
type WorkspaceStore struct {
	root             string // ~/.amux/workspaces-metadata
	defaultAssistant string
	// now supplies the current time when stamping Created on freshly discovered
	// workspaces. It defaults to time.Now and is overridable in tests so
	// discovery timestamps can be asserted deterministically.
	now func() time.Time
}

// NewWorkspaceStore creates a new workspace store
func NewWorkspaceStore(root string) *WorkspaceStore {
	return &WorkspaceStore{
		root:             root,
		defaultAssistant: DefaultAssistant,
		now:              time.Now,
	}
}

// clock returns the store's current time, falling back to time.Now when the
// store was built without the constructor (e.g. a zero-value literal).
func (s *WorkspaceStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// SetDefaultAssistant updates the assistant used when applying defaults while loading metadata.
func (s *WorkspaceStore) SetDefaultAssistant(name string) {
	if s == nil {
		return
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		s.defaultAssistant = DefaultAssistant
		return
	}
	s.defaultAssistant = trimmed
}

// ResolvedDefaultAssistant returns the configured default assistant,
// falling back to DefaultAssistant if none is set.
func (s *WorkspaceStore) ResolvedDefaultAssistant() string {
	if s == nil {
		return DefaultAssistant
	}
	name := strings.TrimSpace(s.defaultAssistant)
	if name == "" {
		return DefaultAssistant
	}
	return name
}

// workspacePath returns the path to the workspace file for a workspace ID
func (s *WorkspaceStore) workspacePath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id), workspaceFilename)
}

func (s *WorkspaceStore) workspaceBackupPath(id WorkspaceID) string {
	return s.workspacePath(id) + ".bak"
}

func (s *WorkspaceStore) workspaceLockPath(id WorkspaceID) string {
	return filepath.Join(s.root, string(id)+".lock")
}

// List returns all workspace IDs stored in the store
func (s *WorkspaceStore) List() ([]WorkspaceID, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var ids []WorkspaceID
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check if workspace.json exists in this directory
		id := WorkspaceID(entry.Name())
		if s.workspaceMetadataExists(id) {
			ids = append(ids, WorkspaceID(entry.Name()))
		}
	}
	return ids, nil
}

func (s *WorkspaceStore) workspaceMetadataExists(id WorkspaceID) bool {
	if _, err := os.Stat(s.workspacePath(id)); err == nil {
		return true
	}
	if _, err := os.Stat(s.workspaceBackupPath(id)); err == nil {
		return true
	}
	return false
}

// Load loads a workspace by its ID
func (s *WorkspaceStore) Load(id WorkspaceID) (*Workspace, error) {
	return s.load(id, true)
}

func (s *WorkspaceStore) load(id WorkspaceID, applyDefaults bool) (*Workspace, error) {
	if err := validateWorkspaceID(id); err != nil {
		return nil, err
	}
	data, err := s.readWorkspaceMetadata(id)
	if err != nil {
		return nil, fmt.Errorf("read workspace %s: %w", id, err)
	}

	// Use workspaceJSON for backward-compatible loading
	var raw workspaceJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode workspace %s: %w", id, err)
	}

	ws := &Workspace{
		Name:           raw.Name,
		Branch:         raw.Branch,
		Base:           raw.Base,
		Repo:           raw.Repo,
		Root:           raw.Root,
		Created:        parseCreated(raw.Created),
		Runtime:        NormalizeRuntime(raw.Runtime),
		Assistant:      raw.Assistant,
		Scripts:        raw.Scripts,
		ScriptMode:     raw.ScriptMode,
		Env:            raw.Env,
		OpenTabs:       raw.OpenTabs,
		ActiveTabIndex: raw.ActiveTabIndex,
		Archived:       raw.Archived,
		ArchivedAt:     parseCreated(raw.ArchivedAt),
		Pinned:         raw.Pinned,
	}
	ws.storeID = id

	if applyDefaults {
		// Apply defaults for missing fields.
		s.applyWorkspaceDefaults(ws)
	}

	return ws, nil
}

func (s *WorkspaceStore) readWorkspaceMetadata(id WorkspaceID) ([]byte, error) {
	data, err := os.ReadFile(s.workspacePath(id))
	if os.IsNotExist(err) {
		backupData, backupErr := os.ReadFile(s.workspaceBackupPath(id))
		if backupErr == nil {
			return backupData, nil
		}
		if !os.IsNotExist(backupErr) {
			return nil, backupErr
		}
	}
	return data, err
}

// Save saves a workspace to the store using atomic write
func (s *WorkspaceStore) Save(ws *Workspace) error {
	if err := validateWorkspaceForSave(ws); err != nil {
		return err
	}
	id := ws.ID()
	oldID := ws.storeID
	if oldID == id {
		oldID = ""
	}
	if oldID != "" {
		if err := validateWorkspaceID(oldID); err != nil {
			logging.Warn("Skipping cleanup for invalid old workspace metadata id %q: %v", oldID, err)
			oldID = ""
		}
	}
	path := s.workspacePath(id)
	dir := filepath.Dir(path)

	lockFiles, err := s.lockWorkspaceIDs(id, oldID)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("save workspace %s: %w", id, err)
	}

	// Atomic replace (temp + fsync + rename) so a crash mid-save can never
	// leave a truncated workspace.json behind.
	if err := fsatomic.WriteJSON(path, ws); err != nil {
		return fmt.Errorf("save workspace %s: %w", id, err)
	}
	if oldID != "" {
		if err := s.deleteWorkspaceDir(oldID); err != nil {
			logging.Warn("Failed to remove old workspace metadata %s: %v", oldID, err)
		}
		// Both id and oldID flocks are held here; remove the stale oldID lock file
		// inside that critical section. Never remove id.lock — id is the live
		// workspace we just wrote.
		s.removeWorkspaceLockFile(oldID)
	}
	ws.storeID = id
	return nil
}

// Rename updates a workspace's human-facing Name only. Repo/Root/Branch and
// therefore ID() are unchanged, so no tmux session, tag, worktree, or in-memory
// map keyed on the ID is affected (Tier-1 label rename). The newName is trimmed
// and validated with the same rule as workspace creation, then the record is
// saved in place atomically — because Repo/Root are untouched, ws.ID() == id and
// Save takes its in-place branch (no flock migration, no old-record deletion).
func (s *WorkspaceStore) Rename(id WorkspaceID, newName string) error {
	newName = strings.TrimSpace(newName)
	if err := validation.ValidateWorkspaceName(newName); err != nil {
		return err
	}
	ws, err := s.Load(id)
	if err != nil {
		return fmt.Errorf("rename workspace %s: %w", id, err)
	}
	// No-op guard: renaming to the current name would only rewrite the file and
	// emit a spurious watch event.
	if ws.Name == newName {
		return nil
	}
	ws.Name = newName
	if err := s.Save(ws); err != nil {
		return fmt.Errorf("rename workspace %s: %w", id, err)
	}
	return nil
}

// saveWorkspaceLocked writes workspace metadata atomically. Unlike Save it does
// not acquire the workspace flock — the caller must already hold it (the
// discovery merge path holds the lock across its whole reload+merge+save).
func (s *WorkspaceStore) saveWorkspaceLocked(id WorkspaceID, ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace is required")
	}
	path := s.workspacePath(id)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Atomic replace (temp + fsync + rename, with backup recovery on platforms
	// that need it), matching Save. The caller already holds the workspace lock.
	// A crash mid-save can never leave a truncated workspace.json behind.
	if err := fsatomic.WriteJSON(path, ws); err != nil {
		return err
	}
	return nil
}

// Delete removes a workspace from the store
func (s *WorkspaceStore) Delete(id WorkspaceID) error {
	if err := validateWorkspaceID(id); err != nil {
		return err
	}
	lockFiles, err := s.lockWorkspaceIDs(id)
	if err != nil {
		return err
	}
	defer unlockRegistryFiles(lockFiles)
	if err := s.deleteWorkspaceDir(id); err != nil {
		return err
	}
	// Remove the leaked sibling <id>.lock while still holding the exclusive flock
	// (before the deferred unlock), so the rendezvous file does not accumulate for
	// every created-then-deleted workspace.
	s.removeWorkspaceLockFile(id)
	return nil
}

// ListByRepo returns all workspaces for a given repository path
func (s *WorkspaceStore) ListByRepo(repoPath string) ([]*Workspace, error) {
	return s.listByRepo(repoPath, false)
}

// LoadMetadataFor loads stored metadata for a workspace and merges it into the provided workspace.
// Uses the workspace's computed ID (based on Repo+Root) to find stored metadata.
// Returns (true, nil) if metadata was found and merged.
// Returns (false, nil) if no metadata file exists (safe to apply defaults).
// Returns (false, err) if metadata exists but couldn't be read (don't overwrite).
func (s *WorkspaceStore) LoadMetadataFor(ws *Workspace) (bool, error) {
	if ws == nil {
		return false, errors.New("workspace is required")
	}
	stored, _, err := s.findStoredWorkspace(ws.Repo, ws.Root)
	if err != nil {
		return false, err
	}
	if stored == nil {
		return false, nil // No metadata file, safe to apply defaults
	}

	// Merge stored metadata into workspace. Store owns metadata/UI state;
	// discovery only updates Root/Repo/Branch (and Name if stored is empty).
	//
	// Merge hierarchy: stored non-empty values win → caller's pre-set values
	// are preserved for empty stored fields → applyWorkspaceDefaults fills
	// any remaining gaps. The conditional "if stored.X != ''" guards below
	// implement the first two tiers of this hierarchy.
	if stored.Name != "" {
		ws.Name = stored.Name
	}
	ws.Created = stored.Created
	ws.Base = stored.Base
	ws.Runtime = stored.Runtime
	if stored.Assistant != "" {
		ws.Assistant = stored.Assistant
	}
	ws.Scripts = stored.Scripts
	ws.ScriptMode = stored.ScriptMode
	ws.Env = stored.Env
	ws.OpenTabs = stored.OpenTabs
	ws.ActiveTabIndex = stored.ActiveTabIndex
	ws.Archived = stored.Archived
	ws.ArchivedAt = stored.ArchivedAt
	ws.storeID = stored.storeID

	// Apply defaults if stored metadata had empty values
	s.applyWorkspaceDefaults(ws)

	return true, nil
}

func (s *WorkspaceStore) applyWorkspaceDefaults(ws *Workspace) {
	if ws.Assistant == "" {
		ws.Assistant = s.ResolvedDefaultAssistant()
	}
	if ws.ScriptMode == "" {
		ws.ScriptMode = "nonconcurrent"
	}
	if ws.Env == nil {
		ws.Env = make(map[string]string)
	}
	if ws.Runtime == "" {
		ws.Runtime = RuntimeLocalWorktree
	}
}

func (s *WorkspaceStore) findStoredWorkspace(repo, root string) (*Workspace, WorkspaceID, error) {
	canonicalID := Workspace{Repo: repo, Root: root}.ID()
	// load with applyDefaults=false so raw stored values are visible for merge
	// logic — empty fields indicate "not set" and influence precedence decisions.
	ws, err := s.load(canonicalID, false)
	if err == nil {
		return ws, canonicalID, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	targetRepo, targetRoot := canonicalLookupPath(repo), canonicalLookupPath(root)
	if targetRepo == "" || targetRoot == "" {
		return nil, "", nil
	}
	// Fallback: scan all workspaces for a resolved repo+root match (O(n), rare).
	ids, err := s.List()
	if err != nil {
		return nil, "", err
	}
	var bestWS *Workspace
	var bestID WorkspaceID
	for _, id := range ids {
		// applyDefaults=false: see comment on first load call above.
		candidate, err := s.load(id, false)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logging.Warn("Skipping unreadable workspace metadata %s during fallback lookup: %v", id, err)
			}
			continue
		}
		if canonicalLookupPath(candidate.Repo) != targetRepo || canonicalLookupPath(candidate.Root) != targetRoot {
			continue
		}
		if shouldPreferWorkspace(candidate, bestWS) {
			bestWS = candidate
			bestID = id
		}
	}
	return bestWS, bestID, nil
}

func (s *WorkspaceStore) ListByRepoIncludingArchived(repoPath string) ([]*Workspace, error) {
	return s.listByRepo(repoPath, true)
}

func (s *WorkspaceStore) listByRepo(repoPath string, includeArchived bool) ([]*Workspace, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}

	targetRepo := canonicalLookupPath(repoPath)
	var workspaces []*Workspace
	seen := make(map[string]int)
	var loadErrors int
	var targetLoadErrors int
	var unknownLoadErrors int
	var loadErrs []error
	for _, id := range ids {
		ws, err := s.Load(id)
		if err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, err)
			loadErrors++
			loadErrs = append(loadErrs, err)
			if repo, ok := s.repoHintForWorkspaceID(id); ok {
				if canonicalLookupPath(repo) == targetRepo {
					targetLoadErrors++
				}
			} else {
				unknownLoadErrors++
			}
			continue
		}
		if ws.Root == "" {
			logging.Warn("Skipping workspace %s with empty Root", id)
			continue
		}
		if !includeArchived && ws.Archived {
			continue
		}
		if canonicalLookupPath(ws.Repo) != targetRepo {
			continue
		}
		repoKey := canonicalLookupPath(ws.Repo)
		rootKey := canonicalLookupPath(ws.Root)
		key := workspaceIdentity(ws.Repo, ws.Root)
		if repoKey != "" && rootKey != "" {
			key = repoKey + "\n" + rootKey
		}
		if idx, ok := seen[key]; ok {
			if shouldPreferWorkspace(ws, workspaces[idx]) {
				workspaces[idx] = ws
			}
			continue
		}
		seen[key] = len(workspaces)
		workspaces = append(workspaces, ws)
	}

	if targetLoadErrors > 0 && len(workspaces) == 0 {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s: %w", targetLoadErrors, repoPath, errors.Join(loadErrs...))
	}
	if unknownLoadErrors > 0 && len(workspaces) == 0 {
		return nil, fmt.Errorf("failed to load %d workspace(s) with unreadable repo for %s: %w", unknownLoadErrors, repoPath, errors.Join(loadErrs...))
	}
	if loadErrors > 0 && len(workspaces) == 0 && loadErrors == len(ids) {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s: %w", loadErrors, repoPath, errors.Join(loadErrs...))
	}

	return workspaces, nil
}
