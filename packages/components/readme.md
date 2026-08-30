# @workbench/components

Workbench web components, built with [Stencil](https://stenciljs.com) — real
props/state/events architecture, framework-free, distributable as standard
custom elements (with optional framework wrappers later).

The first set is the prototyper's review surface:

| Tag | What it is |
| --- | --- |
| `wb-chat` | The chat experience. Owns the transcript + composer; emits `chatSend` for outgoing turns; streams agent text in via the `appendAgentChunk` / `endAgentTurn` methods. |
| `wb-annotation` | A single annotation over a prototype element (selector + box + note). Emits `annotationCommit` / `annotationRemove`. |

## Contract

Components are **harness-neutral**. They emit DOM events (`Annotation`,
`{ text }`, …); the layer above — a `@workbench/adapters` adapter — folds those
into a `@workbench/envelope` payload and pushes it into the running agent turn.
A component never talks to a harness directly.

State/props architecture (Stencil):

- `@Prop()` — inputs, reflected to attributes; the public surface.
- `@State()` — internal reactive state (drafts, streaming buffers).
- `@Event()` — outputs the host page/adapter listens to.
- `@Method()` — imperative hooks for streaming (agent tokens into `wb-chat`).

## Usage

```html
<wb-chat placeholder="Message the agent…"></wb-chat>
<script type="module">
  import { defineCustomElements } from "@workbench/components/loader";
  defineCustomElements();

  const chat = document.querySelector("wb-chat");
  chat.addEventListener("chatSend", (e) => adapter.sendUser(e.detail.text));
  // stream agent tokens back:
  for await (const chunk of adapter.chatStream()) await chat.appendAgentChunk(chunk);
  await chat.endAgentTurn();
</script>
```

## Develop

```sh
pnpm --filter @workbench/components dev     # build + live-reload dev server
pnpm --filter @workbench/components test    # stencil spec tests
```
