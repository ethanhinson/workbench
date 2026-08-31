# @workbench/prototyper

The v1 runnable **prototype-review loop** — and the end-to-end test of the whole
stack (`envelope` + `components` + `adapters`).

```
agent → present prototype → browser (80% iframe + 20% wb-chat)
      → human clicks / annotates / chats → POST /review
      → adapter.submitFeedback(envelope) → running turn continues
```

## Run it

```sh
pnpm --filter @workbench/prototyper build

# fake mode — no live harness; serves a canned prototype, echoes chat.
# This is the deterministic path the integration test drives.
node apps/prototyper/dist/cli.js --fake --port 4319
# → open http://127.0.0.1:4319

# real mode — drives Claude via @workbench/adapters/claude (needs the Agent SDK
# and a working `claude` setup):
node apps/prototyper/dist/cli.js "Propose a layout for the settings page."
```

## What the server does

| Route | Purpose |
| --- | --- |
| `GET /` | the browser shell (80/20 layout, loads `@workbench/components`) |
| `GET /prototype` | the current agent-presented prototype (framed sandboxed) |
| `GET /components/*` | the built components bundle |
| `POST /review` | the human's review → folded into an envelope → `adapter.submitFeedback` |
| `POST /chat` | a human chat message → `adapter.sendChat` |
| `WS /ws` | streams `chat` / `prototype` / `turn-end` events to the browser |

Harness-agnostic: the `HarnessAdapter` is the only harness-specific piece. The
`FakeAdapter` implements it deterministically so `pnpm test` proves the loop
without a live agent.

## Live end-to-end test

`src/live.test.ts` drives the **complete loop against a real `claude`** and
asserts: the agent presents a prototype (served, inspector injected), chat
streams over the WS, the review POST unblocks `request_review`, and the agent
**reacts to the feedback** after the review before the turn ends. It uses
`createRealAdapter` — the exact code path the CLI's real mode uses.

Opt-in only (skipped in CI, no key needed to keep CI green):

```sh
WB_LIVE=1 pnpm --filter @workbench/prototyper test
```

## Status / limits

- The prototype is framed in a **sandboxed iframe** (`allow-scripts allow-forms`);
  agent JS can't reach the parent. An in-frame inspector script (injected by the
  server) does hover-highlight + click-select on **real elements** and posts the
  selector/box to the parent over `postMessage` — see `inspector.ts`.
- Real-mode Claude uses the blocking-review MCP tool; see
  `@workbench/adapters/claude`. Feedback that races ahead of `request_review` is
  buffered, so the loop is order-independent.
