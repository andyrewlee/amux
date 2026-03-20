package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

func newAssistantDXInvoker(version string) assistantDXInvoker {
	if assistantDXShouldForceInternal() {
		return assistantDXInvoker{version: version}
	}
	if explicit := strings.TrimSpace(os.Getenv("AMUX_BIN")); explicit != "" {
		return assistantDXInvoker{version: version, amuxBin: explicit, useExternal: true}
	}
	return assistantDXInvoker{version: version}
}

func assistantDXShouldForceInternal() bool {
	return strings.TrimSpace(os.Getenv("AMUX_ASSISTANT_DX_FORCE_INTERNAL")) != ""
}

func (inv assistantDXInvoker) suggestedAMUXBin() string {
	if strings.TrimSpace(inv.amuxBin) != "" {
		return inv.amuxBin
	}
	if explicit := strings.TrimSpace(os.Getenv("AMUX_BIN")); explicit != "" {
		return explicit
	}
	return "amux"
}

func (inv assistantDXInvoker) call(args ...string) assistantDXCallResult {
	if inv.useExternal {
		return inv.callExternal(args...)
	}
	return inv.callInternal(args...)
}

func (inv assistantDXInvoker) callExternal(args ...string) assistantDXCallResult {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmdArgs := append([]string{"--json"}, args...)
	cmd := exec.Command(inv.amuxBin, cmdArgs...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	result := assistantDXCallResult{}
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = ExitInternalError
		}
	}
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Envelope = assistantDXParseEnvelope(stdout.Bytes())
	return result
}

func (inv assistantDXInvoker) callInternal(args ...string) assistantDXCallResult {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	gf := GlobalFlags{JSON: true}
	prevRequestID, prevCommand := currentResponseContext()
	setResponseContext(prevRequestID, commandFromArgs(args))
	defer setResponseContext(prevRequestID, prevCommand)

	if len(args) == 0 {
		ReturnError(&stdout, "usage_error", "Usage: amux <command> [flags]", nil, inv.version)
		return assistantDXCallResult{
			Envelope: assistantDXParseEnvelope(stdout.Bytes()),
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: ExitUsage,
		}
	}

	var code int
	switch args[0] {
	case "status":
		code = cmdStatus(&stdout, &stderr, gf, args[1:], inv.version)
	case "project":
		code = routeProject(&stdout, &stderr, gf, args[1:], inv.version)
	case "workspace":
		code = routeWorkspace(&stdout, &stderr, gf, args[1:], inv.version)
	case "task":
		code = routeTask(&stdout, &stderr, gf, args[1:], inv.version)
	case "agent":
		code = routeAgent(&stdout, &stderr, gf, args[1:], inv.version)
	case "terminal":
		code = routeTerminal(&stdout, &stderr, gf, args[1:], inv.version)
	case "session":
		code = routeSession(&stdout, &stderr, gf, args[1:], inv.version)
	default:
		ReturnError(&stdout, "unknown_command", "Unknown command: "+args[0], nil, inv.version)
		return assistantDXCallResult{
			Envelope: assistantDXParseEnvelope(stdout.Bytes()),
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: ExitUsage,
		}
	}

	return assistantDXCallResult{
		Envelope: assistantDXParseEnvelope(stdout.Bytes()),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: code,
	}
}

func assistantDXParseEnvelope(raw []byte) *Envelope {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil
	}
	if _, ok := probe["ok"]; !ok {
		return nil
	}
	var env Envelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil
	}
	return &env
}

func (runner assistantDXRunner) invokeOK(commandName string, args ...string) (*Envelope, *assistantDXPayload) {
	call := runner.invoker.call(args...)
	if call.Envelope != nil {
		if call.Envelope.OK {
			return call.Envelope, nil
		}
		message := "amux command failed"
		detail := ""
		if call.Envelope.Error != nil {
			if strings.TrimSpace(call.Envelope.Error.Message) != "" {
				message = strings.TrimSpace(call.Envelope.Error.Message)
			}
			detail = strings.TrimSpace(call.Envelope.Error.Code)
		}
		payload := assistantDXErrorPayload(commandName, message, detail)
		return nil, &payload
	}

	details := strings.TrimSpace(call.Stderr)
	if details == "" {
		details = strings.TrimSpace(call.Stdout)
	}
	message := "amux returned invalid JSON"
	if call.ExitCode != ExitOK {
		message = "amux command failed: " + strings.Join(args, " ")
	}
	payload := assistantDXErrorPayload(commandName, message, details)
	return nil, &payload
}

func (runner assistantDXRunner) invokeQuiet(args ...string) (*Envelope, string) {
	call := runner.invoker.call(args...)
	if call.Envelope != nil {
		if call.Envelope.OK {
			return call.Envelope, assistantDXProbeOK
		}
		code, message := "", ""
		if call.Envelope.Error != nil {
			code = strings.TrimSpace(call.Envelope.Error.Code)
			message = strings.TrimSpace(call.Envelope.Error.Message)
		}
		if state := assistantDXUnsupportedProbeState(code, message, call.Stderr); state != assistantDXProbeError {
			return nil, state
		}
		return nil, assistantDXProbeError
	}
	if state := assistantDXUnsupportedProbeState("", "", call.Stderr); state != assistantDXProbeError {
		return nil, state
	}
	return nil, assistantDXProbeError
}

func assistantDXUnsupportedProbeState(code, message, stderr string) string {
	combined := strings.ToLower(strings.TrimSpace(strings.Join([]string{code, message, stderr}, "\n")))
	switch {
	case combined == "":
		return assistantDXProbeError
	case strings.Contains(combined, "unsupported") ||
		strings.Contains(combined, "flag provided but not defined") ||
		strings.Contains(combined, "unknown flag"):
		switch {
		case strings.Contains(combined, "--all"):
			return assistantDXProbeUnsupportedAll
		case strings.Contains(combined, "--archived"):
			return assistantDXProbeUnsupportedArchive
		default:
			return assistantDXProbeUnsupported
		}
	default:
		return assistantDXProbeError
	}
}
