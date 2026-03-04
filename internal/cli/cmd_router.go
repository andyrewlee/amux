package cli

import (
	"fmt"
	"io"
)

type commandHandler func(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int

type subcommandRouter struct {
	scope    string
	usage    string
	handlers map[string]commandHandler
}

func routeSubcommand(
	w, wErr io.Writer,
	gf GlobalFlags,
	args []string,
	version string,
	router subcommandRouter,
) int {
	if len(args) == 0 {
		if gf.JSON {
			ReturnError(w, "usage_error", router.usage, nil, version)
		} else {
			fmt.Fprintln(wErr, router.usage)
		}
		return ExitUsage
	}

	sub := args[0]
	handler, ok := router.handlers[sub]
	if !ok {
		message := fmt.Sprintf("Unknown %s subcommand: %s", router.scope, sub)
		if gf.JSON {
			ReturnError(w, "unknown_command", message, nil, version)
		} else {
			fmt.Fprintln(wErr, message)
		}
		return ExitUsage
	}
	return handler(w, wErr, gf, args[1:], version)
}
