import { describe, it, expect, afterEach } from "vitest";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

/**
 * Drives the built MCP server over stdio with a real MCP client — no browser,
 * fully deterministic, CI-safe. Proves the product shape: an agent connects,
 * gets prototype_present / prototype_review, and the browser review flows back
 * through the tool as a TOON envelope.
 */
let client: Client | undefined;
let port = 4360;

afterEach(async () => {
  await client?.close();
  client = undefined;
});

async function connect(): Promise<{ client: Client; port: number }> {
  const p = port++;
  const transport = new StdioClientTransport({
    command: "node",
    args: ["dist/index.js"],
    env: { ...process.env, PROTOTYPER_NO_OPEN: "1", PROTOTYPER_PORT: String(p) },
  });
  const c = new Client({ name: "test", version: "1.0.0" });
  await c.connect(transport);
  client = c;
  return { client: c, port: p };
}

describe("prototyper MCP server", () => {
  it("exposes prototype_present and prototype_review", async () => {
    const { client } = await connect();
    const tools = (await client.listTools()).tools.map((t) => t.name);
    expect(tools).toContain("prototype_present");
    expect(tools).toContain("prototype_review");
  });

  it("present boots the viewer and serves the prototype with the inspector", async () => {
    const { client, port } = await connect();
    await client.callTool({
      name: "prototype_present",
      arguments: { id: "t1", html: "<!doctype html><body><h1 id=t>hi</h1></body>" },
    });
    const res = await fetch(`http://127.0.0.1:${port}/prototype`);
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain("<h1 id=t>hi</h1>");
    expect(html).toContain("wb-inspector");
  });

  it("prototype_review blocks until the browser submits, then returns the envelope", async () => {
    const { client, port } = await connect();
    await client.callTool({
      name: "prototype_present",
      arguments: { id: "p1", html: "<body><h1 id=t>hi</h1></body>" },
    });

    const reviewP = client.callTool({
      name: "prototype_review",
      arguments: { prototypeId: "p1" },
    });
    // The browser submits a review.
    await new Promise((r) => setTimeout(r, 500));
    const post = await fetch(`http://127.0.0.1:${port}/review`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        chosen: "dir-2",
        coverage: "complete",
        annotations: [{ id: "n1", selector: "#t", box: "0,0,10,10", note: "bigger" }],
      }),
    });
    expect(post.status).toBe(200);

    const review = await reviewP;
    const text = (review.content as Array<{ type: string; text?: string }>)[0]?.text ?? "";
    expect(text).toContain("chosen: dir-2");
    expect(text).toContain("coverage: complete");
    expect(text).toContain("#t");
  });
});
