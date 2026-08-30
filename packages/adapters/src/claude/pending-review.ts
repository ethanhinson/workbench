import type { Envelope } from "@workbench/envelope";

/**
 * The seam between the blocking MCP tool and the browser.
 *
 * When the agent calls `request_review`, the tool handler awaits a promise held
 * here. When the browser POSTs the human's feedback, the bridge calls
 * `resolve(...)` and the awaiting tool call unblocks with that envelope.
 *
 * Pure logic, no SDK dependency — this is where the whole "block until the
 * human clicks" contract lives, so it is unit-testable in isolation.
 */

/** The structured feedback a completed review yields (a success envelope). */
export type ReviewResult = Envelope;

interface Pending {
  resolve: (result: ReviewResult) => void;
  reject: (reason: Error) => void;
  /** The prototype id this review is for, for correlation/debugging. */
  prototype: string;
}

export class ReviewCancelledError extends Error {
  constructor(message = "review cancelled") {
    super(message);
    this.name = "ReviewCancelledError";
  }
}

/**
 * A single-slot review registry. One review is outstanding at a time — opening
 * a new one cancels the previous (the agent asked again before the human
 * answered), which keeps the browser and the agent from disagreeing about which
 * prototype is under review.
 */
export class PendingReview {
  private current: Pending | undefined;

  /** True while the agent is blocked waiting on human feedback. */
  get isWaiting(): boolean {
    return this.current !== undefined;
  }

  /**
   * Open a review for `prototype` and return a promise the MCP tool awaits.
   * Cancels any review already outstanding.
   */
  open(prototype: string): Promise<ReviewResult> {
    if (this.current) {
      this.current.reject(new ReviewCancelledError("superseded by a new review"));
      this.current = undefined;
    }
    return new Promise<ReviewResult>((resolve, reject) => {
      this.current = { resolve, reject, prototype };
    });
  }

  /**
   * Deliver the human's feedback, unblocking the awaiting tool call. Returns
   * false if no review was outstanding (a stray POST) so the caller can 404.
   */
  submit(result: ReviewResult): boolean {
    if (!this.current) return false;
    const { resolve } = this.current;
    this.current = undefined;
    resolve(result);
    return true;
  }

  /** Cancel the outstanding review (session teardown, browser closed, etc.). */
  cancel(reason = "review cancelled"): void {
    if (!this.current) return;
    const { reject } = this.current;
    this.current = undefined;
    reject(new ReviewCancelledError(reason));
  }
}
