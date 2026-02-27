package app

import "charm.land/bubbles/v2/key"

// KeyMap defines all keybindings for the application
type KeyMap struct {
	// Prefix key (leader)
	Prefix key.Binding

	// Dashboard
	Enter        key.Binding
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
		// Prefix key (leader)
		// Ctrl-Space is reported as ctrl+@ or ctrl+space depending on terminal.
		Prefix: key.NewBinding(
			key.WithKeys("ctrl+@", "ctrl+space"),
			key.WithHelp("C-Space", "Commands"),
		),

		// Dashboard
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "activate"),
		),
		Delete: key.NewBinding(
			key.WithKeys("D"),
			key.WithHelp("D", "delete"),
		),
		ToggleFilter: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "filter"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("g", "r"),
			key.WithHelp("g", "rescan"),
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
