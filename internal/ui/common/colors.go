package common

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// currentTheme holds the active color theme.
var currentTheme = GruvboxTheme()

// Theme-dependent colors (updated by SetCurrentTheme)
var (
	// Base palette
	ColorBackground    = currentTheme.Colors.Background
	ColorForeground    = currentTheme.Colors.Foreground
	ColorMuted         = currentTheme.Colors.Muted
	ColorBorder        = currentTheme.Colors.Border
	ColorBorderFocused = currentTheme.Colors.BorderFocused

	// Semantic colors
	ColorPrimary   = currentTheme.Colors.Primary
	ColorSecondary = currentTheme.Colors.Secondary
	ColorSuccess   = currentTheme.Colors.Success
	ColorWarning   = currentTheme.Colors.Warning
	ColorError     = currentTheme.Colors.Error
	ColorInfo      = currentTheme.Colors.Info

	// Surface colors for layering
	ColorSurface0 = currentTheme.Colors.Surface0
	ColorSurface1 = currentTheme.Colors.Surface1
	ColorSurface2 = currentTheme.Colors.Surface2
	ColorSurface3 = currentTheme.Colors.Surface3

	// Selection/highlight
	ColorSelection = currentTheme.Colors.Selection
	ColorHighlight = currentTheme.Colors.Highlight
)

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
	ColorOpenclaw = lipgloss.Color("#14b8a6")
)

// GetCurrentTheme returns the currently active theme.
func GetCurrentTheme() Theme {
	return currentTheme
}

// SetCurrentTheme applies a new theme and updates all color variables.
func SetCurrentTheme(id ThemeID) {
	currentTheme = GetTheme(id)
	applyThemeColors()
}

// applyThemeColors updates all color variables from the current theme.
func applyThemeColors() {
	ColorBackground = currentTheme.Colors.Background
	ColorForeground = currentTheme.Colors.Foreground
	ColorMuted = currentTheme.Colors.Muted
	ColorBorder = currentTheme.Colors.Border
	ColorBorderFocused = currentTheme.Colors.BorderFocused

	ColorPrimary = currentTheme.Colors.Primary
	ColorSecondary = currentTheme.Colors.Secondary
	ColorSuccess = currentTheme.Colors.Success
	ColorWarning = currentTheme.Colors.Warning
	ColorError = currentTheme.Colors.Error
	ColorInfo = currentTheme.Colors.Info

	ColorSurface0 = currentTheme.Colors.Surface0
	ColorSurface1 = currentTheme.Colors.Surface1
	ColorSurface2 = currentTheme.Colors.Surface2
	ColorSurface3 = currentTheme.Colors.Surface3

	ColorSelection = currentTheme.Colors.Selection
	ColorHighlight = currentTheme.Colors.Highlight
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
	case "openclaw":
		return ColorOpenclaw
	default:
		return ColorPrimary
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
