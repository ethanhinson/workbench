import type { HarnessAdapter } from "../adapter.js";

/**
 * Cursor adapter — TODO(v2): implement.
 *
 * Bridge-owned blocking MCP tool the agent calls, plus a `stop` hook
 * `followup_message` for late feedback. Pull-only; blocking limit is
 * undocumented — validate empirically. See docs/design/v2-toon-foundation-and-review.md §5.
 */
export function createCursorAdapter(): HarnessAdapter {
  throw new Error("@workbench/adapters/cursor: not yet implemented");
}
