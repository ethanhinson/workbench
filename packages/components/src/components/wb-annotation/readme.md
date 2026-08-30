# wb-annotation



<!-- Auto Generated Below -->


## Overview

wb-annotation — a single annotation attached to an element of the prototype.

Presentational + one interaction: the human edits the note and confirms.
State it owns is only the in-progress edit; the committed annotation is
emitted upward via `annotationCommit`. It never talks to a harness — the
shell/adapter collects these into an envelope.

## Properties

| Property                    | Attribute       | Description                                                 | Type     | Default     |
| --------------------------- | --------------- | ----------------------------------------------------------- | -------- | ----------- |
| `annotationId` _(required)_ | `annotation-id` | Stable id for this annotation.                              | `string` | `undefined` |
| `box`                       | `box`           | Bounding box "x,y,w,h" of the target at capture time.       | `string` | `""`        |
| `note`                      | `note`          | The committed note text (empty until the human writes one). | `string` | `""`        |
| `selector` _(required)_     | `selector`      | CSS selector of the annotated element.                      | `string` | `undefined` |


## Events

| Event              | Description                                         | Type                           |
| ------------------ | --------------------------------------------------- | ------------------------------ |
| `annotationCommit` | Fired when the human commits (or updates) the note. | `CustomEvent<Annotation>`      |
| `annotationRemove` | Fired when the human removes this annotation.       | `CustomEvent<{ id: string; }>` |


## Shadow Parts

| Part     | Description |
| -------- | ----------- |
| `"card"` |             |


----------------------------------------------

*Built with [StencilJS](https://stenciljs.com/)*
