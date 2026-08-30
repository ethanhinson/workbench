/**
 * review-ui — the browser shell.
 *
 *   ┌───────────────────────────────┬───────────────┐
 *   │ prototype pane (~80%, resize) │ chat pane 20% │
 *   │  agent-authored HTML/CSS/JS   │ human <-> agent│
 *   │  + review-chrome injected over │ (streamed via  │
 *   │  it (sandboxed iframe — TBD)   │  the bridge)   │
 *   └───────────────────────────────┴───────────────┘
 *
 * TODO(v2): implement. Scaffold placeholder — see /design.md §3.2 and open
 * question §6.3 (prototype isolation).
 */
export const REVIEW_UI_VERSION = "0.0.0";
