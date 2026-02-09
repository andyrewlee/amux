package data

// canonicalLookupPath resolves path to an absolute, symlink-evaluated form.
// Relative paths are resolved against CWD via filepath.Abs, matching the
// behavior of legacy metadata that stored paths relative to the working
// directory at creation time.
func (s *WorkspaceStore) canonicalLookupPath(path string) string {
	return canonicalProjectPathFromBase(path, "")
}
