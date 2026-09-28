module github.com/andyrewlee/amux

go 1.26.0

toolchain go1.26.8

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.0.9
	charm.land/lipgloss/v2 v2.0.6
	github.com/atotto/clipboard v0.1.4 // indirect
	// ultraviolet is Charm's untagged pre-release render engine. Its
	// pseudo-version is driven by charm.land/lipgloss/v2 (lipgloss requires the
	// newer pseudo-version; bubbletea's requirement is older and loses under
	// MVS). Do NOT bump it independently of the Charm stack — a mismatched pair
	// can break internal/ui/compositor with no compiler warning.
	github.com/charmbracelet/ultraviolet v0.0.0-20260811164956-006e29f97886
	github.com/charmbracelet/x/ansi v0.11.8
	github.com/charmbracelet/x/term v0.2.2
	github.com/clipperhouse/displaywidth v0.11.0
	github.com/creack/pty v1.1.24
	github.com/fsnotify/fsnotify v1.10.1
	github.com/mattn/go-runewidth v0.0.27 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
)

require (
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	golang.org/x/exp v0.0.0-20260611194520-c48552f49976 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
