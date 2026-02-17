package cli

import (
	"flag"
	"io"
)

// newFlagSet creates a flag set that never writes parse errors to stderr.
// Commands decide how to surface parse failures in human/JSON modes.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
