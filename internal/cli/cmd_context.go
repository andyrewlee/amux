package cli

import "io"

// cmdCtx bundles the common parameters threaded through CLI command handlers.
type cmdCtx struct {
	w       io.Writer
	wErr    io.Writer
	gf      GlobalFlags
	version string
	cmd     string
	idemKey string
}

// errResult returns a JSON error (when --json is set) or a human-readable
// error message, and stores an idempotent response when an idempotency key
// is present. humanMessage optionally overrides the non-JSON stderr text.
func (c *cmdCtx) errResult(exitCode int, errorCode, message string, details any, humanMessage ...string) int {
	if c.gf.JSON {
		return returnJSONErrorMaybeIdempotent(
			c.w, c.wErr, c.gf, c.version, c.cmd, c.idemKey,
			exitCode, errorCode, message, details,
		)
	}
	text := message
	if len(humanMessage) > 0 && humanMessage[0] != "" {
		text = humanMessage[0]
	}
	Errorf(c.wErr, "%s", text)
	return exitCode
}

// successResult returns a JSON success envelope with idempotency support.
func (c *cmdCtx) successResult(data any) int {
	return returnJSONSuccessWithIdempotency(
		c.w, c.wErr, c.gf, c.version, c.cmd, c.idemKey, data,
	)
}

// maybeReplay checks whether a previous idempotent response can be replayed.
func (c *cmdCtx) maybeReplay() (bool, int) {
	return maybeReplayIdempotentResponse(c.w, c.wErr, c.gf, c.version, c.cmd, c.idemKey)
}
