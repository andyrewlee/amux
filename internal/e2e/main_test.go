package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/clipperhouse/displaywidth"

	"github.com/andyrewlee/amux/internal/panelaunch"
)

func TestMain(m *testing.M) {
	// Prepared pane commands invoke os.Executable — under `go test` that is
	// this binary. Dispatch the private launcher argv before width setup and
	// binary janitors so a pane spawn never recurses into the e2e suite or
	// sweeps parent fixtures.
	if handled, code := panelaunch.HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}

	// Force deterministic glyph widths for snapshots across locales. This
	// diverges e2e width math from production defaults (ambiguous-width chars
	// measure 1 cell here regardless of locale) — deliberate determinism
	// shim. internal/ui/compositor/width_conformance_test.go pins the
	// reachable-input agreement these defaults feed into.
	displaywidth.DefaultOptions.EastAsianWidth = false

	if err := cleanupStaleBuiltAmuxBinaries(os.TempDir(), time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "stale e2e binary cleanup: %v\n", err)
	}
	code := m.Run()
	if err := cleanupBuiltAmuxBinary(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e binary cleanup: %v\n", err)
	}
	if err := cleanupBuiltFakeAgent(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e fakeagent cleanup: %v\n", err)
	}
	os.Exit(code)
}
