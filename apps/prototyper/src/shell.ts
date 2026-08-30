/**
 * The browser shell: 80% prototype pane (sandboxed iframe) + 20% chat pane.
 * The prototype is served from /prototype and framed in a sandboxed iframe so
 * agent-authored JS cannot touch the parent. The parent hosts the review chrome
 * (click-to-annotate) and the chat, and posts feedback back to the server.
 *
 * Kept as a single self-contained document (no bundler) to match the "no build
 * step for the review surface" stance. Components are loaded from the built
 * @workbench/components bundle mounted at /components/.
 */
export function shellHtml(): string {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Workbench Prototyper</title>
<style>
  :root { --wb-accent: #3b5bdb; }
  * { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; font-family: system-ui, sans-serif; }
  #app { display: flex; height: 100vh; }
  #proto { flex: 1 1 80%; position: relative; border-right: 1px solid #d0d0d7; min-width: 0; }
  #proto iframe { width: 100%; height: 100%; border: 0; }
  #proto.empty::after {
    content: "Waiting for a prototype…"; position: absolute; inset: 0;
    display: grid; place-items: center; color: #9a9aa4;
  }
  #side { flex: 0 0 20%; min-width: 300px; display: flex; flex-direction: column; }
  #ann { flex: 0 0 auto; max-height: 40%; overflow-y: auto; padding: 0.5rem; border-bottom: 1px solid #d0d0d7; }
  #ann h3 { font-size: 0.75rem; text-transform: uppercase; color: #6a6a76; margin: 0.25rem 0.25rem 0.5rem; }
  wb-annotation { margin-bottom: 0.4rem; }
  #chat { flex: 1 1 auto; min-height: 0; }
  #bar { flex: 0 0 auto; display: flex; gap: 0.4rem; padding: 0.5rem; border-top: 1px solid #d0d0d7; }
  #bar button { flex: 1; font: inherit; padding: 0.4rem; border-radius: 6px; border: 1px solid var(--wb-accent); background: var(--wb-accent); color: #fff; cursor: pointer; }
  #inspect { display:flex; align-items:center; gap:.3rem; font-size:.75rem; color:#6a6a76; padding:0 .25rem; }
</style>
<script type="module" src="/components/workbench-components.esm.js"></script>
</head>
<body>
<div id="app">
  <div id="proto" class="empty"></div>
  <div id="side">
    <div id="ann">
      <h3>Annotations</h3>
      <label id="inspect"><input type="checkbox" id="inspectToggle" /> click prototype to annotate</label>
      <div id="annList"></div>
    </div>
    <wb-chat id="chat" placeholder="Message the agent…"></wb-chat>
    <div id="bar"><button id="submit">Submit review</button></div>
  </div>
</div>
<script type="module">
  const protoEl = document.getElementById("proto");
  const annList = document.getElementById("annList");
  const chat = document.getElementById("chat");
  const inspectToggle = document.getElementById("inspectToggle");
  const annotations = [];
  let seq = 0;

  // --- chat stream over WS ---
  const ws = new WebSocket(\`ws://\${location.host}/ws\`);
  ws.onmessage = async (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.type === "chat") await chat.appendAgentChunk(msg.chunk);
    else if (msg.type === "turn-end") await chat.endAgentTurn();
    else if (msg.type === "prototype") loadPrototype();
  };
  chat.addEventListener("chatSend", (e) => {
    fetch("/chat", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: e.detail.text }) });
  });

  function loadPrototype() {
    protoEl.classList.remove("empty");
    protoEl.innerHTML = "";
    const iframe = document.createElement("iframe");
    iframe.setAttribute("sandbox", "allow-scripts allow-forms");
    iframe.src = "/prototype?" + Date.now();
    protoEl.appendChild(iframe);
    wireInspector(iframe);
  }

  // --- click-to-annotate (parent-side; the iframe is sandboxed/cross-doc so we
  //     annotate at the iframe level for v1: a real inspector needs an injected
  //     agent inside the frame via postMessage — deferred) ---
  function wireInspector() {
    protoEl.onclick = (e) => {
      if (!inspectToggle.checked) return;
      const note = prompt("Annotation:");
      if (note == null) return;
      const id = "n" + (++seq);
      const selector = "iframe/prototype";
      const box = [Math.round(e.offsetX), Math.round(e.offsetY), 0, 0].join(",");
      annotations.push({ id, selector, box, note });
      const el = document.createElement("wb-annotation");
      el.setAttribute("annotation-id", id);
      el.setAttribute("selector", selector);
      el.setAttribute("box", box);
      el.setAttribute("note", note);
      el.addEventListener("annotationRemove", () => {
        const i = annotations.findIndex((a) => a.id === id);
        if (i >= 0) annotations.splice(i, 1);
        el.remove();
      });
      annList.appendChild(el);
    };
  }

  document.getElementById("submit").onclick = async () => {
    await fetch("/review", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ annotations, coverage: "complete" }),
    });
  };
</script>
</body>
</html>`;
}
