# Plan 017: Replace non-POSIX `sort -V` in `make doctor`'s go-version check

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- Makefile`
> If the file changed since this plan was written, compare the "Current
> state" excerpt against live code; on a mismatch, treat it as a STOP
> condition.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

`make doctor` is the onboarding diagnostic — its go-version floor check uses `sort -V`, a GNU/newer-BSD extension missing on older macOS `text_cmds` sort and busybox. On such hosts `sort -V` errors and emits nothing, so the comparison fails and doctor reports `FAIL go <ver> < <floor>` even when Go is fine — the diagnostic lies on exactly the unknown-host scenario it exists for. A POSIX-safe compare costs nothing.

## Current state

`Makefile:195-202` (inside `doctor`):

```make
	if command -v go >/dev/null 2>&1; then \
		gov=$$(go version | awk '{print $$3}' | sed 's/^go//'); \
		need=$$(awk '/^go [0-9]/{print $$2; exit}' go.mod); \
		if [ "$$(printf '%s\n%s\n' "$$need" "$$gov" | sort -V | head -1)" = "$$need" ]; then \
			echo "ok   go $$gov (>= $$need required)"; \
		else \
			echo "FAIL go $$gov < $$need (go.mod)"; fail=1; \
		fi; \
```

The logic: print `need` and `gov`, sort -V, take first — if first is `need`, installed ≥ floor.

## Commands you will need

| Purpose    | Command           | Expected on success |
|------------|-------------------|---------------------|
| Doctor     | `make doctor`     | `ok   go <ver> (>= <floor> required)` |
| Lint       | `make lint`       | exit 0              |
| Full gate  | `make devcheck`   | exit 0              |

## Scope

**In scope**:
- `Makefile` — the one comparison line.

**Out of scope**:
- Any other `sort` usage in the Makefile — check `grep -n 'sort -V' Makefile`; if this is the only one, done. If others exist and are equally non-POSIX, fix them in the same commit (same bug class).
- Shell scripts — verify `grep -rn 'sort -V' scripts/ .githooks/ install.sh` returns nothing; report if it doesn't.

## Git workflow

- Branch: `advisor/017-posix-sort-v` off `main`.
- Commit style: `fix: use POSIX-safe version compare in make doctor`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Replace the compare

Swap `sort -V` for a numeric field sort that both GNU and old BSD sort support:

```make
		if [ "$$(printf '%s\n%s\n' "$$need" "$$gov" | sort -t. -k1,1n -k2,2n -k3,3n | head -1)" = "$$need" ]; then \
```

`1.26.0`-shaped inputs compare correctly: numeric on each dot-separated field. (Alternative — awk version compare, more code for the same result; the field-sort is the minimal edit.)

**Verify**: `make doctor` → prints `ok   go <ver> (>= <floor> required)`; no `sort: invalid option` error anywhere in output.

### Step 2: Prove both orderings

On a scratch shell (not committed): `printf '1.26.0\n1.25.4\n' | sort -t. -k1,1n -k2,2n -k3,3n | head -1` → `1.25.4` (min first, correct semantic for this check); and `printf '1.26.0\n1.26.0\n' | ...` → `1.26.0`. Also confirm `go.mod`'s floor vs your actual toolchain still prints `ok`.

**Verify**: the two printf checks behave as shown.

### Step 3: Full gate

**Verify**: `make devcheck` → exit 0.

## Test plan

- No unit tests for Makefile recipes; Step 2's manual checks + `make doctor` output are the verification. If `make doctor` has a CI job (`grep -n 'doctor' .github/workflows/*.yml`), it runs there.

## Done criteria

- [ ] `sort -V` is gone from the doctor check (and any other in-repo use found).
- [ ] `make doctor` prints `ok` for a compliant toolchain on this host.
- [ ] The comparison semantics are preserved (min-first ordering → `$$need` first iff installed ≥ floor).
- [ ] `make devcheck` exits 0.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The doctor target was restructured or the version check moved to a script — apply the fix at its new location.
- `grep -rn 'sort -V'` finds uses in `scripts/` or hooks — fix them all in this commit (same class) or report why a site needs `-V` semantics the field-sort can't express.

## Maintenance notes

- If the version compare ever needs pre-release qualifiers (`1.27rc1`), field-sort breaks — revisit then; today versions are `X.Y.Z`.
- A POSIX-safe alternative if this recurs elsewhere: `sort -t. -k1,1n -k2,2n -k3,3n` is the house idiom now — reuse it.
