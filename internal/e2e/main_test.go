package e2e

import (
	"os"
	"testing"

	"github.com/clipperhouse/displaywidth"
	"github.com/mattn/go-runewidth"
)

func TestMain(m *testing.M) {
	// Force deterministic glyph widths for snapshots across locales.
	runewidth.EastAsianWidth = false
	runewidth.DefaultCondition.EastAsianWidth = false
	runewidth.CreateLUT()
	displaywidth.DefaultOptions.EastAsianWidth = false

	os.Exit(m.Run())
}
