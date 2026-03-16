package daytona

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

// Process handles process execution within a sandbox.
type Process struct {
	toolbox func() (*toolboxClient, error)
}

// ExecuteCommand executes a shell command in the sandbox.
func (p *Process) ExecuteCommand(command string, opts ...ExecuteCommandOptions) (*ExecuteResponse, error) {
	client, err := p.toolbox()
	if err != nil {
		return nil, err
	}

	var options ExecuteCommandOptions
	if len(opts) > 0 {
		options = opts[0]
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(command))
	cmd := fmt.Sprintf("echo '%s' | base64 -d | sh", encoded)
	if len(options.Env) > 0 {
		parts := make([]string, 0, len(options.Env))
		for k, v := range options.Env {
			encodedVal := base64.StdEncoding.EncodeToString([]byte(v))
			parts = append(parts, fmt.Sprintf("export %s=$(echo '%s' | base64 -d)", k, encodedVal))
		}
		cmd = fmt.Sprintf("%s; %s", strings.Join(parts, ";"), cmd)
	}
	cmd = fmt.Sprintf("sh -c \"%s\"", cmd)

	payload := map[string]any{"command": cmd}
	if options.Cwd != "" {
		payload["cwd"] = options.Cwd
	}
	if options.Timeout > 0 {
		payload["timeout"] = int(options.Timeout.Seconds())
	}

	var resp struct {
		ExitCode int32  `json:"exitCode"`
		Result   string `json:"result"`
	}
	if err := client.doJSON(context.Background(), http.MethodPost, "/process/execute", nil, payload, &resp); err != nil {
		return nil, err
	}
	artifacts := ParseArtifacts(resp.Result)
	return &ExecuteResponse{ExitCode: resp.ExitCode, Result: artifacts.Stdout, Artifacts: &artifacts}, nil
}
