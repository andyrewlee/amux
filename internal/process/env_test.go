package process

import (
	"os"
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

func TestEnvBuilder_CustomEnvCannotOverrideReservedEnv(t *testing.T) {
	ports := NewPortAllocator(6200, 10)
	builder := NewEnvBuilder(ports)

	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-branch",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/feature-1",
		Env: map[string]string{
			"AMUX_WORKSPACE_NAME":   "poison-name",
			"AMUX_WORKSPACE_ROOT":   "poison-root",
			"AMUX_WORKSPACE_BRANCH": "poison-branch",
			"ROOT_WORKSPACE_PATH":   "poison-repo",
			"AMUX_PORT":             "1",
			"AMUX_PORT_RANGE":       "1-2",
			"CUSTOM_VAR":            "custom-value",
		},
	}

	env := envSliceMap(builder.BuildEnv(wt))
	envMap := builder.BuildEnvMap(wt)
	checks := map[string]string{
		"AMUX_WORKSPACE_NAME":   "feature-1",
		"AMUX_WORKSPACE_ROOT":   "/home/user/.amux/workspaces/feature-1",
		"AMUX_WORKSPACE_BRANCH": "feature-branch",
		"ROOT_WORKSPACE_PATH":   "/home/user/repo",
		"AMUX_PORT":             "6200",
		"AMUX_PORT_RANGE":       "6200-6209",
		"CUSTOM_VAR":            "custom-value",
	}
	for key, wantValue := range checks {
		if got := env[key]; got != wantValue {
			t.Errorf("BuildEnv()[%s] = %q, want %q", key, got, wantValue)
		}
		if got := envMap[key]; got != wantValue {
			t.Errorf("BuildEnvMap()[%s] = %q, want %q", key, got, wantValue)
		}
	}
}

func TestEnvBuilder_CustomEnvOrderIsDeterministic(t *testing.T) {
	builder := NewEnvBuilder(nil)
	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-branch",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/feature-1",
		Env: map[string]string{
			"CUSTOM_B": "b",
			"CUSTOM_A": "a",
		},
	}

	env := builder.BuildEnv(wt)
	if len(env) < 2 {
		t.Fatalf("BuildEnv() returned %d entries, want at least 2", len(env))
	}
	if got := env[len(env)-2]; got != "CUSTOM_A=a" {
		t.Fatalf("second-to-last env = %q, want CUSTOM_A=a", got)
	}
	if got := env[len(env)-1]; got != "CUSTOM_B=b" {
		t.Fatalf("last env = %q, want CUSTOM_B=b", got)
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

func TestEnvBuilder_NilWorkspace(t *testing.T) {
	builder := NewEnvBuilder(NewPortAllocator(6200, 10))

	wantLen := len(os.Environ())
	env := builder.BuildEnv(nil)
	if len(env) != wantLen {
		t.Fatalf("BuildEnv(nil) returned %d entries, want current environment length %d", len(env), wantLen)
	}

	envMap := builder.BuildEnvMap(nil)
	if len(envMap) != 0 {
		t.Fatalf("BuildEnvMap(nil) = %#v, want empty map", envMap)
	}
}

func TestEnvBuilder_NilReceiver(t *testing.T) {
	var builder *EnvBuilder
	wt := &data.Workspace{
		Name:   "feature-1",
		Branch: "feature-1",
		Repo:   "/home/user/repo",
		Root:   "/home/user/.amux/workspaces/feature-1",
	}

	env := builder.BuildEnv(wt)
	foundName := false
	for _, e := range env {
		if strings.HasPrefix(e, "AMUX_WORKSPACE_NAME=") {
			foundName = true
		}
		if strings.HasPrefix(e, "AMUX_PORT=") {
			t.Fatal("nil EnvBuilder receiver should not add AMUX_PORT")
		}
	}
	if !foundName {
		t.Fatal("nil EnvBuilder receiver should still add workspace variables")
	}

	envMap := builder.BuildEnvMap(wt)
	if envMap["AMUX_WORKSPACE_NAME"] != "feature-1" {
		t.Fatalf("AMUX_WORKSPACE_NAME = %q, want feature-1", envMap["AMUX_WORKSPACE_NAME"])
	}
	if _, ok := envMap["AMUX_PORT"]; ok {
		t.Fatal("nil EnvBuilder receiver should not add AMUX_PORT to map")
	}
}

// TestIsReservedScriptEnvKey_MatchesUnexported pins the exported wrapper's
// only contract: it must agree with isReservedScriptEnvKey for every name
// BuildEnv actually injects, plus an arbitrary non-reserved name, so the
// workspace env editor (internal/ui/common's EnvDialog, via internal/app)
// cannot drift from the list this package enforces at injection time.
func TestIsReservedScriptEnvKey_MatchesUnexported(t *testing.T) {
	reserved := []string{
		"AMUX_WORKSPACE_NAME",
		"AMUX_WORKSPACE_ROOT",
		"AMUX_WORKSPACE_BRANCH",
		"ROOT_WORKSPACE_PATH",
		"AMUX_PORT",
		"AMUX_PORT_RANGE",
	}
	for _, key := range reserved {
		if !IsReservedScriptEnvKey(key) {
			t.Errorf("IsReservedScriptEnvKey(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"CUSTOM_VAR", "", "NODE_ENV"} {
		if IsReservedScriptEnvKey(key) {
			t.Errorf("IsReservedScriptEnvKey(%q) = true, want false", key)
		}
	}
}

func envSliceMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
