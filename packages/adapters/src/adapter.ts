/**
 * HarnessAdapter — the ONLY per-harness code in the system. The browser
 * components and the envelope are harness-neutral; an adapter is what turns
 * "the human finished reviewing" into "the running agent turn sees structured
 * feedback."
 *
 * Research (see docs/design/v2-toon-foundation-and-review.md §5):
 *   - codex:  drive `codex app-server` (JSON-RPC); push feedback via turn/steer
 *             (documented mid-turn injection) — the cleanest loop
 *   - claude: long-lived ClaudeSDKClient; an in-process MCP tool blocks for
 *             review (mind MCP_TOOL_TIMEOUT)
 *   - cursor: bridge-owned blocking MCP tool + stop/followup_message; pull-only,
 *             blocking limit undocumented — validate empirically
 */

import type { Envelope } from "@workbench/envelope";

export interface HarnessAdapter {
  /** Adapter id, e.g. "codex" | "claude" | "cursor". */
  readonly id: string;
  /** Start (or attach to) an agent session. */
  start(): Promise<void>;
  /** Subscribe to streamed assistant chat text (for wb-chat). */
  onChat(handler: (chunk: string) => void): void;
  /** Push structured human review feedback back into the running agent turn. */
  submitFeedback(feedback: Envelope): Promise<void>;
  /** Stop the session. */
  stop(): Promise<void>;
}
