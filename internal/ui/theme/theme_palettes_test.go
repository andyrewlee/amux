package theme

import "testing"

// TestThemePalettesComplete locks the palette table: every built-in theme has
// a unique ID, a display name, and a full set of colors. Spot checks pin a
// few well-known hex values so an accidental table edit is caught.
func TestThemePalettesComplete(t *testing.T) {
	themes := AvailableThemes()
	if len(themes) != 19 {
		t.Fatalf("expected 19 built-in themes, got %d", len(themes))
	}
	seen := map[ThemeID]bool{}
	for _, th := range themes {
		if th.ID == "" || th.Name == "" {
			t.Fatalf("theme missing identity: %+v", th)
		}
		if seen[th.ID] {
			t.Fatalf("duplicate theme ID %s", th.ID)
		}
		seen[th.ID] = true
		c := th.Colors
		for name, col := range map[string]interface{ RGBA() (r, g, b, a uint32) }{
			"Background": c.Background, "Foreground": c.Foreground, "Muted": c.Muted,
			"Border": c.Border, "BorderFocused": c.BorderFocused, "Primary": c.Primary,
			"Secondary": c.Secondary, "Success": c.Success, "Warning": c.Warning,
			"Error": c.Error, "Info": c.Info, "Surface0": c.Surface0, "Surface1": c.Surface1,
			"Surface2": c.Surface2, "Surface3": c.Surface3, "Selection": c.Selection,
			"Highlight": c.Highlight,
		} {
			if col == nil {
				t.Fatalf("theme %s: color %s is nil", th.ID, name)
			}
		}
	}

	if got := HexColor(GetTheme(ThemeTokyoNight).Colors.Primary); got != "#7aa2f7" {
		t.Fatalf("tokyo-night primary = %s, want #7aa2f7", got)
	}
	if got := HexColor(GetTheme(ThemeDracula).Colors.Background); got != "#282a36" {
		t.Fatalf("dracula background = %s, want #282a36", got)
	}
	if got := HexColor(GetTheme("not-a-theme").Colors.Background); got != HexColor(GetTheme(ThemeGruvbox).Colors.Background) {
		t.Fatal("unknown theme must fall back to gruvbox")
	}
}

func TestInitDoesNotOverwriteSelectedTheme(t *testing.T) {
	prev := GetCurrentTheme().ID
	defer SetCurrentTheme(prev)

	SetCurrentTheme(ThemeTokyoNight)
	Init()

	if got := GetCurrentTheme().ID; got != ThemeTokyoNight {
		t.Fatalf("current theme after Init() = %q, want %q", got, ThemeTokyoNight)
	}
}
