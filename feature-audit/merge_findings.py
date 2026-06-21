#!/usr/bin/env python3
"""Merge Phase 2/4 verification findings into results.json.

Usage: python3 feature-audit/merge_findings.py <findings.json> [--phase 2|4]

findings.json is a JSON array of objects:
  {id, status: 'Pass'|'Error', evidence, error, severity, fix, area}

Phase 2 (default): writes status/test_method/result/errors_found (+ stashes the
suggested fix into errors_found for Phase 3 to act on).
Phase 4 (--phase 4): writes retest_result and, if now Pass, promotes status to
'Verified'.
results.json is the incremental overlay consumed by build_audit.py.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(HERE, "results.json")


def clip(s, n=600):
    s = (s or "").strip().replace("\n", " ")
    return s if len(s) <= n else s[: n - 1] + "…"


def load_results():
    if os.path.exists(RESULTS):
        with open(RESULTS) as f:
            return json.load(f)
    return {}


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    findings_path = sys.argv[1]
    phase = "2"
    if "--phase" in sys.argv:
        phase = sys.argv[sys.argv.index("--phase") + 1]

    with open(findings_path) as f:
        findings = json.load(f)
    if isinstance(findings, dict) and "findings" in findings:
        findings = findings["findings"]

    results = load_results()
    changed = 0
    for fnd in findings:
        rid = fnd["id"]
        cur = results.get(rid, {})
        if phase == "2":
            cur["status"] = fnd["status"]
            cur["test_method"] = "code-trace (Phase 2 multi-agent verify)"
            cur["result"] = clip(fnd.get("evidence"))
            if fnd["status"] == "Error":
                err = clip(fnd.get("error"))
                sev = fnd.get("severity", "")
                sug = clip(fnd.get("fix"))
                cur["errors_found"] = f"[{sev}] {err}" + (f"  | suggested fix: {sug}" if sug else "")
            else:
                cur["errors_found"] = ""
        elif phase == "4":
            cur["retest_result"] = (fnd["status"] + ": " + clip(fnd.get("evidence"), 400))
            if fnd["status"] == "Pass":
                cur["status"] = "Verified"
            else:
                cur["status"] = "Error"
        results[rid] = cur
        changed += 1

    with open(RESULTS, "w") as f:
        json.dump(results, f, indent=2, sort_keys=True)
    print(f"Merged {changed} findings (phase {phase}) into {RESULTS}")


if __name__ == "__main__":
    main()
