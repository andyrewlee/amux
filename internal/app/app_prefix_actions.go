package app

// The closed set of prefix-mode actions. prefixCommandTable and
// runPrefixAction's switch must stay within this set — a string literal that
// matches no case silently no-ops, so always use the consts (a typo'd const
// name fails to compile; a typo'd literal does not).

// prefixAction is the closed set of commands prefix sequences dispatch to.
// The table below and runPrefixAction's switch must stay in the same set —
// a literal that matches no case silently no-ops, so use the consts.
type prefixAction string

const (
	prefixActionAddProject      prefixAction = "add_project"
	prefixActionDeleteWorkspace prefixAction = "delete_workspace"
	prefixActionOpenSettings    prefixAction = "open_settings"
	prefixActionQuit            prefixAction = "quit"
	prefixActionCleanupTmux     prefixAction = "cleanup_tmux"
	prefixActionFocusLeft       prefixAction = "focus_left"
	prefixActionFocusRight      prefixAction = "focus_right"
	prefixActionNextAttention   prefixAction = "next_attention"
	prefixActionNewAgentTab     prefixAction = "new_agent_tab"
	prefixActionNewTerminalTab  prefixAction = "new_terminal_tab"
	prefixActionNextTab         prefixAction = "next_tab"
	prefixActionPrevTab         prefixAction = "prev_tab"
	prefixActionCloseTab        prefixAction = "close_tab"
	prefixActionDetachTab       prefixAction = "detach_tab"
	prefixActionReattachTab     prefixAction = "reattach_tab"
	prefixActionRestartTab      prefixAction = "restart_tab"
	prefixActionCopyTranscript  prefixAction = "copy_transcript"
	prefixActionSaveTranscript  prefixAction = "save_transcript"
	prefixActionBrowseScripts   prefixAction = "browse_transcripts"
	prefixActionScrollUp        prefixAction = "scroll_up"
	prefixActionScrollDown      prefixAction = "scroll_down"
)
