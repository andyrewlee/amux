# Plan 015: Verify install.sh ↔ release contract in CI (minisign pubkey + asset names)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat af432f7..HEAD -- install.sh internal/update/pubkey.go .goreleaser.yml Makefile scripts/ .github/workflows/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `af432f7`, 2026-09-25

## Why this matters

The install script and release pipeline share a hand-synced contract kept alive only by "Must match" comments: `MINISIGN_PUBKEY` in `install.sh` duplicates `minisignPublicKey` in `internal/update/pubkey.go`, and the release asset names (`checksums.txt`, `checksums.txt.minisig`, `amux_<v>_<os>_<arch>.tar.gz`) are hard-coded on both sides. No test or CI step compares them — rotating the signing key and updating only one anchor, or renaming a release asset in `.goreleaser.yml`, silently breaks fresh installs (while self-update keeps working) and is discovered only when a user runs the installer against a real release.

## Current state

- `install.sh:15` — `MINISIGN_PUBKEY="RWQt..."` (the public key is NOT a secret — it's a verification public key; still, quote only its location in docs, not the value, out of habit).
- `internal/update/pubkey.go:19` — `var minisignPublicKey = "RWQt..."` — same value, "Must match" comment on both sides.
- `install.sh:89,103,111` — asset name strings: `checksums.txt`, `checksums.txt.minisig`, tarball naming pattern.
- `.goreleaser.yml:28,34,61` — the corresponding archive/checksum/signature config.
- `install.sh` is exercised by no CI job — grep `.github/workflows/` for `install.sh` returns nothing.
- `Makefile` has `release-check`-adjacent targets — `grep -n 'release' Makefile` for where a parity check naturally hooks (there may be a `release-check` or `ci` aggregation point; the `ci` target at ~:119 is the all-local-gates aggregator).

Existing verification style: scripts live in `scripts/` (e.g. `scripts/test_pkgs.sh`); Makefile targets wrap them; CI runs `make` targets. A Go test inside `internal/update` that reads `../../install.sh` is also idiomatic here (the repo reads files from tests elsewhere — check `internal/update/*_test.go` for precedent).

## Commands you will need

| Purpose    | Command                                          | Expected on success |
|------------|--------------------------------------------------|---------------------|
| Parity     | `bash scripts/check_install_parity.sh` (or `go test ./internal/update -run Parity -v`) | exit 0 |
| Lint       | `make lint`                                      | exit 0              |
| CI parity  | `make devcheck` (and the target you wire it into) | exit 0             |

## Scope

**In scope**:
- `scripts/check_install_parity.sh` (new) OR `internal/update/install_parity_test.go` (new — pick ONE shape; the Go test gets CI coverage free via the existing test sweep, the shell script needs Makefile wiring — prefer the Go test).
- `Makefile` — only if choosing the script shape or wiring a new make alias.
- `.github/workflows/ci.yml` — only if the parity check isn't already swept in by `go test ./...`/`scripts/test_pkgs.sh` (it should be — verify).

**Out of scope**:
- `install.sh` content changes — it's correct today; the plan adds the guard, not a rewrite.
- `.goreleaser.yml` — same.
- A post-release `workflow_dispatch` job that runs install.sh against a real release — optional enhancement, explicitly deferred (keep this plan to static parity).
- Key rotation procedures.

## Git workflow

- Branch: `advisor/015-install-parity-check` off `main`.
- Commit style: `test: add install.sh release-contract parity check`.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Write the parity check (Go test preferred)

`internal/update/install_parity_test.go`:

```go
// TestInstallScriptParity pins the contract install.sh shares with the
// release pipeline: signing pubkey + asset names. Failure means one side
// rotated without the other — fresh installs break while self-update works.
```

Test body:

1. `os.ReadFile("../../install.sh")` (path relative to `internal/update` — confirm with `ls` from the test's cwd or use `runtime.Caller`-relative resolution if the package does that).
2. Extract `MINISIGN_PUBKEY="..."` via regexp and compare to `minisignPublicKey` (same package — accessible unexported).
3. Assert install.sh contains `checksums.txt`, `checksums.txt.minisig`, and the tarball template consistent with `.goreleaser.yml`'s archive name template (read `.goreleaser.yml` too and assert the literal substrings that must match — keep the assertions literal and few: filename constants, not YAML parsing).

**Verify**: `go test ./internal/update -run 'InstallScriptParity' -v` → pass. Then deliberately break it in-memory (no — don't modify sources; just trust the string compare) — instead, verify the test FAILS when pointed at a mutated copy via a table case with fixture strings, or simply review the extraction logic.

### Step 2: Confirm CI sweeps it

`grep -rn 'internal/update\|test_pkgs' scripts/test_pkgs.sh Makefile .github/workflows/ci.yml` — confirm the new test lands inside the existing `go test` sweeps (the exclusion lists exclude tmux/e2e/pty/app-ish pkgs, not `internal/update`). If it isn't swept, wire it into the nearest aggregator — but it almost certainly is; verify rather than assume.

**Verify**: `make devcheck` → exit 0 (devcheck runs the package test sweep — confirm `internal/update` tests run in it via `make devcheck` output or `scripts/test_pkgs.sh` listing).

### Step 3: Negative-path self-check

Temporarily (in your working copy only, then revert) mutate the test's expected pubkey constant → confirm the test fails loudly → revert. This proves the check isn't vacuous. Do not commit the mutation.

**Verify**: `git diff` clean after revert; `go test ./internal/update -count=1` → pass.

## Test plan

- The parity test IS the deliverable; include a table of expected asset-name substrings so a second contract member can drift without rewriting the test body.
- Verification: `go test ./internal/update -count=1` → all pass; `make devcheck` → exit 0.

## Done criteria

- [ ] A test (or script) compares `install.sh`'s `MINISIGN_PUBKEY` to `internal/update`'s `minisignPublicKey` and the shared asset names to `.goreleaser.yml`.
- [ ] It runs inside the existing CI test sweep (verified, not assumed).
- [ ] Mutating the contract makes it fail (verified via the negative self-check).
- [ ] `make devcheck` exits 0.
- [ ] No changes to `install.sh`, `.goreleaser.yml`, or `pubkey.go` themselves.
- [ ] `plans/README.md` status row updated.

## STOP conditions

- The pubkey/asset contract moved (e.g. install.sh now fetches the pubkey from the release) — re-derive the parity surface.
- `internal/update` tests are excluded from the package sweep (check `scripts/test_pkgs.sh` exclusions) — pick a package that IS swept or wire an explicit make target.
- `install.sh` is generated at release time — parity must then check the generator, not the output; report the new shape.

## Maintenance notes

- When the signing key next rotates, this test failing is the *reminder that install.sh needs the same rotation* — the failure message should say exactly that (write a descriptive `t.Fatalf`).
- If goreleaser asset naming is ever parameterized, extend the test to render the template rather than matching literals.
- Optional future step (out of this plan): a post-release workflow that runs `install.sh` against the just-published assets — catches service-side breakage static parity can't.
