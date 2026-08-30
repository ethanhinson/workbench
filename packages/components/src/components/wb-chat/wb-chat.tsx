import {
  Component,
  Prop,
  State,
  Event,
  EventEmitter,
  Method,
  h,
  Host,
} from "@stencil/core";
import type { ChatMessage } from "../../models";

/**
 * wb-chat — the chat experience for the prototyper's side panel.
 *
 * The human talks to the agent here; the adapter streams agent turns back in.
 * The component owns the transcript and the input draft, and emits outgoing
 * user messages via `chatSend`. Agent text arrives through the imperative
 * `appendAgentChunk`/`endAgentTurn` methods so the adapter can stream tokens
 * without re-passing the whole array each frame.
 */
@Component({
  tag: "wb-chat",
  styleUrl: "wb-chat.css",
  shadow: true,
})
export class WbChat {
  /** Seed transcript. After mount, mutate via methods/events. */
  @Prop() messages: ChatMessage[] = [];
  /** Disable the composer (e.g. while the agent is mid-turn). */
  @Prop() busy = false;
  /** Placeholder for the composer. */
  @Prop() placeholder = "Message the agent…";

  @State() private log: ChatMessage[] = [];
  @State() private draft = "";

  /** Fired when the human sends a message. */
  @Event() chatSend!: EventEmitter<{ text: string }>;

  private seq = 0;

  componentWillLoad() {
    this.log = [...this.messages];
  }

  /** Append (or extend) the current streaming agent turn. */
  @Method()
  async appendAgentChunk(chunk: string): Promise<void> {
    const last = this.log[this.log.length - 1];
    if (last && last.role === "agent" && last.pending) {
      this.log = [...this.log.slice(0, -1), { ...last, text: last.text + chunk }];
    } else {
      this.log = [
        ...this.log,
        { id: this.nextId("a"), role: "agent", text: chunk, pending: true },
      ];
    }
  }

  /** Mark the current streaming agent turn complete. */
  @Method()
  async endAgentTurn(): Promise<void> {
    const last = this.log[this.log.length - 1];
    if (last && last.role === "agent" && last.pending) {
      this.log = [...this.log.slice(0, -1), { ...last, pending: false }];
    }
  }

  private nextId(prefix: string): string {
    this.seq += 1;
    return `${prefix}${this.seq}`;
  }

  private send = () => {
    const text = this.draft.trim();
    if (!text || this.busy) return;
    this.log = [...this.log, { id: this.nextId("u"), role: "user", text }];
    this.draft = "";
    this.chatSend.emit({ text });
  };

  private onKey = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      this.send();
    }
  };

  render() {
    return (
      <Host>
        <div class="log" part="log">
          {this.log.map((m) => (
            <div key={m.id} class={{ msg: true, [m.role]: true, pending: !!m.pending }}>
              {m.text}
            </div>
          ))}
        </div>
        <div class="composer" part="composer">
          <textarea
            class="input"
            value={this.draft}
            disabled={this.busy}
            placeholder={this.placeholder}
            onInput={(e) => (this.draft = (e.target as HTMLTextAreaElement).value)}
            onKeyDown={this.onKey}
          />
          <button class="send" disabled={this.busy || !this.draft.trim()} onClick={this.send}>
            Send
          </button>
        </div>
      </Host>
    );
  }
}
