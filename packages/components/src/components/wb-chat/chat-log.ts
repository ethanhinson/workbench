import type { ChatMessage } from "../../models";

/**
 * Pure transcript reducers for wb-chat. Extracted so the streaming logic — the
 * one non-trivial bit — is testable without a DOM environment.
 */

/**
 * Fold a streamed agent chunk into the log: extend the current pending agent
 * turn if there is one, otherwise start a new pending agent turn.
 */
export function foldAgentChunk(
  log: ChatMessage[],
  chunk: string,
  nextId: () => string,
): ChatMessage[] {
  const last = log[log.length - 1];
  if (last && last.role === "agent" && last.pending) {
    return [...log.slice(0, -1), { ...last, text: last.text + chunk }];
  }
  return [...log, { id: nextId(), role: "agent", text: chunk, pending: true }];
}

/** Mark the trailing pending agent turn complete (no-op if none pending). */
export function endAgentTurn(log: ChatMessage[]): ChatMessage[] {
  const last = log[log.length - 1];
  if (last && last.role === "agent" && last.pending) {
    return [...log.slice(0, -1), { ...last, pending: false }];
  }
  return log;
}

/** Append a committed user turn. */
export function appendUser(
  log: ChatMessage[],
  text: string,
  nextId: () => string,
): ChatMessage[] {
  return [...log, { id: nextId(), role: "user", text }];
}
