// Package common re-exports the internal/ui/theme symbols so existing
// common.* references keep working after theme was split into its own
// package. New code should import internal/ui/theme directly.
//
// DECISION (2026-09-22, plans/021): this shim is permanent API surface, not
// a migration waypoint. Wholesale migration of the ~35 files using common.*
// theme symbols was evaluated and rejected: aliases are free at runtime,
// internal/ui/theme is actively evolving (agent-roster commits), and the
// churn-vs-value trade favors keeping both paths. Convention for new code:
// prefer importing internal/ui/theme directly, but common.* remains stable.
package common

import "github.com/andyrewlee/amux/internal/ui/theme"

type (
	Styles      = theme.Styles
	Theme       = theme.Theme
	ThemeColors = theme.ThemeColors
	ThemeID     = theme.ThemeID
)

const (
	ThemeTokyoNight      = theme.ThemeTokyoNight
	ThemeDracula         = theme.ThemeDracula
	ThemeNord            = theme.ThemeNord
	ThemeCatppuccin      = theme.ThemeCatppuccin
	ThemeGruvbox         = theme.ThemeGruvbox
	ThemeSolarized       = theme.ThemeSolarized
	ThemeMonokai         = theme.ThemeMonokai
	ThemeRosePine        = theme.ThemeRosePine
	ThemeOneDark         = theme.ThemeOneDark
	ThemeKanagawa        = theme.ThemeKanagawa
	ThemeEverforest      = theme.ThemeEverforest
	ThemeAyuDark         = theme.ThemeAyuDark
	ThemeGitHubDark      = theme.ThemeGitHubDark
	ThemeSolarizedLight  = theme.ThemeSolarizedLight
	ThemeGitHubLight     = theme.ThemeGitHubLight
	ThemeCatppuccinLatte = theme.ThemeCatppuccinLatte
	ThemeOneLight        = theme.ThemeOneLight
	ThemeGruvboxLight    = theme.ThemeGruvboxLight
	ThemeRosePineDawn    = theme.ThemeRosePineDawn
)

var (
	AgentColor         = theme.AgentColor
	AvailableThemes    = theme.AvailableThemes
	ColorBackground    = theme.ColorBackground
	ColorBorder        = theme.ColorBorder
	ColorBorderFocused = theme.ColorBorderFocused
	ColorError         = theme.ColorError
	ColorForeground    = theme.ColorForeground
	ColorInfo          = theme.ColorInfo
	ColorMuted         = theme.ColorMuted
	ColorPrimary       = theme.ColorPrimary
	ColorSecondary     = theme.ColorSecondary
	ColorSelection     = theme.ColorSelection
	ColorSuccess       = theme.ColorSuccess
	ColorSurface0      = theme.ColorSurface0
	ColorSurface1      = theme.ColorSurface1
	ColorSurface2      = theme.ColorSurface2
	ColorWarning       = theme.ColorWarning
	DefaultStyles      = theme.DefaultStyles
	GetCurrentTheme    = theme.GetCurrentTheme
	GetTheme           = theme.GetTheme
	HexColor           = theme.HexColor
	SetCurrentTheme    = theme.SetCurrentTheme
	SpinnerFrame       = theme.SpinnerFrame
	Icons              = theme.Icons
	ColorClaude        = theme.ColorClaude
	ColorCodex         = theme.ColorCodex
	ColorOpencode      = theme.ColorOpencode
	ColorDroid         = theme.ColorDroid
	ColorCursor        = theme.ColorCursor
	ColorPi            = theme.ColorPi
	ColorAntigravity   = theme.ColorAntigravity
	ColorFx            = theme.ColorFx
	ColorGrok          = theme.ColorGrok
	ColorAmp           = theme.ColorAmp
	ColorCline         = theme.ColorCline
	ColorOmp           = theme.ColorOmp
	ColorDevin         = theme.ColorDevin
	ColorPrimeAgent    = theme.ColorPrimeAgent
)
