package common

import (
	"image/color"
)

// ThemeID identifies a color theme.
type ThemeID string

const (
	// Dark themes
	ThemeTokyoNight ThemeID = "tokyo-night"
	ThemeDracula    ThemeID = "dracula"
	ThemeNord       ThemeID = "nord"
	ThemeCatppuccin ThemeID = "catppuccin"
	ThemeGruvbox    ThemeID = "gruvbox"
	ThemeSolarized  ThemeID = "solarized"
	ThemeMonokai    ThemeID = "monokai"
	ThemeRosePine   ThemeID = "rose-pine"
	ThemeOneDark    ThemeID = "one-dark"
	ThemeKanagawa   ThemeID = "kanagawa"
	ThemeEverforest ThemeID = "everforest"
	ThemeAyuDark    ThemeID = "ayu-dark"
	ThemeGitHubDark ThemeID = "github-dark"

	// Light themes
	ThemeSolarizedLight  ThemeID = "solarized-light"
	ThemeGitHubLight     ThemeID = "github-light"
	ThemeCatppuccinLatte ThemeID = "catppuccin-latte"
	ThemeOneLight        ThemeID = "one-light"
	ThemeGruvboxLight    ThemeID = "gruvbox-light"
	ThemeRosePineDawn    ThemeID = "rose-pine-dawn"
)

// ThemeColors defines all colors used by the application.
type ThemeColors struct {
	// Base palette
	Background    color.Color
	Foreground    color.Color
	Muted         color.Color
	Border        color.Color
	BorderFocused color.Color

	// Semantic colors
	Primary   color.Color
	Secondary color.Color
	Success   color.Color
	Warning   color.Color
	Error     color.Color
	Info      color.Color

	// Surface colors for layering
	Surface0 color.Color
	Surface1 color.Color
	Surface2 color.Color
	Surface3 color.Color

	// Selection/highlight
	Selection color.Color
	Highlight color.Color
}

// Theme represents a complete color theme.
type Theme struct {
	ID     ThemeID
	Name   string
	Colors ThemeColors
}

// AvailableThemes returns all predefined themes, grouped by family.
func AvailableThemes() []Theme {
	return []Theme{
		// Gruvbox family (default)
		GruvboxTheme(),
		GruvboxLightTheme(),
		// Tokyo Night
		TokyoNightTheme(),
		// Catppuccin family
		CatppuccinTheme(),
		CatppuccinLatteTheme(),
		// Rosé Pine family
		RosePineTheme(),
		RosePineDawnTheme(),
		// Solarized family
		SolarizedTheme(),
		SolarizedLightTheme(),
		// One family (Atom)
		OneDarkTheme(),
		OneLightTheme(),
		// GitHub family
		GitHubDarkTheme(),
		GitHubLightTheme(),
		// Standalone dark themes
		DraculaTheme(),
		NordTheme(),
		MonokaiTheme(),
		KanagawaTheme(),
		EverforestTheme(),
		AyuDarkTheme(),
	}
}

// GetTheme returns a theme by ID, defaulting to Gruvbox.
func GetTheme(id ThemeID) Theme {
	for _, t := range AvailableThemes() {
		if t.ID == id {
			return t
		}
	}
	return GruvboxTheme()
}
