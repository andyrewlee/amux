package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/logging"
	"github.com/andyrewlee/amux/internal/tmux"
)

const (
	defaultExitAfterConsecutiveCaptureMisses = 3
	defaultExitAfterMissingSessionChecks     = 3
	defaultWaitCaptureLines                  = 100
	defaultWaitPollInterval                  = 500 * time.Millisecond
)

type sessionExitDetector struct {
	captureMissesNeeded int
	missingChecksNeeded int
	captureMisses       int
	missingChecks       int
}

func newSessionExitDetector(captureMissesNeeded, missingChecksNeeded int) *sessionExitDetector {
	return &sessionExitDetector{
		captureMissesNeeded: captureMissesNeeded,
		missingChecksNeeded: missingChecksNeeded,
	}
}

func (d *sessionExitDetector) Reset() {
	d.captureMisses = 0
	d.missingChecks = 0
}

func (d *sessionExitDetector) CaptureMissIndicatesExit(sessionName string, opts tmux.Options) bool {
	d.captureMisses++
	if d.captureMisses < d.captureMissesNeeded {
		return false
	}

	state, err := tmuxSessionStateFor(sessionName, opts)
	if err != nil || state.Exists {
		// Capture can miss transiently while the tmux session is still alive,
		// and tmux state checks can also fail under load/timeouts.
		d.Reset()
		return false
	}

	d.missingChecks++
	return d.missingChecks >= d.missingChecksNeeded
}

func runAgentWaitForResponse(
	tmuxOpts tmux.Options,
	sessionName string,
	waitTimeout,
	idleThreshold time.Duration,
	preContent string,
) waitResponseResult {
	preHash := tmux.ContentHash(preContent)

	ctx, cancel := contextWithSignal()
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, waitTimeout)
	defer timeoutCancel()

	return waitForAgentResponse(ctx, waitResponseConfig{
		SessionName:   sessionName,
		CaptureLines:  defaultWaitCaptureLines,
		PollInterval:  defaultWaitPollInterval,
		IdleThreshold: idleThreshold,
		// Never let the initial-change guard expire before caller's wait timeout.
		InitialChangeTimeout: effectiveInitialChangeTimeout(waitTimeout),
	}, tmuxOpts, tmuxCapturePaneTail, preHash, preContent)
}

func effectiveInitialChangeTimeout(waitTimeout time.Duration) time.Duration {
	if waitTimeout > 0 {
		return waitTimeout
	}
	return waitResponseInitialChangeTimeout
}

func captureWaitBaselineWithRetry(sessionName string, opts tmux.Options) string {
	const (
		maxAttempts = 3
		retryDelay  = 75 * time.Millisecond
	)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, ok := tmuxCapturePaneTail(sessionName, defaultWaitCaptureLines, opts)
		if ok {
			return content
		}
		if attempt < maxAttempts {
			time.Sleep(retryDelay)
		}
	}
	logging.Warn(
		"wait baseline capture unavailable for session %s after %d attempts; proceeding with empty baseline",
		sessionName,
		maxAttempts,
	)
	return ""
}

func writeHumanWaitOutcome(w io.Writer, response *waitResponseResult) {
	if response == nil {
		return
	}
	if response.NeedsInput {
		if strings.TrimSpace(response.InputHint) != "" {
			fmt.Fprintf(w, "Agent needs input: %s\n", strings.TrimSpace(response.InputHint))
		} else {
			fmt.Fprintln(w, "Agent needs input")
		}
		return
	}
	if response.TimedOut {
		fmt.Fprintln(w, "Timed out waiting for response")
		return
	}
	if response.SessionExited {
		fmt.Fprintln(w, "Session exited while waiting")
		return
	}
	fmt.Fprintf(w, "Agent idle after %.1fs\n", response.IdleSeconds)
}
