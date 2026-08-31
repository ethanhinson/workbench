import { spawn } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { ok, render } from "@workbench/envelope";
import {
  startPrototyperServer,
  type PrototyperServer,
} from "@workbench/prototyper";
import { McpBridgeAdapter } from "./bridge.js";

/**
 * The prototyper as an MCP server. The agent you're already talking to connects
 * over stdio and gets two tools:
 *
 *   prototype_present(id, html) — on first call, boots the browser viewer and
 *     opens it; serves the prototype (inspector injected). Returns immediately.
 *   prototype_review(prototypeId) — BLOCKS until you finish reviewing in the
 *     browser, then returns your chosen option + annotations + coverage as a
 *     TOON envelope for the agent to act on.
 *
 * The viewer is lazy: nothing opens until the agent actually presents something.
 */

const PORT = Number(process.env.PROTOTYPER_PORT ?? 4319);
const NO_OPEN = process.env.PROTOTYPER_NO_OPEN === "1";

const bridge = new McpBridgeAdapter();
let viewer: PrototyperServer | undefined;
let booting: Promise<PrototyperServer> | undefined;

function openBrowser(url: string): void {
  if (NO_OPEN) return;
  const platform = process.platform;
  const cmd = platform === "darwin" ? "open" : platform === "win32" ? "start" : "xdg-open";
  try {
    spawn(cmd, [url], { stdio: "ignore", detached: true, shell: platform === "win32" }).unref();
  } catch {
    /* best-effort */
  }
}

/** Boot the viewer once, opening the browser. Idempotent. */
async function ensureViewer(): Promise<PrototyperServer> {
  if (viewer) return viewer;
  if (!booting) {
    booting = startPrototyperServer({
      adapter: bridge,
      prompt: "", // no agent turn — the MCP tools drive presentation
      port: PORT,
    }).then((v) => {
      viewer = v;
      openBrowser(v.url);
      // stderr so it never corrupts the stdio MCP channel
      process.stderr.write(`\n[prototyper-mcp] viewer at ${v.url}\n`);
      return v;
    });
  }
  return booting;
}

const server = new McpServer({ name: "workbench-prototyper", version: "0.0.0" });

server.registerTool(
  "prototype_present",
  {
    description:
      "Render an interactive HTML prototype for the human to review in a browser. " +
      "On first use this opens a browser tab. Provide a stable `id` and a complete, " +
      "self-contained HTML document (free-form; may vendor deps via <script src> / " +
      "ESM CDN URLs — no build step). To offer options the human picks, mark each " +
      "choosable element with data-wb-option=\"<optionId>\". Returns immediately; " +
      "call prototype_review next to collect feedback.",
    inputSchema: {
      id: z.string().describe("Stable prototype id, e.g. 'settings-v1'"),
      html: z.string().describe("Full, self-contained HTML document"),
    },
  },
  async (args) => {
    await ensureViewer();
    bridge.present({ id: args.id, html: args.html });
    return {
      content: [
        {
          type: "text",
          text: render(
            ok(
              { presented: args.id, viewer: viewer?.url },
              { help: [`Call prototype_review with prototypeId "${args.id}" to collect feedback`] },
            ),
            "toon",
          ),
        },
      ],
    };
  },
);

server.registerTool(
  "prototype_review",
  {
    description:
      "Block until the human finishes reviewing the presented prototype in the " +
      "browser, then return their feedback (chosen option, annotations with CSS " +
      "selectors, coverage) as a TOON envelope. Call after prototype_present.",
    inputSchema: {
      prototypeId: z.string().describe("The id passed to prototype_present"),
    },
  },
  async () => {
    await ensureViewer();
    const feedback = await bridge.gate.open();
    return {
      content: [{ type: "text", text: render(feedback, "toon") }],
    };
  },
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  process.stderr.write("[prototyper-mcp] MCP server ready on stdio\n");
}

void main();
