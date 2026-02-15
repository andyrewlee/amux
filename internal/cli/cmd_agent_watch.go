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
	Type        string   `json:"type"`
	Content     string   `json:"content,omitempty"`
	NewLines    []string `json:"new_lines,omitempty"`
	Hash        string   `json:"hash,omitempty"`
	IdleSeconds float64  `json:"idle_seconds,omitempty"`
	Timestamp   string   `json:"ts"`
}

// watchConfig holds parsed flags for the watch loop.
type watchConfig struct {
	SessionName   string
	Lines         int
	Interval      time.Duration
	IdleThreshold time.Duration
}

func cmdAgentWatch(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	const usage = "Usage: amux agent watch <session_name> [--lines N] [--interval <duration>] [--idle-threshold <duration>]"
	fs := newFlagSet("agent watch")
	lines := fs.Int("lines", 100, "capture buffer depth")
	interval := fs.Duration("interval", 500*time.Millisecond, "poll interval")
	idleThreshold := fs.Duration("idle-threshold", 5*time.Second, "time before emitting idle event")

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
	}

	ctx := contextWithSignal()
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
	emittedIdle := false

	// Initial capture → snapshot
	content, ok := capture(cfg.SessionName, cfg.Lines, opts)
	if !ok {
		emitEvent(enc, watchEvent{
			Type:      "exited",
			Timestamp: now(),
		})
		return ExitOK
	}

	lastHash = tmux.ContentHash(content)
	lastLines = strings.Split(content, "\n")
	emitEvent(enc, watchEvent{
		Type:      "snapshot",
		Content:   content,
		Hash:      hashStr(lastHash),
		Timestamp: now(),
	})

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ExitOK
		case <-ticker.C:
		}

		content, ok = capture(cfg.SessionName, cfg.Lines, opts)
		if !ok {
			emitEvent(enc, watchEvent{
				Type:      "exited",
				Timestamp: now(),
			})
			return ExitOK
		}

		hash := tmux.ContentHash(content)
		if hash == lastHash {
			// No change — check idle threshold
			elapsed := time.Since(lastChangeTime)
			if elapsed >= cfg.IdleThreshold && !emittedIdle {
				emitEvent(enc, watchEvent{
					Type:        "idle",
					IdleSeconds: elapsed.Seconds(),
					Hash:        hashStr(hash),
					Timestamp:   now(),
				})
				emittedIdle = true
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
			emittedIdle = false
			continue
		}

		emitEvent(enc, watchEvent{
			Type:      "delta",
			NewLines:  newLines,
			Hash:      hashStr(hash),
			Timestamp: now(),
		})

		lastHash = hash
		lastLines = currentLines
		lastChangeTime = time.Now()
		emittedIdle = false
	}
}

// computeNewLines returns lines in current that are new compared to previous.
// It finds the longest suffix of current that doesn't overlap with previous.
func computeNewLines(previous, current []string) []string {
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

func emitEvent(enc *json.Encoder, event watchEvent) {
	_ = enc.Encode(event)
}

func hashStr(h [16]byte) string {
	return hex.EncodeToString(h[:])
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// contextWithSignal returns a context canceled on SIGINT or SIGTERM.
func contextWithSignal() context.Context {
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
	return ctx
}
