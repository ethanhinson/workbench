import { Config } from "@stencil/core";

// Workbench web components. Output both a distributable custom-elements bundle
// (for the browser shell / prototyper) and a lazy loader. Framework wrappers
// (react/vue) can be added later via @stencil/*-output-target when a consumer
// needs them — kept out until then to avoid dragging framework deps.
export const config: Config = {
  namespace: "workbench-components",
  taskQueue: "async",
  outputTargets: [
    {
      type: "dist",
      esmLoaderPath: "../loader",
    },
    {
      type: "dist-custom-elements",
      customElementsExportBehavior: "auto-define-custom-elements",
      externalRuntime: false,
    },
    {
      type: "docs-readme",
    },
    {
      type: "www",
      serviceWorker: null,
    },
  ],
  testing: {
    browserHeadless: "shell",
  },
};
