package theme

import (
	"fmt"
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// themePtr holds the active color theme, protected by atomic access.
var themePtr atomic.Pointer[Theme]

// Init installs the default theme. The app calls this explicitly during
// construction (rather than relying on package init side effects); direct
// library/test use is still safe because currentTheme falls back lazily.
func Init() {
	t := GetTheme(ThemeGruvbox)
	themePtr.Store(&t)
}

// currentTheme returns the active theme, installing the default on first use
// when Init was not called.
func currentTheme() *Theme {
	if t := themePtr.Load(); t != nil {
		return t
	}
	t := GetTheme(ThemeGruvbox)
	themePtr.CompareAndSwap(nil, &t)
	return themePtr.Load()
}

// Theme-dependent color accessors (read from the current theme atomically).

func ColorBackground() color.Color    { return currentTheme().Colors.Background }
func ColorForeground() color.Color    { return currentTheme().Colors.Foreground }
func ColorMuted() color.Color         { return currentTheme().Colors.Muted }
func ColorBorder() color.Color        { return currentTheme().Colors.Border }
func ColorBorderFocused() color.Color { return currentTheme().Colors.BorderFocused }

func ColorPrimary() color.Color   { return currentTheme().Colors.Primary }
func ColorSecondary() color.Color { return currentTheme().Colors.Secondary }
func ColorSuccess() color.Color   { return currentTheme().Colors.Success }
func ColorWarning() color.Color   { return currentTheme().Colors.Warning }
func ColorError() color.Color     { return currentTheme().Colors.Error }
func ColorInfo() color.Color      { return currentTheme().Colors.Info }

func ColorSurface0() color.Color { return currentTheme().Colors.Surface0 }
func ColorSurface1() color.Color { return currentTheme().Colors.Surface1 }
func ColorSurface2() color.Color { return currentTheme().Colors.Surface2 }

func ColorSelection() color.Color { return currentTheme().Colors.Selection }

// Agent colors remain constant across themes for brand recognition.
var (
	ColorClaude   = lipgloss.Color("#CC785C")
	ColorCodex    = lipgloss.Color("#FFFFFF")
	ColorGemini   = lipgloss.Color("#4285f4")
	ColorAmp      = lipgloss.Color("#ED4C3D")
	ColorOpencode = lipgloss.Color("#000000")
	ColorDroid    = lipgloss.Color("#EE6018")
	ColorCline    = lipgloss.Color("#101827")
	ColorCursor   = lipgloss.Color("#1B1812")
	ColorPi       = lipgloss.Color("#0e0e11")
)

// GetCurrentTheme returns the currently active theme.
func GetCurrentTheme() Theme {
	return *currentTheme()
}

// SetCurrentTheme atomically applies a new theme.
func SetCurrentTheme(id ThemeID) {
	t := GetTheme(id)
	themePtr.Store(&t)
}

// AgentColor returns the color for a given agent type
func AgentColor(agent string) color.Color {
	switch agent {
	case "claude":
		return ColorClaude
	case "codex":
		return ColorCodex
	case "gemini":
		return ColorGemini
	case "amp":
		return ColorAmp
	case "opencode":
		return ColorOpencode
	case "droid":
		return ColorDroid
	case "cline":
		return ColorCline
	case "cursor":
		return ColorCursor
	case "pi":
		return ColorPi
	default:
		return ColorPrimary()
	}
}

// HexColor converts a color.Color into a #RRGGBB string.
func HexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
