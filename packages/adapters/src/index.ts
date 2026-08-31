// Root entry is type-only on purpose: importing `@workbench/adapters` must not
// pull any harness SDK. Concrete adapters live behind their subpaths
// (`@workbench/adapters/claude`, `/codex`, `/cursor`).
export type {
  HarnessAdapter,
  AdapterHandlers,
  PrototypePresentation,
} from "./adapter.js";
// A value (composes handlers so multiple observers can register). Node builtins
// only — safe to include in the type-only-ish root without pulling any SDK.
export { chainHandlers } from "./adapter.js";
