/**
 * review-bridge — the local process that closes the loop:
 *
 *   browser  <--HTTP feedback / WS chat-stream-->  bridge  <--adapter-->  harness
 *
 * Responsibilities:
 *   - own the harness session (via a HarnessAdapter)
 *   - serve the agent-authored prototype + review-chrome to the browser
 *   - expose a bridge-owned MCP surface the agent uses to submit prototypes and
 *     request review
 *   - turn browser feedback into a @workbench/envelope payload and push it back
 *     into the running agent turn
 *
 * TODO(v2): implement. Scaffold placeholder — see /design.md §4 and §5.
 */
export {};
