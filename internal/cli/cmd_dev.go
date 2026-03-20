package cli

import "io"

func routeDev(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, subcommandRouter{
		scope: "dev",
		usage: "Usage: amux dev <perf-compare|openclaw-sync> [flags]",
		handlers: map[string]commandHandler{
			"perf-compare":  cmdDevPerfCompare,
			"openclaw-sync": cmdDevOpenClawSync,
		},
	})
}
