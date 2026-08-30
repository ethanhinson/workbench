import type { HarnessAdapter } from "../adapter.js";

/**
 * Codex adapter — TODO(v2): implement.
 *
 * Drive `codex app-server` over JSON-RPC; push review feedback into the live
 * turn via `turn/steer` (documented mid-turn injection). Cleanest loop of the
 * three. See docs/design/v2-toon-foundation-and-review.md §5.
 */
export function createCodexAdapter(): HarnessAdapter {
  throw new Error("@workbench/adapters/codex: not yet implemented");
}
