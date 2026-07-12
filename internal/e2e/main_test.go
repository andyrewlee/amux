package e2e

import (
	"os"
	"testing"

	"github.com/clipperhouse/displaywidth"
)

func TestMain(m *testing.M) {
	// Force deterministic glyph widths for snapshots across locales.
	displaywidth.DefaultOptions.EastAsianWidth = false

	os.Exit(m.Run())
}
