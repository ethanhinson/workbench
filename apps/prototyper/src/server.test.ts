import { describe, it, expect, afterEach } from "vitest";
import { WebSocket } from "ws";
import { startPrototyperServer, type PrototyperServer } from "./server.js";
import { FakeAdapter } from "./fake-adapter.js";

const PROTO = `<!doctype html><html><body><h1 id="t">hi</h1></body></html>`;

let running: PrototyperServer | undefined;
afterEach(async () => {
  await running?.close();
  running = undefined;
});

/** Collect WS messages until a predicate is satisfied or a timeout elapses. */
function waitForWs(url: string, until: (msgs: any[]) => boolean, timeoutMs = 3000) {
  return new Promise<any[]>((resolve, reject) => {
    const ws = new WebSocket(url.replace("http", "ws") + "/ws");
    const msgs: any[] = [];
    const timer = setTimeout(() => {
      ws.close();
      reject(new Error(`timed out; got ${JSON.stringify(msgs)}`));
    }, timeoutMs);
    ws.on("message", (data) => {
      msgs.push(JSON.parse(String(data)));
      if (until(msgs)) {
        clearTimeout(timer);
        ws.close();
        resolve(msgs);
      }
    });
    ws.on("error", reject);
  });
}

describe("prototyper end-to-end (fake adapter)", () => {
  it("presents a prototype and streams chat on start", async () => {
    const adapter = new FakeAdapter(PROTO);
    running = await startPrototyperServer({ adapter, prompt: "settings page", port: 0 });

    // The prototype is retrievable...
    const proto = await fetch(`${running.url}/prototype`);
    expect(proto.status).toBe(200);
    expect(await proto.text()).toContain("<h1 id=\"t\">hi</h1>");

    // ...and a fresh WS subscriber gets the prototype-ready event.
    const msgs = await waitForWs(running.url, (m) => m.some((x) => x.type === "prototype"));
    expect(msgs.some((m) => m.type === "prototype" && m.id === "fake-1")).toBe(true);
  });

  it("routes a browser review into the adapter as an envelope", async () => {
    const adapter = new FakeAdapter(PROTO);
    running = await startPrototyperServer({ adapter, prompt: "x", port: 0 });

    const review = {
      chosen: "option-B",
      coverage: "partial",
      annotations: [
        { id: "n1", selector: "#t", box: "0,0,10,10", note: "too small" },
        { id: "n2", selector: "#t", box: "5,5,10,10", note: "contrast" },
      ],
    };
    const res = await fetch(`${running.url}/review`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(review),
    });
    expect(res.status).toBe(200);

    // The adapter received a well-formed envelope.
    const fb = adapter.lastFeedback!;
    expect(fb).toBeDefined();
    expect(fb.total).toBe(2);
    expect(fb.coverage).toBe("partial");
    expect((fb.data as any).chosen).toBe("option-B");
    expect((fb.data as any).annotations).toHaveLength(2);
    expect((fb.data as any).prototype).toBe("fake-1");
  });

  it("serves the browser shell with the components bundle reference", async () => {
    const adapter = new FakeAdapter(PROTO);
    running = await startPrototyperServer({ adapter, prompt: "x", port: 0 });
    const html = await (await fetch(`${running.url}/`)).text();
    expect(html).toContain("<wb-chat");
    expect(html).toContain("/components/workbench-components.esm.js");
  });

  it("serves the built components bundle", async () => {
    const adapter = new FakeAdapter(PROTO);
    running = await startPrototyperServer({ adapter, prompt: "x", port: 0 });
    const res = await fetch(`${running.url}/components/workbench-components.esm.js`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toContain("javascript");
  });
});
