import { Component, Prop, State, Event, EventEmitter, h, Host } from "@stencil/core";
import type { Annotation } from "../../models";

/**
 * wb-annotation — a single annotation attached to an element of the prototype.
 *
 * Presentational + one interaction: the human edits the note and confirms.
 * State it owns is only the in-progress edit; the committed annotation is
 * emitted upward via `annotationCommit`. It never talks to a harness — the
 * shell/adapter collects these into an envelope.
 */
@Component({
  tag: "wb-annotation",
  styleUrl: "wb-annotation.css",
  shadow: true,
})
export class WbAnnotation {
  /** Stable id for this annotation. */
  @Prop() annotationId!: string;
  /** CSS selector of the annotated element. */
  @Prop() selector!: string;
  /** Bounding box "x,y,w,h" of the target at capture time. */
  @Prop() box = "";
  /** The committed note text (empty until the human writes one). */
  @Prop({ mutable: true }) note = "";

  /** In-progress edit buffer; distinct from the committed `note`. */
  @State() draft = "";
  @State() editing = false;

  /** Fired when the human commits (or updates) the note. */
  @Event() annotationCommit!: EventEmitter<Annotation>;
  /** Fired when the human removes this annotation. */
  @Event() annotationRemove!: EventEmitter<{ id: string }>;

  private beginEdit = () => {
    this.draft = this.note;
    this.editing = true;
  };

  private commit = () => {
    this.note = this.draft.trim();
    this.editing = false;
    this.annotationCommit.emit({
      id: this.annotationId,
      selector: this.selector,
      box: this.box,
      note: this.note,
    });
  };

  private remove = () => {
    this.annotationRemove.emit({ id: this.annotationId });
  };

  render() {
    return (
      <Host>
        <div class="card" part="card">
          <code class="selector" title={this.selector}>
            {this.selector}
          </code>
          {this.editing ? (
            <div class="edit">
              <textarea
                class="input"
                value={this.draft}
                onInput={(e) => (this.draft = (e.target as HTMLTextAreaElement).value)}
                placeholder="Annotation…"
              />
              <div class="actions">
                <button class="primary" onClick={this.commit}>
                  Save
                </button>
                <button onClick={() => (this.editing = false)}>Cancel</button>
              </div>
            </div>
          ) : (
            <div class="view">
              <p class="note">{this.note || <span class="empty">No note</span>}</p>
              <div class="actions">
                <button onClick={this.beginEdit}>Edit</button>
                <button class="danger" onClick={this.remove}>
                  Remove
                </button>
              </div>
            </div>
          )}
        </div>
      </Host>
    );
  }
}
