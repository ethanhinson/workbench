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
  open: boolean;
  prompt: string;
}

function parseArgs(argv: string[]): Args {
  let fake = false;
  let port = 4319;
  let open = true;
  const rest: string[] = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === undefined) continue;
    if (a === "--fake") fake = true;
    else if (a === "--no-open") open = false;
    else if (a === "--port") port = Number(argv[++i] ?? port);
    else rest.push(a);
  }
  return { fake, port, open, prompt: rest.join(" ") || "Propose a layout for the settings page." };
}

async function resolveAdapter(args: Args): Promise<HarnessAdapter> {
  if (args.fake) return new FakeAdapter(CANNED_PROTOTYPE);
  return createRealAdapter();
}

function openBrowser(url: string): void {
  const platform = process.platform;
  const cmd = platform === "darwin" ? "open" : platform === "win32" ? "start" : "xdg-open";
  import("node:child_process").then(({ spawn }) => {
    try {
      spawn(cmd, [url], { stdio: "ignore", detached: true, shell: platform === "win32" }).unref();
    } catch {
      /* opening is best-effort */
    }
  });
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  const adapter = await resolveAdapter(args);

  // Terminal-side visibility so the process never looks "hung": narrate the
  // turn lifecycle and prototype events. (The browser is the real surface.)
  adapter.on({
    onTurnStart: () => process.stderr.write("\n● agent working…\n"),
    onChat: (c) => process.stderr.write(c),
    onPrototype: (p) =>
      process.stderr.write(`\n📐 prototype ready: ${p.id} — reload the browser tab to see it\n`),
    onTurnEnd: () => process.stderr.write("\n○ ready — your move (chat or submit a review in the browser)\n"),
  });

  const { url, close } = await startPrototyperServer({
    adapter,
    prompt: args.prompt,
    port: args.port,
  });

  process.stdout.write(`\nprototyper (${adapter.id}) → ${url}\n`);
  if (args.open) {
    process.stdout.write("opening browser…\n");
    openBrowser(url);
  } else {
    process.stdout.write("open that URL in your browser.\n");
  }
  process.stdout.write("Ctrl-C to stop.\n");

  const shutdown = () => {
    void close().then(() => process.exit(0));
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

void main();
