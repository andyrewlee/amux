package data

// canonicalLookupPath resolves path to an absolute, symlink-evaluated form.
// Relative paths are resolved against the store's metadata root rather than
// the process CWD, so lookups remain stable regardless of launch directory.
func (s *WorkspaceStore) canonicalLookupPath(path string) string {
	return canonicalProjectPathFromBase(path, s.root)
}
