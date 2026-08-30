# @workbench/envelope

The TOON response envelope — the shared **honesty contract** every agent-facing
tool in this monorepo renders. Not a formatter; a way to tell an agent what it
can and cannot trust about the payload it just received.

```ts
import { ok, err, render } from "@workbench/envelope";

const e = ok(annotations, {
  total: 3,                 // aggregate across the whole set, NOT the page size
  coverage: "partial",      // the human reviewed 2 of 3 options
  notes: [{ kind: "ambiguous", ref: "#hero h1" }],
  help: ["Run with the chosen option to generate the next mock"],
});

process.stdout.write(render(e, "toon")); // agents
// render(e, "json") for tests / machines / the bridge wire
```

## The contract

| Field | Meaning | Why it exists |
| --- | --- | --- |
| `data` | the payload (rows, object, scalar) | — |
| `total` | aggregate count across the whole set | so the agent paginates instead of re-running to check |
| `coverage` | `complete \| partial \| unknown` | a partial answer that *says* so beats one that looks authoritative and is wrong |
| `stale` | snapshot no longer trustworthy | freshness is a first-class agent signal |
| `notes` | ambiguity / provenance `Flag[]` | the absence of certainty is never silently omitted |
| `help` | next-step suggestions | contextual disclosure |

**Absence is meaningful.** An omitted field means "not claimed", not "false".
Only populate a field the tool can actually back.

Rendering happens **only at the output boundary** — keep internal logic on plain
objects and convert to TOON/JSON once, at the edge.
