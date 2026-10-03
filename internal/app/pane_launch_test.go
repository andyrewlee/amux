package app

import (
	"os"
	"testing"

	"github.com/andyrewlee/amux/internal/panelaunch"
)

// TestMain lets this test binary stand in for the amux launcher: prepared
// pane commands (including detached run sessions) invoke os.Executable,
// which under `go test` is this binary. Dispatch the private helper argv
// before testing's flag parse or any test runs — a pane spawn must never
// recurse into the test suite.
func TestMain(m *testing.M) {
	if handled, code := panelaunch.HandleInvocation(os.Args[1:]); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}
