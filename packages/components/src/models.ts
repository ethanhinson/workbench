/**
 * Shared component models. These are the shapes the components emit in their
 * events; the bridge/adapter layer folds them into a @workbench/envelope
 * payload before handing them to a harness.
 */

import type { Flag } from "@workbench/envelope";

/** A single human annotation captured over a prototype element. */
export interface Annotation {
  id: string;
  /** CSS selector of the annotated element. */
  selector: string;
  /** Bounding box "x,y,w,h" at capture time. */
  box: string;
  note: string;
  /** Optional ambiguity/provenance flags, envelope-compatible. */
  flags?: Flag[];
}

/** A chat turn rendered in the chat surface. */
export interface ChatMessage {
  id: string;
  role: "user" | "agent";
  /** Rendered text. For agent turns this may stream in incrementally. */
  text: string;
  /** True while an agent turn is still streaming. */
  pending?: boolean;
}

export type { Flag };
