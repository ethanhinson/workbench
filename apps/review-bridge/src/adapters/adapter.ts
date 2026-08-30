/**
 * HarnessAdapter — the ONLY per-harness code. Everything else in the bridge is
 * harness-neutral. Research (see /design.md §5):
 *   - codex:  drive `codex app-server` (JSON-RPC); push feedback via turn/steer
 *   - claude: long-lived ClaudeSDKClient; in-process MCP tool blocks for review
 *   - cursor: bridge-owned blocking MCP tool + stop/followup_message (pull-only)
 */

import type { Envelope } from "@workbench/envelope";

export interface HarnessAdapter {
  /** Human-readable adapter id, e.g. "codex" | "claude" | "cursor". */
  readonly id: string;
  /** Start (or attach to) an agent session. */
  start(): Promise<void>;
  /** Stream assistant chat text out to the caller (browser chat pane). */
  onChat(handler: (chunk: string) => void): void;
  /** Push structured human review feedback back into the running agent turn. */
  submitFeedback(feedback: Envelope): Promise<void>;
  /** Stop the session. */
  stop(): Promise<void>;
}
