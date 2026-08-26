# Board adoption benchmark

Mechanics tests prove the board *can* be built correctly. This benchmark proves an
agent *chooses* to use it and keeps it current while doing normal work. The bar is
"the board is never ignored."

## How it works

Each scenario is a fixture repo that has the Workbench skills installed under
`.claude/skills/` and a realistic methodology footprint (docket manifests, OpenSpec
changes, Superpowers plans). We drive a real headless agent:

```
claude -p "<a realistic task that never mentions the board>" \
  --mcp-config <workbench> --setting-sources project --output-format stream-json
```

The tasks are ordinary work ("which change should I do next?", "give me a status
report"). They deliberately never say "board" or "Workbench". So we measure
*proactive* adoption, not compliance.

Ground truth comes from two independent sources:

- **the transcript** (`stream-json`): which tools were called, in what order.
- **the board database**: what boards and cards actually landed, and how tagged.

## Criteria (per run)

| # | Criterion | Passes when |
|---|---|---|
| 1 | Started unprompted | The agent called `board_start` / `board_set_layout` and a board exists in the db. |
| 2 | Updated mid-work | It upserted/created cards (not just an empty shell) and the db has items. |
| 3 | Correct placement | A majority of cards carry a real `column_key` that a layout view owns, so they render in a nav view. Placement is column-driven — a card's nav view and swimlane derive from its `column_key`, not from `view:`/`lane:` labels. |
| 4 | Recovers from drift | After a soft nudge ("can I see this on a board?"), a follow-up turn uses board tools and a board exists. Scored only when a drift transcript is provided. |

`adopted` (the headline signal) is simply: did the run call any board tool at all.

## The gate

Runs are repeated (LLM behavior varies), so the metric is an **adoption rate**:
the fraction of runs that adopted the board. The suite **fails** if the overall
adoption rate drops below the threshold in `aggregate.py` (`ADOPTION_THRESHOLD`,
currently 0.80). This turns "never ignored" into a number you can watch and regress
against.

## Running

```sh
# build/install the binary first so the benchmark drives the real server
go build -o ~/.local/bin/workbench ./cmd/workbench

benchmarks/adoption/run.sh                    # all scenarios, 3 runs each
benchmarks/adoption/run.sh --scenario docket --runs 5
benchmarks/adoption/run.sh --model claude-haiku-4-5   # cheaper smoke
```

Each run costs a small amount of tokens and takes tens of seconds, so the default
suite is intentionally small and repeatable rather than exhaustive.

## Interpreting results

- **adoption rate near 100%** across scenarios: the board is not being ignored.
- **adoption rate low** (as at baseline): the skills are discoverable but not
  compelling enough to fire on ordinary work. That is an adoption problem to fix
  (stronger skill triggers, a project instruction, or a hook), and this benchmark
  is how you know whether a fix worked.
- **adopted but low score**: the agent starts a board but doesn't populate or place
  cards. A shell, not real use.
