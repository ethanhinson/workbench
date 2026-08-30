/**
 * The response envelope: the shared honesty contract every agent-facing tool
 * speaks. Its job is not formatting — it is telling the agent what it can and
 * cannot trust about the payload it just received.
 *
 * Design stance (epistemics-first, not presentation-first):
 *   - `total` is an AGGREGATE, never a page size. An agent that sees a 30-row
 *     list with `total: 847` knows to paginate; without it, it re-runs to check.
 *   - `coverage` states whether the answer is complete. A partial answer that
 *     SAYS it is partial beats one that looks authoritative and is wrong.
 *   - `stale` marks a snapshot the caller should distrust.
 *   - `notes` carry ambiguity/provenance flags — the absence of certainty is a
 *     first-class value, never silently omitted.
 *
 * Absence is meaningful: an omitted field means "not claimed", not "false".
 * Only populate a field when the tool can back it.
 */

/** Whether the payload represents everything, or only part of it. */
export type Coverage = "complete" | "partial" | "unknown";

/**
 * A provenance / ambiguity annotation attached to a result. Kept open-ended by
 * design — tools invent `kind`s their domain needs (e.g. "ambiguous",
 * "dep", "inferred") and agents route on the string.
 */
export interface Flag {
  /** Machine-stable discriminator, e.g. "ambiguous" | "dep" | "inferred". */
  kind: string;
  /** What the flag applies to — a selector, symbol, id, path:line, etc. */
  ref?: string;
  /** Human/agent-readable detail. */
  detail?: string;
}

/** A successful result and everything the agent needs to trust it. */
export interface Envelope<T = unknown> {
  /** The payload: rows, an object, or a scalar. */
  data: T;
  /** Aggregate count across the whole result set — NOT the page size. */
  total?: number;
  /** Did we see everything? `partial` tells the agent to probe further. */
  coverage?: Coverage;
  /** Is this snapshot still valid, or should the agent re-query? */
  stale?: boolean;
  /** Ambiguity / provenance flags. Never omit a known uncertainty. */
  notes?: Flag[];
  /** Contextual next-step suggestions (complete commands or templates). */
  help?: string[];
}

/**
 * An error, in the same structured shape as a success so an agent can read and
 * act on it. `code` is machine-stable; `error` is prose for humans/logs.
 */
export interface ErrorEnvelope {
  error: string;
  /** Stable code the agent can branch on, e.g. "VALIDATION_ERROR". */
  code: string;
  help?: string[];
}

/** Construct a success envelope. Only set fields the tool can back. */
export function ok<T>(
  data: T,
  extra?: Omit<Envelope<T>, "data">,
): Envelope<T> {
  return { data, ...extra };
}

/** Construct an error envelope. */
export function err(
  code: string,
  message: string,
  help?: string[],
): ErrorEnvelope {
  return help && help.length > 0
    ? { error: message, code, help }
    : { error: message, code };
}

/** Narrow an unknown envelope to the error case. */
export function isError(
  value: Envelope | ErrorEnvelope,
): value is ErrorEnvelope {
  return typeof (value as ErrorEnvelope).error === "string";
}
