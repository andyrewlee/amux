package common

import (
	"strings"
	"testing"
)

func TestSettingsRenderUpdateAvailable(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark, true, false, "", "", "")
	dialog.SetUpdateInfo("v0.0.10", "v0.0.11", true)

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Update to v0.0.11") {
		t.Fatalf("expected update line to be rendered, got:\n%s", joined)
	}
}

func TestSettingsRenderUpdateHiddenWhenUnavailable(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark, true, false, "", "", "")
	dialog.SetUpdateInfo("v0.0.10", "", false)

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Update to") {
		t.Fatalf("expected update line to be hidden, got:\n%s", joined)
	}
}

func TestSettingsRenderHomebrewHint(t *testing.T) {
	dialog := NewSettingsDialog(ThemeAyuDark, true, false, "", "", "")
	dialog.SetUpdateInfo("v0.0.10", "", false)
	dialog.SetUpdateHint("Installed via Homebrew - update with brew upgrade amux")

	lines := dialog.renderLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Installed via Homebrew - update with brew upgrade amux") {
		t.Fatalf("expected Homebrew hint to be rendered, got:\n%s", joined)
	}
}
