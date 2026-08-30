/**
 * Rendering happens ONLY at the output boundary. Internal logic stays on plain
 * objects; you convert to TOON (for agents) or JSON (for machines/tests) at the
 * edge, once.
 */

import { encode } from "@toon-format/toon";
import type { Envelope, ErrorEnvelope } from "./envelope.js";

export type Format = "toon" | "json";

/**
 * Render an envelope to the chosen wire format.
 *
 *   - "toon" — token-efficient, row-shaped; what agents consume on stdout.
 *   - "json" — canonical; what tests, machines, and the bridge wire use.
 *
 * The two are information-equivalent: same fields, same values, different
 * encoding. Nothing an agent can observe differs except token cost.
 */
export function render(
  envelope: Envelope | ErrorEnvelope,
  format: Format = "toon",
): string {
  if (format === "json") {
    return JSON.stringify(envelope, null, 2);
  }
  return encode(envelope as unknown as Record<string, unknown>);
}
