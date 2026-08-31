import { query } from "@anthropic-ai/claude-agent-sdk";
import type { Envelope } from "@workbench/envelope";
import type {
  AdapterHandlers,
  HarnessAdapter,
  PrototypePresentation,
} from "../adapter.js";
import { chainHandlers } from "../adapter.js";
import { PendingReview } from "./pending-review.js";
import {
  createReviewServer,
  REVIEW_TOOLS_GLOB,
} from "./review-server.js";

export interface ClaudeAdapterOptions {
  /** Extra tools to allow beyond the review tools (e.g. "Read", "Grep"). */
  allowedTools?: string[];
  /** Override the blocking review-tool timeout (ms). */
  reviewTimeoutMs?: number;
  /** System prompt appended to the agent's context, if any. */
  systemPrompt?: string;
}

/**
 * Claude Code adapter.
 *
 * There is no native mid-turn injection in the TS Agent SDK, so the loop is a
 * *pull*: the agent calls our in-process `present_prototype` tool (which fires
 * onPrototype so the browser renders it) and then `request_review`, which BLOCKS
 * until the browser submits feedback. Feedback resolves that block and the same
 * turn continues.
 *
 * Multi-turn continuity uses query()'s `resume: sessionId` — captured from the
 * first turn's result/init message.
 *
 * NOTE: streaming a turn and injecting a *new* human chat message concurrently
 * is not possible within a single query() iteration; sendChat() therefore queues
 * the message and starts a fresh turn once the current one parks. This matches
 * Claude's "turn completes, then follow-up" model.
 */
export class ClaudeAdapter implements HarnessAdapter {
  readonly id = "claude";

  private handlers: AdapterHandlers = {};
  private readonly pending = new PendingReview();
  private sessionId: string | undefined;
  private turnActive = false;
  private readonly options: ClaudeAdapterOptions;

  constructor(options: ClaudeAdapterOptions = {}) {
    this.options = options;
  }

  on(handlers: AdapterHandlers): void {
    this.handlers = chainHandlers(this.handlers, handlers);
  }

  async start(prompt: string): Promise<void> {
    await this.runTurn(prompt);
  }

  async sendChat(text: string): Promise<void> {
    // A turn can only be driven once at a time; the browser is expected to gate
    // input while a turn is active (wb-chat `busy`). If called mid-turn we still
    // serialize by awaiting the in-flight turn first.
    while (this.turnActive) {
      await new Promise((r) => setTimeout(r, 25));
    }
    await this.runTurn(text);
  }

  async submitFeedback(feedback: Envelope): Promise<void> {
    // Unblocks the awaiting request_review call, or buffers the feedback if it
    // raced ahead of the agent opening the review (the present->request gap).
    this.pending.submit(feedback);
  }

  async stop(): Promise<void> {
    this.pending.cancel("session stopped");
  }

  /** Drive one query() turn to completion, streaming and wiring MCP. */
  private async runTurn(prompt: string): Promise<void> {
    this.turnActive = true;
    this.handlers.onTurnStart?.();
    const reviewServer = createReviewServer({
      pending: this.pending,
      reviewTimeoutMs: this.options.reviewTimeoutMs,
      presentPrototype: (proto: PrototypePresentation) =>
        this.handlers.onPrototype?.(proto),
    });

    try {
      for await (const message of query({
        prompt,
        options: {
          includePartialMessages: true,
          ...(this.sessionId ? { resume: this.sessionId } : {}),
          ...(this.options.systemPrompt
            ? { appendSystemPrompt: this.options.systemPrompt }
            : {}),
          mcpServers: { workbench_review: reviewServer },
          allowedTools: [REVIEW_TOOLS_GLOB, ...(this.options.allowedTools ?? [])],
        },
      })) {
        this.handleMessage(message);
      }
    } finally {
      this.turnActive = false;
      this.handlers.onTurnEnd?.();
    }
  }

  /** Route a stream message: capture session id, emit chat deltas. */
  private handleMessage(message: unknown): void {
    const m = message as {
      type?: string;
      subtype?: string;
      session_id?: string;
      event?: {
        type?: string;
        delta?: { type?: string; text?: string };
      };
    };

    if (m.session_id && !this.sessionId) {
      this.sessionId = m.session_id;
    }

    if (
      m.type === "stream_event" &&
      m.event?.type === "content_block_delta" &&
      m.event.delta?.type === "text_delta" &&
      typeof m.event.delta.text === "string"
    ) {
      this.handlers.onChat?.(m.event.delta.text);
    }
  }
}

/** Factory matching the other adapters' shape. */
export function createClaudeAdapter(
  options: ClaudeAdapterOptions = {},
): HarnessAdapter {
  return new ClaudeAdapter(options);
}
