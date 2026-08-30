import { describe, it, expect } from "vitest";
import { ok, err, isError, render } from "../src/index.js";

describe("envelope construction", () => {
  it("ok() carries only the fields you set — absence is meaningful", () => {
    const e = ok([{ id: "1" }]);
    expect(e).toEqual({ data: [{ id: "1" }] });
    expect(e.coverage).toBeUndefined();
    expect(e.total).toBeUndefined();
  });

  it("ok() threads honesty fields through", () => {
    const e = ok([{ id: "1" }], {
      total: 847,
      coverage: "partial",
      notes: [{ kind: "ambiguous", ref: "#hero h1" }],
      help: ["Run with --full for the rest"],
    });
    expect(e.total).toBe(847);
    expect(e.coverage).toBe("partial");
    expect(e.notes?.[0]?.kind).toBe("ambiguous");
  });

  it("err() omits help when none given", () => {
    expect(err("VALIDATION_ERROR", "bad flag")).toEqual({
      error: "bad flag",
      code: "VALIDATION_ERROR",
    });
  });

  it("isError narrows the union", () => {
    expect(isError(err("X", "y"))).toBe(true);
    expect(isError(ok(1))).toBe(false);
  });
});

describe("render parity", () => {
  it("json and toon are information-equivalent (round-trippable fields)", () => {
    const e = ok([{ id: "a1", note: "low contrast" }], { total: 3, coverage: "partial" });
    const json = JSON.parse(render(e, "json"));
    expect(json).toEqual(e);

    const toon = render(e, "toon");
    // TOON output should mention the payload and the honesty fields.
    expect(toon).toContain("a1");
    expect(toon).toContain("partial");
    expect(toon).toContain("3");
  });
});
