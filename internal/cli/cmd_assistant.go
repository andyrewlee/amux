package cli

import "io"

func routeAssistant(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	return routeSubcommand(w, wErr, gf, args, version, subcommandRouter{
		scope: "assistant",
		usage: "Usage: amux assistant <step|turn|dx|present|dogfood|poll-agent|wait-for-idle|format-capture> [flags]",
		handlers: map[string]commandHandler{
			"step":           cmdAssistantStep,
			"turn":           cmdAssistantTurn,
			"dx":             cmdAssistantDX,
			"present":        cmdAssistantPresent,
			"dogfood":        cmdAssistantDogfood,
			"poll-agent":     cmdAssistantPollAgent,
			"wait-for-idle":  cmdAssistantWaitForIdle,
			"format-capture": cmdAssistantFormatCapture,
		},
	})
}
