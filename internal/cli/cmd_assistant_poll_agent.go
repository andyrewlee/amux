package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/tmux"
)

const assistantPollAgentUsage = "Usage: amux assistant poll-agent --session <name> [--lines 100] [--interval 2] [--timeout 120]"

type assistantPollAgentConfig struct {
	SessionName string
	Lines       int
	Interval    time.Duration
	Timeout     time.Duration
}

func cmdAssistantPollAgent(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	fs := newFlagSet("assistant poll-agent")
	sessionName := fs.String("session", "", "session name")
	lines := fs.Int("lines", 100, "capture buffer depth")
	intervalRaw := fs.String("interval", "2", "poll interval in seconds or Go duration")
	timeoutRaw := fs.String("timeout", "120", "idle timeout in seconds or Go duration")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(
			w, wErr, gf, assistantPollAgentUsage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}
	if *sessionName == "" {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, nil)
	}
	if *lines <= 0 {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, errors.New("--lines must be > 0"))
	}

	interval, err := assistantParseFlexibleDuration(*intervalRaw)
	if err != nil {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, fmt.Errorf("invalid --interval: %w", err))
	}
	if interval <= 0 {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, errors.New("--interval must be > 0"))
	}

	timeout, err := assistantParseFlexibleDuration(*timeoutRaw)
	if err != nil {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, fmt.Errorf("invalid --timeout: %w", err))
	}
	if timeout <= 0 {
		return returnUsageError(w, wErr, gf, assistantPollAgentUsage, version, errors.New("--timeout must be > 0"))
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

	ctx, cancel := contextWithSignal()
	defer cancel()
	return runAssistantPollAgentLoop(ctx, w, assistantPollAgentConfig{
		SessionName: *sessionName,
		Lines:       *lines,
		Interval:    interval,
		Timeout:     timeout,
	}, svc.TmuxOpts, tmuxCapturePaneTail)
}

func runAssistantPollAgentLoop(
	ctx context.Context,
	w io.Writer,
	cfg assistantPollAgentConfig,
	opts tmux.Options,
	capture captureFn,
) int {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	exitDetector := newSessionExitDetector(
		defaultExitAfterConsecutiveCaptureMisses,
		defaultExitAfterMissingSessionChecks,
	)

	var lastHash [16]byte
	lastChangeTime := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return ExitOK
		default:
		}

		content, ok := capture(cfg.SessionName, cfg.Lines, opts)
		if !ok {
			if exitDetector.CaptureMissIndicatesExit(cfg.SessionName, opts) {
				_ = assistantCompatWriteEvent(w, assistantCompatEvent{
					Type:      "exited",
					Timestamp: now(),
				})
				return ExitOK
			}
		} else {
			exitDetector.Reset()
			hash := tmux.ContentHash(content)
			if hash != lastHash {
				if assistantWriteContent(w, content) != nil {
					return ExitOK
				}
				lastHash = hash
				lastChangeTime = time.Now()
			} else if !lastChangeTime.IsZero() && time.Since(lastChangeTime) >= cfg.Timeout {
				_ = assistantCompatWriteEvent(w, assistantCompatEvent{
					Type:        "idle",
					IdleSeconds: int(time.Since(lastChangeTime).Seconds()),
					Timestamp:   now(),
				})
				return ExitOK
			}
		}

		select {
		case <-ctx.Done():
			return ExitOK
		case <-ticker.C:
		}
	}
}
