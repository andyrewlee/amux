package keymap

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"github.com/andyrewlee/amux/internal/config"
)

// Action identifies a configurable keybinding.
type Action string

const (
	ActionLeader Action = "leader"

	ActionFocusLeft  Action = "focus_left"
	ActionFocusRight Action = "focus_right"
	ActionFocusUp    Action = "focus_up"
	ActionFocusDown  Action = "focus_down"

	ActionTabNext  Action = "tab_next"
	ActionTabPrev  Action = "tab_prev"
	ActionTabNew   Action = "tab_new"
	ActionTabClose Action = "tab_close"

	ActionMonitorToggle Action = "monitor_toggle"
	ActionHome          Action = "home"
	ActionHelp          Action = "help"
	ActionKeymap        Action = "keymap_editor"
	ActionQuit          Action = "quit"

	ActionScrollUpHalf   Action = "scroll_up_half"
	ActionScrollDownHalf Action = "scroll_down_half"

	ActionDashboardUp          Action = "dashboard_up"
	ActionDashboardDown        Action = "dashboard_down"
	ActionDashboardTop         Action = "dashboard_top"
	ActionDashboardBottom      Action = "dashboard_bottom"
	ActionDashboardEnter       Action = "dashboard_enter"
	ActionDashboardNewWorktree Action = "dashboard_new_worktree"
	ActionDashboardDelete      Action = "dashboard_delete"
	ActionDashboardToggle      Action = "dashboard_toggle_filter"
	ActionDashboardRefresh     Action = "dashboard_refresh"

	ActionSidebarUp      Action = "sidebar_up"
	ActionSidebarDown    Action = "sidebar_down"
	ActionSidebarRefresh Action = "sidebar_refresh"

	ActionMonitorLeft     Action = "monitor_left"
	ActionMonitorRight    Action = "monitor_right"
	ActionMonitorUp       Action = "monitor_up"
	ActionMonitorDown     Action = "monitor_down"
	ActionMonitorActivate Action = "monitor_activate"
	ActionMonitorExit     Action = "monitor_exit"
)

type bindingDef struct {
	action Action
	keys   []string
	desc   string
}

// KeyMap defines all keybindings for the application.
type KeyMap struct {
	Leader key.Binding

	FocusLeft  key.Binding
	FocusRight key.Binding
	FocusUp    key.Binding
	FocusDown  key.Binding

	TabNext  key.Binding
	TabPrev  key.Binding
	TabNew   key.Binding
	TabClose key.Binding

	MonitorToggle key.Binding
	Home          key.Binding
	Help          key.Binding
	KeymapEditor  key.Binding
	Quit          key.Binding

	ScrollUpHalf   key.Binding
	ScrollDownHalf key.Binding

	DashboardUp          key.Binding
	DashboardDown        key.Binding
	DashboardTop         key.Binding
	DashboardBottom      key.Binding
	DashboardEnter       key.Binding
	DashboardNewWorktree key.Binding
	DashboardDelete      key.Binding
	DashboardToggle      key.Binding
	DashboardRefresh     key.Binding

	SidebarUp      key.Binding
	SidebarDown    key.Binding
	SidebarRefresh key.Binding

	MonitorLeft     key.Binding
	MonitorRight    key.Binding
	MonitorUp       key.Binding
	MonitorDown     key.Binding
	MonitorActivate key.Binding
	MonitorExit     key.Binding
}

// New builds a keymap from defaults, applying any user overrides.
func New(cfg config.KeyMapConfig) KeyMap {
	return KeyMap{
		Leader: bindingFromDef(cfg, bindingDef{
			action: ActionLeader,
			keys:   []string{"ctrl+space", "ctrl+@", "ctrl+;"},
			desc:   "leader",
		}),

		FocusLeft: bindingFromDef(cfg, bindingDef{
			action: ActionFocusLeft,
			keys:   []string{"alt+h"},
			desc:   "focus left",
		}),
		FocusRight: bindingFromDef(cfg, bindingDef{
			action: ActionFocusRight,
			keys:   []string{"alt+l"},
			desc:   "focus right",
		}),
		FocusUp: bindingFromDef(cfg, bindingDef{
			action: ActionFocusUp,
			keys:   []string{"alt+k"},
			desc:   "focus up",
		}),
		FocusDown: bindingFromDef(cfg, bindingDef{
			action: ActionFocusDown,
			keys:   []string{"alt+j"},
			desc:   "focus down",
		}),

		TabNext: bindingFromDef(cfg, bindingDef{
			action: ActionTabNext,
			keys:   []string{"t"},
			desc:   "next tab",
		}),
		TabPrev: bindingFromDef(cfg, bindingDef{
			action: ActionTabPrev,
			keys:   []string{"T"},
			desc:   "previous tab",
		}),
		TabNew: bindingFromDef(cfg, bindingDef{
			action: ActionTabNew,
			keys:   []string{"n"},
			desc:   "new agent tab",
		}),
		TabClose: bindingFromDef(cfg, bindingDef{
			action: ActionTabClose,
			keys:   []string{"x"},
			desc:   "close tab",
		}),

		MonitorToggle: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorToggle,
			keys:   []string{"alt+m"},
			desc:   "monitor tabs",
		}),
		Home: bindingFromDef(cfg, bindingDef{
			action: ActionHome,
			keys:   []string{"alt+g"},
			desc:   "home",
		}),
		Help: bindingFromDef(cfg, bindingDef{
			action: ActionHelp,
			keys:   []string{"alt+?"},
			desc:   "help",
		}),
		KeymapEditor: bindingFromDef(cfg, bindingDef{
			action: ActionKeymap,
			keys:   []string{"alt+,"},
			desc:   "keymap editor",
		}),
		Quit: bindingFromDef(cfg, bindingDef{
			action: ActionQuit,
			keys:   []string{"alt+q"},
			desc:   "quit",
		}),

		ScrollUpHalf: bindingFromDef(cfg, bindingDef{
			action: ActionScrollUpHalf,
			keys:   []string{"alt+u"},
			desc:   "scroll up",
		}),
		ScrollDownHalf: bindingFromDef(cfg, bindingDef{
			action: ActionScrollDownHalf,
			keys:   []string{"alt+d"},
			desc:   "scroll down",
		}),

		DashboardUp: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardUp,
			keys:   []string{"k", "up"},
			desc:   "up",
		}),
		DashboardDown: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardDown,
			keys:   []string{"j", "down"},
			desc:   "down",
		}),
		DashboardTop: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardTop,
			keys:   []string{"g"},
			desc:   "top",
		}),
		DashboardBottom: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardBottom,
			keys:   []string{"G"},
			desc:   "bottom",
		}),
		DashboardEnter: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardEnter,
			keys:   []string{"enter"},
			desc:   "activate",
		}),
		DashboardNewWorktree: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardNewWorktree,
			keys:   []string{"n"},
			desc:   "new worktree",
		}),
		DashboardDelete: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardDelete,
			keys:   []string{"d", "D"},
			desc:   "delete",
		}),
		DashboardToggle: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardToggle,
			keys:   []string{"f"},
			desc:   "filter",
		}),
		DashboardRefresh: bindingFromDef(cfg, bindingDef{
			action: ActionDashboardRefresh,
			keys:   []string{"r"},
			desc:   "refresh",
		}),

		SidebarUp: bindingFromDef(cfg, bindingDef{
			action: ActionSidebarUp,
			keys:   []string{"k", "up"},
			desc:   "up",
		}),
		SidebarDown: bindingFromDef(cfg, bindingDef{
			action: ActionSidebarDown,
			keys:   []string{"j", "down"},
			desc:   "down",
		}),
		SidebarRefresh: bindingFromDef(cfg, bindingDef{
			action: ActionSidebarRefresh,
			keys:   []string{"g"},
			desc:   "refresh",
		}),

		MonitorLeft: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorLeft,
			keys:   []string{"h", "left"},
			desc:   "left",
		}),
		MonitorRight: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorRight,
			keys:   []string{"l", "right"},
			desc:   "right",
		}),
		MonitorUp: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorUp,
			keys:   []string{"k", "up"},
			desc:   "up",
		}),
		MonitorDown: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorDown,
			keys:   []string{"j", "down"},
			desc:   "down",
		}),
		MonitorActivate: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorActivate,
			keys:   []string{"enter"},
			desc:   "open",
		}),
		MonitorExit: bindingFromDef(cfg, bindingDef{
			action: ActionMonitorExit,
			keys:   []string{"esc"},
			desc:   "exit",
		}),
	}
}

func bindingFromDef(cfg config.KeyMapConfig, def bindingDef) key.Binding {
	keys, ok := cfg.BindingFor(string(def.action))
	if !ok {
		keys = def.keys
	}
	helpKey := strings.Join(keys, "/")
	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, def.desc),
	)
}

// PrimaryKey returns the first key in the binding, if present.
func PrimaryKey(binding key.Binding) string {
	keys := binding.Keys()
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

// BindingHint returns a single key hint for a binding, falling back to help text.
func BindingHint(binding key.Binding) string {
	key := PrimaryKey(binding)
	if key == "" {
		return binding.Help().Key
	}
	return key
}

// PairHint joins two bindings with a slash using their primary keys.
func PairHint(a, b key.Binding) string {
	left := BindingHint(a)
	right := BindingHint(b)
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	return left + "/" + right
}

// SequenceHint joins multiple bindings with slashes using their primary keys.
func SequenceHint(bindings ...key.Binding) string {
	var keys []string
	for _, binding := range bindings {
		key := BindingHint(binding)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return strings.Join(keys, "/")
}

// LeaderSequenceHint formats a leader sequence hint using the primary keys.
func LeaderSequenceHint(km KeyMap, bindings ...key.Binding) string {
	leader := PrimaryKey(km.Leader)
	if leader == "" {
		leader = km.Leader.Help().Key
	}
	var keys []string
	for _, binding := range bindings {
		key := BindingHint(binding)
		if key != "" {
			keys = append(keys, key)
		}
	}
	if leader == "" {
		return strings.Join(keys, "/")
	}
	if len(keys) == 0 {
		return leader
	}
	return leader + " " + strings.Join(keys, "/")
}

// ActionInfo describes a configurable action for UI display.
type ActionInfo struct {
	Action   Action
	Desc     string
	Group    string
	Editable bool
}

// ActionInfos returns the ordered list of actions for UI display.
func ActionInfos() []ActionInfo {
	return []ActionInfo{
		{Action: ActionLeader, Desc: "Tab prefix", Group: "Leader", Editable: true},
		{Action: ActionFocusLeft, Desc: "Focus left", Group: "Focus", Editable: true},
		{Action: ActionFocusRight, Desc: "Focus right", Group: "Focus", Editable: true},
		{Action: ActionFocusUp, Desc: "Focus up", Group: "Focus", Editable: true},
		{Action: ActionFocusDown, Desc: "Focus down", Group: "Focus", Editable: true},
		{Action: ActionTabPrev, Desc: "Previous tab", Group: "Tabs", Editable: true},
		{Action: ActionTabNext, Desc: "Next tab", Group: "Tabs", Editable: true},
		{Action: ActionTabNew, Desc: "New agent tab", Group: "Tabs", Editable: true},
		{Action: ActionTabClose, Desc: "Close tab", Group: "Tabs", Editable: true},
		{Action: ActionMonitorToggle, Desc: "Monitor tabs", Group: "Global", Editable: true},
		{Action: ActionHome, Desc: "Home", Group: "Global", Editable: true},
		{Action: ActionHelp, Desc: "Toggle help", Group: "Global", Editable: true},
		{Action: ActionKeymap, Desc: "Keymap editor", Group: "Global", Editable: true},
		{Action: ActionQuit, Desc: "Quit", Group: "Global", Editable: true},
		{Action: ActionScrollUpHalf, Desc: "Scroll up", Group: "Global", Editable: true},
		{Action: ActionScrollDownHalf, Desc: "Scroll down", Group: "Global", Editable: true},
		{Action: ActionDashboardUp, Desc: "Navigate up", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardDown, Desc: "Navigate down", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardTop, Desc: "Jump to top", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardBottom, Desc: "Jump to bottom", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardEnter, Desc: "Activate worktree", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardNewWorktree, Desc: "New worktree", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardDelete, Desc: "Delete worktree", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardToggle, Desc: "Toggle dirty filter", Group: "Dashboard", Editable: true},
		{Action: ActionDashboardRefresh, Desc: "Refresh dashboard", Group: "Dashboard", Editable: true},
		{Action: ActionSidebarUp, Desc: "Navigate up", Group: "Sidebar", Editable: true},
		{Action: ActionSidebarDown, Desc: "Navigate down", Group: "Sidebar", Editable: true},
		{Action: ActionSidebarRefresh, Desc: "Refresh status", Group: "Sidebar", Editable: true},
		{Action: ActionMonitorLeft, Desc: "Move left", Group: "Monitor", Editable: true},
		{Action: ActionMonitorRight, Desc: "Move right", Group: "Monitor", Editable: true},
		{Action: ActionMonitorUp, Desc: "Move up", Group: "Monitor", Editable: true},
		{Action: ActionMonitorDown, Desc: "Move down", Group: "Monitor", Editable: true},
		{Action: ActionMonitorActivate, Desc: "Open selected agent", Group: "Monitor", Editable: true},
		{Action: ActionMonitorExit, Desc: "Exit monitor", Group: "Monitor", Editable: true},
	}
}

// BindingForAction returns the binding for the given action.
func BindingForAction(km KeyMap, action Action) key.Binding {
	switch action {
	case ActionLeader:
		return km.Leader
	case ActionFocusLeft:
		return km.FocusLeft
	case ActionFocusRight:
		return km.FocusRight
	case ActionFocusUp:
		return km.FocusUp
	case ActionFocusDown:
		return km.FocusDown
	case ActionTabNext:
		return km.TabNext
	case ActionTabPrev:
		return km.TabPrev
	case ActionTabNew:
		return km.TabNew
	case ActionTabClose:
		return km.TabClose
	case ActionMonitorToggle:
		return km.MonitorToggle
	case ActionHome:
		return km.Home
	case ActionHelp:
		return km.Help
	case ActionKeymap:
		return km.KeymapEditor
	case ActionQuit:
		return km.Quit
	case ActionScrollUpHalf:
		return km.ScrollUpHalf
	case ActionScrollDownHalf:
		return km.ScrollDownHalf
	case ActionDashboardUp:
		return km.DashboardUp
	case ActionDashboardDown:
		return km.DashboardDown
	case ActionDashboardTop:
		return km.DashboardTop
	case ActionDashboardBottom:
		return km.DashboardBottom
	case ActionDashboardEnter:
		return km.DashboardEnter
	case ActionDashboardNewWorktree:
		return km.DashboardNewWorktree
	case ActionDashboardDelete:
		return km.DashboardDelete
	case ActionDashboardToggle:
		return km.DashboardToggle
	case ActionDashboardRefresh:
		return km.DashboardRefresh
	case ActionSidebarUp:
		return km.SidebarUp
	case ActionSidebarDown:
		return km.SidebarDown
	case ActionSidebarRefresh:
		return km.SidebarRefresh
	case ActionMonitorLeft:
		return km.MonitorLeft
	case ActionMonitorRight:
		return km.MonitorRight
	case ActionMonitorUp:
		return km.MonitorUp
	case ActionMonitorDown:
		return km.MonitorDown
	case ActionMonitorActivate:
		return km.MonitorActivate
	case ActionMonitorExit:
		return km.MonitorExit
	default:
		return key.Binding{}
	}
}
