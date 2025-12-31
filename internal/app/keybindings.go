package app

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keybindings for the application
type KeyMap struct {
	// Global
	Quit        key.Binding
	FocusLeft   key.Binding
	FocusCenter key.Binding
	FocusRight  key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	CloseTab    key.Binding
	NewAgentTab key.Binding
	Home        key.Binding

	// Dashboard
	Enter        key.Binding
	NewWorktree  key.Binding
	Delete       key.Binding
	ToggleFilter key.Binding
	Refresh      key.Binding

	// Agent/Chat
	Interrupt  key.Binding
	SendEscape key.Binding

	// Navigation
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
}

// DefaultKeyMap returns the default keybindings
func DefaultKeyMap() KeyMap {
	return KeyMap{
		// Global
		Quit: key.NewBinding(
			key.WithKeys("ctrl+q"),
			key.WithHelp("ctrl+q", "quit"),
		),
		FocusLeft: key.NewBinding(
			key.WithKeys("ctrl+h"),
			key.WithHelp("ctrl+h", "focus dashboard"),
		),
		FocusCenter: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("ctrl+l", "focus center"),
		),
		FocusRight: key.NewBinding(
			key.WithKeys("ctrl+;"),
			key.WithHelp("ctrl+;", "focus sidebar"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "previous tab"),
		),
		CloseTab: key.NewBinding(
			key.WithKeys("ctrl+w"),
			key.WithHelp("ctrl+w", "close tab"),
		),
		NewAgentTab: key.NewBinding(
			key.WithKeys("ctrl+t"),
			key.WithHelp("ctrl+t", "new agent tab"),
		),
		Home: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "go home"),
		),

		// Dashboard
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "activate"),
		),
		NewWorktree: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new worktree"),
		),
		Delete: key.NewBinding(
			key.WithKeys("d", "D"),
			key.WithHelp("d", "delete"),
		),
		ToggleFilter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "filter"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("g", "r"),
			key.WithHelp("g", "refresh"),
		),

		// Agent
		Interrupt: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "interrupt"),
		),
		SendEscape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "escape"),
		),

		// Navigation
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/up", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/down", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/left", "left"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/right", "right"),
		),
	}
}
