# Adoption benchmark results

Latest full-matrix run: 3 methodologies x 3 realistic no-mention work tasks,
each run to completion (the runner waits for the terminal result event).

| Methodology | Adoption | Avg score | Board items |
|---|---|---|---|
| docket      | 3/3 (100%) | 3.0/3 | 5 |
| openspec    | 3/3 (100%) | 3.0/3 | 9 |
| superpowers | 3/3 (100%) | 3.0/3 | 8 |

**Overall adoption: 9/9 (100%). Gate: PASS.**

Per criterion: started-unprompted 9/9, updated-mid-work 9/9, correct-placement 9/9.

## Before vs after the skill-trigger fix

Same benchmark, same tasks, before broadening the skill descriptions to fire on the
work rather than on an explicit board request:

| | Before | After |
|---|---|---|
| docket (no-mention) | 0/3 adopted | 3/3 adopted |
| openspec (no-mention) | 0 adopted | 3/3 adopted |
| superpowers (no-mention) | 0 adopted | 3/3 adopted |

Reproduce with `benchmarks/adoption/run.sh` (see `rubric.md`).
