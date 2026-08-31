import type { Envelope } from "@workbench/envelope";
import type { AdapterHandlers, HarnessAdapter } from "@workbench/adapters";
import { chainHandlers } from "@workbench/adapters";

/**
 * A deterministic stand-in for a real harness. It presents a canned prototype
 * on start and streams a short chat, so the full server loop (serve prototype →
 * browser review → feedback resolves the turn) can be driven without a live
 * `claude` binary. This is what makes the app CI-testable.
 *
 * Feedback the browser submits is captured on `lastFeedback` and echoed back
 * into the chat stream, standing in for "the agent acted on the review".
 */
export class FakeAdapter implements HarnessAdapter {
  readonly id = "fake";
  lastFeedback: Envelope | undefined;

  private handlers: AdapterHandlers = {};

  constructor(private readonly prototypeHtml: string) {}

  on(handlers: AdapterHandlers): void {
    this.handlers = chainHandlers(this.handlers, handlers);
  }

  async start(prompt: string): Promise<void> {
    this.handlers.onTurnStart?.();
    this.emitChat(`Here is a prototype for: ${prompt}\n`);
    this.handlers.onPrototype?.({ id: "fake-1", html: this.prototypeHtml });
    this.emitChat("Click elements to annotate, then submit your review.");
    this.handlers.onTurnEnd?.();
  }

  async sendChat(text: string): Promise<void> {
    this.handlers.onTurnStart?.();
    this.emitChat(`(echo) ${text}`);
    this.handlers.onTurnEnd?.();
  }

  async submitFeedback(feedback: Envelope): Promise<void> {
    this.lastFeedback = feedback;
    const count = Array.isArray((feedback.data as { annotations?: unknown[] })?.annotations)
      ? (feedback.data as { annotations: unknown[] }).annotations.length
      : 0;
    this.emitChat(`Got your review (${count} annotation(s)). Iterating…`);
    this.handlers.onTurnEnd?.();
  }

  async stop(): Promise<void> {
    /* nothing to tear down */
  }

  private emitChat(text: string): void {
    for (const ch of text) this.handlers.onChat?.(ch);
  }
}
