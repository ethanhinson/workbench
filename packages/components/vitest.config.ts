import { defineConfig } from "vitest/config";

// Pure-logic tests (node env) for the component reducers. Full DOM/render specs
// for the web components are deferred until @stencil/vitest's custom "stencil"
// environment stabilizes against Vitest 3 (its environment registration errors
// under 3.2 — see wb-chat.spec.tsx). The shipped streaming logic is extracted
// into chat-log.ts and covered here, so the behavior is tested even without the
// DOM harness.
export default defineConfig({
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
