package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureTableHasEveryPackage guards against ARCHITECTURE.md's
// hand-maintained Packages table silently drifting from the real tree: it walks
// `go list ./internal/... ./cmd/...` and fails if any package directory is not
// mentioned in ARCHITECTURE.md. Add a row when this fails (don't just delete
// the package from your mental model).
func TestArchitectureTableHasEveryPackage(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	doc, err := os.ReadFile(filepath.Join(root, "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	// Restrict the match to the Packages *table* so a package named only in the
	// prose or the dependency diagram cannot satisfy the guard: deleting a real
	// table row must fail even if the name still appears elsewhere in the doc. We
	// keep only table-row lines (those beginning with "| `") from the "## Packages"
	// section onward, and match the backtick-wrapped package path against them.
	full := string(doc)
	if i := strings.Index(full, "## Packages"); i >= 0 {
		full = full[i:]
	}
	var rows []string
	for _, line := range strings.Split(full, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| `") {
			rows = append(rows, line)
		}
	}
	table := strings.Join(rows, "\n")

	cmd := exec.Command("go", "list", "./internal/...", "./cmd/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}

	for _, pkg := range strings.Fields(string(out)) {
		// Trim the module prefix so we compare on the repo-relative dir
		// (e.g. internal/ui/common) the table actually lists.
		rel := pkg
		if i := strings.Index(pkg, "/internal/"); i >= 0 {
			rel = pkg[i+1:]
		} else if i := strings.Index(pkg, "/cmd/"); i >= 0 {
			rel = pkg[i+1:]
		}
		// A package counts as documented if its own dir or its parent dir
		// (but not a bare "internal"/"cmd" segment) is in the table, so
		// test-support subpackages such as internal/e2e/fakeagent roll up
		// under their parent's row instead of needing their own.
		documented := strings.Contains(table, "`"+rel+"`")
		if parent := filepath.Dir(rel); !documented && strings.Contains(parent, "/") {
			documented = strings.Contains(table, "`"+parent+"`")
		}
		if !documented {
			t.Errorf("ARCHITECTURE.md Packages table omits %q (add a row; the table is hand-maintained)", rel)
		}
	}
}
