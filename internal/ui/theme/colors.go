package theme

import (
	"fmt"
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"

	"github.com/andyrewlee/amux/internal/config"
)

// themePtr holds the active color theme, protected by atomic access.
var themePtr atomic.Pointer[Theme]

// Init installs the default theme if no theme has been selected yet. The app
// calls this explicitly during construction (rather than relying on package
// init side effects); direct library/test use is still safe because
// currentTheme falls back lazily.
func Init() {
	t := GetTheme(ThemeGruvbox)
	themePtr.CompareAndSwap(nil, &t)
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

// agentColors maps canonical agent names to their brand palette color. Lookups
// are gated by config.IsRegisteredAgent so the roster stays in lockstep with
// the canonical registry; any unmapped registry name falls back to ColorPrimary.
var agentColors = map[string]color.Color{
	"claude":   ColorClaude,
	"codex":    ColorCodex,
	"gemini":   ColorGemini,
	"amp":      ColorAmp,
	"opencode": ColorOpencode,
	"droid":    ColorDroid,
	"cline":    ColorCline,
	"cursor":   ColorCursor,
	"pi":       ColorPi,
}

// GetCurrentTheme returns the currently active theme.
func GetCurrentTheme() Theme {
	return *currentTheme()
}

// SetCurrentTheme atomically applies a new theme.
func SetCurrentTheme(id ThemeID) {
	t := GetTheme(id)
	themePtr.Store(&t)
}

// AgentColor returns the brand color for a registered agent, falling back to
// ColorPrimary for unknown agents. Membership is resolved via the canonical
// registry so the supported roster stays in sync with config and the chat tab.
func AgentColor(agent string) color.Color {
	if config.IsRegisteredAgent(agent) {
		if c, ok := agentColors[agent]; ok {
			return c
		}
	}
	return ColorPrimary()
}

// HexColor converts a color.Color into a #RRGGBB string.
func HexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
