import type { HarnessAdapter } from "../adapter.js";

/**
 * Claude Code adapter — TODO(v2): implement.
 *
 * Hold a long-lived ClaudeSDKClient; expose an in-process MCP tool that blocks
 * until the browser submits review, returning it as an envelope. Mind
 * MCP_TOOL_TIMEOUT on long blocks. See docs/design/v2-toon-foundation-and-review.md §5.
 */
export function createClaudeAdapter(): HarnessAdapter {
  throw new Error("@workbench/adapters/claude: not yet implemented");
}
