package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/andyrewlee/amux/internal/config"
)

// envValue resolves a key the way exec does: the last KEY=value entry wins.
// A missing key reports !ok.
func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	found := ""
	ok := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			found = strings.TrimPrefix(kv, prefix)
			ok = true
		}
	}
	return found, ok
}

// TestPTYSessionEnv characterizes childEnv layering: parent env survives minus
// Git overrides, fixture defaults replace ambient values, the workspaces root
// is pinned under the fixture HOME by default, and explicit PTYOptions.Env
// entries win last — including an explicit blank (production then falls back
// to the fixture HOME root).
func TestPTYSessionEnv(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fixture home")
	defaultRoot := filepath.Join(home, ".amux", "workspaces")

	tests := []struct {
		name     string
		parent   []string
		optEnv   []string
		wantRoot string
	}{
		{
			name:     "absent key gets fixture root",
			parent:   []string{"PATH=/bin"},
			wantRoot: defaultRoot,
		},
		{
			name:     "inherited absolute root is overridden",
			parent:   []string{config.WorkspacesRootEnvVar + "=/real/user/workspaces"},
			wantRoot: defaultRoot,
		},
		{
			name:     "whitespace-only inherited value still overridden",
			parent:   []string{config.WorkspacesRootEnvVar + "=   "},
			wantRoot: defaultRoot,
		},
		{
			name: "duplicate inherited entries all lose",
			parent: []string{
				config.WorkspacesRootEnvVar + "=/first",
				config.WorkspacesRootEnvVar + "=/second",
			},
			wantRoot: defaultRoot,
		},
		{
			name:     "explicit temporary custom root wins",
			optEnv:   []string{config.WorkspacesRootEnvVar + "=" + filepath.Join(t.TempDir(), "custom")},
			wantRoot: "",
		},
		{
			name:     "explicit blank override wins (production falls back to HOME)",
			optEnv:   []string{config.WorkspacesRootEnvVar + "="},
			wantRoot: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := childEnv(tt.parent, home, tt.optEnv)
			got, ok := envValue(env, config.WorkspacesRootEnvVar)
			if !ok {
				t.Fatal("workspaces root key missing from child env")
			}
			want := tt.wantRoot
			if want == "" {
				// Explicit cases: assert the optEnv value is what landed.
				want, _ = envValue(tt.optEnv, config.WorkspacesRootEnvVar)
			}
			if got != want {
				t.Fatalf("workspaces root = %q, want %q", got, want)
			}
		})
	}

	t.Run("fixture defaults land and ambient values lose", func(t *testing.T) {
		env := childEnv([]string{
			"HOME=/real/home",
			"TERM=dumb",
			"AMUX_PROFILE=1",
			"KEEP_ME=yes",
		}, home, nil)
		for key, want := range map[string]string{
			"HOME":                     home,
			"TERM":                     "xterm-256color",
			"AMUX_PROFILE":             "0",
			"AMUX_PROFILE_INTERVAL_MS": "0",
			"KEEP_ME":                  "yes",
		} {
			if got, _ := envValue(env, key); got != want {
				t.Fatalf("%s = %q, want %q", key, got, want)
			}
		}
	})

	t.Run("git overrides stay stripped", func(t *testing.T) {
		env := childEnv([]string{
			"GIT_DIR=/x",
			"GIT_WORK_TREE=/y",
			"GIT_INDEX_FILE=/z",
			"PATH=/bin",
		}, home, nil)
		for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
			if _, ok := envValue(env, key); ok {
				t.Fatalf("%s survived strip", key)
			}
		}
		if got, _ := envValue(env, "PATH"); got != "/bin" {
			t.Fatalf("PATH = %q", got)
		}
	})

	t.Run("explicit env wins over fixture defaults", func(t *testing.T) {
		env := childEnv(nil, home, []string{"TERM=screen-256color"})
		if got, _ := envValue(env, "TERM"); got != "screen-256color" {
			t.Fatalf("TERM = %q, want explicit override", got)
		}
	})
}
