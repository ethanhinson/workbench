import { describe, it, expect } from "vitest";
import { PendingReview, ReviewCancelledError } from "./pending-review.js";
import { ok } from "@workbench/envelope";

describe("PendingReview", () => {
  it("blocks until submit, then resolves with the envelope", async () => {
    const pr = new PendingReview();
    const waiting = pr.open("proto-1");
    expect(pr.isWaiting).toBe(true);

    const feedback = ok({ chosen: "B" }, { coverage: "partial" });
    const delivered = pr.submit(feedback);
    expect(delivered).toBe(true);

    await expect(waiting).resolves.toEqual(feedback);
    expect(pr.isWaiting).toBe(false);
  });

  it("buffers a submission that races ahead of open(), delivering it on the next open()", async () => {
    const pr = new PendingReview();
    // Feedback arrives during the present_prototype -> request_review gap.
    const early = ok({ chosen: "early" });
    expect(pr.submit(early)).toBe(true);
    expect(pr.hasBuffered).toBe(true);
    expect(pr.isWaiting).toBe(false);

    // When the agent finally opens the review, it resolves immediately.
    await expect(pr.open("proto-1")).resolves.toEqual(early);
    expect(pr.hasBuffered).toBe(false);
  });

  it("opening a second review cancels the first", async () => {
    const pr = new PendingReview();
    const first = pr.open("proto-1");
    const second = pr.open("proto-2");

    await expect(first).rejects.toBeInstanceOf(ReviewCancelledError);
    pr.submit(ok({ chosen: "A" }));
    await expect(second).resolves.toMatchObject({ data: { chosen: "A" } });
  });

  it("cancel rejects the outstanding review", async () => {
    const pr = new PendingReview();
    const waiting = pr.open("proto-1");
    pr.cancel("teardown");
    await expect(waiting).rejects.toBeInstanceOf(ReviewCancelledError);
    expect(pr.isWaiting).toBe(false);
  });

  it("cancel with nothing outstanding is a no-op", () => {
    const pr = new PendingReview();
    expect(() => pr.cancel()).not.toThrow();
  });
});
