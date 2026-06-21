#!/usr/bin/env python3
"""Canonical feature-audit generator for amux.

Single source of truth for the feature spreadsheet. Each ROW is one user story
with code-derived expected behaviour and a status that walks through the audit
loop: Catalogued -> Tested -> (Error|Pass) -> Fixed -> Verified.

Run:  python3 feature-audit/build_audit.py
Emits: feature-audit/FEATURES.csv  (the canonical spreadsheet)
       feature-audit/FEATURES.md   (human-readable mirror)
       feature-audit/SUMMARY.md    (status roll-up)

Status vocabulary (column `status`):
  Catalogued  - user story written from code, not yet tested
  Pass        - tested, behaves as expected
  Error       - tested, defect found (see errors_found)
  Fixed       - defect fixed (see fix_applied), awaiting re-test
  Verified    - re-tested after fix, now correct

`test_method` records HOW it was tested (unit test name, e2e test, harness
frame, manual code-trace). `result` is the latest observed outcome.
"""
import csv
import os
import textwrap

HERE = os.path.dirname(os.path.abspath(__file__))

# Column order for the canonical spreadsheet.
COLUMNS = [
    "id", "area", "feature", "user_story", "expected_behavior",
    "source", "tests", "status", "test_method", "result",
    "errors_found", "fix_applied", "retest_result",
]


def R(id, area, feature, user_story, expected_behavior, source, tests,
      status="Catalogued", test_method="", result="",
      errors_found="", fix_applied="", retest_result=""):
    return dict(
        id=id, area=area, feature=feature, user_story=user_story,
        expected_behavior=expected_behavior, source=source, tests=tests,
        status=status, test_method=test_method, result=result,
        errors_found=errors_found, fix_applied=fix_applied,
        retest_result=retest_result,
    )


ROWS = []

# ====================================================================
# DASHBOARD  (left pane: project/workspace tree + toolbar)
# ====================================================================
ROWS += [
    R("DASH-01", "Dashboard", "Home/Welcome row",
      "As a user, I want a Home row at the top of the dashboard, so that I can see the welcome screen with no workspace active.",
      "'[amux]' is the first selectable row. Selecting it (Enter/click) emits ShowWelcome and renders the welcome screen; bold+primary when no workspace active.",
      "dashboard/model.go; dashboard_render.go", "dashboard/model_actions_test.go, active_test.go"),
    R("DASH-02", "Dashboard", "Project rows",
      "As a user, I want each project shown as a selectable row, so that I can navigate and manage projects.",
      "Each project is one row showing its name + main-workspace status (dirty/deleting). Enter/click activates the project's main/primary workspace.",
      "dashboard/dashboard_state.go; dashboard_render.go", "dashboard/model_actions_test.go, click_test.go"),
    R("DASH-03", "Dashboard", "Workspace rows",
      "As a user, I want child workspaces listed under their project, so that I can select a specific branch workspace.",
      "Non-main workspaces appear indented, sorted newest-first then by name. Each shows name + status (creating/deleting spinner, activity, dirty). Enter/click activates it.",
      "dashboard/dashboard_state.go; dashboard_render.go", "dashboard/model_actions_test.go, model_test.go"),
    R("DASH-04", "Dashboard", "'+ New' workspace row",
      "As a user, I want a '+ New' row per project, so that I can create a workspace from the dashboard.",
      "A selectable '+ New' row follows each project; Enter/click opens the Create Workspace dialog.",
      "dashboard/dashboard_state.go; dashboard_render.go", "dashboard/model_actions_test.go, click_test.go"),
    R("DASH-05", "Dashboard", "Vertical navigation (j/k/arrows)",
      "As a user, I want to move up/down through rows, so that I can select workspaces with the keyboard.",
      "j/Down and k/Up move one selectable row, skipping spacer rows; selected row highlighted; movement auto-activates (preview) the new row.",
      "dashboard/dashboard_navigation.go; model_update.go", "dashboard/model_test.go, model_cursor_test.go"),
    R("DASH-06", "Dashboard", "Half-page scroll (PgUp/PgDn, Ctrl-U/D)",
      "As a user with many workspaces, I want half-page jumps, so that I can move quickly.",
      "PageDown/Ctrl+D and PageUp/Ctrl+U move ~half the visible height, skipping spacers; clamps at the first/last selectable row (no dead-end). (Toolbar focus is reached via j/Down only.)",
      "dashboard/model_update.go", "dashboard/model_cursor_test.go"),
    R("DASH-07", "Dashboard", "Jump to top/bottom (g/G)",
      "As a user, I want to jump to the first/last row, so that I can reach Home or the newest workspace fast.",
      "'g' jumps to first selectable (Home); 'G' jumps to last selectable row.",
      "dashboard/model_update.go", "(trace)"),
    R("DASH-08", "Dashboard", "Activate workspace (Enter/click)",
      "As a user, I want to activate a workspace, so that the center pane shows its agents.",
      "Enter on a workspace/project row emits WorkspaceActivated; Enter on Home emits ShowWelcome; the activated workspace becomes 'active'.",
      "dashboard/dashboard_navigation.go", "dashboard/model_actions_test.go, click_test.go; e2e/workspace_agent_test.go"),
    R("DASH-09", "Dashboard", "Delete workspace via 'D'",
      "As a user, I want to press D to delete the selected workspace, so that I can remove a branch by keyboard.",
      "On a workspace row, 'D' opens the Delete Workspace dialog; on a project row, 'D' opens Remove Project; lowercase 'd' is ignored.",
      "dashboard/dashboard_navigation.go", "dashboard/model_actions_test.go"),
    R("DASH-10", "Dashboard", "Delete via row 'x' icon (mouse)",
      "As a user, I want a delete 'x' on the selected row, so that I can delete with the mouse.",
      "The selected workspace/project row shows an 'x' delete icon; clicking it opens the matching delete/remove dialog.",
      "dashboard/dashboard_render.go; model_update.go", "dashboard/click_test.go"),
    R("DASH-11", "Dashboard", "Rescan workspaces (r)",
      "As a user, I want to refresh the workspace list, so that externally-created worktrees appear.",
      "Pressing 'r' emits RescanWorkspaces, triggering an async worktree scan; dashboard updates with new/removed workspaces. ('g' is jump-to-top, not rescan.)",
      "dashboard/dashboard_navigation.go", "app/app_operations_rescan_test.go"),
    R("DASH-12", "Dashboard", "Toolbar Commands button",
      "As a user, I want a [Commands] button, so that I can open the command palette without keybindings.",
      "j/Down from the last row focuses the toolbar; Left/Right or h/l move between buttons; Enter/click on [Commands] opens the command palette. (No Tab binding.)",
      "dashboard/dashboard_toolbar.go; model_update.go", "dashboard/toolbar_click_test.go"),
    R("DASH-13", "Dashboard", "Toolbar Settings button",
      "As a user, I want a [Settings] button, so that I can open settings from the dashboard.",
      "Enter/click on [Settings] opens the Settings dialog.",
      "dashboard/dashboard_toolbar.go", "dashboard/toolbar_click_test.go"),
    R("DASH-14", "Dashboard", "Mouse wheel scroll + click select",
      "As a user, I want wheel scroll and click selection, so that I can use a mouse/trackpad.",
      "Wheel moves cursor ~10% of pane height and auto-activates; left-click on a selectable row selects+activates it; clicks on borders ignored.",
      "dashboard/model_update.go", "dashboard/click_test.go"),
    R("DASH-15", "Dashboard", "Creating/Deleting spinners",
      "As a user, I want progress feedback during create/delete, so that I know an operation is in flight.",
      "Rows being created show an animated spinner + 'creating'; deleting rows show spinner + 'deleting' (updates ~80ms); delete status overrides active styling.",
      "dashboard/dashboard_render.go; dashboard_state.go", "dashboard/dashboard_state_test.go"),
    R("DASH-16", "Dashboard", "Git dirty indicator",
      "As a user, I want to see which workspaces have uncommitted changes, so that I can spot ones needing attention.",
      "When git status is not clean, the row text renders in muted/secondary colour (when not selected/active). Status cached and invalidated on update.",
      "dashboard/dashboard_render.go; model.go", "(trace)"),
    R("DASH-17", "Dashboard", "Active workspace + agent-activity indicator",
      "As a user, I want the active workspace and workspaces with running agents highlighted, so that I know my context.",
      "Active row renders in primary colour; workspaces with running chat agents (activeWorkspaceIDs) render in active style.",
      "dashboard/dashboard_render.go; dashboard_state.go", "dashboard/active_test.go"),
    R("DASH-18", "Dashboard", "Auto-scroll keeps cursor visible",
      "As a user, I want the list to scroll so my selection stays on screen, so that I never lose my place.",
      "scrollOffset adjusts so the cursor stays within the viewport; clamped so it never exceeds rows minus visible height.",
      "dashboard/model_update.go; dashboard_state.go", "dashboard/click_test.go"),
    R("DASH-19", "Dashboard", "Cursor anchor after deletion",
      "As a user, I want the cursor to land on the predecessor after delete, so that repeated D deletes walk upward intuitively.",
      "After a delete rebuild, cursor re-anchors to the row above the deleted one (predecessor), not the successor.",
      "dashboard/dashboard_navigation.go", "dashboard/dashboard_cursor_anchor_test.go"),
    R("DASH-20", "Dashboard", "Long-name truncation + resize",
      "As a user, I want names truncated and layout to adapt on resize, so that the dashboard stays readable.",
      "Names exceeding available width are truncated with ellipsis accounting for status/delete-icon/borders; SetSize recomputes layout.",
      "dashboard/dashboard_render.go; model.go", "(trace)"),
    R("DASH-21", "Dashboard", "Keymap hints line",
      "As a user, I want a hints line of keybindings, so that I can learn controls in place.",
      "When showKeymapHints is on, a wrapped hint line shows k/j, enter, D, r, g/G, C-Space, C-Space S, C-Space q.",
      "dashboard/dashboard_render.go; model.go", "(trace)"),
]

# ====================================================================
# CENTER  (agent/terminal tabs, scrolling, selection, diff viewer)
# ====================================================================
ROWS += [
    R("CTR-01", "Center", "Create agent tab (C-Space t a)",
      "As a user, I want to create an agent tab, so that I can run Claude/Codex/etc in parallel.",
      "C-Space t a opens the assistant picker; selecting one spawns a tmux session running the agent, creates a vterm, captures scrollback, adds the tab, makes it active.",
      "center/model_tabs.go; app/app_prefix.go", "center/model_tabs_test.go, model_tabs_session_add_test.go; e2e/workspace_agent_test.go"),
    R("CTR-02", "Center", "Create terminal tab (C-Space t t)",
      "As a user, I want a plain terminal tab, so that I can run a shell without an agent.",
      "C-Space t t creates a terminal tab in the sidebar terminal model (workspace-global, no sidebar focus needed).",
      "app/app_prefix.go; sidebar/terminal_tab_ops.go", "sidebar/terminal_tab_ops_test.go"),
    R("CTR-03", "Center", "Next/Prev tab (C-Space t n / t p)",
      "As a user, I want to cycle tabs, so that I can move between agents.",
      "t n / t p cycle circularly via TabSet Next/PrevIdx, set active idx, trigger selection-change (auto-reattach if detached) and persist.",
      "center/model_tabs_actions.go", "center/model_tabs_actions_test.go"),
    R("CTR-04", "Center", "Jump to tab by number (C-Space 1-9)",
      "As a user, I want to jump to a tab by digit, so that I can switch quickly.",
      "In prefix mode, 1-9 selects that tab (0-indexed); only shown when center has multiple tabs; terminates prefix immediately.",
      "app/app_prefix.go; center/model_tabs_actions.go", "app/app_prefix_test.go"),
    R("CTR-05", "Center", "Close tab (C-Space t x)",
      "As a user, I want to close the active tab, so that I can clean up finished agents.",
      "t x marks tab closing, stops PTY reader, closes agent, clears refs, removes from slice, adjusts active idx, async-kills the tmux session; emits TabClosed.",
      "center/model_tabs_actions.go", "center/model_tabs_actions_test.go"),
    R("CTR-06", "Center", "Detach tab (C-Space t d)",
      "As a user, I want to detach a tab while keeping its tmux session alive, so that I can reconnect later.",
      "t d stops the PTY reader, marks detached, saves SessionName, closes the PTY client but does NOT kill tmux; indicator goes muted; reattachable.",
      "center/model_tabs_session.go", "center/model_tabs_session_detach_test.go"),
    R("CTR-07", "Center", "Reattach detached tab (C-Space t r)",
      "As a user, I want to reattach a detached session, so that I can resume a backgrounded agent.",
      "t r queries the tmux session, captures scrollback/snapshot, creates a new PTY client (not session), updates the tab in place by TabID, restarts the reader. Also auto-reattaches on tab select.",
      "center/model_tabs_session_reattach.go", "center/model_tabs_session_reattach_order_test.go, auto_reattach_test.go"),
    R("CTR-08", "Center", "Restart tab (C-Space t s)",
      "As a user, I want to restart a stopped/detached agent fresh, so that I can resurrect a crashed agent.",
      "t s kills the old tmux session (if any), creates a brand-new one, starts a fresh agent PTY, updates tab in place — clean slate vs reattach.",
      "center/model_tabs_session_reattach.go", "center/model_tabs_session_reattach_test.go"),
    R("CTR-09", "Center", "Attached-agent tab limit auto-detach",
      "As a user, I want idle agent tabs auto-detached at a limit, so that I don't run too many agents at once.",
      "EnforceAttachedAgentTabLimit sorts attached chat tabs by lastFocusedAt and auto-detaches the excess; the focused tab is protected; non-chat tabs ignored.",
      "center/model_tabs_limit.go; app/app_attached_limit.go", "center/model_tabs_limit_test.go; app/app_attached_limit_test.go"),
    R("CTR-10", "Center", "Tab bar rendering + indicators",
      "As a user, I want a compact tab bar with status, so that I can see which agents are running/disconnected.",
      "Single-line bar: brand-colour running/idle dot, assistant name (muted if disconnected), close ×, active-tab highlight, '+New' on right; hit regions for clicks.",
      "center/model_render_tabbar.go", "center/model_render_test.go"),
    R("CTR-11", "Center", "Tab bar mouse (switch/close/new)",
      "As a user, I want to click the tab bar, so that I can switch/close/create tabs by mouse.",
      "Click close-button region closes; click tab region selects+flushes; click '+New' opens the assistant picker.",
      "center/model_render_tabbar.go; model_input_mouse.go", "center/model_input_mouse_test.go"),
    R("CTR-12", "Center", "Wheel scroll terminal scrollback",
      "As a user, I want to wheel-scroll terminal history, so that I can review past output.",
      "Wheel up/down on an active terminal scrolls scrollback by delta lines, clamped to max; routed via tab-actor (sync fallback). Diff viewer wheel checked first.",
      "center/model_input_mouse.go; model_scrolled_history.go", "center/selection_scroll_test.go; e2e/mouse_scroll_test.go"),
    R("CTR-13", "Center", "Keyboard page scroll (PgUp/PgDn)",
      "As a user, I want PgUp/PgDn to scroll history, so that I can review output by keyboard.",
      "PgUp scrolls up, PgDn down by ~¼ height (via tab-actor); typing any other key snaps back to the live bottom.",
      "center/model_input_keys.go; model_scrolled_history.go", "(trace)"),
    R("CTR-14", "Center", "Mouse text selection + copy",
      "As a user, I want to drag-select terminal text and copy it, so that I can reuse output.",
      "Click starts selection, drag extends (auto-scrolls when out of bounds), release auto-copies to clipboard; Cmd+C copies the active selection without clearing.",
      "center/model_input_mouse.go; tab_actor_selection.go", "center/selection_test.go, tab_actor_selection_test.go"),
    R("CTR-15", "Center", "Scrolled chat-history view",
      "As a user, I want scrollback to show only history while scrolled up, so that live output doesn't intrude while reading.",
      "For chat tabs with ViewOffset>0, the screen is replaced by a scrollback-only snapshot; cursor/selection rebased to the visible range.",
      "center/model_scrolled_history.go; model_render.go", "center/model_scrolled_history_test.go"),
    R("CTR-16", "Center", "Open diff viewer tab",
      "As a user, I want to open a git diff in a tab, so that I can review file changes inside amux.",
      "An OpenDiff message creates/reuses a diff tab (diff.Model) replacing the terminal in that tab; diff loads async; tab remains switchable/closable.",
      "center/model_input.go; diff/model.go", "center/model_tabs_diff_reuse_test.go; app/app_input_open_diff_test.go"),
    R("CTR-17", "Center", "Diff viewer scroll + hunk nav",
      "As a user, I want to scroll a diff and jump hunks, so that I can review efficiently.",
      "j/k line scroll; PgDn/Ctrl+D & PgUp/Ctrl+U half-page; g/Home top, G/End bottom; n/p next/prev hunk; w toggles wrap.",
      "diff/model.go; wheel.go", "diff/model_test.go"),
    R("CTR-18", "Center", "Diff viewer rendering",
      "As a user, I want colour-coded diffs, so that I can spot additions/removals quickly.",
      "Added green, removed red, context gray, header bold; clipped/wrapped to width; untracked shows full content; binary/large(>2MB) show warnings; loading/error states.",
      "diff/view.go", "diff/view_test.go"),
    R("CTR-19", "Center", "Diff viewer close (q/esc)",
      "As a user, I want to close the diff and return to the terminal, so that I can resume the agent.",
      "q/Esc in a diff tab sends CloseTab, returning to the terminal (or closing the tab if created only for the diff).",
      "diff/model.go", "(trace)"),
    R("CTR-20", "Center", "Tab restore on workspace open",
      "As a user, I want my agent tabs restored when reopening a workspace, so that I don't recreate them.",
      "RestoreTabsFromWorkspace reads OpenTabs, creates detached placeholders, async-reattaches each to its saved tmux session; success attaches+starts reader, failure stays detached with a toast.",
      "center/model_tabs_restore.go; model_tabs_session.go", "center/model_tabs_restore_test.go; e2e/persistence_test.go"),
    R("CTR-21", "Center", "Selection-change backlog flush",
      "As a user, I want switching to a busy tab to jump to latest output, so that I see new activity immediately.",
      "On selection change, a catch-up flush replays buffered output at accelerated pace to jump the viewport to the live bottom.",
      "center/model_tabs_session.go", "center/model_tabs_session_selection_flush_test.go"),
    R("CTR-22", "Center", "Center keymap hints",
      "As a user, I want center-pane hints, so that I can learn tab/scroll commands in place.",
      "helpLines lists C-Space t a/x/d/r/s, t p/n, 1-9, PgUp/PgDn; wrapped to width; gated by showKeymapHints.",
      "center/model_render.go", "center/model_render_test.go"),
]


# ====================================================================
# SIDEBAR  (file tree + git changes + embedded terminal)
# ====================================================================
ROWS += [
    R("SIDE-01", "Sidebar", "Browse workspace file tree",
      "As a user, I want to explore the workspace directory tree, so that I can locate project files.",
      "Project tab shows a hierarchical file/dir list with a cursor; navigate with j/k or wheel.",
      "sidebar/project_tree.go; project_tree_view.go", "sidebar/project_tree_test.go"),
    R("SIDE-02", "Sidebar", "Expand/collapse directories",
      "As a user, I want to expand/collapse directories, so that I can focus on relevant parts.",
      "l/Right expands, h/Left collapses; indicator shows expanded/collapsed; dirs sorted before files.",
      "sidebar/project_tree.go", "sidebar/project_tree_test.go"),
    R("SIDE-03", "Sidebar", "Open file from tree",
      "As a user, I want to open a file from the tree, so that I can view/edit it.",
      "enter/o/click on a file emits an open message routing the file to the center pane.",
      "sidebar/project_tree.go", "sidebar/project_tree_test.go"),
    R("SIDE-04", "Sidebar", "Toggle hidden files",
      "As a user, I want to show/hide dotfiles, so that I can control tree clutter.",
      "'.' toggles hidden-file visibility; tree re-filters, preserving navigation where possible.",
      "sidebar/project_tree.go", "sidebar/project_tree_test.go"),
    R("SIDE-05", "Sidebar", "Refresh file tree",
      "As a user, I want to reload the tree from disk, so that external changes appear.",
      "'r' re-reads and re-sorts the root directory.",
      "sidebar/project_tree.go", "sidebar/project_tree_test.go"),
    R("SIDE-06", "Sidebar", "Git changes view (staged/unstaged/untracked)",
      "As a user, I want to see staged/unstaged/untracked files, so that I can manage my git workflow.",
      "Changes tab shows three counted sections with per-file status codes (M/A/D/R/U), colour-coded.",
      "sidebar/model.go; model_view.go", "sidebar/model_view_test.go, rebuild_display_list_test.go"),
    R("SIDE-07", "Sidebar", "Filter git changes (/)",
      "As a user, I want to filter the changes list by filename, so that I can find files fast.",
      "'/' enters filter mode; typing filters case-insensitively; enter/esc exits; header shows 'filter: <q>'.",
      "sidebar/model_input.go; model_view.go", "sidebar/model_input_test.go"),
    R("SIDE-08", "Sidebar", "Navigate changes + open diff",
      "As a user, I want to move through changes and open a file's diff, so that I can review exactly what changed.",
      "j/k move (skipping headers); enter/space/o emits OpenDiff with change + mode (staged/unstaged) + workspace.",
      "sidebar/model_input.go", "sidebar/model_input_test.go"),
    R("SIDE-09", "Sidebar", "Refresh git status (g)",
      "As a user, I want to refresh git status, so that the sidebar stays current.",
      "'g' triggers async GetStatus; UI updates on GitStatusResult.",
      "sidebar/model_input.go", "sidebar/model_input_test.go"),
    R("SIDE-10", "Sidebar", "Branch + change statistics header",
      "As a user, I want to see the branch and changed-file/line counts, so that I can gauge scope.",
      "Changes tab shows 'branch: <name>' and '<N> changed files' with optional '+X -Y' line stats.",
      "sidebar/model_view.go", "sidebar/model_view_test.go"),
    R("SIDE-11", "Sidebar", "Switch sidebar tabs (1 Changes / 2 Project)",
      "As a user, I want to switch between Changes and Project views, so that I can reach files or git status.",
      "'1' selects Changes, '2' selects Project; click tab label switches; active tab highlighted; only focused tab gets keys.",
      "sidebar/tabs.go", "sidebar/tabs_test.go, tabs_view_test.go"),
    R("SIDE-12", "Sidebar", "Embedded terminal pane",
      "As a user, I want a terminal embedded in the sidebar, so that I can run shell commands without leaving amux.",
      "Sidebar shows terminal tabs with a tab bar (+New) and terminal content; focus/blur controls cursor + key routing.",
      "sidebar/terminal.go; terminal_render.go", "sidebar/terminal_test.go, terminal_render_test.go"),
    R("SIDE-13", "Sidebar", "Create/close/switch terminal tabs",
      "As a user, I want multiple terminal tabs I can create/close/switch, so that I can run several shells.",
      "+New / C-Space t t creates 'Terminal N'; ×/C-Space t x closes (kills unattached session after ~1.2s); C-Space t n/p cycle circularly.",
      "sidebar/terminal.go; terminal_selection.go; terminal_tab_ops.go", "sidebar/terminal_tab_ops_test.go, terminal_selection_test.go"),
    R("SIDE-14", "Sidebar", "Terminal input + bracketed paste",
      "As a user, I want to type commands and paste into the terminal, so that I can run shell work.",
      "All keys (except PgUp/PgDn) forward to the PTY; Cmd+C copies selection (not interrupt); bracketed paste supported.",
      "sidebar/terminal_update.go", "sidebar/terminal_update_test.go"),
    R("SIDE-15", "Sidebar", "Terminal scrollback (PgUp/PgDn + wheel)",
      "As a user, I want to scroll terminal history, so that I can review past output.",
      "PgUp/PgDn scroll half-screen; wheel scrolls ~12.5% height; 'SCROLL: N/M lines up' indicator; any keystroke returns to live.",
      "sidebar/terminal_update.go; terminal_render.go", "sidebar/terminal_scroll_test.go"),
    R("SIDE-16", "Sidebar", "Terminal text selection + copy",
      "As a user, I want to select and copy terminal text, so that I can reuse output.",
      "Click-drag selects (auto-scroll at edges); release copies to clipboard; Cmd+C copies current selection; highlight shown.",
      "sidebar/terminal_selection.go", "sidebar/terminal_selection_test.go"),
    R("SIDE-17", "Sidebar", "Terminal status indicators",
      "As a user, I want to see terminal status, so that I know if it's scrolled/detached/stopped.",
      "Shows 'SCROLL', 'DETACHED' (orange), or 'STOPPED' (red) above the help bar.",
      "sidebar/terminal_render.go; terminal_update_pty.go", "sidebar/terminal_update_pty_test.go"),
    R("SIDE-18", "Sidebar", "Terminal detach/reattach/restart",
      "As a user, I want to detach/reattach/restart sidebar terminals, so that I can manage long-running shells.",
      "C-Space t d closes PTY client but keeps tmux; t r reattaches (captures scrollback); t s kills+recreates the session.",
      "sidebar/terminal_pty_attach.go; terminal_update_session.go", "sidebar/terminal_reattach_test.go, terminal_update_session_test.go"),
    R("SIDE-19", "Sidebar", "Terminal session persistence across workspace switch",
      "As a user, I want terminals to persist across workspace switches, so that I can resume seamlessly.",
      "On workspace switch, terminals with matching workspace ID are preserved; PTY reader stops but tmux survives; reattaches on return with scrollback.",
      "sidebar/terminal_workspace_rebind.go; terminal_sessions.go", "sidebar/terminal_workspace_rebind_test.go"),
    R("SIDE-20", "Sidebar", "Terminal PTY auto-restart with backoff",
      "As a user, I want terminals to recover from transient reader failures, so that a glitch doesn't kill my session.",
      "On reader exit, restart with exponential backoff; after the failure limit it marks detached; tmux session survives.",
      "sidebar/terminal_update_pty.go", "sidebar/terminal_update_pty_test.go"),
    R("SIDE-21", "Sidebar", "Wheel-consumption focus detection",
      "As a user, I want empty panes to not steal wheel focus, so that scrolling targets the right pane.",
      "CanConsumeWheel returns false for empty Changes/Project/terminal-without-scrollback, preventing wheel-focus theft.",
      "sidebar/wheel.go", "sidebar/wheel_test.go"),
    R("SIDE-22", "Sidebar", "Per-tab help hints",
      "As a user, I want context help per sidebar tab, so that I can discover shortcuts.",
      "Project: k/j, h/l, ., r. Changes: k/j, /. Terminal: C-Space t t/n/p/x/d/r/s, PgUp/PgDn. Gated by showKeymapHints.",
      "sidebar/project_tree_view.go; model_view.go; terminal_render.go", "sidebar/project_tree_view_test.go"),
]

# ====================================================================
# WORKSPACE & PROJECT LIFECYCLE  (git worktree model, scripts)
# ====================================================================
ROWS += [
    R("WS-01", "Workspace", "Add project (file picker)",
      "As a user, I want to add a git repo as a project, so that amux can manage its workspaces.",
      "Pick a local repo path via the file picker; validated to contain .git; added to the projects registry; appears in the dashboard.",
      "app/workspace_service.go; data/registry.go", "app/workspace_service_add_project_test.go"),
    R("WS-02", "Workspace", "Remove project",
      "As a user, I want to remove a project, so that it no longer appears in amux.",
      "Project removed from registry; its workspaces remain in metadata but orphaned; active workspace cleared if it belonged to it; no files deleted.",
      "app/workspace_service.go; app_operations.go", ""),
    R("WS-03", "Workspace", "Create workspace (worktree + branch)",
      "As a user, I want to create a workspace backed by a git worktree on a new branch, so that agents work in isolation.",
      "Provide name/base/assistant; worktree created under ~/.amux/workspaces/<project>/<name> on a new branch from base; metadata saved; setup scripts queued; appears in dashboard.",
      "app/workspace_service.go; git/workspace.go", "app/workspace_service_test.go; e2e/workspace_agent_test.go"),
    R("WS-04", "Workspace", "Delete workspace (full cleanup)",
      "As a user, I want to delete a workspace, so that its worktree, branch, metadata, and tmux sessions are cleaned up.",
      "Confirm; tombstone written first; marked delete-in-flight; worktree removed; branch deleted; metadata removed; tmux sessions killed; failed deletes leave it intact.",
      "app/workspace_service.go; git/workspace.go", "app/workspace_service_delete_test.go; e2e/workspace_agent_test.go"),
    R("WS-05", "Workspace", "Archive workspace when worktree gone",
      "As a user, I want workspaces whose worktrees vanished to be archived, so that the dashboard reflects reality.",
      "During rescan, workspaces not discovered on disk are marked Archived with a timestamp and hidden from the main list.",
      "app/workspace_service_load.go", "app/app_operations_rescan_test.go"),
    R("WS-06", "Workspace", "Rescan/import existing worktrees",
      "As a user, I want rescan to import externally-created worktrees, so that I don't have to recreate them.",
      "'git worktree list --porcelain' per project; discovered worktrees upserted (default assistant if new); missing ones archived; respects delete-in-flight guard.",
      "app/workspace_service_load.go; git/workspace.go", "app/app_operations_rescan_test.go"),
    R("WS-07", "Workspace", "Load projects/workspaces on startup",
      "As a user, I want everything loaded on startup, so that the dashboard shows all projects/workspaces.",
      "LoadProjects reads registry, loads metadata, synthesizes primary checkout, recovers completed deletes from tombstones, restores UI state; stale reloads dropped via load tokens.",
      "app/workspace_service_load.go", "app/app_operations_test.go, app_projects_load_token_test.go"),
    R("WS-08", "Workspace", "Setup scripts on create (trust-gated)",
      "As a maintainer, I want setup-workspace scripts to run on create, so that env setup is automated.",
      "RunSetup loads .amux/workspaces.json; setup-workspace commands run only if the repo is trusted (fail-closed); run sequentially in workspace root with env vars; failure reported, workspace stays usable.",
      "app/workspace_service.go; process/scripts.go", "process/scripts_test.go"),
    R("WS-09", "Workspace", "Run/archive scripts + output",
      "As a user, I want to run a workspace's run/archive script and see output, so that I can build/serve/archive.",
      "Run/archive command launched in workspace root with env vars as an `sh -c` subprocess (monitored via safego); user-entered scripts bypass the trust gate; repo-config scripts are gated.",
      "process/scripts.go; env.go", "process/scripts_test.go, scripts_trust_gate_test.go"),
    R("WS-10", "Workspace", "Base ref selection/resolution",
      "As a user, I want to pick (or auto-resolve) the base ref, so that the workspace branches from the right point.",
      "Optional base; if empty tries main/master/develop/dev, then remote tracking, then symbolic-ref default, falling back to HEAD; stored in metadata.",
      "app/workspace_service.go; git/branch.go", "git/branch_test.go"),
    R("WS-11", "Workspace", "Metadata persistence + restore",
      "As a user, I want workspace config/tabs to persist across restarts, so that I don't lose state.",
      "Atomic write to ~/.amux/workspaces-metadata/<id>/workspace.json (temp+fsync+rename); includes name/branch/base/repo/root/assistant/scripts/env/openTabs/activeTab/archived; backup recovery on corruption.",
      "data/workspace_store.go; workspace.go", "data/workspace_store_test.go"),
    R("WS-12", "Workspace", "Primary checkout as a workspace",
      "As a user, I want the main repo checkout shown as a workspace, so that I can work the primary branch too.",
      "Synthesized workspace where Root==Repo; always exists (never deleted); current HEAD branch; listed first; UI state can persist.",
      "app/workspace_service_load.go; data/workspace.go", "app/app_projects_loaded_canonical_test.go"),
    R("WS-13", "Workspace", "Lifecycle FSM + reload guard",
      "As the system, I want create/delete/rescan races prevented, so that workspace state can't corrupt.",
      "States active/creating/deleting (mutually exclusive, settle back to active); delete-in-flight blocks rescan import; stale LoadProjects dropped by token; guarded by a phase-map RWMutex plus per-repo mutex and per-workspace flock.",
      "app/workspace_lifecycle_state.go; workspace_reload_guard.go", "app/workspace_lifecycle_fsm_test.go"),
    R("WS-14", "Workspace", "Managed-root path scoping",
      "As the system, I want workspaces scoped to a managed dir, so that cleanup/validation can't touch the repo root.",
      "Workspaces under ~/.amux/workspaces/<project>/<workspace>; paths validated within the project root; strict nesting prevents deleting the project root.",
      "app/workspace_paths.go", "app/workspace_paths_test.go"),
]

# ====================================================================
# COMMAND PALETTE (leader key) + NAVIGATION
# ====================================================================
ROWS += [
    R("PAL-01", "Palette", "Open command palette (C-Space)",
      "As a user, I want C-Space to open a command palette, so that I can run commands without memorizing keys.",
      "C-Space opens a bottom palette of grouped root commands (General, Tabs); stays open until a command completes, Esc, or timeout.",
      "app/app_prefix.go; app_input_keys.go; app_prefix_palette.go", "app/app_prefix_palette_test.go"),
    R("PAL-02", "Palette", "Narrowing + leaf execution",
      "As a user, I want sequences to narrow then execute on a unique leaf, so that multi-key commands feel predictable.",
      "Typing a token narrows the palette; a unique leaf (e.g. t a) executes immediately; ambiguous prefixes stay in narrowing mode.",
      "app/app_prefix.go", "app/app_prefix_test.go"),
    R("PAL-03", "Palette", "Backspace undo / Esc cancel",
      "As a user, I want Backspace to undo and Esc to cancel, so that I can correct or abort a sequence.",
      "Backspace pops the last token (stays at root as harmless undo); Esc exits prefix mode without forwarding the key to the terminal.",
      "app/app_prefix.go; app_input_keys.go", "(trace)"),
    R("PAL-04", "Palette", "C-Space reset / double C-Space literal NUL",
      "As a user, I want C-Space again to reset, and C-Space C-Space to send a literal NUL, so that terminal apps can receive Ctrl-Space.",
      "With a sequence started, C-Space resets to root; with empty sequence, a second C-Space sends NUL (0x00) to the focused terminal and exits prefix.",
      "app/app_input_keys.go; app_prefix.go", "(trace)"),
    R("PAL-05", "Palette", "Open Settings (C-Space S)",
      "As a user, I want C-Space S to open settings, so that I can configure the app.",
      "Sequence S opens the Settings dialog (theme, version/update).",
      "app/app_prefix.go; app_input_messages_dialogs.go", "(trace)"),
    R("PAL-06", "Palette", "Quit (C-Space q)",
      "As a user, I want C-Space q to quit, so that I can exit safely.",
      "Sequence q shows a quit confirmation dialog; confirm proceeds to shutdown.",
      "app/app_prefix.go; app_input_dialogs.go", "(trace)"),
    R("PAL-07", "Palette", "Cleanup tmux (C-Space K)",
      "As a user, I want C-Space K to clean up amux tmux sessions, so that I can clear stale sessions.",
      "Sequence K shows a confirmation; confirm kills all @amux-tagged and amux-* sessions on the server; a success toast confirms the cleanup.",
      "app/app_prefix.go; app_tmux.go", "app/app_tmux_test.go"),
    R("NAV-01", "Navigation", "Focus left/right pane (C-Space h / l)",
      "As a user, I want to move focus between panes by keyboard, so that I can drive everything from the keyboard.",
      "C-Space h/l move focus one pane left/right (relative directional movement, not absolute targets); prefix exits after.",
      "app/app_prefix.go; app_focus_sync.go", "(trace)"),
    R("NAV-02", "Navigation", "Center scroll via palette (C-Space u / d)",
      "As a user, I want C-Space u/d to page-scroll center output, so that I can review logs by keyboard.",
      "When center has an active terminal, the palette exposes u=scroll up / d=scroll down a page; otherwise d maps to delete-workspace.",
      "app/app_prefix.go", "(trace)"),
    R("NAV-03", "Navigation", "Numeric tab jump shown contextually",
      "As a user, I want the palette to offer 1-9 jump only when useful, so that the menu stays relevant.",
      "The Tabs group lists '1-9 jump tab' only when the center has multiple tabs (showNumericTabJump).",
      "app/app_prefix_palette.go; app_prefix_palette_visibility.go", "app/app_prefix_palette_test.go"),
]

# ====================================================================
# DIALOGS
# ====================================================================
ROWS += [
    R("DLG-01", "Dialogs", "File picker (navigate/filter/select)",
      "As a user, I want a file picker to choose a project dir, so that I can add a project by browsing.",
      "Starts at HOME; arrows/wheel move; type to fuzzy-filter; Tab autocompletes into a dir; Backspace goes to parent; Ctrl+H toggles hidden; Enter/'Add as project' confirms; Esc/Cancel aborts.",
      "common/filepicker.go; filepicker_navigation.go", "common/filepicker_test.go, filepicker_navigation_test.go"),
    R("DLG-02", "Dialogs", "Create Workspace dialog + live validation",
      "As a user, I want a validated name input, so that I can't create an invalid workspace.",
      "Single-line input with per-keystroke validation; red error below input on invalid names; Enter blocked while invalid; OK/Cancel buttons; Esc cancels.",
      "common/dialog.go; app/app_input_messages_dialogs.go", "common/dialog_test.go"),
    R("DLG-03", "Dialogs", "Delete Workspace confirmation",
      "As a user, I want to confirm before deleting a workspace + branch, so that I don't lose work accidentally.",
      "Confirm dialog 'Delete workspace \"<name>\" and its branch?'; Yes/No with h/l/tab/click; default No.",
      "common/dialog.go; app/app_input_messages_dialogs.go", "common/dialog_confirm_test.go"),
    R("DLG-04", "Dialogs", "Trust Scripts confirmation",
      "As a user, I want to confirm trusting a repo's scripts, so that I control which repo commands run.",
      "Dialog 'Trust .amux/workspaces.json scripts for \"<ws>\" and run setup now?'; default No; Yes records approval and runs setup.",
      "app/app_input_messages_dialogs.go", "process/scripts_trust_gate_test.go"),
    R("DLG-05", "Dialogs", "Remove Project confirmation",
      "As a user, I want to confirm removing a project, so that I don't drop a project by mistake.",
      "Dialog 'Remove project \"<name>\" from AMUX? This won't delete any files.'; default No.",
      "app/app_input_messages_dialogs.go", ""),
    R("DLG-06", "Dialogs", "Select Assistant / agent picker",
      "As a user, I want to pick an assistant for a new agent tab, so that I can choose which agent runs.",
      "'New Agent' picker lists available assistants with static brand-colour markers; fuzzy filter; up/down/tab navigate; Enter selects; Esc cancels.",
      "common/agent_picker.go; dialog_update.go", "common/dialog_test.go"),
    R("DLG-07", "Dialogs", "Quit + Cleanup-tmux confirmations",
      "As a user, I want explicit confirmations for quit and tmux cleanup, so that destructive actions are deliberate.",
      "Quit: 'Are you sure you want to quit?'. Cleanup: 'Kill all amux-* tmux sessions on server \"<name>\"?'. Both default No; Esc cancels.",
      "app/app_input_dialogs.go; app_input_messages_dialogs.go", ""),
    R("DLG-08", "Dialogs", "Safe defaults + modal input blocking",
      "As a user, I want dialogs to default to the safe option and block background input, so that I focus on the modal.",
      "Dangerous confirmations default to No; while a dialog/file-picker/settings/error is visible, keyboard+click route to the modal (wheel excepted).",
      "common/dialog.go; app/app_input_dialogs.go; app_input_mouse.go", "common/dialog_confirm_test.go"),
    R("DLG-09", "Dialogs", "Toast notifications (info/success/warn/error)",
      "As a user, I want transient status toasts, so that I get feedback without modal interruption.",
      "Info/success ~3s, warning ~4s, error ~5s; level icon + colour; auto-dismiss; rendered at a fixed bottom-center position.",
      "common/toast.go", "common/toast_test.go"),
    R("DLG-10", "Dialogs", "Error overlay dismiss",
      "As a user, I want to dismiss an error overlay easily, so that I can return to work.",
      "When an error overlay shows, any click or key dismisses it and restores normal input handling.",
      "app/app_input_dialogs.go; app_input_keys.go", ""),
]

# ====================================================================
# SETTINGS
# ====================================================================
ROWS += [
    R("SET-01", "Settings", "Theme selection + live preview",
      "As a user, I want to browse themes with live preview, so that I can customize appearance before committing.",
      "Settings lists themes; up/down (or click) cycles with immediate live preview via ThemePreview; not persisted until the dialog closes.",
      "common/settings.go; settings_render.go", "common/settings_test.go"),
    R("SET-02", "Settings", "Version info + update trigger",
      "As a user, I want to see my version and update if newer, so that I can stay current.",
      "Shows current version (or 'Development build'); if an update is available, '[Update to vX]' triggers the upgrade flow.",
      "common/settings.go; app/app_input_messages_dialogs.go", "common/settings_test.go"),
    R("SET-03", "Settings", "Section navigation + close (persist)",
      "As a user, I want Tab/Shift+Tab navigation and a Close that saves, so that I can move through and persist settings.",
      "Tab/Shift+Tab cycle Theme→Update→Close (Update skipped if unavailable; Version is a non-interactive label, not a stop); Esc or Close saves theme to config and closes.",
      "common/settings.go; settings_render.go", "common/settings_nav_test.go"),
]

# ====================================================================
# MOUSE
# ====================================================================
ROWS += [
    R("MOU-01", "Mouse", "Click to focus pane",
      "As a user, I want to click a pane to focus it, so that I can switch focus by mouse.",
      "Left-click in any visible pane moves keyboard focus there; coordinates translated to pane-local for handling.",
      "app/app_input_mouse.go; app_focus_sync.go", "app/app_input_mouse_test.go"),
    R("MOU-02", "Mouse", "Wheel routing to pane under pointer",
      "As a user, I want the wheel to scroll the pane under the pointer, so that scrolling targets what I'm looking at.",
      "Wheel routes to the hovered pane if it can scroll, else the focused pane; blocked while modals/toasts are visible; palette region is click-inert.",
      "app/app_input_mouse.go", "e2e/mouse_scroll_test.go"),
    R("MOU-03", "Mouse", "Welcome / workspace-info link clicks",
      "As a user, I want clickable links on the welcome and workspace-info screens, so that I can act without keybindings.",
      "Click '[Settings]'/'[Add project]' on welcome opens those dialogs; click '[New agent]' on workspace info opens the agent picker.",
      "app/app_ui_click.go", "app/app_ui_click_test.go"),
]

# ====================================================================
# AGENTS  (run coding agents, input/send path, interrupt)
# ====================================================================
ROWS += [
    R("AGT-01", "Agents", "Launch Claude agent",
      "As a user, I want to launch Claude, so that I can interact with the primary agent.",
      "C-Space t a → pick 'claude' → spawns a tmux session running 'claude', creates an attached tab with the agent PTY, shows working indicator.",
      "config/agents.go; center/model_tabs.go; pty/agent.go", "e2e/workspace_agent_test.go, persistence_test.go"),
    R("AGT-02", "Agents", "Launch any registered agent",
      "As a user, I want to launch any of the 9 supported agents (claude, codex, gemini, amp, opencode, droid, cline, cursor, pi), so that I can choose my preferred AI.",
      "Registry lists all 9 in display order with default command + interrupt config; chosen from the assistant picker.",
      "config/agents.go", "config/agents_test.go"),
    R("AGT-03", "Agents", "Override agent command/interrupt in config",
      "As a user, I want to override an agent's command and interrupt behavior, so that I can use custom binaries or stop semantics.",
      "config.json assistants[name].command/interrupt_count/interrupt_delay_ms override built-in defaults (empty command disables); applied at launch/interrupt.",
      "config/config.go; pty/agent.go", "config/agents_test.go; pty/agent_test.go"),
    R("AGT-04", "Agents", "Type + Enter delivered to agent",
      "As a user, I want my keystrokes and Enter to reach the agent intact, so that I can drive it.",
      "Keystrokes route via the tab actor (or direct fallback) to Terminal.SendString; Enter is delivered as \\r (0x0D); activity tagged.",
      "center/model_input_keys.go; pty/terminal.go", "e2e/sendkeys_realagent_test.go, closeloop_test.go"),
    R("AGT-05", "Agents", "Editing keys + paste",
      "As a user, I want arrow/editing keys and paste to work, so that I can edit prompts and paste content.",
      "Letters/arrows/backspace/Ctrl+A/E/K/U etc. convert to bytes and send immediately; paste wrapped in bracketed-paste; local echo suppressed for instant feedback.",
      "common/keys.go; center/model_input_echo.go", "center/model_input_echo_test.go"),
    R("AGT-06", "Agents", "Send Escape",
      "As a user, I want Esc delivered to the agent, so that I can cancel/exit modes in the agent UI.",
      "Esc sends 0x1b to the terminal immediately.",
      "common/keys.go; center/model_input_keys.go", "(trace)"),
    R("AGT-07", "Agents", "Interrupt with Ctrl-C (per-agent count)",
      "As a user, I want Ctrl-C to interrupt the agent, so that I can stop long operations.",
      "Ctrl+C sends 0x03; claude sends 2 signals with 200ms delay, others send 1, per registry/config.",
      "center/model_input_keys.go; pty/agent.go; config/agents.go", "pty/agent_test.go"),
]

# ====================================================================
# ACTIVITY  (busy/idle detection)
# ====================================================================
ROWS += [
    R("ACT-01", "Activity", "Working indicator when agent active",
      "As a user, I want a working indicator while an agent processes, so that I know it isn't hung.",
      "A periodic (~5s, eager on tab create) tmux activity scan detects visible PTY output, sets activeWorkspaceIDs, and renders a working indicator (the dashboard uses a colour change; the animated spinner is only for create/delete).",
      "app/app_tmux_activity.go; activity/*.go", "e2e/activity_restore_test.go; app/app_tmux_activity_test.go"),
    R("ACT-02", "Activity", "Idle indicator after output stops",
      "As a user, I want the indicator to clear when the agent goes idle, so that I know it awaits input.",
      "After the settle window with no visible output, hysteresis marks the tab idle and the spinner disappears.",
      "app/app_tmux_activity_hysteresis.go; activity/*.go", "app/app_tmux_activity_settle_test.go, hysteresis_test.go"),
    R("ACT-03", "Activity", "Single-scanner leader lease (multi-instance)",
      "As a user running multiple amux instances, I want one scanner at a time, so that tmux isn't hammered.",
      "Activity scan acquires a leader lease in tmux global options; owner heartbeats ~5s, followers read its snapshot; stale owners lose the lease via epoch numbering.",
      "app/app_tmux_activity_shared.go; activity/lease.go", "app/app_tmux_activity_test.go; activity/lease_test.go"),
]

# ====================================================================
# PERSISTENCE  (restore across restart, reattach)
# ====================================================================
ROWS += [
    R("PERS-01", "Persistence", "Agent sessions survive amux restart",
      "As a user, I want agents to keep running in tmux after amux exits, so that I can reconnect later.",
      "Agents run in named tmux sessions that persist after amux closes; on restart amux finds them by name and reattaches.",
      "pty/agent.go; tmux", "e2e/persistence_test.go, sidebar_terminal_discovery_test.go"),
    R("PERS-02", "Persistence", "Restore tabs + active tab on restart",
      "As a user, I want my open tabs and active tab restored on restart, so that I continue where I left off.",
      "Tab list + active index persisted (debounced) to workspace.json; on restart center recreates placeholders and reattaches each in order.",
      "app/app_persistence.go; center/model_tabs_restore.go", "e2e/persistence_test.go; center/model_tabs_restore_test.go"),
    R("PERS-03", "Persistence", "Reattach restores pane snapshot/scrollback",
      "As a user, I want history restored on reattach, so that I don't lose prior output.",
      "Reattach captures the tmux pane snapshot (or scrollback for stopped sessions) and restores it into the terminal before live output.",
      "center/model_tabs_session_reattach.go; ptyio/session_restore.go", "center/model_tabs_session_reattach_history_size_test.go; ptyio/session_restore_test.go"),
    R("PERS-04", "Persistence", "Auto-detach excess attached agent tabs",
      "As a user, I want old agent tabs auto-detached past a limit, so that I don't exhaust PTY resources.",
      "AMUX_MAX_ATTACHED_AGENT_TABS bounds attached chat tabs; on exceed, least-recently-focused tabs auto-detach (session stays live) and the change persists.",
      "app/app_attached_limit.go; center/model_tabs_limit.go", "app/app_attached_limit_test.go; center/model_tabs_limit_test.go"),
    R("PERS-05", "Persistence", "Multi-instance orphan-GC safety",
      "As a user running multiple instances, I want GC to not kill another instance's live workspace sessions, so that concurrent use is safe.",
      "Orphan GC only kills sessions for unknown workspace IDs after a grace period and instance scoping, never a peer's active workspace.",
      "app/app_tmux_gc.go", "e2e/multi_instance_gc_test.go; app/app_tmux_gc_safety_test.go"),
]

# ====================================================================
# TRUST  (repo-supplied script gating)
# ====================================================================
ROWS += [
    R("TRUST-01", "Trust", "First-run trust prompt for repo scripts",
      "As a user, I want to approve a repo's scripts before they run, so that I review commands the repo author chose.",
      "When repo-supplied setup/run/archive commands (from .amux/workspaces.json) are not approved, execution is blocked with ScriptsNotTrustedError and the Trust Scripts dialog is shown.",
      "process/scripts.go; script_trust.go; app/app_input_dialogs.go", "process/scripts_test.go, scripts_trust_gate_test.go"),
    R("TRUST-02", "Trust", "Re-gate on .amux/workspaces.json change",
      "As a user, I want edits to the scripts file to require re-approval, so that changed commands can't run silently.",
      "Trust is a SHA-256 of file content; IsTrusted returns false when the hash changes; trusting refuses if content changed since the prompt.",
      "process/script_trust.go; scripts.go", "process/script_trust_test.go"),
    R("TRUST-03", "Trust", "User-entered scripts bypass the gate",
      "As a user, I want scripts I type in the UI to run without prompts, so that I don't self-gate my own input.",
      "Run/archive from ws.Scripts (user input) run directly; only repo-config-sourced commands pass through the trust gate.",
      "process/scripts.go", "process/scripts_trust_gate_test.go"),
    R("TRUST-04", "Trust", "Persistent trust registry (fail-closed)",
      "As a user, I want approvals to persist and default-deny, so that I don't re-approve trusted repos and untrusted ones never run.",
      "Approvals (repo→hash) written atomically to ~/.amux/trusted-scripts.json; missing/corrupt file → empty map (fail-closed).",
      "process/script_trust.go; config/paths.go", "process/script_trust_test.go"),
]

# ====================================================================
# UPDATE  (self-update)
# ====================================================================
ROWS += [
    R("UPD-01", "Update", "Version check vs GitHub latest",
      "As a user, I want to know when a newer amux exists, so that I can stay current.",
      "Updater.Check queries GitHub latest release, compares semver; skips Homebrew/dev builds; returns current/latest/available/notes.",
      "update/updater.go; github.go; version.go", "update/updater_test.go, version_test.go"),
    R("UPD-02", "Update", "Download + checksum verification",
      "As a user, I want updates verified against checksums, so that corrupt/tampered binaries are rejected.",
      "Upgrade fetches checksums.txt, downloads the platform asset, verifies SHA-256; mismatch aborts.",
      "update/updater.go; checksum.go", "update/updater_test.go, checksum_test.go"),
    R("UPD-03", "Update", "Safe extract + atomic install w/ rollback",
      "As a user, I want a failed update to never break my binary, so that I can recover.",
      "Extract only the 'amux' entry (0755); stage in target dir (avoid EXDEV); back up to .bak; atomic rename; restore .bak on failure.",
      "update/install.go; updater.go", "update/updater_upgrade_test.go, extract_hostile_test.go"),
    R("UPD-04", "Update", "Install-method + permission guards",
      "As a user, I want correct guidance for my install method/permissions, so that I update the right way.",
      "Homebrew → 'brew upgrade amux'; go install → 'go install ...@latest'; no write permission → 'try sudo'.",
      "update/install.go; buildinfo.go; updater.go", "update/updater_test.go"),
]

# ====================================================================
# TMUX  (session lifecycle, discovery, cleanup, GC)
# ====================================================================
ROWS += [
    R("TMUX-01", "Tmux", "Session creation with tags + per-session options",
      "As a user, I want each agent/terminal in its own tagged tmux session, so that lifecycles are isolated and discoverable.",
      "new-session -As creates/attaches; sets @amux tags (workspace/tab/type/assistant/instance/owner/lease); disables prefix/status/mouse per session; attaches.",
      "tmux/command.go; tmux.go", "tmux/send_test.go, create_pipeline_test.go"),
    R("TMUX-02", "Tmux", "Session discovery from live tmux",
      "As a user, I want amux to find tagged sessions (incl. from other instances), so that I can reattach and manage them.",
      "Queries sessions by @amux/@amux_workspace/@amux_type tags and hydrates tabs/terminals from the results.",
      "app/app_tmux_discover.go; tmux/tags.go", "app/app_tmux_discover_test.go"),
    R("TMUX-03", "Tmux", "Session state + activity queries",
      "As a user, I want to know which sessions are alive/active, so that I see running vs dead agents.",
      "AllSessionStates aggregates pane-dead per session; activity queries use window_activity timestamps and an activity window.",
      "tmux/tmux.go; activity.go", "tmux/tmux_test.go, activity_test.go"),
    R("TMUX-04", "Tmux", "Periodic sync ticker (discover+status+GC)",
      "As a user, I want session changes reflected without manual refresh, so that the UI stays accurate.",
      "TmuxSyncTick (default ~7s) discovers tabs, syncs pane status, and runs orphan GC; token-guarded against stale messages.",
      "app/app_tmux_sync.go; app_tmux_discover.go", "app/app_tmux_sync_test.go"),
    R("TMUX-05", "Tmux", "Cleanup command kills amux sessions",
      "As a user, I want one action to kill all amux sessions, so that I can reset cleanly.",
      "Confirmed C-Space K kills @amux=1-tagged sessions and amux-* prefixed sessions; a success toast confirms the cleanup (no count).",
      "app/app_tmux.go; app_prefix.go", "app/app_tmux_test.go"),
    R("TMUX-06", "Tmux", "Orphan + stale-detached GC",
      "As a user, I want sessions for deleted workspaces / crashed idle agents cleaned up, so that tmux doesn't accumulate junk.",
      "Orphan GC kills sessions for unknown workspace IDs after grace; stale-detached GC kills unattached idle agent sessions past TTL; instance-scoped.",
      "app/app_tmux_gc.go", "app/app_tmux_gc_test.go, app_tmux_gc_safety_test.go"),
    R("TMUX-07", "Tmux", "monitor-activity enabled globally + per-session",
      "As a user, I want activity timestamps tracked, so that busy/idle detection works.",
      "set-option -g monitor-activity on at startup/server change; per-session -w monitor-activity on at create.",
      "tmux/activity.go; command.go", "tmux/activity_test.go"),
    R("TMUX-08", "Tmux", "Missing-tmux detection + install hint",
      "As a user, I want a clear message if tmux is missing, so that I know how to install it.",
      "EnsureAvailable LookPaths tmux; on absence shows an OS-specific install hint (brew/apt/dnf/pacman); tab actions blocked with a toast.",
      "tmux/tmux.go; app/app_prefix.go", "(trace)"),
    R("TMUX-09", "Tmux", "Stale test-socket janitor on startup",
      "As a user, I want stale tmux test sockets cleaned at startup, so that amux starts quickly.",
      "Background scan of /tmp/tmux-* removes dead amux-test-*/amux-e2e-check-* sockets (75ms dial probe).",
      "cmd/amux/tmux_socket_janitor.go", "cmd/amux/tmux_socket_janitor_test.go"),
]

# ====================================================================
# OPS / CONFIG  (logging, profiling, paths, CLI)
# ====================================================================
ROWS += [
    R("OPS-01", "Ops", "Daily log file + retention",
      "As an operator, I want daily logs with retention, so that I can debug without unbounded disk use.",
      "Logs at ~/.amux/logs/amux-YYYY-MM-DD.log; AMUX_LOG_RETENTION_DAYS (default 14) prunes older files; AMUX_LOG_LEVEL sets verbosity.",
      "logging/logger.go; cmd/amux/main.go", "logging/logger_test.go"),
    R("OPS-02", "Ops", "Profiling + pprof + signal dumps",
      "As an operator, I want runtime profiling/diagnostics toggles, so that I can investigate perf/hangs.",
      "AMUX_PROFILE emits timing snapshots (interval via AMUX_PROFILE_INTERVAL_MS); AMUX_PPROF exposes pprof on 127.0.0.1; AMUX_DEBUG_SIGNALS + SIGUSR1 dumps goroutines.",
      "cmd/amux/main.go; perf/perf.go", "perf/perf_test.go"),
    R("OPS-03", "Ops", "PTY byte-level tracing",
      "As an operator, I want byte-level SEND/RECV traces, so that I can debug dropped/delayed input.",
      "AMUX_PTY_TRACE=1 or assistant list writes traces (RECV=agent→amux, SEND=amux→agent incl. delayed CR) to the log/temp dir.",
      "center/model_pty_trace.go; README Operations", "center/model_pty_trace_test.go"),
    R("OPS-04", "Config", "Config + data locations under ~/.amux",
      "As a user, I want standard config/data locations, so that I know where state lives.",
      "~/.amux: config.json, projects.json, workspaces/, workspaces-metadata/, trusted-scripts.json, logs/; dirs created at startup.",
      "config/paths.go; config.go", "config/paths_test.go"),
    R("OPS-05", "Config", "config.json assistants + UI settings persist",
      "As a user, I want assistants and UI prefs configurable + persisted, so that they survive restarts.",
      "config.json 'assistants' (command/interrupt) and 'ui' (show_keymap_hints, theme, tmux_server, tmux_config, tmux_sync_interval); per-section decode isolation; atomic save.",
      "config/config.go; user_settings.go", "config/config_test.go, user_settings_test.go"),
    R("OPS-06", "Config", "Custom tmux server name (env/config)",
      "As a user, I want a custom tmux server name, so that I can run isolated amux instances.",
      "AMUX_TMUX_SERVER (or ui.tmux_server) overrides default 'amux'; passed via -S to all tmux commands.",
      "tmux/tmux.go; config/user_settings.go", "tmux/tmux_test.go"),
    R("OPS-07", "Config", "CLI: --version and TTY guard",
      "As a user, I want --version and a clear non-TTY error, so that I can script checks and get feedback when run non-interactively.",
      "--version/-v prints version/commit/date and exits 0; if any of stdin/stdout/stderr is not a TTY, prints a requires-TTY message and exits 1.",
      "cmd/amux/main.go", "cmd/amux/main_test.go"),
]

def apply_results(rows):
    """Overlay Phase 2/3/4 results from results.json (if present) onto rows.

    results.json maps id -> {status, test_method, result, errors_found,
    fix_applied, retest_result}; only provided keys are overwritten so the
    file can be filled incrementally across phases.
    """
    import json
    rp = os.path.join(HERE, "results.json")
    if not os.path.exists(rp):
        return rows
    with open(rp) as f:
        results = json.load(f)
    by_id = {r["id"]: r for r in rows}
    for rid, upd in results.items():
        if rid in by_id:
            for k, v in upd.items():
                if k in COLUMNS:
                    by_id[rid][k] = v
    return rows


def write_outputs(rows):
    import json
    # Structured JSON for downstream agents/workflows
    json_path = os.path.join(HERE, "FEATURES.json")
    with open(json_path, "w") as f:
        json.dump(rows, f, indent=2)
    # Canonical CSV
    csv_path = os.path.join(HERE, "FEATURES.csv")
    with open(csv_path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=COLUMNS)
        w.writeheader()
        for r in rows:
            w.writerow(r)

    # Markdown mirror
    md_path = os.path.join(HERE, "FEATURES.md")
    with open(md_path, "w") as f:
        f.write("# amux Feature Audit\n\n")
        f.write(f"Total user stories: **{len(rows)}**\n\n")
        cur_area = None
        for r in rows:
            if r["area"] != cur_area:
                cur_area = r["area"]
                f.write(f"\n## {cur_area}\n\n")
            f.write(f"### {r['id']} — {r['feature']}  `[{r['status']}]`\n")
            f.write(f"- **Story:** {r['user_story']}\n")
            f.write(f"- **Expected:** {r['expected_behavior']}\n")
            f.write(f"- **Source:** {r['source']}\n")
            f.write(f"- **Tests:** {r['tests'] or '—'}\n")
            if r["test_method"]:
                f.write(f"- **Test method:** {r['test_method']}\n")
            if r["result"]:
                f.write(f"- **Result:** {r['result']}\n")
            if r["errors_found"]:
                f.write(f"- **Error:** {r['errors_found']}\n")
            if r["fix_applied"]:
                f.write(f"- **Fix:** {r['fix_applied']}\n")
            if r["retest_result"]:
                f.write(f"- **Re-test:** {r['retest_result']}\n")
            f.write("\n")

    # Summary roll-up
    sum_path = os.path.join(HERE, "SUMMARY.md")
    by_status = {}
    by_area = {}
    for r in rows:
        by_status[r["status"]] = by_status.get(r["status"], 0) + 1
        by_area.setdefault(r["area"], {})
        by_area[r["area"]][r["status"]] = by_area[r["area"]].get(r["status"], 0) + 1
    with open(sum_path, "w") as f:
        f.write("# Feature Audit — Status Summary\n\n")
        f.write(f"Total user stories: **{len(rows)}**\n\n")
        f.write("## By status\n\n")
        for s in ["Catalogued", "Pass", "Error", "Fixed", "Verified"]:
            if s in by_status:
                f.write(f"- {s}: {by_status[s]}\n")
        f.write("\n## By area\n\n")
        f.write("| Area | Total | " + " | ".join(["Catalogued", "Pass", "Error", "Fixed", "Verified"]) + " |\n")
        f.write("|------|------|" + "|".join(["------"] * 5) + "|\n")
        for area in sorted(by_area):
            counts = by_area[area]
            total = sum(counts.values())
            cells = [str(counts.get(s, 0)) for s in ["Catalogued", "Pass", "Error", "Fixed", "Verified"]]
            f.write(f"| {area} | {total} | " + " | ".join(cells) + " |\n")

    return csv_path, md_path, sum_path


if __name__ == "__main__":
    rows = apply_results(ROWS)
    paths = write_outputs(rows)
    print(f"Wrote {len(rows)} rows to:")
    for p in paths:
        print(f"  {p}")
