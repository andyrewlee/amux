package cli

import (
	"fmt"
	"io"
)

func returnUsageError(w, wErr io.Writer, gf GlobalFlags, usage, version string, parseErr error) int {
	if gf.JSON {
		message := usage
		details := any(nil)
		if parseErr != nil {
			message = parseErr.Error()
			details = map[string]any{"usage": usage}
		}
		ReturnError(w, "usage_error", message, details, version)
		return ExitUsage
	}

	if parseErr != nil {
		Errorf(wErr, "%v", parseErr)
	}
	fmt.Fprintln(wErr, usage)
	return ExitUsage
}
