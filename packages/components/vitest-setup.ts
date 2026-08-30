// Load the built Stencil components (lazy loader) into the test environment.
// Requires `pnpm build` (or turbo's ^build dependency) to have produced dist/.
await import("./dist/workbench-components/workbench-components.esm.js");

export {};
