package common

import (
	"fmt"
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// themePtr holds the active color theme, protected by atomic access.
var themePtr atomic.Pointer[Theme]

func init() {
	t := GruvboxTheme()
	themePtr.Store(&t)
}

// Theme-dependent color accessors (read from the current theme atomically).

func ColorBackground() color.Color    { return themePtr.Load().Colors.Background }
func ColorForeground() color.Color    { return themePtr.Load().Colors.Foreground }
func ColorMuted() color.Color         { return themePtr.Load().Colors.Muted }
func ColorBorder() color.Color        { return themePtr.Load().Colors.Border }
func ColorBorderFocused() color.Color { return themePtr.Load().Colors.BorderFocused }

func ColorPrimary() color.Color   { return themePtr.Load().Colors.Primary }
func ColorSecondary() color.Color { return themePtr.Load().Colors.Secondary }
func ColorSuccess() color.Color   { return themePtr.Load().Colors.Success }
func ColorWarning() color.Color   { return themePtr.Load().Colors.Warning }
func ColorError() color.Color     { return themePtr.Load().Colors.Error }
func ColorInfo() color.Color      { return themePtr.Load().Colors.Info }

func ColorSurface0() color.Color { return themePtr.Load().Colors.Surface0 }
func ColorSurface1() color.Color { return themePtr.Load().Colors.Surface1 }
func ColorSurface2() color.Color { return themePtr.Load().Colors.Surface2 }
func ColorSurface3() color.Color { return themePtr.Load().Colors.Surface3 }

func ColorSelection() color.Color { return themePtr.Load().Colors.Selection }
func ColorHighlight() color.Color { return themePtr.Load().Colors.Highlight }

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
	return *themePtr.Load()
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
