package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateSBPL(t *testing.T) {
	home, _ := os.UserHomeDir()
	worktreeRoot := "/tmp/test-workspace"
	gitDir := "/tmp/test-repo/.git"
	configDir := home + "/.medusa/profiles/Default"

	sbpl := GenerateSBPL(worktreeRoot, gitDir, configDir)

	t.Run("header", func(t *testing.T) {
		if !strings.HasPrefix(sbpl, "(version 1)\n(deny default)\n") {
			t.Error("profile should start with (version 1) and (deny default)")
		}
	})

	t.Run("global_reads_allowed", func(t *testing.T) {
		if !strings.Contains(sbpl, "(allow file-read*)") {
			t.Error("profile should allow file-read* globally")
		}
	})

	t.Run("sensitive_reads_denied", func(t *testing.T) {
		for _, dir := range []string{".ssh", ".gnupg", ".aws", ".docker", ".kube"} {
			expected := `(deny file-read* (subpath "` + home + "/" + dir + `"))`
			if !strings.Contains(sbpl, expected) {
				t.Errorf("profile should deny reads to %s, want:\n  %s", dir, expected)
			}
		}
	})

	t.Run("dyld_file_map_executable", func(t *testing.T) {
		if !strings.Contains(sbpl, "(allow file-map-executable)") {
			t.Error("profile should allow file-map-executable for dyld")
		}
	})

	t.Run("workspace_write", func(t *testing.T) {
		expected := `(allow file-write* (subpath "` + worktreeRoot + `"))`
		if !strings.Contains(sbpl, expected) {
			t.Errorf("profile should allow writes to worktreeRoot, want:\n  %s", expected)
		}
	})

	t.Run("git_dir_write", func(t *testing.T) {
		expected := `(allow file-write* (subpath "` + gitDir + `"))`
		if !strings.Contains(sbpl, expected) {
			t.Errorf("profile should allow writes to gitDir, want:\n  %s", expected)
		}
	})

	t.Run("config_dir_write", func(t *testing.T) {
		expected := `(allow file-write* (subpath "` + configDir + `"))`
		if !strings.Contains(sbpl, expected) {
			t.Errorf("profile should allow writes to claudeConfigDir, want:\n  %s", expected)
		}
	})

	t.Run("dev_write", func(t *testing.T) {
		if !strings.Contains(sbpl, `(allow file-write* (regex #"^/dev/"))`) {
			t.Error("profile should allow writes to /dev/")
		}
	})

	t.Run("npm_cache_write", func(t *testing.T) {
		expected := `(allow file-write* (subpath "` + home + `/.npm"))`
		if !strings.Contains(sbpl, expected) {
			t.Errorf("profile should allow writes to ~/.npm for MCP servers, want:\n  %s", expected)
		}
	})

	t.Run("temp_writes", func(t *testing.T) {
		if !strings.Contains(sbpl, `(allow file-write* (subpath "/private/tmp"))`) {
			t.Error("profile should allow writes to /private/tmp")
		}
		if !strings.Contains(sbpl, `(allow file-write* (subpath "/private/var/folders"))`) {
			t.Error("profile should allow writes to /private/var/folders")
		}
	})

	t.Run("terminal_ioctl", func(t *testing.T) {
		if !strings.Contains(sbpl, "(allow file-ioctl)") {
			t.Error("profile should allow file-ioctl for terminal raw mode")
		}
	})

	t.Run("process_execution", func(t *testing.T) {
		for _, op := range []string{"(allow process-exec)", "(allow process-fork)", "(allow process-info*)", "(allow signal)"} {
			if !strings.Contains(sbpl, op) {
				t.Errorf("profile should contain %s", op)
			}
		}
	})

	t.Run("system_operations", func(t *testing.T) {
		for _, op := range []string{"(allow sysctl-read)", "(allow mach-lookup)", "(allow ipc-posix-shm*)"} {
			if !strings.Contains(sbpl, op) {
				t.Errorf("profile should contain %s", op)
			}
		}
	})

	t.Run("network", func(t *testing.T) {
		if !strings.Contains(sbpl, "(allow network*)") {
			t.Error("profile should allow network*")
		}
	})
}

func TestGenerateSBPL_EmptyGitDir(t *testing.T) {
	sbpl := GenerateSBPL("/tmp/ws", "", "/tmp/config")

	if strings.Contains(sbpl, "git internals") {
		t.Error("profile should omit git dir section when gitDir is empty")
	}
}

func TestGenerateSBPL_EmptyConfigDir(t *testing.T) {
	sbpl := GenerateSBPL("/tmp/ws", "/tmp/.git", "")

	if strings.Contains(sbpl, "Claude config dir") {
		t.Error("profile should omit config dir section when claudeConfigDir is empty")
	}
}

func TestGenerateSBPL_NoClaudeHomePaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	sbpl := GenerateSBPL("/tmp/ws", "/tmp/.git", "/tmp/config")

	if strings.Contains(sbpl, home+"/.claude") {
		t.Error("profile should not reference ~/.claude")
	}
	if strings.Contains(sbpl, home+"/.claude.json") {
		t.Error("profile should not reference ~/.claude.json")
	}
}

func TestWrapCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		sbpl    string
		want    string
	}{
		{
			name:    "basic",
			command: "echo hello",
			sbpl:    "/tmp/profile.sb",
			want:    "sandbox-exec -f '/tmp/profile.sb' sh -lc 'echo hello'",
		},
		{
			name:    "command_with_single_quote",
			command: "echo it's working",
			sbpl:    "/tmp/profile.sb",
			want:    `sandbox-exec -f '/tmp/profile.sb' sh -lc 'echo it'\''s working'`,
		},
		{
			name:    "path_with_spaces",
			command: "cat file.txt",
			sbpl:    "/tmp/my profile.sb",
			want:    "sandbox-exec -f '/tmp/my profile.sb' sh -lc 'cat file.txt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrapCommand(tt.command, tt.sbpl)
			if got != tt.want {
				t.Errorf("WrapCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteTempProfile(t *testing.T) {
	content := "(version 1)\n(deny default)\n"

	path, cleanup, err := WriteTempProfile(content)
	if err != nil {
		t.Fatalf("WriteTempProfile() error = %v", err)
	}

	t.Run("file_exists", func(t *testing.T) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("temp profile file should exist: %v", err)
		}
	})

	t.Run("has_sb_extension", func(t *testing.T) {
		if filepath.Ext(path) != ".sb" {
			t.Errorf("temp profile should have .sb extension, got %q", filepath.Ext(path))
		}
	})

	t.Run("correct_content", func(t *testing.T) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read temp profile: %v", err)
		}
		if string(data) != content {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", string(data), content)
		}
	})

	t.Run("cleanup_removes_file", func(t *testing.T) {
		cleanup()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("cleanup should remove the temp profile file")
		}
	})
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", "'hello'"},
		{"with_single_quote", "it's", `'it'\''s'`},
		{"with_spaces", "hello world", "'hello world'"},
		{"empty", "", "''"},
		{"with_double_quotes", `say "hi"`, `'say "hi"'`},
		{"with_special_chars", "a&b|c;d", "'a&b|c;d'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
