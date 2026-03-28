package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type capturingUploadSandbox struct {
	*MockRemoteSandbox
	uploaded map[string][]byte
}

func newCapturingUploadSandbox(id string) *capturingUploadSandbox {
	return &capturingUploadSandbox{
		MockRemoteSandbox: NewMockRemoteSandbox(id),
		uploaded:          make(map[string][]byte),
	}
}

func (s *capturingUploadSandbox) UploadFile(ctx context.Context, src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	s.uploaded[dst] = data
	return s.MockRemoteSandbox.UploadFile(ctx, src, dst)
}

func TestSyncSettingsToVolumeHonorsExplicitCodexPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(localPath, []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sb := newCapturingUploadSandbox("sb-codex")
	sb.SetExecResult("sh -lc", "/home/testuser", 0)

	err := SyncSettingsToVolume(sb, SettingsSyncConfig{
		Enabled: true,
		Files:   []string{"~/.codex/config.toml"},
	}, false)
	if err != nil {
		t.Fatalf("SyncSettingsToVolume() error = %v", err)
	}

	uploads := sb.GetUploadHistory()
	if len(uploads) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploads))
	}
	if got, want := uploads[0].Dest, "/home/testuser/.config/codex/config.toml"; got != want {
		t.Fatalf("upload destination = %q, want %q", got, want)
	}
	content := string(sb.uploaded["/home/testuser/.config/codex/config.toml"])
	if !strings.Contains(content, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("uploaded content = %q, want cli_auth_credentials_store preserved", content)
	}
}

func TestSyncSettingsToVolumePreservesCodexFileStoreWithLegacyFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(localPath, []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sb := newCapturingUploadSandbox("sb-codex-legacy")
	sb.SetExecResult("sh -lc", "/home/testuser", 0)

	err := SyncSettingsToVolume(sb, SettingsSyncConfig{
		Enabled: true,
		Codex:   true,
	}, false)
	if err != nil {
		t.Fatalf("SyncSettingsToVolume() error = %v", err)
	}

	content := string(sb.uploaded["/home/testuser/.config/codex/config.toml"])
	if !strings.Contains(content, `cli_auth_credentials_store = "file"`) {
		t.Fatalf("uploaded content = %q, want cli_auth_credentials_store preserved", content)
	}
}

func TestSyncSettingsToVolumeInsertsCodexFileStoreAtRootScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	content := "model = \"gpt-5\"\n[profiles.default]\nprofile = \"default\"\n"
	if err := os.WriteFile(localPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sb := newCapturingUploadSandbox("sb-codex-root-scope")
	sb.SetExecResult("sh -lc", "/home/testuser", 0)

	err := SyncSettingsToVolume(sb, SettingsSyncConfig{
		Enabled: true,
		Files:   []string{"~/.codex/config.toml"},
	}, false)
	if err != nil {
		t.Fatalf("SyncSettingsToVolume() error = %v", err)
	}

	uploaded := string(sb.uploaded["/home/testuser/.config/codex/config.toml"])
	settingIndex := strings.Index(uploaded, `cli_auth_credentials_store = "file"`)
	tableIndex := strings.Index(uploaded, "[profiles.default]")
	if settingIndex < 0 {
		t.Fatalf("uploaded content = %q, want cli_auth_credentials_store setting", uploaded)
	}
	if tableIndex < 0 {
		t.Fatalf("uploaded content = %q, want table header", uploaded)
	}
	if settingIndex > tableIndex {
		t.Fatalf("uploaded content = %q, want cli_auth_credentials_store before first table", uploaded)
	}
}

func TestEnsureCodexFileStoreSettingTreatsCommentedTableAsRootBoundary(t *testing.T) {
	content := "model = \"gpt-5\"\n[profiles.default] # work\nprofile = \"default\"\n"
	updated := string(ensureCodexFileStoreSetting([]byte(content)))
	settingIndex := strings.Index(updated, `cli_auth_credentials_store = "file"`)
	tableIndex := strings.Index(updated, "[profiles.default]")
	if settingIndex < 0 {
		t.Fatalf("updated content = %q, want cli_auth_credentials_store setting", updated)
	}
	if tableIndex < 0 {
		t.Fatalf("updated content = %q, want table header", updated)
	}
	if settingIndex > tableIndex {
		t.Fatalf("updated content = %q, want cli_auth_credentials_store before commented table", updated)
	}
}

func TestFilterSensitiveJSONPreservesNonSecretKeyNames(t *testing.T) {
	input := `{"hotkeyMode":"vim","keyboard":"us","primaryKey":"id","apiKey":"secret","key":"sk-live-xxx","nested":{"private_key":"x","private":"rsa-data","value":1}}`
	out, err := filterSensitiveJSON([]byte(input))
	if err != nil {
		t.Fatalf("filterSensitiveJSON() error = %v", err)
	}
	content := string(out)
	for _, safe := range []string{"hotkeyMode", "keyboard", "primaryKey"} {
		if !strings.Contains(content, safe) {
			t.Fatalf("expected %q to survive filtering, got: %s", safe, content)
		}
	}
	for _, secret := range []string{"apiKey", "private_key"} {
		if strings.Contains(content, secret) {
			t.Fatalf("expected %q to be filtered, got: %s", secret, content)
		}
	}
	// Bare "key" and "private" should be caught by exact-match filtering
	if strings.Contains(content, "sk-live-xxx") {
		t.Fatalf("expected bare \"key\" to be filtered, got: %s", content)
	}
	if strings.Contains(content, "rsa-data") {
		t.Fatalf("expected bare \"private\" to be filtered, got: %s", content)
	}
}

func TestFilterSensitiveJSONFiltersCompoundSecretKeyNames(t *testing.T) {
	input := `{"licenseKey":"abc","service_key":"def","signing-key":"ghi","primaryKey":"id","keyboard":"us"}`
	out, err := filterSensitiveJSON([]byte(input))
	if err != nil {
		t.Fatalf("filterSensitiveJSON() error = %v", err)
	}
	content := string(out)
	for _, secret := range []string{"licenseKey", "service_key", "signing-key", "abc", "def", "ghi"} {
		if strings.Contains(content, secret) {
			t.Fatalf("expected %q to be filtered, got: %s", secret, content)
		}
	}
	for _, safe := range []string{"primaryKey", "keyboard"} {
		if !strings.Contains(content, safe) {
			t.Fatalf("expected %q to survive filtering, got: %s", safe, content)
		}
	}
}

func TestAgentFromPathComponentMatching(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"~/.config/amp/config.json", "amp"},
		{"/home/user/amplifier/settings.json", "unknown"},
		{"~/.config/opencode/config.json", "opencode"},
		{"~/.codex/config.toml", "codex"},
		{"/path/to/codex/file.toml", "codex"},
		{"~/.claude/settings.json", "claude"},
		{"~/.gitconfig", "git"},
		{"~/.gemini/settings.json", "gemini"},
	}
	for _, tt := range tests {
		if got := agentFromPath(tt.path); got != tt.want {
			t.Errorf("agentFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSyncSettingsToVolumeHonorsExplicitJSONPathAndFiltersSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	localPath := filepath.Join(home, ".config", "opencode", "config.json")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(localPath, []byte(`{"theme":"dark","apiKey":"secret-value"}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	sb := newCapturingUploadSandbox("sb-opencode")
	sb.SetExecResult("sh -lc", "/home/testuser", 0)

	err := SyncSettingsToVolume(sb, SettingsSyncConfig{
		Enabled: true,
		Files:   []string{"~/.config/opencode/config.json"},
	}, false)
	if err != nil {
		t.Fatalf("SyncSettingsToVolume() error = %v", err)
	}

	dest := "/home/testuser/.config/opencode/config.json"
	uploads := sb.GetUploadHistory()
	if len(uploads) != 1 {
		t.Fatalf("upload count = %d, want 1", len(uploads))
	}
	if got := uploads[0].Dest; got != dest {
		t.Fatalf("upload destination = %q, want %q", got, dest)
	}
	content := string(sb.uploaded[dest])
	if strings.Contains(content, "secret-value") || strings.Contains(strings.ToLower(content), "apikey") {
		t.Fatalf("uploaded content = %q, expected filtered JSON", content)
	}
	if !strings.Contains(content, "\"theme\": \"dark\"") {
		t.Fatalf("uploaded content = %q, expected non-secret settings to remain", content)
	}
}

func TestFilterSensitiveJSONRecursesIntoArrays(t *testing.T) {
	input := `{"profiles":[{"name":"default","apiKey":"secret"},{"nested":[{"token":"secret-token","theme":"dark"}]}]}`
	out, err := filterSensitiveJSON([]byte(input))
	if err != nil {
		t.Fatalf("filterSensitiveJSON() error = %v", err)
	}
	content := string(out)
	for _, secret := range []string{"secret", "secret-token", "apiKey", "token"} {
		if strings.Contains(content, secret) {
			t.Fatalf("expected %q to be filtered from array payloads, got: %s", secret, content)
		}
	}
	if !strings.Contains(content, "\"name\": \"default\"") || !strings.Contains(content, "\"theme\": \"dark\"") {
		t.Fatalf("expected non-secret array values to remain, got: %s", content)
	}
}
