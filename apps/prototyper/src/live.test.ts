import { describe, it, expect } from "vitest";
import { WebSocket } from "ws";
import { startPrototyperServer, type PrototyperServer } from "./server.js";
import { createRealAdapter } from "./claude-mode.js";

/**
 * LIVE end-to-end test against a real `claude`. Opt-in only: set WB_LIVE=1 to
 * run it. It is skipped in CI (which never sets WB_LIVE) so it can't break the
 * normal pipeline, but is runnable on demand so the real loop can't silently
 * regress:
 *
 *   WB_LIVE=1 pnpm --filter @workbench/prototyper test
 *
 * It drives the exact real code path (createRealAdapter = the CLI's real mode)
 * and asserts the COMPLETE loop:
 *   1. the agent presents a prototype (served, inspector injected)
 *   2. chat streams over the WS during the turn
 *   3. the review POST unblocks request_review
 *   4. the agent reacts to the feedback AFTER the review (proves the envelope
 *      re-entered the turn and was consumed) and the turn ends
 */
const LIVE = !!process.env.WB_LIVE;
const TIMEOUT = 180_000;

interface WsTap {
  events: Array<{ type: string; chunk?: string; id?: string }>;
  chatText(): string;
  chatAfter(marker: number): string;
  markCount(): number;
  close(): void;
}

function tapWs(httpUrl: string): WsTap {
  const ws = new WebSocket(httpUrl.replace("http", "ws") + "/ws");
  const events: WsTap["events"] = [];
  ws.on("message", (d) => events.push(JSON.parse(String(d))));
  return {
    events,
    chatText: () => events.filter((e) => e.type === "chat").map((e) => e.chunk).join(""),
    chatAfter: (marker) =>
      events.slice(marker).filter((e) => e.type === "chat").map((e) => e.chunk).join(""),
    markCount: () => events.length,
    close: () => ws.close(),
  };
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function waitUntil<T>(fn: () => T | Promise<T>, ms: number): Promise<T> {
  const deadline = Date.now() + ms;
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() > deadline) throw new Error("waitUntil timed out");
    await wait(500);
  }
}

describe.skipIf(!LIVE)("LIVE prototyper loop (real Claude)", () => {
  it(
    "presents → streams chat → accepts review → reacts to feedback",
    async () => {
      const adapter = await createRealAdapter({ reviewTimeoutMs: 120_000 });
      let server: PrototyperServer | undefined;
      try {
        server = await startPrototyperServer({
          adapter,
          prompt:
            "Propose a simple settings page with a Save and a Cancel button. " +
            "Present ONE small prototype, then request review.",
          port: 0,
        });
        const tap = tapWs(server.url);

        // 1 + 2: the agent presents a prototype, and chat streams.
        await waitUntil(async () => {
          const r = await fetch(`${server!.url}/prototype`);
          return r.status === 200;
        }, 90_000);

        const protoHtml = await (await fetch(`${server.url}/prototype`)).text();
        expect(protoHtml.length).toBeGreaterThan(200);
        expect(protoHtml).toContain("wb-inspector"); // inspector injected
        expect(tap.chatText().length).toBeGreaterThan(0); // chat streamed

        // 3: submit a review with a distinctive annotation.
        const reviewMark = tap.markCount();
        const res = await fetch(`${server.url}/review`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            chosen: null,
            coverage: "complete",
            annotations: [
              {
                id: "n1",
                selector: "#save",
                box: "0,0,60,30",
                note: "Make the Save button the primary action (filled, high contrast).",
              },
            ],
          }),
        });
        expect(res.status).toBe(200);

        // 4: the agent reacts AFTER the review, then the turn ends.
        await waitUntil(() => tap.events.some((e) => e.type === "turn-end"), 90_000);
        const reaction = tap.chatAfter(reviewMark).toLowerCase();
        expect(reaction.length).toBeGreaterThan(0);
        // It acted on the feedback: references the Save button / primary action.
        expect(reaction).toMatch(/save|primary|contrast|button/);

        tap.close();
      } finally {
        await server?.close();
      }
    },
    TIMEOUT,
  );
});
