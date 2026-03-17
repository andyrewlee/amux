package cli

import (
	"fmt"
	"io"
	"strings"
)

type cmdHandler = func(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int

type subcommand struct {
	names   []string
	handler cmdHandler
}

// routeSubcommand dispatches to the matching subcommand handler.
// It handles empty args (usage error) and unknown subcommands uniformly.
func routeSubcommand(
	w, wErr io.Writer, gf GlobalFlags, args []string, version string,
	parent string,
	subs []subcommand,
) int {
	usage := buildRouterUsage(parent, subs)
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", usage, nil, version)
		} else {
			fmt.Fprintln(wErr, usage)
		}
		return ExitUsage
	}
	sub := args[0]
	subArgs := args[1:]
	for _, s := range subs {
		for _, name := range s.names {
			if sub == name {
				return s.handler(w, wErr, gf, subArgs, version)
			}
		}
	}
	msg := "Unknown " + parent + " subcommand: " + sub
	if gf.JSON {
		ReturnError(w, "unknown_command", msg, nil, version)
	} else {
		fmt.Fprintln(wErr, msg)
	}
	return ExitUsage
}

func buildRouterUsage(parent string, subs []subcommand) string {
	names := make([]string, 0, len(subs))
	for _, s := range subs {
		names = append(names, s.names[0])
	}
	return "Usage: amux " + parent + " <" + strings.Join(names, "|") + "> [flags]"
}
