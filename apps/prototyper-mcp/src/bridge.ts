import type { Envelope } from "@workbench/envelope";
import type {
  AdapterHandlers,
  HarnessAdapter,
  PrototypePresentation,
} from "@workbench/adapters";

/**
 * The seam between the blocking prototype_review MCP tool and the browser.
 * When the tool is called it awaits open(); when the browser POSTs review the
 * viewer calls submit(), unblocking the tool. Buffers an early submission (the
 * present -> review gap) so nothing is lost.
 */
export class ReviewGate {
  private resolveCurrent: ((r: Envelope) => void) | undefined;
  private buffered: Envelope | undefined;

  open(): Promise<Envelope> {
    if (this.buffered !== undefined) {
      const r = this.buffered;
      this.buffered = undefined;
      return Promise.resolve(r);
    }
    return new Promise<Envelope>((resolve) => {
      this.resolveCurrent = resolve;
    });
  }

  submit(result: Envelope): void {
    if (this.resolveCurrent) {
      const resolve = this.resolveCurrent;
      this.resolveCurrent = undefined;
      resolve(result);
    } else {
      this.buffered = result;
    }
  }
}

/**
 * A HarnessAdapter that is driven by MCP tools rather than an embedded agent.
 * The viewer (startPrototyperServer) speaks to any HarnessAdapter, so this lets
 * the MCP server reuse the exact browser viewer, inspector, and review plumbing.
 *
 *   - start()          — no-op; the MCP tools drive presentation, not a turn.
 *   - present(proto)   — pushes a prototype to the browser (fires onPrototype).
 *   - submitFeedback() — resolves the ReviewGate the prototype_review tool awaits.
 */
export class McpBridgeAdapter implements HarnessAdapter {
  readonly id = "mcp-bridge";
  readonly gate = new ReviewGate();
  private handlers: AdapterHandlers = {};

  on(handlers: AdapterHandlers): void {
    this.handlers = { ...this.handlers, ...handlers };
  }

  async start(): Promise<void> {
    /* driven by MCP tools, not an agent turn */
  }

  /** Push a prototype to the browser viewer. */
  present(proto: PrototypePresentation): void {
    this.handlers.onTurnStart?.();
    this.handlers.onPrototype?.(proto);
    this.handlers.onTurnEnd?.();
  }

  async sendChat(): Promise<void> {
    /* the MCP client is the chat; nothing to inject here */
  }

  async submitFeedback(feedback: Envelope): Promise<void> {
    this.gate.submit(feedback);
  }

  async stop(): Promise<void> {
    /* viewer lifecycle handled by the server */
  }
}
