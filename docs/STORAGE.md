# Storage policies

Every `internal/data` store answers "what does a corrupt or missing file
mean" differently, and deliberately so — a registry that silently empties
loses the project list, while a script-env lookup that fails wedges every
run. This table documents CURRENT behavior per store; it is the contract a
future unification change must cite, not an aspirational target.

All atomic writes go through `internal/fsatomic` (temp + fsync + rename; on
platforms without atomic rename the previous file shuffles to `.bak` and
restores on failure). Cross-process serialization is per-file or
per-identity flock, never assumed from the atomic write.

| File | Store | Missing primary | Corrupt/undecodable primary | Newer schema version | Backup behavior |
|------|-------|-----------------|-----------------------------|----------------------|-----------------|
| `~/.amux/projects.json` | `Registry` | read falls back to `.bak` silently; absent backup = empty list | read recovers from `.bak`; both bad = joined error | read **refuses** (`ErrUnsupportedSchemaVersion`) — a newer-schema primary is intact data from a newer binary, never recovered over | `WriteJSON` rewrites primary after any backup recovery, healing the file |
| `~/.amux/workspaces/<id>/workspace.json` | `WorkspaceStore` | read falls back to `workspace.json.bak` | read errors (record skipped by `List`/`ByRepo` callers); **no** corrupt→backup fallback | read errors; load refuses version > current | `.bak` only exists as an fsatomic rename artifact; used when primary is absent |
| `~/.amux/project-env.json` | `ProjectEnvStore` | empty map | read → empty map (a corrupt file never blocks script execution); **write preserves** the corrupt file to `.bak` and **refuses** (`CorruptStoreError`) — the retry writes clean | read → empty map; **write refuses** | `.bak` quarantines the corrupt bytes for manual recovery — reads never consult it |
| `~/.amux/project-scripts.json` | `ProjectScriptStore` | empty config | same split as env: read → zero config, write → preserve to `.bak` + refuse (`CorruptStoreError`) | read → zero config; **write refuses** | same quarantine `.bak` as env |
| `~/.amux/port-reservations.json` | `PortReservationStore` | empty non-nil map | **fails closed** — full-envelope validation rejects truncated, null, overlapping, or undecodable bytes; reads never substitute an empty map | refuses (v1 only, exactly) | none |
| `~/.amux/workspaces/<id>/script-transcripts.json` | `ScriptTranscriptStore` | empty map | read → skip dir silently; **write refuses** the whole transaction (never replaces unknown data) | read → skip; write refuses | none |
| `~/.amux/workspaces/<id>/.deleting` | tombstone marker | n/a (presence/absence only) | n/a — a 1-byte marker, not decoded | n/a | n/a |

## Read/write asymmetry is the pattern

- **Silent-empty reads** (`project-env`, `project-scripts`): the file
  feeds script execution; a parse error must not wedge a feature the user
  can simply re-set.
- **Refuse-on-corrupt writes** (`script-transcripts`, `port-reservations`,
  registry's newer-schema case): bytes this binary cannot inspect may hold
  data it doesn't know about — overwriting them is the corruption, not
  the fix.
- **Preserve-and-refuse writes** (`project-env`, `project-scripts`): the
  file holds user-authored data, so a decode-corrupt primary moves to
  `.bak` — a quarantine for manual recovery, not a recovery source —
  and the write refuses with `CorruptStoreError` so the failure surfaces.
  The primary is then absent and the retry writes clean. If `.bak` is
  already occupied the write refuses and leaves both files untouched: an
  earlier preservation is never overwritten (the registry's
  never-recover-a-stale-backup rule). Decode failures only — a newer
  schema or an I/O error is refused without touching the file, since it
  may hold intact data this binary cannot inspect. The `.bak` name is
  shared with fsatomic's non-atomic-rename fallback, so a subsequent
  write may reclaim the slot there: the quarantine is best-effort, not a
  durable archive.

`port-reservations.json` has one extra contract: the first-adoption guard
(see `Initialize`) runs only while this process has never seen an existing
registry. A registry file deleted mid-run is recreated empty without the
guard — re-running it would wedge every spawn on this instance's own
surviving sessions.

## Investigate (divergences, not yet adjudicated)

- `projects.json` recovers a corrupt primary from `.bak` on read, but
  `workspace.json` does not — a corrupt per-workspace record errors even
  when a usable `.bak` sits beside it. Either direction may be right;
  aligning them is a maintainer decision.
- ~~`project-env.json`/`project-scripts.json` write-replace corrupt bytes
  without keeping a copy~~ — adjudicated: both files are user-authored,
  so writes now preserve the corrupt primary to `.bak` and refuse.
