#!/usr/bin/env python3
"""Aggregate per-run scorecards into an adoption report + a pass/fail gate.

The headline metric is the adoption RATE (fraction of runs where the agent used
the board at all), because LLM behavior is probabilistic. The gate fails if the
overall adoption rate falls below ADOPTION_THRESHOLD, so "the board is never
ignored" becomes a number you can watch and regress against.

Usage: aggregate.py <index.jsonl>
"""
import json
import sys
from collections import defaultdict

ADOPTION_THRESHOLD = 0.80  # >=80% of runs must adopt the board to pass


def main():
    if len(sys.argv) < 2:
        print("usage: aggregate.py <index.jsonl>", file=sys.stderr)
        sys.exit(2)
    rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
    if not rows:
        print("no runs recorded", file=sys.stderr)
        sys.exit(2)

    by_scen = defaultdict(list)
    for r in rows:
        by_scen[r["scenario"]].append(r)

    print("\n=== Board Adoption Benchmark ===\n")
    total_adopted = 0
    crit_totals = defaultdict(lambda: [0, 0])  # criterion -> [passed, evaluated]

    for scen, runs in sorted(by_scen.items()):
        adopted = sum(1 for r in runs if r["adopted"])
        rate = adopted / len(runs)
        avg = sum(r["score"] for r in runs) / len(runs)
        total_adopted += adopted
        print(f"{scen:16}  adoption {adopted}/{len(runs)} ({rate:.0%})   avg score {avg:.1f}")
        for r in runs:
            for k, v in r["criteria"].items():
                if v is None:
                    continue
                crit_totals[k][1] += 1
                if v:
                    crit_totals[k][0] += 1

    overall = total_adopted / len(rows)
    print(f"\noverall adoption: {total_adopted}/{len(rows)} ({overall:.0%})")
    print("\nper-criterion pass rate:")
    for k, (p, n) in crit_totals.items():
        print(f"  {k:22} {p}/{n} ({(p/n if n else 0):.0%})")

    passed = overall >= ADOPTION_THRESHOLD
    print(f"\ngate: adoption {overall:.0%} >= {ADOPTION_THRESHOLD:.0%}  ->  {'PASS' if passed else 'FAIL'}")
    print("(the board must not be ignored: if this FAILs, adoption regressed)\n")
    sys.exit(0 if passed else 1)


if __name__ == "__main__":
    main()
