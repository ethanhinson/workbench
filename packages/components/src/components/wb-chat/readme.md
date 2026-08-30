# wb-chat



<!-- Auto Generated Below -->


## Overview

wb-chat — the chat experience for the prototyper's side panel.

The human talks to the agent here; the adapter streams agent turns back in.
The component owns the transcript and the input draft, and emits outgoing
user messages via `chatSend`. Agent text arrives through the imperative
`appendAgentChunk`/`endAgentTurn` methods so the adapter can stream tokens
without re-passing the whole array each frame.

## Properties

| Property      | Attribute     | Description                                              | Type            | Default                |
| ------------- | ------------- | -------------------------------------------------------- | --------------- | ---------------------- |
| `busy`        | `busy`        | Disable the composer (e.g. while the agent is mid-turn). | `boolean`       | `false`                |
| `messages`    | --            | Seed transcript. After mount, mutate via methods/events. | `ChatMessage[]` | `[]`                   |
| `placeholder` | `placeholder` | Placeholder for the composer.                            | `string`        | `"Message the agent…"` |


## Events

| Event      | Description                           | Type                             |
| ---------- | ------------------------------------- | -------------------------------- |
| `chatSend` | Fired when the human sends a message. | `CustomEvent<{ text: string; }>` |


## Methods

### `appendAgentChunk(chunk: string) => Promise<void>`

Append (or extend) the current streaming agent turn.

#### Parameters

| Name    | Type     | Description |
| ------- | -------- | ----------- |
| `chunk` | `string` |             |

#### Returns

Type: `Promise<void>`



### `endAgentTurn() => Promise<void>`

Mark the current streaming agent turn complete.

#### Returns

Type: `Promise<void>`




## Shadow Parts

| Part         | Description |
| ------------ | ----------- |
| `"composer"` |             |
| `"log"`      |             |


----------------------------------------------

*Built with [StencilJS](https://stenciljs.com/)*
