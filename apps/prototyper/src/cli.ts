import type { HarnessAdapter } from "@workbench/adapters";
import { startPrototyperServer } from "./server.js";
import { FakeAdapter } from "./fake-adapter.js";
import { createRealAdapter } from "./claude-mode.js";

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

async function resolveAdapter(args: Args): Promise<HarnessAdapter> {
  if (args.fake) return new FakeAdapter(CANNED_PROTOTYPE);
  return createRealAdapter();
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
