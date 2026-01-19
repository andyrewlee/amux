package common

import (
	"charm.land/lipgloss/v2"
)

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
