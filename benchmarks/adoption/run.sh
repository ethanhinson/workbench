#!/usr/bin/env bash
# Run the board-adoption benchmark: drive a real headless agent (claude -p) on
# realistic tasks in fixture repos that have the workbench skills installed, then
# score whether the agent adopted the board. Ground truth is the tool-call
# transcript + the resulting board database.
#
# Usage:
#   run.sh [--runs N] [--scenario NAME] [--out DIR] [--model MODEL]
#
# Requires: claude CLI, a `workbench` binary on PATH (or set WORKBENCH_BIN).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
WORKBENCH_BIN="${WORKBENCH_BIN:-$(command -v workbench || echo "$HOME/.local/bin/workbench")}"
RUNS=3
ONLY=""
OUT="$HERE/results/$(date +%Y%m%d-%H%M%S)"
MODEL=""
# A full hydrate (start board, set layout, upsert many cards) can take several
# minutes. Too short a timeout truncates the run mid-adoption and under-reports it.
TIMEOUT="${BENCH_TIMEOUT:-600}"

while [ $# -gt 0 ]; do
  case "$1" in
    --runs) RUNS="$2"; shift 2;;
    --scenario) ONLY="$2"; shift 2;;
    --out) OUT="$2"; shift 2;;
    --model) MODEL="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[ -x "$WORKBENCH_BIN" ] || { echo "workbench binary not found: $WORKBENCH_BIN" >&2; exit 1; }
command -v claude >/dev/null || { echo "claude CLI not found" >&2; exit 1; }
mkdir -p "$OUT"

# wait_done STREAM TIMEOUT — wait until the run emits its terminal `result` event
# (the true end of a claude -p turn) or the deadline passes. Returns 0 if the run
# completed, 1 if it was cut off. This matters: a run killed mid-adoption must NOT
# be scored as "not adopted" — it never finished.
wait_done() {
  local stream=$1 t=$2
  for _ in $(seq 1 "$t"); do
    grep -q '"type":"result"' "$stream" 2>/dev/null && return 0
    sleep 1
  done
  return 1
}

results_index="$OUT/index.jsonl"
: > "$results_index"

for scen_dir in "$HERE"/scenarios/*/; do
  scen="$(basename "$scen_dir")"
  [ -n "$ONLY" ] && [ "$scen" != "$ONLY" ] && continue
  [ -f "$scen_dir/scenario.json" ] || continue

  # scenario.json: { "repo": "relative repo dir", "tasks": ["prompt", ...], "drift_nudge": "..." }
  repo="$scen_dir/$(python3 -c "import json;print(json.load(open('$scen_dir/scenario.json'))['repo'])")"
  ntasks=$(python3 -c "import json;print(len(json.load(open('$scen_dir/scenario.json'))['tasks']))")

  for ti in $(seq 0 $((ntasks-1))); do
    task=$(python3 -c "import json;print(json.load(open('$scen_dir/scenario.json'))['tasks'][$ti])")
    for run in $(seq 1 "$RUNS"); do
      tag="${scen}-t${ti}-r${run}"
      db="$OUT/$tag.db"; rm -f "$db" "$db"-wal "$db"-shm 2>/dev/null || true
      mcp="$OUT/$tag.mcp.json"
      cat > "$mcp" <<EOF
{ "mcpServers": { "workbench": { "type":"stdio","command":"$WORKBENCH_BIN","args":["--db","$db","--agent","claude"] } } }
EOF
      stream="$OUT/$tag.jsonl"; : > "$stream"
      model_arg=(); [ -n "$MODEL" ] && model_arg=(--model "$MODEL")
      echo "run $tag ..." >&2
      ( cd "$repo" && claude -p "$task" \
          --mcp-config "$mcp" --permission-mode bypassPermissions --setting-sources project \
          --output-format stream-json --verbose "${model_arg[@]}" > "$stream" 2>/dev/null ) &
      cpid=$!
      complete=1
      wait_done "$stream" "$TIMEOUT" || complete=0
      kill "$cpid" 2>/dev/null || true   # only reaps if it timed out; a done run has already exited

      card=$(python3 "$HERE/score.py" "$stream" "$db")
      echo "$card" > "$OUT/$tag.score.json"
      python3 -c "import json;c=json.loads('''$card''');print(json.dumps({'tag':'$tag','scenario':'$scen','task':$ti,'run':$run,'complete':bool($complete),**c}))" >> "$results_index"
      [ "$complete" = 1 ] || echo "  WARN $tag did not complete within ${TIMEOUT}s (excluded from adoption rate)" >&2
    done
  done
done

# Aggregate.
python3 "$HERE/aggregate.py" "$results_index"
echo "results in $OUT" >&2
