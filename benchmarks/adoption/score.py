#!/usr/bin/env python3
"""Score one adoption run against the four criteria.

Ground truth comes from two independent sources:
  1. the stream-json transcript (what tools were called, in what order)
  2. the board database (what actually landed, and where)

Usage:
    score.py <transcript.jsonl> <board.db> [--drift-transcript <t2.jsonl>]

Emits a JSON scorecard on stdout. Exit code 0 always (the score is the signal).
"""
import json
import os
import sqlite3
import sys


def load_tool_calls(path):
    """Return the ordered list of tool_use events: [{name, input}, ...]."""
    calls = []
    if not path or not os.path.exists(path):
        return calls
    for line in open(path):
        line = line.strip()
        if not line:
            continue
        try:
            e = json.loads(line)
        except json.JSONDecodeError:
            continue
        if e.get("type") == "assistant":
            for c in e.get("message", {}).get("content", []):
                if isinstance(c, dict) and c.get("type") == "tool_use":
                    calls.append({"name": c.get("name", ""), "input": c.get("input", {})})
    return calls


def board_state(db):
    """Return (boards, items) from the board db, or ([], []) if absent."""
    if not db or not os.path.exists(db):
        return [], []
    con = sqlite3.connect(db)
    boards = [dict(id=r[0], name=r[1], project=r[2]) for r in
              con.execute("SELECT id, name, project FROM plan")]
    items = []
    for r in con.execute("SELECT id, plan_id, title, ext_key FROM item"):
        labels = [f"{ns}:{v}" for ns, v in
                  con.execute("SELECT ns, value FROM label WHERE item_id=?", (r[0],))]
        items.append(dict(id=r[0], plan_id=r[1], title=r[2], ext_key=r[3], labels=labels))
    return boards, items


# --- the four criteria -----------------------------------------------------

def c_started_unprompted(calls, boards):
    """1. The agent stood up a board on its own.
    True if it called board_start / board_set_layout (or a card upsert that
    implies a board), evidenced in the transcript AND a board exists in the db.
    """
    started = any(t["name"].endswith(("board_start", "board_set_layout")) for t in calls)
    return bool(started and boards)


def c_updated_mid_work(calls, items):
    """2. It populated the board with the work, not just an empty shell.
    True if cards were created/upserted (item_create/item_upsert) AND the db has
    items. A board with a layout but zero cards is an empty gesture, not adoption.
    """
    populated = any(t["name"].endswith(("item_upsert", "item_create")) for t in calls)
    return bool(populated and items)


def c_correct_placement(items):
    """3. Cards are placed, not just created.
    True if a majority of items carry a view: label (so they'd actually render in
    a nav view) — the difference between a real board and a bag of untagged cards.
    """
    if not items:
        return False
    placed = sum(1 for it in items if any(l.startswith("view:") for l in it["labels"]))
    return placed >= max(1, len(items) // 2)


def c_recovers_from_drift(drift_calls, drift_boards):
    """4. If it initially drifted, a soft nudge brings it back.
    Scored only when a drift-recovery transcript is provided: did the follow-up
    turn end with board tool use + a board in the db?
    """
    if drift_calls is None:
        return None  # not evaluated
    return bool(any("workbench" in t["name"] for t in drift_calls) and drift_boards)


def score(transcript, db, drift_transcript=None, drift_db=None):
    calls = load_tool_calls(transcript)
    boards, items = board_state(db)
    board_tool_calls = [t["name"] for t in calls if "workbench" in t["name"]]

    drift_calls = drift_boards = None
    if drift_transcript:
        drift_calls = load_tool_calls(drift_transcript)
        drift_boards, _ = board_state(drift_db or db)

    crit = {
        "started_unprompted": c_started_unprompted(calls, boards),
        "updated_mid_work": c_updated_mid_work(calls, items),
        "correct_placement": c_correct_placement(items),
        "recovers_from_drift": c_recovers_from_drift(drift_calls, drift_boards),
    }
    # Adopted at all = the headline "never ignored" signal.
    adopted = bool(board_tool_calls)
    scored = [v for v in crit.values() if v is not None]
    return {
        "adopted": adopted,
        "board_tool_calls": board_tool_calls,
        "criteria": crit,
        "score": sum(1 for v in scored if v),
        "max_score": len(scored),
        "boards": len(boards),
        "items": len(items),
    }


def main():
    args = sys.argv[1:]
    drift = None
    if "--drift-transcript" in args:
        i = args.index("--drift-transcript")
        drift = args[i + 1]
        args = args[:i] + args[i + 2:]
    if len(args) < 2:
        print("usage: score.py <transcript.jsonl> <board.db> [--drift-transcript t2.jsonl]", file=sys.stderr)
        sys.exit(2)
    print(json.dumps(score(args[0], args[1], drift_transcript=drift), indent=2))


if __name__ == "__main__":
    main()
