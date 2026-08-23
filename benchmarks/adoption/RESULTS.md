# Adoption benchmark results

Latest full-matrix run: 3 methodologies x 3 realistic no-mention work tasks x 3
runs each (27 runs), every run to completion (the runner waits for the terminal
result event; no truncated runs).

| Methodology | Adoption | Avg score |
|---|---|---|
| docket      | 9/9 (100%) | 3.0/3 |
| openspec    | 9/9 (100%) | 3.0/3 |
| superpowers | 9/9 (100%) | 3.0/3 |

**Overall adoption: 27/27 (100%). Gate: PASS.**

Per criterion: started-unprompted 27/27, updated-mid-work 27/27,
correct-placement 27/27. No incomplete or non-adopted runs.

## Before vs after the skill-trigger fix

Same benchmark, same tasks, before broadening the skill descriptions to fire on the
work rather than on an explicit board request:

| | Before | After (27-run matrix) |
|---|---|---|
| docket (no-mention) | 0/3 adopted | 9/9 adopted |
| openspec (no-mention) | 0 adopted | 9/9 adopted |
| superpowers (no-mention) | 0 adopted | 9/9 adopted |

The board is no longer ignored on realistic work, across every methodology.

Reproduce with `benchmarks/adoption/run.sh --runs 3` (see `rubric.md`).
