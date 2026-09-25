# Plan 028: Replace the unmaintained `atotto/clipboard` fallback with explicit tool detection

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/ui/common/clipboard.go go.mod go.sum`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`atotto/clipboard` v0.1.4 (Feb 2021, unmaintained — no tagged release in ~5 years) is used in exactly one file, only as the non-darwin/pbcopy-failure path. On Linux/BSD it itself just shells out to `xclip`/`xsel`/`wl-copy` — an abandoned wrapper duplicating what ~30 lines of explicit detection does, carrying supply-chain surface for nothing. The alternative swap (`golang.design/x/clipboard`) trades one dep for another with its own quirks; the honest fix is hand-rolling the tool fallback the file already half-implements — OR deciding the dep is fine and closing the question. This plan implements the drop; the STOP conditions cover the keep-it verdict.

## Current state

`internal/ui/common/clipboard.go:52-63`:

```go
// CopyToClipboard writes text to the system clipboard with a macOS pbcopy fallback.
func CopyToClipboard(text string) error {
	// Prioritize pbcopy on macOS as it is more reliable in various environments.
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback to library for other OS or if pbcopy fails.
	return clipboard.WriteAll(text)
}
```

Constraints to honor (from the file and its callers):

- `clipboard.go:36-39` documents a contract about NOT holding a mutex during shell-out (verify what mutex it refers to — read lines 20-50; probably the copy-button's state mutex or a clipboard lock — preserve whatever ordering it specifies).
- `CopyToClipboardWithLog` (:40-50) wraps it — unchanged.
- Non-darwin tool order: `wl-copy` (Wayland) → `xclip`/`xsel` (X11). `atotto` itself picks similarly; `which`-style detection via `exec.LookPath`.
- Failure semantics: return an error when no tool exists (the WithLog wrapper logs it) — same as today.

## Commands you will need

| Purpose    | Command                                  | Expected on success |
|------------|------------------------------------------|---------------------|
| Build      | `go build ./internal/ui/common`          | exit 0              |
| Unit tests | `go test ./internal/ui/common -count=1`  | all pass            |
| Tidy       | `go mod tidy`                            | dep removed (check `go mod why github.com/atotto/clipboard`) |
| Full gate  | `make devcheck`                          | exit 0              |

## Scope

**In scope**:
- `internal/ui/common/clipboard.go` — the fallback implementation.
- `internal/ui/common/clipboard*_test.go` — tool-selection tests.
- `go.mod`/`go.sum` — tidy.

**Out of scope**:
- `tea.SetClipboard`/OSC52 — the auditor verified it writes OSC52 to the outer terminal, a weaker guarantee than a real clipboard tool; not a substitute (don't mix).
- Callers (`CopyToClipboardWithLog` callers elsewhere) — signature unchanged.
- Windows clipboard — `atotto` covers it today; on `runtime.GOOS=="windows"` keep whatever mechanism exists… actually check: `atotto` handles windows via syscall not shell-out. If dropping the dep removes windows support, the plan must either (a) keep windows via a minimal `clip` exec (`exec.Command("clip")` works on Windows) or (b) STOP — evaluate in Step 1.

## Git workflow

- Branch: `advisor/028-clipboard-dep` off `main`.
- Commit style: `chore: replace atotto/clipboard with explicit tool fallback`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Map what `atotto` actually provides per-platform

Read the vendored behavior: darwin → pbcopy (already hand-rolled above the dep call); linux/bsd → `xsel --input`/`xclip`/`wl-copy`/`termux-clipboard-set`; windows → `clipboard_win` syscall; plan9 → `/dev/snarf`. Determine which platforms amux realistically ships to (`release.yml` targets — check `.goreleaser.yml`/`release.yml`: likely darwin+linux; if windows is shipped, the replacement MUST cover it — `exec.Command("clip")` is the honest Windows fallback).

**Verify**: write down the per-platform tool list your replacement covers vs what `atotto` covered; anything dropped needs a deliberate note in the PR.

### Step 2: Implement the explicit fallback

Replace the `clipboard.WriteAll` call with `copyWithTool(text)`:

```go
// copyWithTool tries the platform clipboard tools in preference order —
// the same set atotto/clipboard shelled out to, without the dependency.
func copyWithTool(text string) error {
```

Per-platform candidates:
- darwin: `pbcopy` (already above — the fallback covers the pbcopy-failure case too, so its candidate list may include `pbcopy` again harmlessly or skip it).
- linux/bsd: `wl-copy`, `xclip -selection clipboard`, `xsel --clipboard --input`.
- windows: `clip`.
Use `exec.LookPath` to pick the first present tool; none → error `no clipboard tool found (tried: wl-copy, xclip, xsel, clip)`.

Preserve the no-mutex-during-shell-out contract — read `clipboard.go:20-50` for what it says and keep it true.

**Verify**: `go build ./internal/ui/common` → exit 0.

### Step 3: Tests

`clipboard_test.go` (create if absent): the tool-selection logic is testable by injecting the candidate list/`LookPath` (small seam: `var lookPath = exec.LookPath` or a candidate table — check whether the file already has test seams). Cases: no tools → error; first tool present → used; args correct per tool. Do NOT exec real clipboard tools in tests — fake `exec.Command` via the seam the file has (`var execCommand = exec.Command` pattern if present, else add it minimal).

**Verify**: `go test ./internal/ui/common -count=1 -v` → pass.

### Step 4: Tidy + gates

`go mod tidy` → `atotto/clipboard` leaves go.mod (verify no other importer: `grep -rn 'atotto' --include='*.go' .` → none). `make devcheck` → exit 0.

**Verify**: `go mod why github.com/atotto/clipboard` → not needed.

## Test plan

- Tool-selection unit tests (Step 3); the real clipboard path is manual-verification-only on Linux (note in PR — CI can't paste).
- Verification: `go test ./internal/ui/common -count=1` + `make devcheck`.

## Done criteria

- [ ] `atotto/clipboard` is gone from go.mod and all imports.
- [ ] darwin pbcopy path unchanged; linux/bsd tool fallback covers `wl-copy`/`xclip`/`xsel`; windows decision recorded (covered or documented limitation).
- [ ] The no-mutex-during-shell-out contract preserved.
- [ ] `go test ./internal/ui/common`, `make devcheck` pass.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- Windows is a shipped target AND `clip` can't be verified — either keep `atotto` just for windows (build-tagged file) or report the trade-off rather than silently dropping windows clipboard.
- The file's mutex contract can't be preserved with the new shape — report.
- amux ships to a platform whose clipboard only `atotto` reaches (e.g. plan9/termux actually used) — weigh before dropping.
- A reviewer or operator prefers keeping the dep — the finding is MED-worth by design; a "keep, monitored" verdict is legitimate. Record the decision in the index rather than forcing the change.

## Maintenance notes

- The candidate-tool table is now the documentation of supported clipboard paths — keep it accurate when adding tools (e.g. `termux-clipboard-set` if termux support ever matters).
- If Wayland/X11 detection ever needs to be session-aware (`$WAYLAND_DISPLAY`), the table ordering is where that logic goes — currently preference-order only.
