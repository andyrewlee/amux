package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func cmdAssistantStep(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = gf
	_ = version

	if len(args) == 0 {
		assistantStepUsage(wErr)
		payload := assistantStepErrorPayload{
			OK:      false,
			Status:  "command_error",
			Summary: "Invalid mode",
			Error:   "mode must be run or send",
		}
		assistantStepWriteJSON(w, payload)
		return ExitUsage
	}

	opts, parsePayload, exitCode := parseAssistantStepOptions(args[0], args[1:])
	if parsePayload != nil {
		assistantStepWriteJSON(w, parsePayload)
		return exitCode
	}

	payload, code := runAssistantStep(opts)
	assistantStepWriteJSON(w, payload)
	return code
}

func assistantStepUsage(w io.Writer) {
	fmt.Fprint(w, "Usage:\n  assistant-step.sh run  --workspace <id> --assistant <name> --prompt <text> [--wait-timeout 60s] [--idle-threshold 10s]\n  assistant-step.sh send --agent <id> [--text <text>] [--enter] [--wait-timeout 60s] [--idle-threshold 10s]\n")
}

func parseAssistantStepOptions(mode string, args []string) (assistantStepOptions, *assistantStepErrorPayload, int) {
	opts := assistantStepOptions{
		Mode:          strings.TrimSpace(mode),
		WaitTimeout:   "60s",
		IdleThreshold: "10s",
	}

	if opts.Mode != assistantStepModeRun && opts.Mode != assistantStepModeSend {
		return opts, &assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "Invalid mode",
			Error:   "mode must be run or send",
		}, ExitUsage
	}

	for i := 0; i < len(args); {
		token := args[i]
		switch token {
		case "--wait-timeout":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.WaitTimeout = value
			i += 2
		case "--idle-threshold":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.IdleThreshold = value
			i += 2
		case "--idempotency-key":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.IdempotencyKey = value
			i += 2
		case "--workspace":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.Workspace = value
			i += 2
		case "--assistant":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.Assistant = value
			i += 2
		case "--prompt":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, true)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.Prompt = value
			i += 2
		case "--agent":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, false)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.AgentID = value
			i += 2
		case "--text":
			value, errPayload := assistantStepRequireValue(opts.Mode, token, args, i, true)
			if errPayload != nil {
				return opts, errPayload, ExitUsage
			}
			opts.Text = value
			i += 2
		case "--enter":
			opts.Enter = true
			i++
		default:
			return opts, &assistantStepErrorPayload{
				OK:      false,
				Mode:    opts.Mode,
				Status:  "command_error",
				Summary: "Invalid flag",
				Error:   "unknown flag: " + token,
			}, ExitUsage
		}
	}

	if opts.Mode == assistantStepModeRun {
		if strings.TrimSpace(opts.Workspace) == "" || strings.TrimSpace(opts.Assistant) == "" || strings.TrimSpace(opts.Prompt) == "" {
			return opts, &assistantStepErrorPayload{
				OK:      false,
				Mode:    opts.Mode,
				Status:  "command_error",
				Summary: "Missing required flags",
				Error:   "run requires --workspace, --assistant, --prompt",
			}, ExitUsage
		}
	} else if strings.TrimSpace(opts.AgentID) == "" {
		return opts, &assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "Missing required flags",
			Error:   "send requires --agent",
		}, ExitUsage
	}

	autoIdempotency := strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_STEP_AUTO_IDEMPOTENCY"))
	if strings.TrimSpace(opts.IdempotencyKey) == "" && autoIdempotency != "false" {
		base := strings.Join([]string{
			opts.Mode,
			opts.WaitTimeout,
			opts.IdleThreshold,
			opts.Workspace,
			opts.Assistant,
			opts.Prompt,
			opts.AgentID,
			opts.Text,
			strconv.FormatBool(opts.Enter),
		}, "|")
		sum := sha256.Sum256([]byte(base))
		opts.IdempotencyKey = "tgstep-" + hex.EncodeToString(sum[:])[:20]
	}

	return opts, nil, ExitOK
}

func assistantStepRequireValue(mode, flag string, args []string, idx int, allowFlagLike bool) (string, *assistantStepErrorPayload) {
	if idx+1 >= len(args) {
		return "", &assistantStepErrorPayload{
			OK:      false,
			Mode:    mode,
			Status:  "command_error",
			Summary: "Missing value for flag",
			Error:   "missing value for " + flag,
		}
	}
	value := args[idx+1]
	if strings.HasPrefix(value, "--") && !allowFlagLike {
		return "", &assistantStepErrorPayload{
			OK:      false,
			Mode:    mode,
			Status:  "command_error",
			Summary: "Missing value for flag",
			Error:   "missing value for " + flag,
		}
	}
	return value, nil
}

func runAssistantStep(opts assistantStepOptions) (any, int) {
	amuxBin := assistantStepAMUXBin()
	if _, err := exec.LookPath(amuxBin); err != nil && !strings.Contains(amuxBin, "/") {
		return assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "amux is not installed",
			Error:   "missing binary: " + amuxBin,
		}, 127
	}
	if strings.Contains(amuxBin, "/") {
		if _, err := os.Stat(amuxBin); err != nil {
			return assistantStepErrorPayload{
				OK:      false,
				Mode:    opts.Mode,
				Status:  "command_error",
				Summary: "amux is not installed",
				Error:   "missing binary: " + amuxBin,
			}, 127
		}
	}

	cmdArgs := []string{"--json"}
	switch opts.Mode {
	case assistantStepModeRun:
		cmdArgs = append(cmdArgs,
			"agent", "run",
			"--workspace", opts.Workspace,
			"--assistant", opts.Assistant,
			"--prompt", opts.Prompt,
			"--wait",
			"--wait-timeout", opts.WaitTimeout,
			"--idle-threshold", opts.IdleThreshold,
		)
	case assistantStepModeSend:
		cmdArgs = append(cmdArgs, "agent", "send", "--agent", opts.AgentID)
		if strings.TrimSpace(opts.Text) != "" {
			cmdArgs = append(cmdArgs, "--text", opts.Text)
		}
		cmdArgs = append(cmdArgs,
			"--wait",
			"--wait-timeout", opts.WaitTimeout,
			"--idle-threshold", opts.IdleThreshold,
		)
		if opts.Enter {
			cmdArgs = append(cmdArgs, "--enter")
		}
	}
	if strings.TrimSpace(opts.IdempotencyKey) != "" {
		cmdArgs = append(cmdArgs, "--idempotency-key", opts.IdempotencyKey)
	}

	waitTimeoutSeconds := assistantStepDurationToSeconds(opts.WaitTimeout, 60)
	if waitTimeoutSeconds <= 0 {
		waitTimeoutSeconds = 60
	}
	hardTimeoutBuffer := assistantStepEnvDurationToSeconds("AMUX_ASSISTANT_STEP_HARD_TIMEOUT_BUFFER", 30)
	if hardTimeoutBuffer < 0 {
		hardTimeoutBuffer = 30
	}
	hardTimeoutSeconds := waitTimeoutSeconds + hardTimeoutBuffer
	hardTimeoutCap := assistantStepEnvDurationToSeconds("AMUX_ASSISTANT_STEP_HARD_TIMEOUT_CAP", 120)
	if hardTimeoutCap > 0 && hardTimeoutSeconds > hardTimeoutCap {
		hardTimeoutSeconds = hardTimeoutCap
	}
	if hardTimeoutSeconds < waitTimeoutSeconds {
		hardTimeoutSeconds = waitTimeoutSeconds
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(hardTimeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, amuxBin, cmdArgs...)
	out, err := cmd.CombinedOutput()
	rawOutput := string(out)
	if ctx.Err() == context.DeadlineExceeded {
		detail := fmt.Sprintf("hard timeout (%ds) exceeded while running amux step", hardTimeoutSeconds)
		if strings.TrimSpace(rawOutput) != "" {
			detail += "\n" + rawOutput
		}
		return assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "amux command exceeded hard timeout",
			Error:   detail,
		}, 124
	}

	var env assistantStepUnderlyingEnvelope
	jsonOK := json.Unmarshal(out, &env) == nil
	if err != nil {
		exitCode := 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if jsonOK {
			errMsg := "agent step failed"
			errCode := "unknown_error"
			if env.Error != nil {
				if strings.TrimSpace(env.Error.Message) != "" {
					errMsg = env.Error.Message
				}
				if strings.TrimSpace(env.Error.Code) != "" {
					errCode = env.Error.Code
				}
			}
			return assistantStepErrorPayload{
				OK:      false,
				Mode:    opts.Mode,
				Status:  "agent_error",
				Summary: errMsg,
				Error:   errCode,
			}, exitCode
		}
		return assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "amux command failed",
			Error:   rawOutput,
		}, exitCode
	}

	if !jsonOK {
		return assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "command_error",
			Summary: "amux returned non-JSON output",
			Error:   rawOutput,
		}, 65
	}
	if !env.OK {
		errMsg := "agent step failed"
		errCode := "unknown_error"
		if env.Error != nil {
			if strings.TrimSpace(env.Error.Message) != "" {
				errMsg = env.Error.Message
			}
			if strings.TrimSpace(env.Error.Code) != "" {
				errCode = env.Error.Code
			}
		}
		return assistantStepErrorPayload{
			OK:      false,
			Mode:    opts.Mode,
			Status:  "agent_error",
			Summary: errMsg,
			Error:   errCode,
		}, ExitInternalError
	}

	return buildAssistantStepPayload(opts, env.Data, amuxBin), ExitOK
}
