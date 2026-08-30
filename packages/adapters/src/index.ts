// Root entry is type-only on purpose: importing `@workbench/adapters` must not
// pull any harness SDK. Concrete adapters live behind their subpaths
// (`@workbench/adapters/claude`, `/codex`, `/cursor`).
export type {
  HarnessAdapter,
  AdapterHandlers,
  PrototypePresentation,
} from "./adapter.js";
