/**
 * review-chrome — the fixed UI layer injected OVER an agent-authored prototype.
 *
 * Three components, none authored by the agent:
 *   - inspector:     hover-highlight + click-select; captures selector + box + role/label
 *   - annotation:    attach a text note to a selected element
 *   - option-picker: mark which inline architectural option the human chose
 *
 * The chrome only ever talks to the local bridge (never the harness directly),
 * which is what keeps it harness-agnostic. Feedback it emits conforms to the
 * @workbench/envelope contract.
 *
 * TODO(v2): implement. This is a scaffold placeholder — see /design.md §3.3.
 */

import type { Flag } from "@workbench/envelope";

/** A single human annotation captured from the review surface. */
export interface Annotation {
  id: string;
  /** CSS selector of the annotated element. */
  selector: string;
  /** Bounding box "x,y,w,h" at capture time. */
  box: string;
  note: string;
  flags?: Flag[];
}

/** The structured feedback the chrome POSTs back to the bridge. */
export interface ReviewFeedback {
  prototype: string;
  /** Chosen option id, when the prototype presented an option set. */
  chosen?: string;
  annotations: Annotation[];
}

export const REVIEW_CHROME_VERSION = "0.0.0";
