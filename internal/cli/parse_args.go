package cli

import (
	"flag"
	"fmt"
	"strings"
)

// parseSinglePositionalWithFlags supports both:
//
//	command <positional> --flag value
//	command --flag value <positional>
func parseSinglePositionalWithFlags(fs *flag.FlagSet, args []string) (string, error) {
	if len(args) == 0 {
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		return "", nil
	}

	// Most command docs use positional-first syntax.
	if !strings.HasPrefix(args[0], "-") {
		if err := fs.Parse(args[1:]); err != nil {
			return "", err
		}
		if fs.NArg() > 0 {
			return "", fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
		}
		return args[0], nil
	}

	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() < 1 {
		return "", nil
	}
	if fs.NArg() > 1 {
		return "", fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args()[1:], " "))
	}
	return fs.Arg(0), nil
}
