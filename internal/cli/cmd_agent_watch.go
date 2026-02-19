package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

// watchEvent is a single NDJSON line emitted by agent watch.
type watchEvent struct {
	Type             string   `json:"type"`
	Content          string   `json:"content,omitempty"`
	NewLines         []string `json:"new_lines,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	LatestLine       string   `json:"latest_line,omitempty"`
	NeedsInput       bool     `json:"needs_input,omitempty"`
	InputHint        string   `json:"input_hint,omitempty"`
	Hash             string   `json:"hash,omitempty"`
	IdleSeconds      float64  `json:"idle_seconds,omitempty"`
	HeartbeatSeconds float64  `json:"heartbeat_seconds,omitempty"`
	Timestamp        string   `json:"ts"`
}

// watchConfig holds parsed flags for the watch loop.
type watchConfig struct {
	SessionName   string
	Lines         int
	Interval      time.Duration
	IdleThreshold time.Duration
	Heartbeat     time.Duration
}

const (
	watchExitAfterConsecutiveCaptureMisses = 3
	watchExitAfterMissingSessionChecks     = 3
)

func cmdAgentWatch(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent watch <session_name> [--lines N] [--interval <duration>] [--idle-threshold <duration>] [--heartbeat <duration>]"
	fs := newFlagSet("agent watch")
	lines := fs.Int("lines", 100, "capture buffer depth")
	interval := fs.Duration("interval", 500*time.Millisecond, "poll interval")
	idleThreshold := fs.Duration("idle-threshold", 5*time.Second, "time before emitting idle event")
	heartbeat := fs.Duration("heartbeat", 10*time.Second, "emit heartbeat updates while waiting (0 disables)")

	sessionName, err := parseSinglePositionalWithFlags(fs, args)
	if err != nil {
		return returnUsageError(w, wErr, gf, usage, version, err)
	}
	if sessionName == "" {
		return returnUsageError(w, wErr, gf, usage, version, nil)
	}
	if *lines <= 0 {
		if gf.JSON {
			ReturnError(w, "invalid_lines", "--lines must be > 0",
				map[string]any{"lines": *lines}, version)
		} else {
			Errorf(wErr, "--lines must be > 0")
		}
		return ExitUsage
	}
	if *interval <= 0 {
		if gf.JSON {
			ReturnError(w, "invalid_interval", "--interval must be > 0", nil, version)
		} else {
			Errorf(wErr, "--interval must be > 0")
		}
		return ExitUsage
	}
	if *idleThreshold <= 0 {
		if gf.JSON {
			ReturnError(w, "invalid_idle_threshold", "--idle-threshold must be > 0", nil, version)
		} else {
			Errorf(wErr, "--idle-threshold must be > 0")
		}
		return ExitUsage
	}
	if *heartbeat < 0 {
		if gf.JSON {
			ReturnError(w, "invalid_heartbeat", "--heartbeat must be >= 0", nil, version)
		} else {
			Errorf(wErr, "--heartbeat must be >= 0")
		}
		return ExitUsage
	}

	svc, err := NewServices(version)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "init_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to initialize: %v", err)
		}
		return ExitInternalError
	}

	cfg := watchConfig{
		SessionName:   sessionName,
		Lines:         *lines,
		Interval:      *interval,
		IdleThreshold: *idleThreshold,
		Heartbeat:     *heartbeat,
	}

	ctx, cancel := contextWithSignal()
	defer cancel()
	return runWatchLoop(ctx, w, cfg, svc.TmuxOpts)
}

// captureFn abstracts tmux.CapturePaneTail for testing.
type captureFn func(sessionName string, lines int, opts tmux.Options) (string, bool)

// runWatchLoop is the core watch loop, separated for testability.
func runWatchLoop(ctx context.Context, w io.Writer, cfg watchConfig, opts tmux.Options) int {
	return runWatchLoopWith(ctx, w, cfg, opts, tmux.CapturePaneTail)
}

// runWatchLoopWith runs the watch loop with an injectable capture function.
func runWatchLoopWith(ctx context.Context, w io.Writer, cfg watchConfig, opts tmux.Options, capture captureFn) int {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	var lastHash [16]byte
	var lastLines []string
	lastChangeTime := time.Now()
	lastHeartbeatTime := time.Now()
	emittedIdle := false
	captureMisses := 0
	missingSessionChecks := 0

	// Initial capture → snapshot. A transient capture miss should not be
	// treated as exit if the tmux session still exists.
	var content string
	for {
		select {
		case <-ctx.Done():
			return ExitOK
		default:
		}
		captured, ok := capture(cfg.SessionName, cfg.Lines, opts)
		if ok {
			content = captured
			resetWatchCaptureMissState(&captureMisses, &missingSessionChecks)
			break
		}
		if watchShouldEmitExited(cfg.SessionName, opts, &captureMisses, &missingSessionChecks) {
			if !emitEvent(enc, watchEvent{
				Type:      "exited",
				Timestamp: now(),
			}) {
				return ExitOK
			}
			return ExitOK
		}
		select {
		case <-ctx.Done():
			return ExitOK
		case <-time.After(cfg.Interval):
		}
	}

	lastHash = tmux.ContentHash(content)
	lastLines = strings.Split(content, "\n")
	snapshotLatest := latestLineForContent(content)
	snapshotNeedsInput, snapshotInputHint := detectNeedsInput(content)
	if !emitEvent(enc, watchEvent{
		Type:       "snapshot",
		Content:    content,
		Hash:       hashStr(lastHash),
		LatestLine: snapshotLatest,
		NeedsInput: snapshotNeedsInput,
		InputHint:  snapshotInputHint,
		Summary: summarizeWatchEvent(
			"snapshot",
			snapshotLatest,
			snapshotNeedsInput,
			snapshotInputHint,
			0,
		),
		Timestamp: now(),
	}) {
		return ExitOK
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ExitOK
		case <-ticker.C:
		}

		content, ok := capture(cfg.SessionName, cfg.Lines, opts)
		if !ok {
			if !watchShouldEmitExited(cfg.SessionName, opts, &captureMisses, &missingSessionChecks) {
				continue
			}
			if !emitEvent(enc, watchEvent{
				Type:      "exited",
				Timestamp: now(),
			}) {
				return ExitOK
			}
			return ExitOK
		}
		resetWatchCaptureMissState(&captureMisses, &missingSessionChecks)

		hash := tmux.ContentHash(content)
		if hash == lastHash {
			// No change — check idle threshold
			elapsed := time.Since(lastChangeTime)
			if elapsed >= cfg.IdleThreshold && !emittedIdle {
				latestLine := latestLineForContent(content)
				needsInput, inputHint := detectNeedsInput(content)
				if !emitEvent(enc, watchEvent{
					Type:        "idle",
					IdleSeconds: elapsed.Seconds(),
					Hash:        hashStr(hash),
					LatestLine:  latestLine,
					NeedsInput:  needsInput,
					InputHint:   inputHint,
					Summary: summarizeWatchEvent(
						"idle",
						latestLine,
						needsInput,
						inputHint,
						elapsed.Seconds(),
					),
					Timestamp: now(),
				}) {
					return ExitOK
				}
				emittedIdle = true
			}
			if cfg.Heartbeat > 0 && time.Since(lastHeartbeatTime) >= cfg.Heartbeat {
				latestLine := latestLineForContent(content)
				needsInput, inputHint := detectNeedsInput(content)
				if !emitEvent(enc, watchEvent{
					Type:             "heartbeat",
					HeartbeatSeconds: elapsed.Seconds(),
					Hash:             hashStr(hash),
					LatestLine:       latestLine,
					NeedsInput:       needsInput,
					InputHint:        inputHint,
					Summary: summarizeWatchEvent(
						"heartbeat",
						latestLine,
						needsInput,
						inputHint,
						elapsed.Seconds(),
					),
					Timestamp: now(),
				}) {
					return ExitOK
				}
				lastHeartbeatTime = time.Now()
			}
			continue
		}

		// Content changed — compute delta
		currentLines := strings.Split(content, "\n")
		newLines := computeNewLines(lastLines, currentLines)
		if len(newLines) == 0 {
			lastHash = hash
			lastLines = currentLines
			lastChangeTime = time.Now()
			lastHeartbeatTime = time.Now()
			emittedIdle = false
			continue
		}

		deltaText := strings.TrimSpace(strings.Join(newLines, "\n"))
		compactDelta := compactAgentOutput(deltaText)
		latestLine := lastNonEmptyLine(compactDelta)
		if latestLine == "" {
			latestLine = lastNonEmptyLine(deltaText)
		}
		if latestLine == "" {
			latestLine = latestLineForContent(content)
		}
		needsInput, inputHint := detectNeedsInput(compactDelta)
		if !needsInput {
			needsInput, inputHint = detectNeedsInput(deltaText)
		}
		if !needsInput {
			needsInput, inputHint = detectNeedsInput(content)
		}

		if !emitEvent(enc, watchEvent{
			Type:       "delta",
			NewLines:   newLines,
			Hash:       hashStr(hash),
			LatestLine: latestLine,
			NeedsInput: needsInput,
			InputHint:  inputHint,
			Summary: summarizeWatchEvent(
				"delta",
				latestLine,
				needsInput,
				inputHint,
				0,
			),
			Timestamp: now(),
		}) {
			return ExitOK
		}

		lastHash = hash
		lastLines = currentLines
		lastChangeTime = time.Now()
		lastHeartbeatTime = time.Now()
		emittedIdle = false
	}
}

func watchShouldEmitExited(
	sessionName string,
	opts tmux.Options,
	captureMisses, missingSessionChecks *int,
) bool {
	*captureMisses = *captureMisses + 1
	if *captureMisses < watchExitAfterConsecutiveCaptureMisses {
		return false
	}
	state, err := tmuxSessionStateFor(sessionName, opts)
	if err != nil || state.Exists {
		// Capture can miss transiently while the tmux session is still alive,
		// and tmux state checks can also fail under load/timeouts.
		resetWatchCaptureMissState(captureMisses, missingSessionChecks)
		return false
	}
	*missingSessionChecks = *missingSessionChecks + 1
	return *missingSessionChecks >= watchExitAfterMissingSessionChecks
}

func resetWatchCaptureMissState(captureMisses, missingSessionChecks *int) {
	*captureMisses = 0
	*missingSessionChecks = 0
}

func latestLineForContent(content string) string {
	compact := compactAgentOutput(content)
	if line := lastNonEmptyLine(compact); line != "" {
		return line
	}
	return lastNonEmptyLine(content)
}

// computeNewLines returns lines in current that are new compared to previous.
// It finds the longest suffix of current that doesn't overlap with previous.
//
// Limitation: this heuristic matches the last line of previous in current and
// assumes sequential appending. Interleaved or rewritten output may cause
// missed or duplicated lines. For the terminal-capture use case this is an
// acceptable tradeoff; verifyOverlap provides additional correctness.
// trimTrailingEmpty removes a single trailing empty string produced by
// strings.Split when content ends with "\n". Keep additional empty lines
// so blank-line deltas can be observed.
func trimTrailingEmpty(lines []string) []string {
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func computeNewLines(previous, current []string) []string {
	// strings.Split produces a trailing "" when content ends with "\n";
	// strip one trailing empty element to avoid anchoring overlap on the
	// synthetic terminator while preserving intentional blank-line output.
	previous = trimTrailingEmpty(previous)
	current = trimTrailingEmpty(current)

	if len(previous) == 0 {
		return current
	}

	// Find the last line of previous in current, searching backwards.
	// This handles the common case where new lines are appended.
	lastPrev := previous[len(previous)-1]
	matchIdx := -1
	for i := len(current) - 1; i >= 0; i-- {
		if current[i] == lastPrev {
			// Verify the match extends backwards
			if verifyOverlap(previous, current, i) {
				matchIdx = i
				break
			}
		}
	}

	if matchIdx < 0 || matchIdx+1 >= len(current) {
		// No overlap found or no new lines after overlap — no new lines.
		if matchIdx < 0 {
			if isPrefix(previous, current) {
				return nil
			}
			return current
		}
		return nil
	}

	return current[matchIdx+1:]
}

// verifyOverlap checks that previous lines match ending at current[endIdx].
func verifyOverlap(previous, current []string, endIdx int) bool {
	pLen := len(previous)
	// Check as many lines as we can
	checkCount := pLen
	if endIdx+1 < checkCount {
		checkCount = endIdx + 1
	}
	for i := 0; i < checkCount; i++ {
		if previous[pLen-1-i] != current[endIdx-i] {
			return false
		}
	}
	return true
}

func isPrefix(previous, current []string) bool {
	if len(current) > len(previous) {
		return false
	}
	for i := range current {
		if previous[i] != current[i] {
			return false
		}
	}
	return true
}

func emitEvent(enc *json.Encoder, event watchEvent) bool {
	return enc.Encode(event) == nil
}

func hashStr(h [16]byte) string {
	return hex.EncodeToString(h[:])
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// contextWithSignal returns a context canceled on SIGINT or SIGTERM.
// The caller must invoke the returned cancel function to avoid leaking the
// signal-forwarding goroutine.
func contextWithSignal() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
