package process

import (
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/data"
)

func TestEnvBuilder_BuildEnv(t *testing.T) {
	ports := NewPortAllocator(6200, 10)
	builder := NewEnvBuilder(ports)

	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/feature-1",
		Env: map[string]string{
			"CUSTOM_VAR": "custom_value",
		},
	}

	env := builder.BuildEnv(wt)

	// Check required variables are present
	checks := map[string]string{
		"AMUX_WORKSPACE_NAME":   "feature-1",
		"AMUX_WORKSPACE_ROOT":   "/home/user/.amux/workspaces/feature-1",
		"AMUX_WORKSPACE_BRANCH": "feature-1",
		"ROOT_WORKSPACE_PATH":   "/home/user/repo",
		"CUSTOM_VAR":            "custom_value",
	}

	for key, wantValue := range checks {
		found := false
		for _, e := range env {
			if strings.HasPrefix(e, key+"=") {
				found = true
				gotValue := strings.TrimPrefix(e, key+"=")
				if gotValue != wantValue {
					t.Errorf("%s = %v, want %v", key, gotValue, wantValue)
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing env var: %s", key)
		}
	}

	// Check port variables
	portFound := false
	for _, e := range env {
		if strings.HasPrefix(e, "AMUX_PORT=") {
			portFound = true
			break
		}
	}
	if !portFound {
		t.Error("Missing AMUX_PORT env var")
	}
}

func TestEnvBuilder_BuildEnvMap(t *testing.T) {
	ports := NewPortAllocator(6200, 10)
	builder := NewEnvBuilder(ports)

	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/feature-1",
	}

	envMap := builder.BuildEnvMap(wt)

	if envMap["AMUX_WORKSPACE_NAME"] != "feature-1" {
		t.Errorf("AMUX_WORKSPACE_NAME = %v, want feature-1", envMap["AMUX_WORKSPACE_NAME"])
	}
	if envMap["AMUX_PORT"] != "6200" {
		t.Errorf("AMUX_PORT = %v, want 6200", envMap["AMUX_PORT"])
	}
}

func TestEnvBuilder_NilPortAllocator(t *testing.T) {
	builder := NewEnvBuilder(nil)

	wt := &data.Workspace{
		Name: "feature-1",
		Root: "/path/to/wt",
	}

	env := builder.BuildEnv(wt)

	// Should not crash with nil port allocator
	// And should not have port vars
	for _, e := range env {
		if strings.HasPrefix(e, "AMUX_PORT=") {
			t.Error("Should not have AMUX_PORT with nil allocator")
		}
	}
}
