import { createSdkMcpServer, tool } from "@anthropic-ai/claude-agent-sdk";
import { z } from "zod";
import { render } from "@workbench/envelope";
import type { PendingReview } from "./pending-review.js";

/** The MCP server name; tools are exposed as `mcp__workbench_review__<tool>`. */
export const REVIEW_SERVER_NAME = "workbench_review";

/** allowedTools entry that grants the agent every review tool. */
export const REVIEW_TOOLS_GLOB = `mcp__${REVIEW_SERVER_NAME}__*`;

/**
 * Human review can take minutes. The default MCP tool timeout (~30s) would kill
 * the blocking `request_review` call, so we raise it here. Callers can override.
 */
export const DEFAULT_REVIEW_TIMEOUT_MS = 30 * 60 * 1000; // 30 min

export interface ReviewServerDeps {
  /** Where a newly-presented prototype goes (serve it to the browser). */
  presentPrototype: (input: { id: string; html: string }) => void | Promise<void>;
  /** The promise registry the blocking review tool awaits. */
  pending: PendingReview;
  /** Override the review tool timeout (ms). */
  reviewTimeoutMs?: number;
}

/**
 * Build the in-process MCP server that closes the review loop. Two tools:
 *
 *   present_prototype(id, html) — hand a rendered HTML prototype to the browser.
 *                                 Returns immediately.
 *   request_review(prototypeId) — BLOCK until the human submits feedback in the
 *                                 browser, then return it as a TOON envelope.
 *
 * This is the whole Claude integration story: no native mid-turn injection, so
 * the agent *pulls* for feedback by calling a tool that blocks.
 */
export function createReviewServer(deps: ReviewServerDeps) {
  const presentPrototype = tool(
    "present_prototype",
    "Render an HTML prototype for the human to review in the browser. " +
      "Provide a stable id and the full HTML document (free-form; may vendor deps " +
      "via <script src> / ESM URLs — no build step). Returns immediately; call " +
      "request_review next to collect feedback.",
    {
      id: z.string().describe("Stable id for this prototype, e.g. 'auth-flow-v2'"),
      html: z.string().describe("Full HTML document for the prototype"),
    },
    async (args) => {
      await deps.presentPrototype({ id: args.id, html: args.html });
      return {
        content: [
          { type: "text", text: render({ data: { presented: args.id } }, "toon") },
        ],
      };
    },
  );

  const requestReview = tool(
    "request_review",
    "Block until the human finishes reviewing the presented prototype in the " +
      "browser, then return their structured feedback (chosen option, annotations, " +
      "coverage) as a TOON envelope. Call after present_prototype.",
    {
      prototypeId: z.string().describe("The id passed to present_prototype"),
    },
    async (args) => {
      const result = await deps.pending.open(args.prototypeId);
      return {
        content: [{ type: "text", text: render(result, "toon") }],
        structuredContent: result as Record<string, unknown>,
      };
    },
  );

  return createSdkMcpServer({
    name: REVIEW_SERVER_NAME,
    version: "0.0.0",
    tools: [presentPrototype, requestReview],
    timeout: deps.reviewTimeoutMs ?? DEFAULT_REVIEW_TIMEOUT_MS,
  });
}
