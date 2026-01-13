package common

import (
	"fmt"
	"image/color"

	"charm.land/lipgloss/v2"
)

// Tokyo Night-inspired color palette
// Muted, accessible, easy on eyes
var (
	// Base palette
	ColorBackground    = lipgloss.Color("#1a1b26") // Dark blue-gray
	ColorForeground    = lipgloss.Color("#a9b1d6") // Soft lavender-white
	ColorMuted         = lipgloss.Color("#565f89") // Dimmed text
	ColorBorder        = lipgloss.Color("#292e42") // Subtle borders
	ColorBorderFocused = lipgloss.Color("#7aa2f7") // Blue highlight

	// Semantic colors
	ColorPrimary   = lipgloss.Color("#7aa2f7") // Blue - primary actions, focus
	ColorSecondary = lipgloss.Color("#bb9af7") // Purple - secondary elements
	ColorSuccess   = lipgloss.Color("#9ece6a") // Green - clean status
	ColorWarning   = lipgloss.Color("#e0af68") // Yellow - warnings, modified
	ColorError     = lipgloss.Color("#f7768e") // Red - errors, dirty status
	ColorInfo      = lipgloss.Color("#7dcfff") // Cyan - info messages

	// Agent colors (distinct for quick recognition)
	ColorClaude   = lipgloss.Color("#CC785C")
	ColorCodex    = lipgloss.Color("#FFFFFF")
	ColorGemini   = lipgloss.Color("#4285f4")
	ColorAmp      = lipgloss.Color("#ED4C3D")
	ColorOpencode = lipgloss.Color("#000000")
	ColorDroid    = lipgloss.Color("#EE6018")

	// Surface colors for layering
	ColorSurface0 = lipgloss.Color("#1a1b26") // Base background
	ColorSurface1 = lipgloss.Color("#1f2335") // Slightly elevated
	ColorSurface2 = lipgloss.Color("#24283b") // More elevated
	ColorSurface3 = lipgloss.Color("#292e42") // Most elevated

	// Selection/highlight
	ColorSelection = lipgloss.Color("#33467c") // Selection background
	ColorHighlight = lipgloss.Color("#3d59a1") // Highlighted text background
)

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
