package common

import (
	"image/color"

	"charm.land/lipgloss/v2"
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

// TokyoNightTheme - cool blue tones
func TokyoNightTheme() Theme {
	return Theme{
		ID:   ThemeTokyoNight,
		Name: "Tokyo Night",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#1a1b26"),
			Foreground:    lipgloss.Color("#a9b1d6"),
			Muted:         lipgloss.Color("#565f89"),
			Border:        lipgloss.Color("#292e42"),
			BorderFocused: lipgloss.Color("#7aa2f7"),

			Primary:   lipgloss.Color("#7aa2f7"),
			Secondary: lipgloss.Color("#bb9af7"),
			Success:   lipgloss.Color("#9ece6a"),
			Warning:   lipgloss.Color("#e0af68"),
			Error:     lipgloss.Color("#f7768e"),
			Info:      lipgloss.Color("#7dcfff"),

			Surface0: lipgloss.Color("#1a1b26"),
			Surface1: lipgloss.Color("#1f2335"),
			Surface2: lipgloss.Color("#24283b"),
			Surface3: lipgloss.Color("#292e42"),

			Selection: lipgloss.Color("#33467c"),
			Highlight: lipgloss.Color("#3d59a1"),
		},
	}
}

// DraculaTheme - purple/pink accents
func DraculaTheme() Theme {
	return Theme{
		ID:   ThemeDracula,
		Name: "Dracula",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#282a36"),
			Foreground:    lipgloss.Color("#f8f8f2"),
			Muted:         lipgloss.Color("#6272a4"),
			Border:        lipgloss.Color("#44475a"),
			BorderFocused: lipgloss.Color("#bd93f9"),

			Primary:   lipgloss.Color("#bd93f9"),
			Secondary: lipgloss.Color("#ff79c6"),
			Success:   lipgloss.Color("#50fa7b"),
			Warning:   lipgloss.Color("#f1fa8c"),
			Error:     lipgloss.Color("#ff5555"),
			Info:      lipgloss.Color("#8be9fd"),

			Surface0: lipgloss.Color("#282a36"),
			Surface1: lipgloss.Color("#2d303e"),
			Surface2: lipgloss.Color("#343746"),
			Surface3: lipgloss.Color("#44475a"),

			Selection: lipgloss.Color("#44475a"),
			Highlight: lipgloss.Color("#6272a4"),
		},
	}
}

// NordTheme - cool, muted arctic colors
func NordTheme() Theme {
	return Theme{
		ID:   ThemeNord,
		Name: "Nord",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#2e3440"),
			Foreground:    lipgloss.Color("#eceff4"),
			Muted:         lipgloss.Color("#4c566a"),
			Border:        lipgloss.Color("#3b4252"),
			BorderFocused: lipgloss.Color("#88c0d0"),

			Primary:   lipgloss.Color("#88c0d0"),
			Secondary: lipgloss.Color("#b48ead"),
			Success:   lipgloss.Color("#a3be8c"),
			Warning:   lipgloss.Color("#ebcb8b"),
			Error:     lipgloss.Color("#bf616a"),
			Info:      lipgloss.Color("#81a1c1"),

			Surface0: lipgloss.Color("#2e3440"),
			Surface1: lipgloss.Color("#3b4252"),
			Surface2: lipgloss.Color("#434c5e"),
			Surface3: lipgloss.Color("#4c566a"),

			Selection: lipgloss.Color("#434c5e"),
			Highlight: lipgloss.Color("#4c566a"),
		},
	}
}

// GruvboxTheme - warm, retro, earthy tones with orange accent
func GruvboxTheme() Theme {
	return Theme{
		ID:   ThemeGruvbox,
		Name: "Gruvbox",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#282828"),
			Foreground:    lipgloss.Color("#ebdbb2"),
			Muted:         lipgloss.Color("#928374"),
			Border:        lipgloss.Color("#3c3836"),
			BorderFocused: lipgloss.Color("#fe8019"),

			Primary:   lipgloss.Color("#fe8019"),
			Secondary: lipgloss.Color("#d3869b"),
			Success:   lipgloss.Color("#b8bb26"),
			Warning:   lipgloss.Color("#fabd2f"),
			Error:     lipgloss.Color("#fb4934"),
			Info:      lipgloss.Color("#83a598"),

			Surface0: lipgloss.Color("#282828"),
			Surface1: lipgloss.Color("#3c3836"),
			Surface2: lipgloss.Color("#504945"),
			Surface3: lipgloss.Color("#665c54"),

			Selection: lipgloss.Color("#504945"),
			Highlight: lipgloss.Color("#665c54"),
		},
	}
}

// RosePineTheme - elegant rose/pink tones
func RosePineTheme() Theme {
	return Theme{
		ID:   ThemeRosePine,
		Name: "Rosé Pine",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#191724"),
			Foreground:    lipgloss.Color("#e0def4"),
			Muted:         lipgloss.Color("#6e6a86"),
			Border:        lipgloss.Color("#26233a"),
			BorderFocused: lipgloss.Color("#ebbcba"),

			Primary:   lipgloss.Color("#ebbcba"),
			Secondary: lipgloss.Color("#c4a7e7"),
			Success:   lipgloss.Color("#9ccfd8"),
			Warning:   lipgloss.Color("#f6c177"),
			Error:     lipgloss.Color("#eb6f92"),
			Info:      lipgloss.Color("#31748f"),

			Surface0: lipgloss.Color("#191724"),
			Surface1: lipgloss.Color("#1f1d2e"),
			Surface2: lipgloss.Color("#26233a"),
			Surface3: lipgloss.Color("#403d52"),

			Selection: lipgloss.Color("#403d52"),
			Highlight: lipgloss.Color("#524f67"),
		},
	}
}

// CatppuccinTheme - pastel with mauve/lavender accent (distinct from Tokyo Night)
func CatppuccinTheme() Theme {
	return Theme{
		ID:   ThemeCatppuccin,
		Name: "Catppuccin",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#1e1e2e"),
			Foreground:    lipgloss.Color("#cdd6f4"),
			Muted:         lipgloss.Color("#6c7086"),
			Border:        lipgloss.Color("#313244"),
			BorderFocused: lipgloss.Color("#cba6f7"),

			Primary:   lipgloss.Color("#cba6f7"), // Mauve instead of blue
			Secondary: lipgloss.Color("#f5c2e7"), // Pink
			Success:   lipgloss.Color("#a6e3a1"),
			Warning:   lipgloss.Color("#f9e2af"),
			Error:     lipgloss.Color("#f38ba8"),
			Info:      lipgloss.Color("#94e2d5"),

			Surface0: lipgloss.Color("#1e1e2e"),
			Surface1: lipgloss.Color("#181825"),
			Surface2: lipgloss.Color("#313244"),
			Surface3: lipgloss.Color("#45475a"),

			Selection: lipgloss.Color("#45475a"),
			Highlight: lipgloss.Color("#585b70"),
		},
	}
}

// MonokaiTheme - classic vibrant colors
func MonokaiTheme() Theme {
	return Theme{
		ID:   ThemeMonokai,
		Name: "Monokai",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#272822"),
			Foreground:    lipgloss.Color("#f8f8f2"),
			Muted:         lipgloss.Color("#75715e"),
			Border:        lipgloss.Color("#3e3d32"),
			BorderFocused: lipgloss.Color("#a6e22e"),

			Primary:   lipgloss.Color("#a6e22e"), // Green
			Secondary: lipgloss.Color("#ae81ff"), // Purple
			Success:   lipgloss.Color("#a6e22e"),
			Warning:   lipgloss.Color("#e6db74"),
			Error:     lipgloss.Color("#f92672"),
			Info:      lipgloss.Color("#66d9ef"),

			Surface0: lipgloss.Color("#272822"),
			Surface1: lipgloss.Color("#3e3d32"),
			Surface2: lipgloss.Color("#49483e"),
			Surface3: lipgloss.Color("#75715e"),

			Selection: lipgloss.Color("#49483e"),
			Highlight: lipgloss.Color("#3e3d32"),
		},
	}
}

// SolarizedTheme - precise, scientific color scheme
func SolarizedTheme() Theme {
	return Theme{
		ID:   ThemeSolarized,
		Name: "Solarized",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#002b36"),
			Foreground:    lipgloss.Color("#839496"),
			Muted:         lipgloss.Color("#586e75"),
			Border:        lipgloss.Color("#073642"),
			BorderFocused: lipgloss.Color("#2aa198"),

			Primary:   lipgloss.Color("#2aa198"), // Cyan
			Secondary: lipgloss.Color("#6c71c4"), // Violet
			Success:   lipgloss.Color("#859900"),
			Warning:   lipgloss.Color("#b58900"),
			Error:     lipgloss.Color("#dc322f"),
			Info:      lipgloss.Color("#268bd2"),

			Surface0: lipgloss.Color("#002b36"),
			Surface1: lipgloss.Color("#073642"),
			Surface2: lipgloss.Color("#094656"),
			Surface3: lipgloss.Color("#586e75"),

			Selection: lipgloss.Color("#094656"),
			Highlight: lipgloss.Color("#073642"),
		},
	}
}

// OneDarkTheme - Atom's signature theme with cyan accent
func OneDarkTheme() Theme {
	return Theme{
		ID:   ThemeOneDark,
		Name: "One Dark",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#282c34"),
			Foreground:    lipgloss.Color("#abb2bf"),
			Muted:         lipgloss.Color("#5c6370"),
			Border:        lipgloss.Color("#3e4451"),
			BorderFocused: lipgloss.Color("#61afef"),

			Primary:   lipgloss.Color("#61afef"), // Blue
			Secondary: lipgloss.Color("#c678dd"), // Magenta
			Success:   lipgloss.Color("#98c379"),
			Warning:   lipgloss.Color("#e5c07b"),
			Error:     lipgloss.Color("#e06c75"),
			Info:      lipgloss.Color("#56b6c2"),

			Surface0: lipgloss.Color("#282c34"),
			Surface1: lipgloss.Color("#2c323c"),
			Surface2: lipgloss.Color("#3e4451"),
			Surface3: lipgloss.Color("#4b5263"),

			Selection: lipgloss.Color("#3e4451"),
			Highlight: lipgloss.Color("#4b5263"),
		},
	}
}

// KanagawaTheme - Japanese ink painting inspired, wave blue
func KanagawaTheme() Theme {
	return Theme{
		ID:   ThemeKanagawa,
		Name: "Kanagawa",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#1f1f28"),
			Foreground:    lipgloss.Color("#dcd7ba"),
			Muted:         lipgloss.Color("#727169"),
			Border:        lipgloss.Color("#2a2a37"),
			BorderFocused: lipgloss.Color("#7e9cd8"),

			Primary:   lipgloss.Color("#7e9cd8"), // Wave blue
			Secondary: lipgloss.Color("#957fb8"), // Spring violet
			Success:   lipgloss.Color("#98bb6c"), // Spring green
			Warning:   lipgloss.Color("#e6c384"), // Autumn yellow
			Error:     lipgloss.Color("#c34043"), // Autumn red
			Info:      lipgloss.Color("#7fb4ca"), // Dragon blue

			Surface0: lipgloss.Color("#1f1f28"),
			Surface1: lipgloss.Color("#2a2a37"),
			Surface2: lipgloss.Color("#363646"),
			Surface3: lipgloss.Color("#54546d"),

			Selection: lipgloss.Color("#2d4f67"),
			Highlight: lipgloss.Color("#363646"),
		},
	}
}

// EverforestTheme - warm green forest tones
func EverforestTheme() Theme {
	return Theme{
		ID:   ThemeEverforest,
		Name: "Everforest",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#2d353b"),
			Foreground:    lipgloss.Color("#d3c6aa"),
			Muted:         lipgloss.Color("#859289"),
			Border:        lipgloss.Color("#3d484d"),
			BorderFocused: lipgloss.Color("#a7c080"),

			Primary:   lipgloss.Color("#a7c080"), // Green
			Secondary: lipgloss.Color("#d699b6"), // Purple
			Success:   lipgloss.Color("#a7c080"),
			Warning:   lipgloss.Color("#dbbc7f"),
			Error:     lipgloss.Color("#e67e80"),
			Info:      lipgloss.Color("#7fbbb3"),

			Surface0: lipgloss.Color("#2d353b"),
			Surface1: lipgloss.Color("#3d484d"),
			Surface2: lipgloss.Color("#475258"),
			Surface3: lipgloss.Color("#505a60"),

			Selection: lipgloss.Color("#475258"),
			Highlight: lipgloss.Color("#3d484d"),
		},
	}
}

// AyuDarkTheme - modern minimal with orange accent
func AyuDarkTheme() Theme {
	return Theme{
		ID:   ThemeAyuDark,
		Name: "Ayu Dark",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#0a0e14"),
			Foreground:    lipgloss.Color("#b3b1ad"),
			Muted:         lipgloss.Color("#626a73"),
			Border:        lipgloss.Color("#1d242c"),
			BorderFocused: lipgloss.Color("#ffb454"),

			Primary:   lipgloss.Color("#ffb454"), // Orange
			Secondary: lipgloss.Color("#c2d94c"), // Green
			Success:   lipgloss.Color("#c2d94c"),
			Warning:   lipgloss.Color("#ffb454"),
			Error:     lipgloss.Color("#ff3333"),
			Info:      lipgloss.Color("#59c2ff"),

			Surface0: lipgloss.Color("#0a0e14"),
			Surface1: lipgloss.Color("#0d1016"),
			Surface2: lipgloss.Color("#1d242c"),
			Surface3: lipgloss.Color("#273747"),

			Selection: lipgloss.Color("#273747"),
			Highlight: lipgloss.Color("#1d242c"),
		},
	}
}

// GitHubDarkTheme - GitHub's dark mode
func GitHubDarkTheme() Theme {
	return Theme{
		ID:   ThemeGitHubDark,
		Name: "GitHub Dark",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#0d1117"),
			Foreground:    lipgloss.Color("#c9d1d9"),
			Muted:         lipgloss.Color("#8b949e"),
			Border:        lipgloss.Color("#30363d"),
			BorderFocused: lipgloss.Color("#58a6ff"),

			Primary:   lipgloss.Color("#58a6ff"), // Blue
			Secondary: lipgloss.Color("#bc8cff"), // Purple
			Success:   lipgloss.Color("#3fb950"),
			Warning:   lipgloss.Color("#d29922"),
			Error:     lipgloss.Color("#f85149"),
			Info:      lipgloss.Color("#58a6ff"),

			Surface0: lipgloss.Color("#0d1117"),
			Surface1: lipgloss.Color("#161b22"),
			Surface2: lipgloss.Color("#21262d"),
			Surface3: lipgloss.Color("#30363d"),

			Selection: lipgloss.Color("#264f78"),
			Highlight: lipgloss.Color("#21262d"),
		},
	}
}

// ============================================================================
// LIGHT THEMES
// ============================================================================

// SolarizedLightTheme - light version of Solarized
func SolarizedLightTheme() Theme {
	return Theme{
		ID:   ThemeSolarizedLight,
		Name: "Solarized Light",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#fdf6e3"),
			Foreground:    lipgloss.Color("#657b83"),
			Muted:         lipgloss.Color("#93a1a1"),
			Border:        lipgloss.Color("#eee8d5"),
			BorderFocused: lipgloss.Color("#268bd2"),

			Primary:   lipgloss.Color("#268bd2"), // Blue
			Secondary: lipgloss.Color("#6c71c4"), // Violet
			Success:   lipgloss.Color("#859900"),
			Warning:   lipgloss.Color("#b58900"),
			Error:     lipgloss.Color("#dc322f"),
			Info:      lipgloss.Color("#2aa198"),

			Surface0: lipgloss.Color("#fdf6e3"),
			Surface1: lipgloss.Color("#eee8d5"),
			Surface2: lipgloss.Color("#e4dcc8"),
			Surface3: lipgloss.Color("#d6cfb9"),

			Selection: lipgloss.Color("#eee8d5"),
			Highlight: lipgloss.Color("#e4dcc8"),
		},
	}
}

// GitHubLightTheme - GitHub's light mode
func GitHubLightTheme() Theme {
	return Theme{
		ID:   ThemeGitHubLight,
		Name: "GitHub Light",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#ffffff"),
			Foreground:    lipgloss.Color("#24292f"),
			Muted:         lipgloss.Color("#656d76"),
			Border:        lipgloss.Color("#d0d7de"),
			BorderFocused: lipgloss.Color("#0969da"),

			Primary:   lipgloss.Color("#0969da"), // Blue
			Secondary: lipgloss.Color("#8250df"), // Purple
			Success:   lipgloss.Color("#1a7f37"),
			Warning:   lipgloss.Color("#9a6700"),
			Error:     lipgloss.Color("#cf222e"),
			Info:      lipgloss.Color("#0969da"),

			Surface0: lipgloss.Color("#ffffff"),
			Surface1: lipgloss.Color("#f6f8fa"),
			Surface2: lipgloss.Color("#eaeef2"),
			Surface3: lipgloss.Color("#d0d7de"),

			Selection: lipgloss.Color("#ddf4ff"),
			Highlight: lipgloss.Color("#eaeef2"),
		},
	}
}

// CatppuccinLatteTheme - light pastel variant
func CatppuccinLatteTheme() Theme {
	return Theme{
		ID:   ThemeCatppuccinLatte,
		Name: "Catppuccin Latte",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#eff1f5"),
			Foreground:    lipgloss.Color("#4c4f69"),
			Muted:         lipgloss.Color("#9ca0b0"),
			Border:        lipgloss.Color("#ccd0da"),
			BorderFocused: lipgloss.Color("#8839ef"),

			Primary:   lipgloss.Color("#8839ef"), // Mauve
			Secondary: lipgloss.Color("#ea76cb"), // Pink
			Success:   lipgloss.Color("#40a02b"),
			Warning:   lipgloss.Color("#df8e1d"),
			Error:     lipgloss.Color("#d20f39"),
			Info:      lipgloss.Color("#179299"),

			Surface0: lipgloss.Color("#eff1f5"),
			Surface1: lipgloss.Color("#e6e9ef"),
			Surface2: lipgloss.Color("#ccd0da"),
			Surface3: lipgloss.Color("#bcc0cc"),

			Selection: lipgloss.Color("#ccd0da"),
			Highlight: lipgloss.Color("#e6e9ef"),
		},
	}
}

// OneLightTheme - Atom's light theme
func OneLightTheme() Theme {
	return Theme{
		ID:   ThemeOneLight,
		Name: "One Light",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#fafafa"),
			Foreground:    lipgloss.Color("#383a42"),
			Muted:         lipgloss.Color("#a0a1a7"),
			Border:        lipgloss.Color("#e5e5e6"),
			BorderFocused: lipgloss.Color("#4078f2"),

			Primary:   lipgloss.Color("#4078f2"), // Blue
			Secondary: lipgloss.Color("#a626a4"), // Magenta
			Success:   lipgloss.Color("#50a14f"),
			Warning:   lipgloss.Color("#c18401"),
			Error:     lipgloss.Color("#e45649"),
			Info:      lipgloss.Color("#0184bc"),

			Surface0: lipgloss.Color("#fafafa"),
			Surface1: lipgloss.Color("#f0f0f1"),
			Surface2: lipgloss.Color("#e5e5e6"),
			Surface3: lipgloss.Color("#d4d4d5"),

			Selection: lipgloss.Color("#e5e5e6"),
			Highlight: lipgloss.Color("#f0f0f1"),
		},
	}
}

// GruvboxLightTheme - warm retro light variant
func GruvboxLightTheme() Theme {
	return Theme{
		ID:   ThemeGruvboxLight,
		Name: "Gruvbox Light",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#fbf1c7"),
			Foreground:    lipgloss.Color("#3c3836"),
			Muted:         lipgloss.Color("#928374"),
			Border:        lipgloss.Color("#ebdbb2"),
			BorderFocused: lipgloss.Color("#d65d0e"),

			Primary:   lipgloss.Color("#d65d0e"), // Orange
			Secondary: lipgloss.Color("#b16286"), // Purple
			Success:   lipgloss.Color("#98971a"),
			Warning:   lipgloss.Color("#d79921"),
			Error:     lipgloss.Color("#cc241d"),
			Info:      lipgloss.Color("#458588"),

			Surface0: lipgloss.Color("#fbf1c7"),
			Surface1: lipgloss.Color("#f2e5bc"),
			Surface2: lipgloss.Color("#ebdbb2"),
			Surface3: lipgloss.Color("#d5c4a1"),

			Selection: lipgloss.Color("#ebdbb2"),
			Highlight: lipgloss.Color("#f2e5bc"),
		},
	}
}

// RosePineDawnTheme - light rose variant
func RosePineDawnTheme() Theme {
	return Theme{
		ID:   ThemeRosePineDawn,
		Name: "Rosé Pine Dawn",
		Colors: ThemeColors{
			Background:    lipgloss.Color("#faf4ed"),
			Foreground:    lipgloss.Color("#575279"),
			Muted:         lipgloss.Color("#9893a5"),
			Border:        lipgloss.Color("#f2e9e1"),
			BorderFocused: lipgloss.Color("#d7827e"),

			Primary:   lipgloss.Color("#d7827e"), // Rose
			Secondary: lipgloss.Color("#907aa9"), // Iris
			Success:   lipgloss.Color("#56949f"),
			Warning:   lipgloss.Color("#ea9d34"),
			Error:     lipgloss.Color("#b4637a"),
			Info:      lipgloss.Color("#286983"),

			Surface0: lipgloss.Color("#faf4ed"),
			Surface1: lipgloss.Color("#fffaf3"),
			Surface2: lipgloss.Color("#f2e9e1"),
			Surface3: lipgloss.Color("#dfdad9"),

			Selection: lipgloss.Color("#f2e9e1"),
			Highlight: lipgloss.Color("#fffaf3"),
		},
	}
}
