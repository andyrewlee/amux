# feature-audit

Point-in-time audit bundle. Originally generated 2026-02 at commit `376d33d`;
regenerated outputs self-date via a `Generated <date> at commit <sha>` line in
`FEATURES.md`/`SUMMARY.md`.

**This is not living documentation.** `file:line` references, statuses, and
behavioral claims reflect the code as it was at generation time and drift as
the code moves. Treat it as a historical audit snapshot — for current truth,
read the code.

## Contents

| File | Role |
|------|------|
| `build_audit.py` | Canonical generator. `ROWS` holds the user-story catalog; emits `FEATURES.csv`/`FEATURES.md`/`FEATURES.json`/`SUMMARY.md`. |
| `merge_findings.py` | Merges verification findings into `results.json` (`--phase 2|4`). |
| `results.json` | Incremental overlay consumed by `build_audit.py` (status/test_method/result per row id). |
| `phase2_findings.json` | Raw Phase-2 multi-agent findings (input to `merge_findings.py`). |
| `REPORT.md` | Hand-written phase report. |
| `FEATURES.csv`/`FEATURES.md`/`FEATURES.json`/`SUMMARY.md` | Generated outputs — regenerate, don't hand-edit. |

## Regenerating

```sh
python3 feature-audit/build_audit.py   # deterministic; no network
```

New findings go through `merge_findings.py` into `results.json`, then re-run
`build_audit.py`. Fix catalog claims in `build_audit.py`'s `ROWS`, not in the
generated outputs.
