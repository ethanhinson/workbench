// DEFERRED: full DOM/render spec via @stencil/vitest. Not picked up by the
// current vitest config (which includes only *.test.ts) because @stencil/vitest's
// "stencil" environment fails to register under Vitest 3.2 ("not a valid
// environment / transformMode"). Re-enable by switching vitest.config.ts to the
// @stencil/vitest defineVitestConfig once that integration stabilizes. The shipped
// streaming logic is meanwhile covered by chat-log.test.ts.
import { render, h, describe, it, expect } from "@stencil/vitest";

describe("wb-chat", () => {
  it("renders a seeded transcript", async () => {
    const { root, waitForChanges } = await render(
      <wb-chat
        messages={[
          { id: "u1", role: "user", text: "hi" },
          { id: "a1", role: "agent", text: "hello" },
        ]}
      ></wb-chat>,
    );
    await waitForChanges();
    const msgs = root.shadowRoot!.querySelectorAll(".msg");
    expect(msgs.length).toBe(2);
  });

  it("streams agent chunks into one pending turn, then ends it", async () => {
    const { root, waitForChanges } = await render(<wb-chat></wb-chat>);
    const el = root as HTMLElement & {
      appendAgentChunk: (c: string) => Promise<void>;
      endAgentTurn: () => Promise<void>;
    };

    await el.appendAgentChunk("Hel");
    await el.appendAgentChunk("lo");
    await waitForChanges();
    const agent = root.shadowRoot!.querySelector(".msg.agent")!;
    expect(agent.textContent).toBe("Hello");
    expect(agent.classList.contains("pending")).toBe(true);

    await el.endAgentTurn();
    await waitForChanges();
    expect(
      root.shadowRoot!.querySelector(".msg.agent")!.classList.contains("pending"),
    ).toBe(false);
  });

  it("emits chatSend when the human sends", async () => {
    const { root, waitForChanges } = await render(<wb-chat></wb-chat>);
    const sent: string[] = [];
    root.addEventListener("chatSend", (e: Event) => {
      sent.push((e as CustomEvent<{ text: string }>).detail.text);
    });

    const textarea = root.shadowRoot!.querySelector(".input") as HTMLTextAreaElement;
    textarea.value = "ship it";
    textarea.dispatchEvent(new Event("input"));
    await waitForChanges();
    (root.shadowRoot!.querySelector(".send") as HTMLButtonElement).click();
    await waitForChanges();

    expect(sent).toEqual(["ship it"]);
  });
});
