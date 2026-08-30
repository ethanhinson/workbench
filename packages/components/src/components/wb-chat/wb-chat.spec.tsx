import { newSpecPage } from "@stencil/core/testing";
import { WbChat } from "./wb-chat";

describe("wb-chat", () => {
  it("renders a seeded transcript", async () => {
    const page = await newSpecPage({
      components: [WbChat],
      html: `<wb-chat></wb-chat>`,
    });
    page.rootInstance.messages = [
      { id: "u1", role: "user", text: "hi" },
      { id: "a1", role: "agent", text: "hello" },
    ];
    // re-run lifecycle with the seeded prop
    (page.rootInstance as WbChat).componentWillLoad();
    await page.waitForChanges();
    const msgs = page.root!.shadowRoot!.querySelectorAll(".msg");
    expect(msgs.length).toBe(2);
  });

  it("streams agent chunks into one pending turn", async () => {
    const page = await newSpecPage({
      components: [WbChat],
      html: `<wb-chat></wb-chat>`,
    });
    const el = page.rootInstance as WbChat;
    await el.appendAgentChunk("Hel");
    await el.appendAgentChunk("lo");
    await page.waitForChanges();
    const agent = page.root!.shadowRoot!.querySelector(".msg.agent")!;
    expect(agent.textContent).toBe("Hello");
    expect(agent.classList.contains("pending")).toBe(true);

    await el.endAgentTurn();
    await page.waitForChanges();
    expect(
      page.root!.shadowRoot!.querySelector(".msg.agent")!.classList.contains("pending"),
    ).toBe(false);
  });

  it("emits chatSend on send", async () => {
    const page = await newSpecPage({
      components: [WbChat],
      html: `<wb-chat></wb-chat>`,
    });
    const el = page.rootInstance as WbChat;
    const sent: string[] = [];
    page.root!.addEventListener("chatSend", (e: Event) => {
      sent.push((e as CustomEvent<{ text: string }>).detail.text);
    });
    el["draft"] = "ship it";
    el["send"]();
    await page.waitForChanges();
    expect(sent).toEqual(["ship it"]);
  });
});
