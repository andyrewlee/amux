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

const assistantWaitForIdleUsage = "Usage: amux assistant wait-for-idle --session <name> [--timeout 300] [--idle-threshold 10]"

type assistantWaitForIdleConfig struct {
	SessionName   string
	Timeout       time.Duration
	TimeoutLabel  string
	IdleThreshold time.Duration
	PollInterval  time.Duration
	Lines         int
}

func cmdAssistantWaitForIdle(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	fs := newFlagSet("assistant wait-for-idle")
	sessionName := fs.String("session", "", "session name")
	timeoutRaw := fs.String("timeout", "300", "overall timeout in seconds or Go duration")
	idleThresholdRaw := fs.String("idle-threshold", "10", "idle threshold in seconds or Go duration")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(
			w, wErr, gf, assistantWaitForIdleUsage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}
	if *sessionName == "" {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, nil)
	}

	timeout, err := assistantParseFlexibleDuration(*timeoutRaw)
	if err != nil {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, fmt.Errorf("invalid --timeout: %w", err))
	}
	if timeout <= 0 {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, errors.New("--timeout must be > 0"))
	}

	idleThreshold, err := assistantParseFlexibleDuration(*idleThresholdRaw)
	if err != nil {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, fmt.Errorf("invalid --idle-threshold: %w", err))
	}
	if idleThreshold <= 0 {
		return returnUsageError(w, wErr, gf, assistantWaitForIdleUsage, version, errors.New("--idle-threshold must be > 0"))
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
	ctx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	return runAssistantWaitForIdleLoop(ctx, w, wErr, assistantWaitForIdleConfig{
		SessionName:   *sessionName,
		Timeout:       timeout,
		TimeoutLabel:  assistantTimeoutLabel(*timeoutRaw, timeout),
		IdleThreshold: idleThreshold,
		PollInterval:  2 * time.Second,
		Lines:         100,
	}, svc.TmuxOpts, tmuxCapturePaneTail)
}

func runAssistantWaitForIdleLoop(
	ctx context.Context,
	w, wErr io.Writer,
	cfg assistantWaitForIdleConfig,
	opts tmux.Options,
	capture captureFn,
) int {
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	exitDetector := newSessionExitDetector(
		defaultExitAfterConsecutiveCaptureMisses,
		defaultExitAfterMissingSessionChecks,
	)

	var lastHash [16]byte
	lastContent := ""
	lastChangeTime := time.Time{}

	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				_, _ = fmt.Fprintf(wErr, "Timeout after %s waiting for idle\n", cfg.TimeoutLabel)
				if lastContent != "" {
					_ = assistantWriteContent(w, lastContent)
				}
				return ExitInternalError
			}
			return ExitOK
		default:
		}

		content, ok := capture(cfg.SessionName, cfg.Lines, opts)
		if !ok {
			if exitDetector.CaptureMissIndicatesExit(cfg.SessionName, opts) {
				if lastContent != "" {
					_ = assistantWriteContent(w, lastContent)
				}
				return ExitOK
			}
		} else {
			exitDetector.Reset()
			hash := tmux.ContentHash(content)
			if hash != lastHash {
				lastHash = hash
				lastContent = content
				lastChangeTime = time.Now()
			} else if !lastChangeTime.IsZero() && time.Since(lastChangeTime) >= cfg.IdleThreshold {
				if lastContent != "" {
					_ = assistantWriteContent(w, lastContent)
				}
				return ExitOK
			}
		}

		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				_, _ = fmt.Fprintf(wErr, "Timeout after %s waiting for idle\n", cfg.TimeoutLabel)
				if lastContent != "" {
					_ = assistantWriteContent(w, lastContent)
				}
				return ExitInternalError
			}
			return ExitOK
		case <-ticker.C:
		}
	}
}
