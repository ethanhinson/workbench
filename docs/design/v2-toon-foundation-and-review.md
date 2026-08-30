# Design: TOON Foundation + Browser Prototype-Review Tool

**Status:** draft for reaction · **Date:** 2026-08-30
**Author context:** epistemics-first agent tooling (see codeindex/workbench/fuse). This doc
proposes a shared **foundation layer** and its **first consumer**, a browser-based
prototype/mock review loop.

> **Package-name reconciliation (post-scaffold).** This doc predates the final
> package split. Concepts map to packages as follows:
> - the envelope → `packages/envelope` (`@workbench/envelope`)
> - the "chrome" / browser UI components → `packages/components` (`@workbench/components`;
>   first components `wb-chat`, `wb-annotation`) — built with Stencil web components
> - the harness-specific side of the "bridge" → `packages/adapters`
>   (`@workbench/adapters/{codex,claude,cursor}`)
>
> "Bridge" below refers to the *runtime role* (serve the prototype, push feedback into
> the turn); its reusable part is the adapters package. There is no standalone bridge app
> yet — libraries only.

---

## 0. Thesis in one paragraph

An AI agent, running in a harness (Claude Code, Codex, Cursor), proposes work by
**rendering an interactive HTML prototype** — a UI mock, a workflow, or a set of
architectural options laid out inline. A human reviews it in a browser: clicks
elements (inspector-style), attaches annotations, picks among options, and chats in a
side panel. All of that — selections, annotations, metadata, chat — flows **back into
the running agent** as one structured payload, so the agent acts on clicks instead of
walls of prose. The structured payload is expressed in **TOON** via a small shared
**envelope** — the foundation layer that every future tool of ours also sits on.

This replaces "agent writes three options as paragraphs → human replies in prose."
It also seeds a **kanban+prototyping methodology** ("agent proposes by rendering, human
steers by clicking") to move off docket-style text change-manifests.

---

## 1. Two layers

### Layer 1 — TOON envelope (the foundation)

A small, stable **TypeScript** package. New-generation only; existing Go tools
(codeindex, workbench) are **not** retrofitted. It owns exactly one thing: the
**response envelope contract** every tool of ours speaks, rendered to TOON for agents
and JSON for machines/tests.

Deliberately **excludes** AXI's `update`/`hooks` modules — those are npm-distribution
plumbing, irrelevant here. This is format + envelope, not a CLI framework.

### Layer 2 — Browser prototype-review tool (first consumer)

The browser UI + local bridge that produces Layer-1 envelopes as its return channel.
Harness-neutral by construction; a thin per-harness adapter is the only variable part.

---

## 2. The envelope contract (Layer 1)

Fields are driven by the epistemics principles — say what you don't know, aggregates
not page sizes, freshness as a first-class signal.

```ts
type Coverage = "complete" | "partial" | "unknown";

interface Envelope<T> {
  data: T;                 // the payload (rows, an object, a scalar)
  total?: number;          // aggregate count, NOT page size (principle: pre-computed aggregates)
  coverage?: Coverage;     // did we see everything? partial => agent knows to probe more
  stale?: boolean;         // is this snapshot still valid?
  notes?: Flag[];          // ambiguity/provenance flags, e.g. { kind: "ambiguous", ref: "..." }
  help?: string[];         // next-step suggestions (contextual disclosure)
}

interface ErrorEnvelope {
  error: string;
  code: string;            // machine-stable code, not prose
  help?: string[];
}
```

Rendering:

- `render(env, "toon")` → TOON on stdout for agents (~row-shaped payloads compress best)
- `render(env, "json")` → JSON for tests, machines, the bridge's own wire
- TOON conversion happens **only at the output boundary**; internal logic stays JSON.

Error/exit contract (for any CLI built on this): errors on stdout as `ErrorEnvelope`,
exit `2` for usage errors, `1` for failures, `0` for success incl. no-ops.

**Why this is a real library and not a wrapper:** raw TOON gives you `encode(v)`. The
envelope gives you the *trust contract* — `coverage`/`stale`/`notes`/`total` are the
signals that make agent output honest, and standardizing them means every tool we ship
agrees on shape. That's the value AXI's SDK doesn't carry.

**Open:** does the envelope get a language-neutral spec now, or stay TS-idiomatic until
a second language needs it? Current lean: **TS-idiomatic; formalize later** (let the
browser tool teach the fields first).

---

## 3. Browser prototype-review tool (Layer 2)

### 3.1 The loop

```
agent → renders HTML prototype → bridge serves it → human reviews in browser
      → structured feedback (TOON envelope) → bridge → back into agent turn → agent iterates
```

### 3.2 UI shape

```
┌──────────────────────────────────────────────┬───────────────────┐
│  PROTOTYPE PANE  (~80%, resizable)            │  CHAT PANE (~20%) │
│                                               │                   │
│  free-form HTML / CSS / JS, interactive       │  user <-> agent   │
│  deps vendored via <script src> / ESM URLs    │  (streamed from   │
│  NO build pipeline for the prototype          │   the harness)    │
│                                               │                   │
│  [in-house CHROME injected OVER the proto:]   │  in-context with  │
│   · element inspector (hover-highlight,       │  what's clicked   │
│     click-select)                             │                   │
│   · annotation attach (note on a selector)    │                   │
│   · option-picker (choose an inline option)   │                   │
└──────────────────────────────────────────────┴───────────────────┘
```

### 3.3 Two distinct authorship zones

- **Prototype (agent-authored, free-form):** arbitrary HTML/CSS/JS. Interactive.
  Dependencies pulled at runtime via `<script src=...>`, `<script type="module">`,
  ESM CDN URLs (esm.sh/unpkg/skypack). **No build step** — the agent writes a file,
  the bridge serves it.
- **Chrome (in-house, fixed component set):** injected *over/around* the prototype,
  never authored by the agent. Small stable kit:
  - `inspector` — hover to highlight, click to select an element; captures
    `selector`, bounding `box`, role/label.
  - `annotation` — attach a text note to a selected element.
  - `option-picker` — when the agent presents architectural options inline, marks
    which the human chose.

  The chrome only ever talks to the **bridge**, so it's fully harness-agnostic.

### 3.4 The return payload (a Layer-1 envelope)

```
feedback:
  prototype: auth-flow-v2
  chosen: option-B
annotations[2]{id,selector,box,note}:
  n1,"#option-b .cta","900,20,120,40","yes, make this primary"
  n2,".sidebar nav","0,60,240,600","too many items, cut to 4"
coverage: partial         # human reviewed 2 of 3 options
help[1]: Run with the chosen option to generate the next mock
```

This is the **first real consumer** of Layer 1 — annotations are row-shaped, TOON
crushes them vs JSON, and `chosen`/`coverage` are exactly the envelope's honesty fields.

### 3.5 Isolation note (needs a decision)

Agent-authored HTML/JS running in the review browser is untrusted-ish code executing on
the human's machine. Options: serve the prototype in a sandboxed `<iframe>` (the chrome
lives in the parent frame, communicates via `postMessage`), and/or serve from the bridge
over `localhost` with a strict origin. **Open — see §6.**

---

## 4. Bridge architecture (harness-neutral core + thin adapters)

```
Browser UI
   │  HTTP  (POST feedback: clicks, annotations, chosen option, chat text)
   │  WS    (stream: agent chat tokens, "prototype ready", turn events)
   ▼
Local bridge (Node/TS)
   ·  owns the harness session
   ·  serves the agent's HTML prototype to the browser
   ·  exposes a bridge-owned MCP server to the agent (submit_prototype / get_review)
   ·  converts browser feedback → Layer-1 TOON envelope → into the agent turn
   ▼
Harness adapter (the ONLY per-harness code)
   ├─ Codex:  drive `codex app-server` (JSON-RPC); push feedback via turn/steer
   ├─ Claude: long-lived ClaudeSDKClient; in-process MCP tool blocks for review
   └─ Cursor: bridge-owned blocking MCP tool + stop/followup_message (pull-only)
```

The **browser + bridge core is the product**; the adapter is a leaf (matches our
"SDK-is-a-leaf, one core / many surfaces" instinct).

---

## 5. Harness integration findings (researched 2026-08-30, primary-source-cited)

The one axis that matters: **can the bridge push structured feedback into a *live* turn?**

| Harness | Feedback-return path | Fit |
|---|---|---|
| **Codex** | `codex app-server` JSON-RPC + **`turn/steer`** = documented **mid-turn injection**; elicitation wait exempt from `tool_timeout_sec`; official TS/Python SDKs; `codex exec --json` JSONL headless | **Best** |
| **Claude Code** | Agent SDK, long-lived `ClaudeSDKClient` (`query`/`receive_response`); in-process MCP tool (`createSdkMcpServer`/`tool`) blocks for review; `includePartialMessages` streams chat | **Strong** — watch `MCP_TOOL_TIMEOUT` on long blocks |
| **Cursor** | **Pull only** — agent must call a bridge MCP tool whose blocking limit is **undocumented**; chat pane not scriptable; extension API is config-only; Cloud Agents API is cloud-only | **Weakest** — validate empirically, or accept cloud |

Key specifics:
- **Codex `turn/steer`**: "append user input to the active in-flight turn" — the cleanest
  loop; feedback doesn't even need a new turn. Caveat: elicitation deadlock bug if the
  client can't answer (openai/codex#11816).
- **Claude**: no true mid-turn inject (current turn finishes, then follow-up); the
  block-in-MCP-tool pattern is idiomatic but bounded by `MCP_TOOL_TIMEOUT` — raise it or
  use the stateless "review pending → feedback returns as next turn" variant.
- **Cursor**: `stop` hook `followup_message` auto-submits late feedback as the next turn
  (respects `loop_limit`); no `onIdle`/external wake; MCP-tool-return is the only live
  structured channel.

Convergent conclusion: **all three = MCP + local bridge + structured payload.** The
browser tool is harness-neutral; only the adapter changes.

**Recommended first target:** Codex (cleanest loop) **or** Claude Code (dogfood the
harness we're already in). Cursor is a later adapter.

---

## 6. Open questions (decide before/early in build)

1. **Envelope: spec now or later?** Lean: TS-idiomatic now, formalize once a 2nd
   consumer/language appears.
2. **First harness adapter:** Codex (best loop) vs Claude Code (dogfood-now)?
3. **Prototype isolation:** sandboxed iframe + postMessage to the chrome? localhost-only
   origin? How much untrusted-JS risk do we accept on the reviewer's machine?
4. **Block vs steer for review handoff:** blocking MCP tool (simple, timeout-bound) vs
   stateless "pending → next turn" (robust, more parts). Codex `turn/steer` sidesteps
   this; Claude/Cursor don't.
5. **Chat pane wiring:** does the browser chat talk to the *harness* (via bridge) end to
   end, or to a model API directly for lightweight turns? (Lean: through the bridge, so
   there's one session/context.)
6. **Prototype authoring surface:** truly arbitrary HTML, or a documented set of
   conventions/`data-*` hooks the chrome relies on to find option-cards/workflow-steps?
7. **Naming:** the foundation layer and the browser tool.

---

## 7. Kill criteria / cheapest falsifying gate

Before investing in the full multi-harness build, prove the loop end-to-end **once**:

> Serve one hardcoded HTML prototype → inject the inspector+annotation chrome → human
> clicks & annotates → POST to bridge → bridge emits a TOON envelope → the envelope
> reaches a live agent turn in **one** harness and the agent visibly acts on it.

If that thin vertical slice is awkward or the feedback can't cleanly re-enter the turn,
the concept needs rework before any foundation polish. (Cheap gate before expensive
build — the house discipline.)
```
