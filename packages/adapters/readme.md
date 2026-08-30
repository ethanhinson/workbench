# @workbench/adapters

The per-harness code that closes the prototype-review loop: it streams the
agent's chat out to the browser and pushes the human's structured feedback back
into the running agent turn. Everything else (`@workbench/components`,
`@workbench/envelope`) is harness-neutral.

Import only the harness you use — each is a separate entry point so a Codex
consumer never pulls Claude's SDK:

```ts
import { createClaudeAdapter } from "@workbench/adapters/claude";
import { createCodexAdapter } from "@workbench/adapters/codex";   // stub
import { createCursorAdapter } from "@workbench/adapters/cursor"; // stub
```

The Claude/Codex SDKs are **optional peer deps** — installed only when you use
that adapter.

## The neutral contract

```ts
interface HarnessAdapter {
  readonly id: string;
  on(handlers: { onChat?; onPrototype?; onTurnEnd? }): void;
  start(prompt: string): Promise<void>;      // open the session
  sendChat(text: string): Promise<void>;     // human -> agent (chat pane)
  submitFeedback(feedback: Envelope): Promise<void>; // review -> running turn
  stop(): Promise<void>;
}
```

## Claude adapter

No native mid-turn injection in the Agent SDK, so the loop is a **pull**:

```
agent → present_prototype(id, html)  ──onPrototype──▶  browser renders it
agent → request_review(id)  ─────────BLOCKS──────────▶  human clicks/annotates
                                       ▲                        │
        submitFeedback(envelope) ──────┘◀──── bridge POST ◀─────┘
        (resolves the blocked tool; the same turn continues)
```

Implementation:

- **`present_prototype` / `request_review`** are in-process MCP tools
  (`createSdkMcpServer` + `tool`, Zod schemas). `request_review` awaits a
  `PendingReview` promise — the seam the browser POST resolves.
- **Blocking is real** and bounded by the per-server `timeout`; we raise it to
  30 min (`DEFAULT_REVIEW_TIMEOUT_MS`) since human review is slow. Override via
  `createClaudeAdapter({ reviewTimeoutMs })`.
- **Chat** streams via `query({ includePartialMessages: true })` →
  `stream_event` / `content_block_delta` / `text_delta`, forwarded to `onChat`.
- **Continuity** uses `query`'s `resume: sessionId`, captured from the first
  turn.

### Wiring (bridge sketch)

```ts
const adapter = createClaudeAdapter({ allowedTools: ["Read", "Grep"] });
adapter.on({
  onChat: (chunk) => ws.send({ type: "chat", chunk }),
  onPrototype: ({ id, html }) => servePrototype(id, html),
});
await adapter.start("Propose three layouts for the settings page.");

// browser POSTs review feedback:
app.post("/review", (req) =>
  adapter.submitFeedback(ok(req.body, { coverage: req.body.coverage })));

// browser chat pane:
app.post("/chat", (req) => adapter.sendChat(req.body.text));
```

## Status

- **claude** — implemented (session loop, streaming, blocking review MCP tool).
  `PendingReview` is unit-tested; the SDK-driven turn loop needs an integration
  test against a live `claude` install.
- **codex**, **cursor** — stubs. See `docs/design/v2-toon-foundation-and-review.md` §5.
