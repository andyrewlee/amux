// Package messages defines the shared Bubble Tea message vocabulary exchanged
// between the app message pump and the UI panes: user intents, lifecycle
// events, and cross-pane notifications. See internal/app/MESSAGE_FLOW.md.
//
// Convention: a type the app's dispatch switch must name lives here; a type
// only one leaf pane emits and consumes stays leaf-local. (Exceptions are
// documented where they live — e.g. sidebar.SidebarTerminalCreated carries
// ptyio payloads that would make messages→ptyio an import cycle.)
package messages
