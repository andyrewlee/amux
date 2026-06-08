package sidebar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoTParallelInSeamPackage enforces the constraint documented on this
// package's test seams (mutable package-global vars saved/restored via defer):
// a test using t.Parallel would race those seams and pass or fail spuriously.
// The constraint was only a comment; this guard turns it into a build break if
// any test file introduces t.Parallel.
func TestNoTParallelInSeamPackage(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob test files: %v", err)
	}
	const self = "no_parallel_guard_test.go"
	for _, f := range files {
		if f == self {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(src), "t.Parallel()") {
			t.Errorf("%s calls t.Parallel(), but this package's test seams are mutable package globals restored via defer; parallel tests race them. Remove t.Parallel() or inject the seam per-test instead.", f)
		}
	}
}
