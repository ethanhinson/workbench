import type { HarnessAdapter } from "@workbench/adapters";
import { startPrototyperServer } from "./server.js";
import { FakeAdapter } from "./fake-adapter.js";

const CANNED_PROTOTYPE = `<!doctype html><html><body style="font-family:system-ui;padding:2rem">
<h1 id="title">Settings</h1>
<button id="save" style="padding:.5rem 1rem">Save</button>
<button id="cancel" style="padding:.5rem 1rem">Cancel</button>
<p>A canned prototype served by the fake adapter for the end-to-end loop.</p>
</body></html>`;

interface Args {
  fake: boolean;
  port: number;
  prompt: string;
}

function parseArgs(argv: string[]): Args {
  let fake = false;
  let port = 4319;
  const rest: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === undefined) continue;
    if (a === "--fake") fake = true;
    else if (a === "--port") port = Number(argv[++i] ?? port);
    else rest.push(a);
  }
  return { fake, port, prompt: rest.join(" ") || "Propose a layout for the settings page." };
}

const PROTOTYPER_SYSTEM_PROMPT = [
  "You are driving a browser prototype-review loop. To propose UI, workflows, or",
  "architectural options, do NOT describe them in prose — RENDER them.",
  "",
  "Workflow for every proposal:",
  "1. Call present_prototype(id, html) with a complete, self-contained HTML",
  "   document. It may be interactive (inline <script>/<style>) and may vendor",
  "   dependencies via <script src> / ESM CDN URLs. No build step is available.",
  "   When offering multiple options, mark each choosable element with",
  "   data-wb-option=\"<optionId>\" so the human can click to choose one.",
  "2. Immediately call request_review(prototypeId). It BLOCKS until the human",
  "   finishes reviewing; it returns their chosen option, annotations (with CSS",
  "   selectors), and coverage as structured data.",
  "3. Act on that feedback — iterate by presenting a new prototype, or summarize.",
  "",
  "Keep chat concise; the prototype is the artifact, not the text.",
].join("\n");

async function resolveAdapter(args: Args): Promise<HarnessAdapter> {
  if (args.fake) return new FakeAdapter(CANNED_PROTOTYPE);
  // Lazy import so --fake never pulls the Claude SDK.
  const { createClaudeAdapter } = await import("@workbench/adapters/claude");
  return createClaudeAdapter({ systemPrompt: PROTOTYPER_SYSTEM_PROMPT });
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  const adapter = await resolveAdapter(args);
  const { url, close } = await startPrototyperServer({
    adapter,
    prompt: args.prompt,
    port: args.port,
  });
  // eslint-disable-next-line no-console
  console.log(`prototyper (${adapter.id}) listening at ${url}`);

  const shutdown = () => {
    void close().then(() => process.exit(0));
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

void main();
