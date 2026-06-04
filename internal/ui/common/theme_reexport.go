// Package common re-exports the internal/ui/theme symbols so existing
// common.* references keep working after theme was split into its own
// package. New code should import internal/ui/theme directly.
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
	AgentColor           = theme.AgentColor
	AvailableThemes      = theme.AvailableThemes
	AyuDarkTheme         = theme.AyuDarkTheme
	CatppuccinLatteTheme = theme.CatppuccinLatteTheme
	CatppuccinTheme      = theme.CatppuccinTheme
	ColorBackground      = theme.ColorBackground
	ColorBorder          = theme.ColorBorder
	ColorBorderFocused   = theme.ColorBorderFocused
	ColorError           = theme.ColorError
	ColorForeground      = theme.ColorForeground
	ColorInfo            = theme.ColorInfo
	ColorMuted           = theme.ColorMuted
	ColorPrimary         = theme.ColorPrimary
	ColorSecondary       = theme.ColorSecondary
	ColorSelection       = theme.ColorSelection
	ColorSuccess         = theme.ColorSuccess
	ColorSurface0        = theme.ColorSurface0
	ColorSurface1        = theme.ColorSurface1
	ColorSurface2        = theme.ColorSurface2
	ColorWarning         = theme.ColorWarning
	DefaultStyles        = theme.DefaultStyles
	DraculaTheme         = theme.DraculaTheme
	EverforestTheme      = theme.EverforestTheme
	GetCurrentTheme      = theme.GetCurrentTheme
	GetTheme             = theme.GetTheme
	GitHubDarkTheme      = theme.GitHubDarkTheme
	GitHubLightTheme     = theme.GitHubLightTheme
	GruvboxLightTheme    = theme.GruvboxLightTheme
	GruvboxTheme         = theme.GruvboxTheme
	HexColor             = theme.HexColor
	KanagawaTheme        = theme.KanagawaTheme
	MonokaiTheme         = theme.MonokaiTheme
	NordTheme            = theme.NordTheme
	OneDarkTheme         = theme.OneDarkTheme
	OneLightTheme        = theme.OneLightTheme
	RosePineDawnTheme    = theme.RosePineDawnTheme
	RosePineTheme        = theme.RosePineTheme
	SetCurrentTheme      = theme.SetCurrentTheme
	SolarizedLightTheme  = theme.SolarizedLightTheme
	SolarizedTheme       = theme.SolarizedTheme
	SpinnerFrame         = theme.SpinnerFrame
	TokyoNightTheme      = theme.TokyoNightTheme
	Icons                = theme.Icons
	ColorClaude          = theme.ColorClaude
	ColorCodex           = theme.ColorCodex
	ColorGemini          = theme.ColorGemini
	ColorAmp             = theme.ColorAmp
	ColorOpencode        = theme.ColorOpencode
	ColorDroid           = theme.ColorDroid
	ColorCline           = theme.ColorCline
	ColorCursor          = theme.ColorCursor
	ColorPi              = theme.ColorPi
)
