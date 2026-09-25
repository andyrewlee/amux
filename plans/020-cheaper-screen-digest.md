# Plan 020: Replace SHA-256 screen digest with a cheaper change signal

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- internal/ui/center/model_activity_visibility.go internal/vterm/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW
- **Depends on**: none
- **Category**: perf
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`visibleScreenDigest` UTF-8-encodes every cell of the live screen into a scratch buffer and SHA-256s it, once per output flush per tab — under the same `tab.mu` the render snapshot path contends for. The digest only feeds an equality check ("did visible content change since last flush?"), so collision strength is irrelevant. The work is bounded (~O(W×H) + crypto per flush) but avoidable — either hash a non-crypto function or, better, lean on the vterm epoch/dirty tracking that already knows which lines changed.

## Current state

`internal/ui/center/model_activity_visibility.go:197-245`:

```go
func visibleScreenDigest(term *vterm.VTerm) [16]byte {
	...
	screen, _ := term.RenderBuffers()
	hash := sha256.New()
	var scratch []byte
	for _, row := range screen {
		...                       // trim trailing blanks, utf8.AppendRune each cell
		scratch = append(scratch, '\n')
		hash.Write(scratch)
	}
	var sum [sha256.Size]byte
	var digest [16]byte
	copy(digest[:], hash.Sum(sum[:0])[:16])
	return digest
}
```

Called at ~:147 per flush that carried pending visible output, under `tab.mu` (the `hash.Write(scratch)` also shows up at :240).

The alternative signal that already exists: `internal/vterm` tracks per-line dirty epochs (`markDirtyLine`/`markDirtyRange` — see `internal/vterm/*.go` grep for `epoch\|dirtyEpoch\|dirty`) — the render snapshot path uses it. If an epoch/dirty version per line is exposed, hashing `rowIndex+epoch` pairs is O(dirty-lines) not O(W×H) and drops the UTF-8 pass entirely. Verify `RenderBuffers`'s cost too — if it allocates per call, prefer the epoch path.

Caveat to preserve: the comment at :201-205 explains WHY the live screen (not viewport) is hashed — user scrollback must not fake "activity". Any replacement must keep that semantic.

## Commands you will need

| Purpose    | Command                                      | Expected on success |
|------------|----------------------------------------------|---------------------|
| Build      | `go build ./internal/ui/center ./internal/vterm` | exit 0            |
| Unit tests | `go test ./internal/ui/center ./internal/vterm -count=1` | all pass |
| Perf       | `make perf-check` (hot-tabs preset is the relevant one) | within or better than baseline |
| Lint       | `make lint && make lint-strict-new`          | exit 0              |
| Full gate  | `make devcheck`                              | exit 0              |

## Scope

**In scope**:
- `internal/ui/center/model_activity_visibility.go` — the digest function and its call site.
- `internal/vterm/` — ONLY if exposing an existing epoch/dirty counter requires a small accessor (e.g. `DirtyEpoch(y int) uint64` or a bulk `LineEpochs()`); prefer using what's already exported.

**Out of scope**:
- The flush cadence / call site frequency — the digest is the target, not when it runs.
- `RenderBuffers` internals.
- Any change to what "changed" means semantically (the live-screen rule stays).

## Git workflow

- Branch: `advisor/020-cheaper-screen-digest` off `main`.
- Commit style: `perf: use dirty-epoch signal for visible-screen digest`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Pick the replacement signal

`grep -rn 'epoch\|dirtyEpoch\|DirtyEpoch\|LineDirty\|lineEpoch' internal/vterm --include='*.go' | grep -v _test | head -20` — find the cheapest existing change signal:

- If a per-line epoch exists and is exposed (or a one-line accessor exposes it): digest = fold `(rowIndex, epoch)` pairs into `hash/maphash` or FNV (`hash/fnv`) — O(H) worst, O(1)-per-line cost, no UTF-8, no crypto.
- If no per-line signal exists: keep the full-screen scan but swap `sha256.New()` for `hash/maphash` (`maphash.Hash` — process-local seed, perfect for in-process equality) — same scan, much cheaper hash. Still returns a fixed-size digest; adjust the `[16]byte` shape if needed (a `uint64` digest suffices — `visibleDigestHash` callers must then compare uint64s; check the call sites).

Choose whichever is honestly smaller; document the choice in the comment replacing the current rationale.

**Verify**: `go build ./internal/ui/center` → exit 0.

### Step 2: Preserve the live-screen semantic

Whatever the signal, it must reflect the *live* screen content (not the scrollback viewport) — epochs on the live buffer are correct; if epochs live on a different buffer representation, verify the mapping (check `RenderBuffers` return and which buffer is "live").

**Verify**: write a unit test — mutate the live screen below the viewport (write a line that lands outside the scrolled view), assert the digest changes; scroll the viewport without new output, assert it does NOT (unless scrollback affects live lines — reason it through the code, don't guess).

### Step 3: Equality semantics + tests

Two calls with no intervening change MUST produce equal digests; a one-cell change MUST flip it. For `maphash`, note its per-process seed makes digests non-comparable across processes — fine here (digest never leaves the process) but say so in a comment.

**Verify**: `go test ./internal/ui/center -run 'Digest|Visibility' -count=1 -v` → pass.

### Step 4: Perf gate

**Verify**: `make perf-check` → within baseline (expect improvement on hot-tabs; if a preset lacks a darwin-arm64 baseline it skips — that's expected, don't chase it).

## Test plan

- New: digest-stability tests (Step 2-3 cases): unchanged → equal; live-screen mutation → different; viewport-only scroll → unchanged.
- Existing: `model_activity_visibility*_test.go` (or wherever the digest's consumers are tested) must pass unchanged — the function's contract is "equal iff same visible content", not the byte value.
- Verification: `go test ./internal/ui/center -count=1` → all pass.

## Done criteria

- [ ] Digest computation no longer UTF-8-encodes the full screen AND no longer uses SHA-256 (either epoch-based, or maphash/FNV over the existing scan).
- [ ] Live-screen (not viewport) semantics preserved — asserted by test.
- [ ] `go test ./internal/ui/center -count=1` exits 0; `make perf-check` passes.
- [ ] `make devcheck` exits 0.
- [ ] No files outside the in-scope list are modified.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The digest feeds something stronger than in-process equality (e.g. persisted, cross-process compared) — then maphash is wrong (seed differs per process); use FNV or report.
- No dirty-epoch signal exists and exposing one requires real vterm surgery — fall back to the maphash-only change (still a win) rather than expanding scope.
- `RenderBuffers` is itself the dominant cost (allocates) — then the epoch path is mandatory or the finding stands only partially; report the measurement.
- Visible-content semantics changed (e.g. viewport-vs-live distinction removed elsewhere) — re-derive the equality contract.

## Maintenance notes

- If a proper "lines changed since epoch E" API lands in vterm later, the digest could go O(dirty) — note this as the natural evolution, not required now.
- `maphash` per-process seeding means digests must never be logged across runs for comparison — keep them in-memory only (already true).
- Reviewer focus: the live-vs-viewport test is the one that proves semantics didn't drift.
