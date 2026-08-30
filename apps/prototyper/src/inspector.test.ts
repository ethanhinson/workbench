import { describe, it, expect } from "vitest";
import { injectInspector, INSPECTOR_SOURCE } from "./inspector.js";

describe("injectInspector", () => {
  it("injects the inspector before </body>", () => {
    const out = injectInspector("<html><body><h1>hi</h1></body></html>");
    expect(out).toContain("<h1>hi</h1>");
    expect(out).toMatch(/<script>.*<\/script><\/body>/s);
    expect(out.indexOf("<script>")).toBeGreaterThan(out.indexOf("<h1>hi</h1>"));
  });

  it("appends when there is no </body>", () => {
    const out = injectInspector("<div>bare fragment</div>");
    expect(out.startsWith("<div>bare fragment</div>")).toBe(true);
    expect(out).toContain("<script>");
  });

  it("passes the source marker into the injected agent", () => {
    const out = injectInspector("<body></body>");
    expect(out).toContain(JSON.stringify(INSPECTOR_SOURCE));
  });

  it("serializes a self-contained function (no external identifiers leak)", () => {
    const out = injectInspector("<body></body>");
    // The IIFE should be invoked with the source string argument.
    expect(out).toMatch(/\(function[\s\S]*\}\)\("wb-inspector"\);/);
  });
});
