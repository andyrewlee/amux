# Plan 060: Prefix palette intermittently fails after workspace restore

> Found during plan-059/036 stack execution (2026-09-26): `TestShelveRestorePurgeLifecycle` fails ~2/3 of runs at `lifecycle_shelve_test.go:126` — `sendPrefixCommand` waits 5s for the palette footer "Esc cancel" and times out. Reproduces identically at `7c530ee`, at the 032/033 baseline commit, and under plan-059 — **pre-existing**, not introduced by this stack.

## Symptoms

- Deterministic location: the *third* prefix use (the post-restore `sendPrefixCommand "h"` at line 126), after step-2 restore. Earlier prefix arms in the same test work.
- Screen at timeout: normal dashboard, `shelveme ?` row — no palette, no visible dialog.
- Failure runs take longer overall (30–58s vs ~20s pass), consistent with an extra wait/timeout upstream slowing the sequence.

## Working hypotheses (unverified)

1. **Swallowed keypress**: `a.err` (recovered panic or error overlay) or a pending modal consumes the `\x00` leader byte in `handleKeyPress`/`handlePreSwitchInput` before `isPrefixKey` runs. `enterPrefix` itself is unconditional, so a key that reaches it always arms. The `?` row badge may hint the restore left a pending/in-flight state.
2. **Queued overlay storm**: restore emits async results; a stuck `pendingOverlayOpens` entry could keep `overlayBusy` true and starve the palette arm path.
3. **Input routing race**: restore re-binds focus to the center pane; if a terminal-focused pane eats NUL before the global prefix check, the palette never arms — but earlier arms in the same test suggest the trigger is the restore-completion interleaving, not plain focus.

## Suggested approach

1. Reproduce with the app log captured: modify `waitForPrefixPalette` locally (don't commit) to dump `readLogTail(t, home)` on timeout, or make the failure path always include the log tail.
2. Check for `panic in app.Update` / `a.err` entries in the log at the failure point — a recovered panic would explain exactly one swallowed keypress.
3. If no panic: trace whether `prefixActive` was set and cleared immediately (`prefixTimeoutMsg` race) or the byte never arrived.
4. Fix the root cause — not the test timeout. If it's a lost keypress on the modal/err path, the fix belongs in input routing, not `prefixArmTimeout`.

## Verification

- `env -u AMUX_WORKSPACES_ROOT go test ./internal/e2e -run '^TestShelveRestorePurgeLifecycle$' -count=10` — 10/10 pass.
- `env -u AMUX_WORKSPACES_ROOT make devcheck` — e2e green.
