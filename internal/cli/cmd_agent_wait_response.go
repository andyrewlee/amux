package cli

import (
	"context"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

const (
	waitResponseExitAfterConsecutiveCaptureMisses = 3
	waitResponseExitAfterMissingSessionChecks     = 3
)

// waitResponseInitialChangeTimeout bounds how long --wait blocks when pane
// content never changes after a send/run prompt. This avoids very long hangs
// when the prompt is dropped or the agent never starts responding.
var waitResponseInitialChangeTimeout = 90 * time.Second

// waitResponseConfig holds parameters for waiting on an agent response.
type waitResponseConfig struct {
	SessionName   string
	CaptureLines  int
	PollInterval  time.Duration
	IdleThreshold time.Duration
}

// waitResponseResult holds the outcome of waiting for an agent response.
type waitResponseResult struct {
	Status        string  `json:"status"`
	Content       string  `json:"content"`
	Delta         string  `json:"delta,omitempty"`
	LatestLine    string  `json:"latest_line,omitempty"`
	Summary       string  `json:"summary,omitempty"`
	NeedsInput    bool    `json:"needs_input"`
	InputHint     string  `json:"input_hint,omitempty"`
	IdleSeconds   float64 `json:"idle_seconds"`
	TimedOut      bool    `json:"timed_out"`
	SessionExited bool    `json:"session_exited"`
	Changed       bool    `json:"changed"`
}

// waitForAgentResponse polls the tmux pane until the agent produces new output
// and then goes idle (unchanged for idleThreshold). preHash is a snapshot of
// the pane content taken right after sending text — the function waits until
// content differs from preHash at least once before considering idle.
// preContent is the raw text from that same snapshot, used as fallback content
// if the session exits before any new output is captured.
func waitForAgentResponse(
	ctx context.Context,
	cfg waitResponseConfig,
	opts tmux.Options,
	capture captureFn,
	preHash [16]byte,
	preContent string,
) waitResponseResult {
	var lastHash [16]byte
	var lastContent string
	var lastNonEmptyContent string
	var lastDifferentFromPre string
	contentChanged := false
	var lastChangeTime time.Time
	captureMisses := 0
	missingSessionChecks := 0
	waitStartedAt := time.Now()
	preStableHash := waitResponseContentHash(preContent)

	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return buildTimedOutWaitResponse(
				cfg,
				opts,
				capture,
				preContent,
				lastContent,
				lastNonEmptyContent,
				lastDifferentFromPre,
				contentChanged,
			)
		case <-ticker.C:
		}

		content, ok := capture(cfg.SessionName, cfg.CaptureLines, opts)
		if !ok {
			captureMisses++
			if captureMisses < waitResponseExitAfterConsecutiveCaptureMisses {
				continue
			}
			state, err := tmuxSessionStateFor(cfg.SessionName, opts)
			if err != nil || state.Exists {
				// Capture can miss transiently while the tmux session is still alive,
				// and tmux state checks can also fail under load/timeouts.
				// Reset misses and continue waiting rather than reporting a false exit.
				captureMisses = 0
				missingSessionChecks = 0
				continue
			}
			missingSessionChecks++
			if missingSessionChecks < waitResponseExitAfterMissingSessionChecks {
				continue
			}
			fallback := preferredWaitContent(
				lastContent,
				lastNonEmptyContent,
				lastDifferentFromPre,
				preContent,
				contentChanged,
			)
			delta, latestLine := buildWaitResponseView(preContent, fallback, contentChanged)
			if strings.TrimSpace(latestLine) == "" {
				latestLine = "(no output yet)"
			}
			needsInput, inputHint := detectNeedsInput(delta)
			if !needsInput {
				needsInput, inputHint = detectNeedsInput(fallback)
			}
			summary := summarizeWaitResponse(
				"session_exited",
				latestLine,
				needsInput,
				inputHint,
			)
			return waitResponseResult{
				Status:        "session_exited",
				Content:       fallback,
				Delta:         delta,
				LatestLine:    latestLine,
				Summary:       summary,
				NeedsInput:    needsInput,
				InputHint:     inputHint,
				SessionExited: true,
				Changed:       contentChanged,
			}
		}
		captureMisses = 0
		missingSessionChecks = 0

		hash := waitResponseContentHash(content)
		rawHash := tmux.ContentHash(content)
		lastContent = content
		if strings.TrimSpace(content) != "" {
			lastNonEmptyContent = content
		}
		if rawHash != preHash && strings.TrimSpace(content) != "" {
			lastDifferentFromPre = content
		}

		// Return immediately when the agent is explicitly waiting on user input
		// (approval gates, choice prompts, etc.) so chat orchestrators can ask
		// the user right away instead of waiting for idle/timeout.
		if rawHash != preHash {
			if explicitNeedsInput, explicitHint := detectNeedsInputPrompt(content); explicitNeedsInput {
				finalContent := preferredWaitContent(
					content,
					lastNonEmptyContent,
					lastDifferentFromPre,
					preContent,
					true,
				)
				delta, latestLine := buildWaitResponseView(preContent, finalContent, true)
				needsInput, inputHint := detectNeedsInput(delta)
				if !needsInput {
					needsInput, inputHint = detectNeedsInput(finalContent)
				}
				if strings.TrimSpace(inputHint) == "" {
					inputHint = explicitHint
					needsInput = true
				}
				// In needs-input mode, surface the prompt itself as the latest line
				// so chat orchestrators can notify users with a direct action hint.
				if strings.TrimSpace(inputHint) != "" {
					latestLine = strings.TrimSpace(inputHint)
				}
				summary := summarizeWaitResponse(
					"needs_input",
					latestLine,
					needsInput,
					inputHint,
				)
				return waitResponseResult{
					Status:      "needs_input",
					Content:     finalContent,
					Delta:       delta,
					LatestLine:  latestLine,
					Summary:     summary,
					NeedsInput:  needsInput,
					InputHint:   inputHint,
					IdleSeconds: 0,
					Changed:     true,
				}
			}
		}

		if !contentChanged {
			if hash != preStableHash {
				contentChanged = true
				lastHash = hash
				lastChangeTime = time.Now()
			} else if waitResponseInitialChangeTimeout > 0 &&
				time.Since(waitStartedAt) >= waitResponseInitialChangeTimeout {
				return buildTimedOutWaitResponse(
					cfg,
					opts,
					capture,
					preContent,
					lastContent,
					lastNonEmptyContent,
					lastDifferentFromPre,
					contentChanged,
				)
			}
			continue
		}

		if hash != lastHash {
			lastHash = hash
			lastChangeTime = time.Now()
			continue
		}

		// Content unchanged — check idle threshold.
		elapsed := time.Since(lastChangeTime)
		if elapsed >= cfg.IdleThreshold {
			finalContent := preferredWaitContent(
				content,
				lastNonEmptyContent,
				lastDifferentFromPre,
				preContent,
				true,
			)
			delta, latestLine := buildWaitResponseView(preContent, finalContent, true)
			needsInput, inputHint := detectNeedsInput(delta)
			if !needsInput {
				needsInput, inputHint = detectNeedsInput(finalContent)
			}
			summary := summarizeWaitResponse("idle", latestLine, needsInput, inputHint)
			return waitResponseResult{
				Status:      "idle",
				Content:     finalContent,
				Delta:       delta,
				LatestLine:  latestLine,
				Summary:     summary,
				NeedsInput:  needsInput,
				InputHint:   inputHint,
				IdleSeconds: elapsed.Seconds(),
				Changed:     true,
			}
		}
	}
}

func buildTimedOutWaitResponse(
	cfg waitResponseConfig,
	opts tmux.Options,
	capture captureFn,
	preContent string,
	lastContent string,
	lastNonEmptyContent string,
	lastDifferentFromPre string,
	contentChanged bool,
) waitResponseResult {
	content := preferredWaitContent(
		lastContent,
		lastNonEmptyContent,
		lastDifferentFromPre,
		preContent,
		contentChanged,
	)
	if strings.TrimSpace(content) == "" {
		if captured, ok := capture(cfg.SessionName, cfg.CaptureLines, opts); ok &&
			strings.TrimSpace(captured) != "" {
			content = captured
		}
	}
	delta, latestLine := buildWaitResponseView(preContent, content, contentChanged)
	if strings.TrimSpace(latestLine) == "" {
		latestLine = "(no output yet)"
	}
	needsInput, inputHint := detectNeedsInput(delta)
	if !needsInput {
		needsInput, inputHint = detectNeedsInput(content)
	}
	summary := summarizeWaitResponse("timed_out", latestLine, needsInput, inputHint)
	return waitResponseResult{
		Status:      "timed_out",
		Content:     content,
		Delta:       delta,
		LatestLine:  latestLine,
		Summary:     summary,
		NeedsInput:  needsInput,
		InputHint:   inputHint,
		TimedOut:    true,
		IdleSeconds: 0,
		Changed:     contentChanged,
	}
}

func preferredWaitContent(
	content, lastNonEmptyContent, lastDifferentFromPre, preContent string,
	contentChanged bool,
) string {
	trimmedCurrent := strings.TrimSpace(content)
	trimmedPre := strings.TrimSpace(preContent)
	if contentChanged {
		if trimmedCurrent != "" && trimmedCurrent != trimmedPre {
			return content
		}
		if strings.TrimSpace(lastDifferentFromPre) != "" {
			return lastDifferentFromPre
		}
	}
	if trimmedCurrent != "" {
		return content
	}
	if strings.TrimSpace(lastNonEmptyContent) != "" {
		return lastNonEmptyContent
	}
	return preContent
}

// buildWaitResponseView returns a concise delta plus a single-line summary that
// orchestrators can use for compact notifications (e.g. chat UIs on mobile).
func buildWaitResponseView(preContent, content string, changed bool) (string, string) {
	if !changed {
		return "", lastNonEmptyLine(content)
	}
	preLines := strings.Split(preContent, "\n")
	currentLines := strings.Split(content, "\n")
	deltaLines := computeNewLines(preLines, currentLines)
	delta := strings.TrimSpace(strings.Join(deltaLines, "\n"))
	if delta == "" && strings.TrimSpace(content) != strings.TrimSpace(preContent) {
		// Fallback when overlap heuristics cannot isolate appended lines.
		delta = strings.TrimSpace(content)
	}
	if delta != "" {
		if compact := compactAgentOutput(delta); compact != "" {
			delta = compact
		}
	}
	if delta != "" {
		return delta, lastNonEmptyLine(delta)
	}
	return "", lastNonEmptyLine(content)
}

func waitResponseContentHash(content string) [16]byte {
	return tmux.ContentHash(waitResponseStableContent(content))
}

func waitResponseStableContent(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if isVolatileWaitProgressLine(line) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func isVolatileWaitProgressLine(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	// Keep explicit prompt/approval gates stable so needs_input semantics remain
	// accurate even when the TUI also renders interrupt hints.
	if looksLikeExplicitNeedsInputLine(line) {
		return false
	}
	return isAgentProgressNoiseLine(line)
}
