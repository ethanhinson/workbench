import { createServer, type Server, type IncomingMessage, type ServerResponse } from "node:http";
import { createRequire } from "node:module";
import { readFile } from "node:fs/promises";
import { dirname, join, normalize } from "node:path";
import { WebSocketServer, WebSocket } from "ws";
import { ok } from "@workbench/envelope";
import type { HarnessAdapter, PrototypePresentation } from "@workbench/adapters";
import { shellHtml } from "./shell.js";
import { injectInspector } from "./inspector.js";

/** Resolve the built @workbench/components bundle dir (dist/workbench-components). */
function componentsDir(): string {
  const require = createRequire(import.meta.url);
  // resolve the package.json to find its root, then the built esm bundle dir
  const pkgJson = require.resolve("@workbench/components/package.json");
  return join(dirname(pkgJson), "dist", "workbench-components");
}

export interface PrototyperServer {
  server: Server;
  url: string;
  close: () => Promise<void>;
}

/**
 * The runtime that closes the loop. Given any HarnessAdapter it:
 *   - serves the browser shell at `/`
 *   - serves the current agent-presented prototype at `/prototype`
 *   - accepts the human's review at POST `/review` (→ adapter.submitFeedback)
 *   - accepts chat at POST `/chat` (→ adapter.sendChat)
 *   - streams adapter chat + "prototype ready" events over a WebSocket
 *
 * Harness-agnostic: the adapter is the only harness-specific piece.
 */
export async function startPrototyperServer(opts: {
  adapter: HarnessAdapter;
  prompt: string;
  port?: number;
  host?: string;
}): Promise<PrototyperServer> {
  const host = opts.host ?? "127.0.0.1";
  let currentPrototype: PrototypePresentation | undefined;
  const sockets = new Set<WebSocket>();

  const broadcast = (msg: unknown) => {
    const data = JSON.stringify(msg);
    for (const ws of sockets) {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    }
  };

  opts.adapter.on({
    onTurnStart: () => broadcast({ type: "turn-start" }),
    onChat: (chunk) => broadcast({ type: "chat", chunk }),
    onPrototype: (proto) => {
      currentPrototype = proto;
      broadcast({ type: "prototype", id: proto.id });
    },
    onTurnEnd: () => broadcast({ type: "turn-end" }),
  });

  const server = createServer((req, res) => {
    void handle(req, res).catch((err) => {
      sendJson(res, 500, { error: String(err) });
    });
  });

  async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const url = new URL(req.url ?? "/", `http://${req.headers.host}`);

    if (req.method === "GET" && url.pathname === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      res.end(shellHtml());
      return;
    }

    if (req.method === "GET" && url.pathname === "/prototype") {
      if (!currentPrototype) {
        res.writeHead(204);
        res.end();
        return;
      }
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      res.end(injectInspector(currentPrototype.html));
      return;
    }

    if (req.method === "POST" && url.pathname === "/review") {
      const body = await readJson(req);
      // Fold the browser's raw review into an envelope: coverage/notes travel
      // through, annotations are the payload.
      const feedback = ok(
        {
          prototype: currentPrototype?.id ?? null,
          chosen: body.chosen ?? null,
          annotations: body.annotations ?? [],
        },
        {
          total: Array.isArray(body.annotations) ? body.annotations.length : 0,
          coverage: body.coverage ?? "unknown",
        },
      );
      try {
        await opts.adapter.submitFeedback(feedback);
        sendJson(res, 200, { ok: true });
      } catch (err) {
        sendJson(res, 409, { ok: false, error: String(err) });
      }
      return;
    }

    if (req.method === "POST" && url.pathname === "/chat") {
      const body = await readJson(req);
      await opts.adapter.sendChat(String(body.text ?? ""));
      sendJson(res, 200, { ok: true });
      return;
    }

    if (req.method === "GET" && url.pathname.startsWith("/components/")) {
      const rel = normalize(url.pathname.slice("/components/".length)).replace(/^(\.\.[/\\])+/, "");
      const dir = componentsDir();
      const file = join(dir, rel);
      if (!file.startsWith(dir)) {
        res.writeHead(403);
        res.end("forbidden");
        return;
      }
      try {
        const buf = await readFile(file);
        res.writeHead(200, { "content-type": contentType(file) });
        res.end(buf);
      } catch {
        res.writeHead(404);
        res.end("not found");
      }
      return;
    }

    res.writeHead(404);
    res.end("not found");
  }

  const wss = new WebSocketServer({ server, path: "/ws" });
  wss.on("connection", (ws) => {
    sockets.add(ws);
    if (currentPrototype) ws.send(JSON.stringify({ type: "prototype", id: currentPrototype.id }));
    ws.on("close", () => sockets.delete(ws));
  });

  await new Promise<void>((resolve) => server.listen(opts.port ?? 0, host, resolve));
  const addr = server.address();
  const port = typeof addr === "object" && addr ? addr.port : opts.port;
  const url = `http://${host}:${port}`;

  // Kick off the first turn WITHOUT awaiting it: the turn will call
  // request_review, which blocks until the browser POSTs feedback to this
  // server — so the server must already be accepting requests. Awaiting start()
  // here would deadlock (turn waits for a review the un-returned server can't
  // receive). Turn failures are surfaced over the chat channel.
  void opts.adapter.start(opts.prompt).catch((err) => {
    broadcast({ type: "chat", chunk: `\n[turn error: ${String(err)}]\n` });
    broadcast({ type: "turn-end" });
  });

  return {
    server,
    url,
    close: async () => {
      await opts.adapter.stop();
      for (const ws of sockets) ws.close();
      wss.close();
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}

function sendJson(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

function contentType(file: string): string {
  if (file.endsWith(".js") || file.endsWith(".mjs")) return "text/javascript; charset=utf-8";
  if (file.endsWith(".css")) return "text/css; charset=utf-8";
  if (file.endsWith(".json")) return "application/json";
  if (file.endsWith(".map")) return "application/json";
  return "application/octet-stream";
}

async function readJson(req: IncomingMessage): Promise<Record<string, any>> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(chunk as Buffer);
  const raw = Buffer.concat(chunks).toString("utf-8");
  if (!raw) return {};
  try {
    return JSON.parse(raw) as Record<string, any>;
  } catch {
    return {};
  }
}
