package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func assistantDogfoodRunDX(
	rt *assistantDogfoodRuntime,
	slug string,
	args ...string,
) (assistantDogfoodRecordedCommand, error) {
	env := append(slices.Clone(os.Environ()), "AMUX_ASSISTANT_DX_CONTEXT_FILE="+rt.DXContextFile)
	commandArgs := append(slices.Clone(rt.DXInvoker.PrefixArgs), args...)
	start := time.Now()
	out, _ := assistantDogfoodRunExec("", env, rt.DXInvoker.Path, commandArgs...)
	record := assistantDogfoodRecordedCommand{
		Slug:       slug,
		RawPath:    filepath.Join(rt.ReportDir, slug+".raw"),
		JSONPath:   filepath.Join(rt.ReportDir, slug+".json"),
		StatusPath: filepath.Join(rt.ReportDir, slug+".status"),
	}
	jsonPayload, jsonText := assistantDogfoodParseJSONObject(string(out))
	record.Payload = jsonPayload
	if err := assistantDogfoodWriteTextFile(record.RawPath, string(out)); err != nil {
		return record, err
	}
	if err := assistantDogfoodWriteTextFile(record.JSONPath, jsonText); err != nil {
		return record, err
	}
	if jsonPayload != nil {
		record.StatusLine = assistantDogfoodStatusLine(
			assistantDogfoodNestedString(jsonPayload, "status"),
			assistantDogfoodNestedString(jsonPayload, "summary"),
			assistantDogfoodElapsedLabel(start),
		)
	} else {
		record.StatusLine = "command_error|non-json terminal output|latency=" + assistantDogfoodElapsedLabel(start)
	}
	if err := assistantDogfoodWriteTextFile(record.StatusPath, record.StatusLine); err != nil {
		return record, err
	}
	if _, err := fmt.Fprintf(rt.Output, "%s\t%s\n", slug, record.StatusLine); err != nil {
		return record, err
	}
	return record, nil
}

func assistantDogfoodRunAssistantLocalPing(
	rt *assistantDogfoodRuntime,
	slug, sessionID string,
) (assistantDogfoodRecordedCommand, error) {
	start := time.Now()
	out, _ := assistantDogfoodRunExec(
		"",
		nil,
		rt.AssistantBin,
		"agent",
		"--local",
		"--json",
		"--session-id",
		sessionID,
		"--message",
		"Dogfood ping: summarize current state in one line.",
	)
	record := assistantDogfoodRecordedCommand{
		Slug:       slug,
		RawPath:    filepath.Join(rt.ReportDir, slug+".raw"),
		JSONPath:   filepath.Join(rt.ReportDir, slug+".json"),
		StatusPath: filepath.Join(rt.ReportDir, slug+".status"),
	}
	jsonPayload, jsonText := assistantDogfoodParseJSONObject(string(out))
	record.Payload = jsonPayload
	if err := assistantDogfoodWriteTextFile(record.RawPath, string(out)); err != nil {
		return record, err
	}
	if err := assistantDogfoodWriteTextFile(record.JSONPath, jsonText); err != nil {
		return record, err
	}
	elapsed := assistantDogfoodElapsedLabel(start)
	switch {
	case assistantDogfoodFirstPayloadText(jsonPayload, "payloads") != "":
		record.StatusLine = assistantDogfoodStatusLine("ok", assistantDogfoodFirstPayloadText(jsonPayload, "payloads"), elapsed)
	case assistantDogfoodNestedString(jsonPayload, "status") != "":
		record.StatusLine = assistantDogfoodStatusLine(
			assistantDogfoodNestedString(jsonPayload, "status"),
			assistantDogfoodNestedString(jsonPayload, "summary"),
			elapsed,
		)
	case jsonPayload != nil:
		record.StatusLine = assistantDogfoodStatusLine("ok", "assistant local ping completed", elapsed)
	default:
		record.StatusLine = "command_error|non-json terminal output|latency=" + elapsed
	}
	if err := assistantDogfoodWriteTextFile(record.StatusPath, record.StatusLine); err != nil {
		return record, err
	}
	if _, err := fmt.Fprintf(rt.Output, "%s\t%s\n", slug, record.StatusLine); err != nil {
		return record, err
	}
	return record, nil
}

func assistantDogfoodRunAssistantChannelCommand(
	rt *assistantDogfoodRuntime,
	slug, sessionID, channel, commandText, expectedToken string,
	retryOnMissingMarkers bool,
) (assistantDogfoodRecordedCommand, error) {
	cfg := assistantDogfoodChannelConfigFromEnv()
	record := assistantDogfoodRecordedCommand{
		Slug:       slug,
		RawPath:    filepath.Join(rt.ReportDir, slug+".raw"),
		JSONPath:   filepath.Join(rt.ReportDir, slug+".json"),
		StatusPath: filepath.Join(rt.ReportDir, slug+".status"),
	}
	nonceToken := ""
	nonceFile := ""
	proofToken := ""
	proofFile := ""
	commandWithMarkers := commandText
	if cfg.RequireNonce {
		nonceToken = fmt.Sprintf("%d-%d-%d", time.Now().Unix(), time.Now().Nanosecond(), os.Getpid())
		file, err := os.CreateTemp("", "assistant-dogfood-nonce.*")
		if err != nil {
			return record, err
		}
		nonceFile = file.Name()
		if err := assistantDogfoodWriteTextFile(nonceFile, nonceToken); err != nil {
			_ = os.Remove(nonceFile)
			return record, err
		}
		commandWithMarkers = "cat " + shellQuoteCommandValue(nonceFile) + "; " + commandWithMarkers
	}
	if cfg.RequireProof {
		proofToken = fmt.Sprintf("proof-%d-%d-%d", time.Now().Unix(), time.Now().Nanosecond(), os.Getpid())
		proofFile = filepath.Join(rt.ReportDir, slug+".proof")
		_ = os.Remove(proofFile)
		commandWithMarkers += "; printf '%s\\n' " + shellQuoteCommandValue(proofToken) + " > " + shellQuoteCommandValue(proofFile)
	}
	defer func() {
		if nonceFile != "" {
			_ = os.Remove(nonceFile)
		}
		if proofFile != "" {
			_ = os.Remove(proofFile)
		}
	}()

	messageText := "Run exactly this shell command.\nDo not substitute workspace IDs or paths.\nReturn only the raw command output.\n\n" + commandWithMarkers
	agentUsed := cfg.PrimaryAgent
	start := time.Now()
	out, _ := assistantDogfoodRunExec(
		"",
		nil,
		rt.AssistantBin,
		"agent",
		"--agent",
		agentUsed,
		"--channel",
		channel,
		"--thinking",
		"off",
		"--session-id",
		sessionID,
		"--json",
		"--timeout",
		cfg.TimeoutLabel,
		"--message",
		messageText,
	)
	if agentUsed != cfg.FallbackAgent && strings.Contains(string(out), "not found") && strings.Contains(string(out), "agent") {
		agentUsed = cfg.FallbackAgent
		start = time.Now()
		out, _ = assistantDogfoodRunExec(
			"",
			nil,
			rt.AssistantBin,
			"agent",
			"--agent",
			agentUsed,
			"--channel",
			channel,
			"--thinking",
			"off",
			"--session-id",
			sessionID,
			"--json",
			"--timeout",
			cfg.TimeoutLabel,
			"--message",
			messageText,
		)
	}
	if err := assistantDogfoodWriteTextFile(record.RawPath, string(out)); err != nil {
		return record, err
	}
	jsonPayload, jsonText := assistantDogfoodParseJSONObject(string(out))
	record.Payload = jsonPayload
	if err := assistantDogfoodWriteTextFile(record.JSONPath, jsonText); err != nil {
		return record, err
	}
	totalElapsed := assistantDogfoodElapsedSeconds(start)
	record.StatusLine = assistantDogfoodRenderChannelStatus(jsonPayload, totalElapsed)

	missingMarkers := assistantDogfoodMissingMarkers(jsonText, nonceToken, expectedToken)
	if missingMarkers && !retryOnMissingMarkers {
		record.StatusLine = "attention|channel output missing execution markers|latency=" + assistantDogfoodSecondsLabel(totalElapsed)
	} else if missingMarkers {
		retryPrompt := messageText + "\n\nPrevious output was invalid because expected execution markers were missing. Run the exact command now and return only raw output."
		retryJSON, retryElapsed, err := assistantDogfoodRetryChannelCommand(
			rt,
			record.RawPath,
			agentUsed,
			channel,
			sessionID+"-retry",
			retryPrompt,
		)
		if err != nil {
			return record, err
		}
		if assistantDogfoodMissingMarkers(retryJSON.jsonText, nonceToken, expectedToken) {
			if agentUsed != cfg.FallbackAgent {
				fallbackJSON, fallbackElapsed, fallbackErr := assistantDogfoodRetryChannelCommand(
					rt,
					record.RawPath,
					cfg.FallbackAgent,
					channel,
					sessionID+"-fallback",
					retryPrompt,
				)
				if fallbackErr != nil {
					return record, fallbackErr
				}
				totalElapsed += retryElapsed + fallbackElapsed
				if !assistantDogfoodMissingMarkers(fallbackJSON.jsonText, nonceToken, expectedToken) {
					agentUsed = cfg.FallbackAgent
					record.Payload = fallbackJSON.payload
					if err := assistantDogfoodWriteTextFile(record.JSONPath, fallbackJSON.jsonText); err != nil {
						return record, err
					}
					record.StatusLine = assistantDogfoodRenderChannelStatus(fallbackJSON.payload, totalElapsed)
				} else {
					record.StatusLine = "attention|channel output missing execution markers|latency=" + assistantDogfoodSecondsLabel(totalElapsed)
				}
			} else {
				totalElapsed += retryElapsed
				record.StatusLine = "attention|channel output missing execution markers|latency=" + assistantDogfoodSecondsLabel(totalElapsed)
			}
		} else {
			totalElapsed += retryElapsed
			record.Payload = retryJSON.payload
			if err := assistantDogfoodWriteTextFile(record.JSONPath, retryJSON.jsonText); err != nil {
				return record, err
			}
			record.StatusLine = assistantDogfoodRenderChannelStatus(retryJSON.payload, totalElapsed)
		}
	}

	if cfg.RequireProof {
		proofValue, _ := os.ReadFile(proofFile)
		if strings.TrimSpace(string(proofValue)) != proofToken {
			record.StatusLine = "attention|channel output unverified: command execution proof missing|latency=" + assistantDogfoodSecondsLabel(totalElapsed)
		}
	}
	record.StatusLine += "|agent=" + agentUsed
	if err := assistantDogfoodWriteTextFile(record.StatusPath, record.StatusLine); err != nil {
		return record, err
	}
	if _, err := fmt.Fprintf(rt.Output, "%s\t%s\n", slug, record.StatusLine); err != nil {
		return record, err
	}
	return record, nil
}

type assistantDogfoodRetryJSON struct {
	payload  map[string]any
	jsonText string
}

func assistantDogfoodRetryChannelCommand(
	rt *assistantDogfoodRuntime,
	rawPath, agentID, channel, sessionID, prompt string,
) (assistantDogfoodRetryJSON, int, error) {
	start := time.Now()
	out, _ := assistantDogfoodRunExec(
		"",
		nil,
		rt.AssistantBin,
		"agent",
		"--agent",
		agentID,
		"--channel",
		channel,
		"--thinking",
		"off",
		"--session-id",
		sessionID,
		"--json",
		"--timeout",
		assistantDogfoodChannelConfigFromEnv().TimeoutLabel,
		"--message",
		prompt,
	)
	if err := assistantDogfoodAppendTextFile(rawPath, string(out)); err != nil {
		return assistantDogfoodRetryJSON{}, 0, err
	}
	payload, jsonText := assistantDogfoodParseJSONObject(string(out))
	return assistantDogfoodRetryJSON{payload: payload, jsonText: jsonText}, assistantDogfoodElapsedSeconds(start), nil
}

func assistantDogfoodChannelConfigFromEnv() assistantDogfoodChannelConfig {
	timeoutSeconds := assistantStepEnvDurationToSeconds("AMUX_ASSISTANT_DOGFOOD_CHANNEL_TIMEOUT_SECONDS", 180)
	return assistantDogfoodChannelConfig{
		Channel:        assistantDogfoodFirstNonEmptyEnv("AMUX_ASSISTANT_DOGFOOD_CHANNEL", "telegram"),
		PrimaryAgent:   assistantDogfoodFirstNonEmptyEnv("AMUX_ASSISTANT_DOGFOOD_AGENT", "amux-dx"),
		FallbackAgent:  assistantDogfoodFirstNonEmptyEnv("AMUX_ASSISTANT_DOGFOOD_CHANNEL_FALLBACK_AGENT", "main"),
		RequireNonce:   assistantDogfoodEnvBool("AMUX_ASSISTANT_DOGFOOD_CHANNEL_REQUIRE_NONCE", false),
		RequireProof:   assistantDogfoodEnvBool("AMUX_ASSISTANT_DOGFOOD_CHANNEL_REQUIRE_PROOF", true),
		RequireMarkers: true,
		Timeout:        time.Duration(timeoutSeconds) * time.Second,
		TimeoutLabel:   strconv.Itoa(timeoutSeconds),
	}
}
