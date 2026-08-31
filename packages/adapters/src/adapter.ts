/**
 * HarnessAdapter — the ONLY per-harness code in the system. The browser
 * components and the envelope are harness-neutral; an adapter is what turns
 * "the human finished reviewing" into "the running agent turn sees structured
 * feedback", and streams the agent's chat back out to the browser.
 *
 * Research (see docs/design/v2-toon-foundation-and-review.md §5):
 *   - codex:  drive `codex app-server` (JSON-RPC); push feedback via turn/steer
 *             (documented mid-turn injection) — the cleanest loop
 *   - claude: drive query() (continue/resume); an in-process MCP tool blocks for
 *             review, feedback resolves it (raise the per-server timeout)
 *   - cursor: bridge-owned blocking MCP tool + stop/followup_message; pull-only,
 *             blocking limit undocumented — validate empirically
 */

import type { Envelope } from "@workbench/envelope";

/** A prototype the agent wants the browser to render for review. */
export interface PrototypePresentation {
  id: string;
  /** Full HTML document (free-form; may vendor deps via URLs — no build step). */
  html: string;
}

/** Callbacks the browser/bridge layer registers on an adapter. */
export interface AdapterHandlers {
  /** A turn started (the agent is now working). */
  onTurnStart?: () => void;
  /** Streamed assistant chat text (feeds wb-chat). */
  onChat?: (chunk: string) => void;
  /** The agent presented a prototype; serve it to the browser. */
  onPrototype?: (proto: PrototypePresentation) => void;
  /** A turn finished (parked / awaiting the human). */
  onTurnEnd?: () => void;
}

/**
 * Compose two handler sets so BOTH run for each event, in order (existing first,
 * then added). This lets multiple observers register via on() — e.g. a terminal
 * logger and the WS broadcaster — without the later on() overwriting the earlier.
 */
export function chainHandlers(
  a: AdapterHandlers,
  b: AdapterHandlers,
): AdapterHandlers {
  const both = <T extends unknown[]>(
    fa: ((...args: T) => void) | undefined,
    fb: ((...args: T) => void) | undefined,
  ): ((...args: T) => void) | undefined => {
    if (fa && fb) return (...args: T) => {
      fa(...args);
      fb(...args);
    };
    return fb ?? fa;
  };
  return {
    onTurnStart: both(a.onTurnStart, b.onTurnStart),
    onChat: both(a.onChat, b.onChat),
    onPrototype: both(a.onPrototype, b.onPrototype),
    onTurnEnd: both(a.onTurnEnd, b.onTurnEnd),
  };
}

export interface HarnessAdapter {
  /** Adapter id, e.g. "codex" | "claude" | "cursor". */
  readonly id: string;
  /** Register browser-facing callbacks. Call before start(). */
  on(handlers: AdapterHandlers): void;
  /** Start (or attach to) an agent session with an opening prompt. */
  start(prompt: string): Promise<void>;
  /** Send a human chat message into the session (from the browser chat pane). */
  sendChat(text: string): Promise<void>;
  /**
   * Deliver structured human review feedback so the running agent turn can act
   * on it. For Claude this resolves the blocking review tool.
   */
  submitFeedback(feedback: Envelope): Promise<void>;
  /** Stop the session and cancel any outstanding review. */
  stop(): Promise<void>;
}
