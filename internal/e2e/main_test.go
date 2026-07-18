package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"
)

func TestMain(m *testing.M) {
	// Force deterministic glyph widths for snapshots across locales.
	displaywidth.DefaultOptions.EastAsianWidth = false

	if err := cleanupStaleBuiltAmuxBinaries(os.TempDir(), time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "stale e2e binary cleanup: %v\n", err)
	}
	code := m.Run()
	if err := cleanupBuiltAmuxBinary(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e binary cleanup: %v\n", err)
	}
	os.Exit(code)
}
