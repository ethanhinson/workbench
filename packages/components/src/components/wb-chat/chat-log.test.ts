import { describe, it, expect } from "vitest";
import { foldAgentChunk, endAgentTurn, appendUser } from "./chat-log.js";
import type { ChatMessage } from "../../models.js";

const ids = () => {
  let n = 0;
  return () => `x${(n += 1)}`;
};

describe("chat-log reducers", () => {
  it("folds consecutive agent chunks into one pending turn", () => {
    const next = ids();
    let log: ChatMessage[] = [];
    log = foldAgentChunk(log, "Hel", next);
    log = foldAgentChunk(log, "lo", next);
    expect(log).toHaveLength(1);
    expect(log[0]).toMatchObject({ role: "agent", text: "Hello", pending: true });
  });

  it("starts a new agent turn after a user turn interrupts", () => {
    const next = ids();
    let log: ChatMessage[] = [];
    log = foldAgentChunk(log, "one", next);
    log = endAgentTurn(log);
    log = appendUser(log, "reply", next);
    log = foldAgentChunk(log, "two", next);
    expect(log.map((m) => m.role)).toEqual(["agent", "user", "agent"]);
    expect(log[2]).toMatchObject({ text: "two", pending: true });
  });

  it("endAgentTurn clears the pending flag", () => {
    const next = ids();
    let log = foldAgentChunk([], "hi", next);
    log = endAgentTurn(log);
    expect(log[0]!.pending).toBe(false);
  });

  it("endAgentTurn is a no-op when nothing is pending", () => {
    const next = ids();
    const log = appendUser([], "hi", next);
    expect(endAgentTurn(log)).toEqual(log);
  });
});
