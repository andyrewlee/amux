# Plan 044: Accept bracketed paste as text in custom configuration editors

> **Executor instructions:** Implement only this plan, following the verification steps. Do not commit or push. Update the index status only if the reviewer delegates that edit.
>
> **Drift check first:** Run `git status --short`, `git diff --stat 7c530ee..HEAD -- internal/ui/common/env_dialog.go internal/ui/common/scripts_dialog.go internal/ui/common/settings.go internal/ui/common/settings_assistants.go internal/ui/common/field_paste.go internal/ui/common/field_paste_test.go internal/ui/common/editor_paste_test.go internal/app/app_editor_paste_test.go README.md docs/CONFIG.md docs/ORCHESTRATION.md`, and `git diff HEAD --` with the same paths. Read untracked in-scope files. Compare Current state to live code; the planned baseline includes the dirty tree audited on 2026-09-26. Preserve existing source/docs edits and do not stash/clean/reset them.

## Status

- **Priority:** P2
- **Effort:** S
- **Risk:** LOW–MED
- **Depends on:** none; coordinate common-package/doc edits with plan 036
- **Category:** bug / usability
- **Planned at:** commit `7c530ee`, plus audited uncommitted working tree, 2026-09-26

## Why this matters

Environment, lifecycle-script, assistant-command, and tmux settings editors accept typed keys but silently discard bracketed paste messages. Long commands and values are exactly the inputs users commonly paste. Treat paste as text through the existing field validation, without executing shortcuts or accepting accidental multiline commands.

## Current state

`internal/ui/common/env_dialog.go:143–150` and `scripts_dialog.go:70–77` gate all input like this:

```go
if !d.visible {
    return d, nil
}
keyMsg, ok := msg.(tea.KeyPressMsg)
if !ok {
    return d, nil
}
```

`settings.go:185–243` switches only on mouse clicks and KeyPressMsg. `internal/app/app_input_dialogs.go:42–50` forwards overlay input, including paste, to the widget and consumes it; it does not convert PasteMsg into keypresses.

Existing text helpers in `settings.go:288–295` are the local convention:

```go
func keepRunes(s string, keep func(rune) bool) string {
    var b strings.Builder
    for _, r := range s {
        if keep(r) {
            b.WriteRune(r)
        }
    }
    return b.String()
}
```

`isPrintableFieldRune` uses `unicode.IsGraphic`; `isDurationRune` restricts tmux-sync text. `EnvDialog.appendFocusedText`, `ScriptsDialog.appendFocusedText`, `SettingsDialog.appendFocusedTmuxText`, and assistant helpers own field mutation. Environment and assistant add modes maintain two separate text fields; lifecycle mode, theme, update, and close rows are not text fields. Env maps are copied so cancel discards edits. Preserve these conventions.

## Commands you will need

| Purpose | Command | Expected result |
|---|---|---|
| Baseline | `go test ./internal/ui/common -count=1` | PASS |
| Paste regressions | `go test ./internal/ui/common -run 'TestFieldPaste|TestEditorPaste' -count=1 -v` | Named tests PASS after implementation |
| Overlay route | `go test ./internal/app -run TestEditorPasteOverlay -count=1 -v` | PASS |
| Package tests | `go test ./internal/ui/common -count=1` | PASS |
| Real input | `env -u AMUX_WORKSPACES_ROOT make verify-loop` | Both raw-agent input tests PASS, not SKIP |
| Required checks | `env -u AMUX_WORKSPACES_ROOT make devcheck` | Exit 0 or separately reported existing baseline failure |
| Lint | `make lint-strict-new` | Exit 0 |
| Hygiene | `git diff --check` | Exit 0 |

The audit's devcheck had real-e2e failures and verify-loop passed. Inherited `AMUX_WORKSPACES_ROOT` later proved to defeat test HOME isolation; [plan 059](059-isolate-e2e-workspace-root.md) repairs that setup. Use sanitized commands until it lands. Plan 036 owns the separately reproduced agent-picker failure. Require green final gates or report BLOCKED with exact remaining failures; do not assume failures are unrelated or weaken the gates.

## Scope

**In scope:** `internal/ui/common/env_dialog.go`, `scripts_dialog.go`, `settings.go`, `settings_assistants.go`; new `internal/ui/common/field_paste.go`, `field_paste_test.go`, `editor_paste_test.go`; new `internal/app/app_editor_paste_test.go`; `README.md`, `docs/CONFIG.md`, `docs/ORCHESTRATION.md`; index status if delegated.

**Out of scope:** Persistence, config schemas, textinput-backed generic dialogs, PTY/agent paste encoding, multiline editors, clipboard reads, field length-limit changes, cursor/editing redesign, and logging input values.

## Git workflow

Use the operator's checkout (optional isolated branch `advisor/044-editor-paste`). Save the initial status/diff for comparison and preserve unrelated dirty changes. No stash, clean, reset, commit, push, or PR without authorization. Test values must be synthetic; never paste or print real credentials.

## Steps

### Step 1: Define and test a shared single-line paste policy

Add a small helper in `field_paste.go`, used only by these custom editors. Decision: normalize CRLF and lone CR to LF, take the first logical line (do not trim ordinary leading/trailing spaces), discard nongraphic control runes, and then let the destination field's existing filter run. Tabs/control bytes are dropped; text after the first newline is not appended. Do not use the display sanitizer: an ANSI-looking printable suffix is input text, and display stripping would silently reinterpret command content. Paste never submits, cancels, navigates, deletes rows, or toggles a setting. Empty first lines append nothing. This deliberately stays a single-line editor, and the docs must state that only the first pasted line is used.

Add tests for LF, CRLF, lone CR, leading/trailing ordinary spaces, empty content, C0/DEL/C1 controls, Unicode combining characters, and invalid UTF-8 handling consistent with range/graphic filtering (no invalid string emitted).

**Verify:** `go test ./internal/ui/common -run TestFieldPaste -count=1 -v` → PASS for the new helper with no external clipboard access.

### Step 2: Route paste directly to the focused text field

Handle `tea.PasteMsg` before the KeyPressMsg-only gate in EnvDialog/ScriptsDialog and as its own case in SettingsDialog. Use the shared helper once, then append through existing field helpers. In env/assistant add mode, append to the current name/value or name/command field without calling the key shortcut switch. Tmux sync still uses `isDurationRune`; normal text fields use their existing printable filter. Noneditable rows ignore paste entirely. Do not synthesize a KeyPressMsg from pasted content because that would risk interpreting structural shortcuts or diverging from multi-rune semantics.

Add table-driven tests named `TestEditorPaste...` for env existing-value and both add fields, all four script command fields, assistant existing-command and both add fields, and all three tmux fields. Test hidden editors and noneditable rows. Assert no returned submit/cancel/theme/update command and no cursor movement.

**Verify:** `go test ./internal/ui/common -run 'TestFieldPaste|TestEditorPaste' -count=1 -v` → all PASS; `go test ./internal/ui/common -count=1` → existing keyboard/save/cancel tests PASS.

### Step 3: Verify overlay integration and document behavior

Add `app_editor_paste_test.go` using `newEnvTestHarness` from `app_input_workspace_env_test.go`, `newScriptsTestHarness` from `app_input_workspace_scripts_test.go`, and the Settings setup in `app_input_messages_dialogs_assistants_test.go:TestHandleSettingsResult_PersistsAssistantCommandEdit`. That Settings test uses `NewHarness` and pins ConfigPath under t.TempDir; retain this isolation. Open an editor through the real app overlay state, dispatch a PasteMsg, and verify the overlay's edited value plus that paste was consumed rather than forwarded to center/terminal. Cover cancel leaving original workspace/config values unchanged and save/result read-back retaining the pasted value; use temp persistence or existing seams rather than writing the user's config.

Document bracketed paste and first-line-only behavior in all three contract docs, including that paste does not submit or toggle rows. Do not add new bindings.

**Verify:** `go test ./internal/app -run TestEditorPasteOverlay -count=1 -v` → PASS; `env -u AMUX_WORKSPACES_ROOT make verify-loop` → both required tests PASS; `git diff --check` → clean.

### Step 4: Run final gates and check scope

Run devcheck and changed-code lint, retaining separate evidence for unrelated baseline failures. No renderer algorithm changes are needed; if implementation introduces wrapping/layout changes, stop rather than widening this plan into a render redesign.

**Verify:** `make lint-strict-new` → exit 0; `env -u AMUX_WORKSPACES_ROOT make devcheck` → exit 0 or recorded independent baseline blocker; `git diff --name-only` and `git status --short` → implementation-added changes stay in the allowlist beyond starting dirty work.

## Test plan

Tests must use actual PasteMsg values and assert exact stored synthetic text. Include pasted j/k, spaces, and control characters so text cannot become a shortcut. Check env scope routing, duplicate/add-name validation still running on Enter, duration filtering, cancel and save, hidden editor no-op, mode-row no-op, and first-line handling. Follow the existing keyboard tests in `env_dialog_test.go`, `scripts_dialog_test.go`, `settings_assistants_test.go`, and `settings_nav_test.go`; do not require a system clipboard or live credentials.

## Done criteria

All required final gates must pass before this plan is marked DONE. A known or newly discovered gate failure leaves the plan BLOCKED with the exact command/test and evidence; recording a failure is not a substitute for passing. Do not expand implementation scope to repair other findings. Until [plan 059](059-isolate-e2e-workspace-root.md) lands, remove `AMUX_WORKSPACES_ROOT` from every command reaching e2e, including devcheck, verify-loop, and test-race-tmux.

- [ ] New helper/editor tests and `TestEditorPasteOverlay...` execute and PASS.
- [ ] `go test ./internal/ui/common -count=1`, `env -u AMUX_WORKSPACES_ROOT make verify-loop`, and `make lint-strict-new` pass.
- [ ] `env -u AMUX_WORKSPACES_ROOT make devcheck` result is accurately recorded with any baseline blockers.
- [ ] README, CONFIG, and ORCHESTRATION document the same paste policy.
- [ ] `git diff --check` passes, no secret values are logged/tested, and no out-of-scope implementation files are edited.
- [ ] The index owner receives completion and verification details.

## STOP conditions

Stop if app overlay dispatch does not pass PasteMsg to these widgets, if a requested behavior requires multiline editing/persistence/schema changes, if existing tests require a conflicting paste policy, or after two unsuccessful focused fixes. Do not silently reinterpret a pasted newline as submission. Do not touch the separate configuration-map race or unreadable-config persistence plans.

## Maintenance notes

Every future custom text field should implement both typed text and PasteMsg through the same destination filter. Keep paste sanitization shared to avoid per-widget newline/control drift. Reviewers should verify cancel isolation and ensure test failures never print real environment values.
