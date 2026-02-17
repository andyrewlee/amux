package data

import (
	"os"
	"path/filepath"
	"sort"
)

func (s *WorkspaceStore) lockWorkspaceIDs(ids ...WorkspaceID) ([]*os.File, error) {
	unique := make(map[WorkspaceID]struct{}, len(ids))
	ordered := make([]WorkspaceID, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := validateWorkspaceID(id); err != nil {
			return nil, err
		}
		if _, ok := unique[id]; ok {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}

	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i] < ordered[j]
	})

	locks := make([]*os.File, 0, len(ordered))
	for _, id := range ordered {
		lockFile, err := lockRegistryFile(s.workspaceLockPath(id), false)
		if err != nil {
			unlockRegistryFiles(locks)
			return nil, err
		}
		locks = append(locks, lockFile)
	}
	return locks, nil
}

func unlockRegistryFiles(files []*os.File) {
	for _, file := range files {
		unlockRegistryFile(file)
	}
}

func (s *WorkspaceStore) deleteWorkspaceDir(id WorkspaceID) error {
	dir := filepath.Join(s.root, string(id))
	return os.RemoveAll(dir)
}
