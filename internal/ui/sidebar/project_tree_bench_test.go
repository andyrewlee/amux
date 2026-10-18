package sidebar

import (
	"fmt"
	"strings"
	"testing"
)

// benchLoadedTree builds an already-loaded tree (~1200 nodes across three
// levels) so the benchmark isolates the per-keystroke filter walk.
func benchLoadedTree(b *testing.B) *ProjectTree {
	b.Helper()
	m := NewProjectTree()
	m.showHidden = true
	m.root = &projectTreeNode{
		Name:      "repo",
		Path:      "/repo",
		lowerPath: "/repo",
		IsDir:     true,
		Expanded:  true,
		Depth:     -1,
	}
	for d := 0; d < 40; d++ {
		dir := &projectTreeNode{
			Name:     fmt.Sprintf("dir-%02d", d),
			Path:     fmt.Sprintf("/repo/dir-%02d", d),
			IsDir:    true,
			Expanded: true,
			Depth:    0,
			Parent:   m.root,
		}
		dir.lowerPath = strings.ToLower(dir.Path)
		for f := 0; f < 30; f++ {
			p := fmt.Sprintf("%s/file-%02d.go", dir.Path, f)
			dir.Children = append(dir.Children, &projectTreeNode{
				Name:      fmt.Sprintf("file-%02d.go", f),
				Path:      p,
				lowerPath: strings.ToLower(p),
				Depth:     1,
				Parent:    dir,
			})
		}
		m.root.Children = append(m.root.Children, dir)
	}
	return m
}

// BenchmarkProjectTreeFilterKeystroke measures one query edit — the walk
// that used to strings.ToLower every loaded path on every keystroke.
func BenchmarkProjectTreeFilterKeystroke(b *testing.B) {
	m := benchLoadedTree(b)
	queries := []string{"file-1", "dir-2", "file-13", "dir-31", "file-1"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.filter.SetQuery(queries[i%len(queries)])
		m.rebuildFlatList()
	}
}
