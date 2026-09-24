package data

import (
	"errors"
	"fmt"

	"github.com/andyrewlee/amux/internal/logging"
)

// WorkspaceRecordSet is one List+Load round over the store, kept so a caller
// iterating several repos in one pass (e.g. LoadProjects) pays the metadata
// reads once instead of once per repo. Records preserve s.List() order so
// ByRepo's dedup sees entries in the same order listByRepo did.
type WorkspaceRecordSet struct {
	ids     []WorkspaceID
	records map[WorkspaceID]workspaceRecordEntry
}

type workspaceRecordEntry struct {
	ws       *Workspace
	err      error
	repoHint string
	hasHint  bool
}

// ListAll loads every workspace record in one pass. Per-record load errors
// are captured on the entry (with the raw-JSON repo hint listByRepo uses for
// error attribution) rather than failing the whole set, mirroring listByRepo.
func (s *WorkspaceStore) ListAll() (*WorkspaceRecordSet, error) {
	ids, err := s.List()
	if err != nil {
		return nil, err
	}
	set := &WorkspaceRecordSet{
		ids:     ids,
		records: make(map[WorkspaceID]workspaceRecordEntry, len(ids)),
	}
	for _, id := range ids {
		ws, loadErr := s.Load(id)
		entry := workspaceRecordEntry{ws: ws}
		if loadErr != nil {
			entry.err = loadErr
			if repo, ok := s.repoHintForWorkspaceID(id); ok {
				entry.repoHint = repo
				entry.hasHint = true
			}
		}
		set.records[id] = entry
	}
	return set, nil
}

// ByRepo filters the snapshot for one repo with semantics identical to
// listByRepo: archived filtering, empty-root skips, canonical repo match,
// first-seen dedup via shouldPreferWorkspace, and the same error aggregation.
func (r *WorkspaceRecordSet) ByRepo(repoPath string, includeArchived bool) ([]*Workspace, error) {
	targetRepo := canonicalLookupPath(repoPath)
	var workspaces []*Workspace
	seen := make(map[string]int)
	var loadErrors int
	var targetLoadErrors int
	var unknownLoadErrors int
	var loadErrs []error
	for _, id := range r.ids {
		entry := r.records[id]
		if entry.err != nil {
			logging.Warn("Failed to load workspace %s: %v", id, entry.err)
			loadErrors++
			loadErrs = append(loadErrs, entry.err)
			if entry.hasHint {
				if canonicalLookupPath(entry.repoHint) == targetRepo {
					targetLoadErrors++
				}
			} else {
				unknownLoadErrors++
			}
			continue
		}
		ws := entry.ws
		if ws == nil {
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
	if loadErrors > 0 && len(workspaces) == 0 && loadErrors == len(r.ids) {
		return nil, fmt.Errorf("failed to load %d workspace(s) for repo %s: %w", loadErrors, repoPath, errors.Join(loadErrs...))
	}

	return workspaces, nil
}
