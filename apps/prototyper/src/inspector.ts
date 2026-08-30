/// <reference lib="dom" />
/**
 * The in-frame inspector: a small script injected into the agent-authored
 * prototype (inside the sandboxed iframe). It does hover-highlight + click-select
 * on REAL elements, computes a stable selector + bounding box, and postMessages
 * the selection up to the parent shell. Option elements (`[data-wb-option]`) post
 * an option-choice instead.
 *
 * The prototype stays free-form: the agent writes whatever HTML/CSS/JS it wants;
 * the inspector is layered on top and never requires cooperation, except the
 * optional `data-wb-option="<id>"` convention for the option-picker.
 *
 * Messages posted to the parent (all tagged `source: "wb-inspector"`):
 *   { type: "select",  selector, box, label }   — element clicked in inspect mode
 *   { type: "option",  option, selector }        — a [data-wb-option] was chosen
 *   { type: "hover",   selector }                — element hovered (optional)
 */

/** Marker so the parent can distinguish inspector messages from prototype noise. */
export const INSPECTOR_SOURCE = "wb-inspector";

/** Inject the inspector script into a prototype document before </body>. */
export function injectInspector(html: string): string {
  const script = `<script>(${inspectorAgent.toString()})(${JSON.stringify(INSPECTOR_SOURCE)});</script>`;
  if (/<\/body>/i.test(html)) {
    return html.replace(/<\/body>/i, `${script}</body>`);
  }
  return html + script;
}

/**
 * The function that actually runs INSIDE the prototype iframe. It is serialized
 * via toString(), so it must be self-contained (no imports, no closure over
 * module scope) — everything it needs is passed as arguments or read off the DOM.
 */
function inspectorAgent(source: string): void {
  const post = (msg: Record<string, unknown>) =>
    parent.postMessage({ source, ...msg }, "*");

  let inspecting = false;
  let hi: HTMLElement | null = null;

  // Listen for the parent toggling inspect mode.
  window.addEventListener("message", (e: MessageEvent) => {
    const d = e.data;
    if (d && d.source === source && d.type === "set-inspect") {
      inspecting = !!d.value;
      if (!inspecting) clearHi();
    }
  });

  const clearHi = () => {
    if (hi) {
      hi.style.outline = hi.dataset["wbPrevOutline"] ?? "";
      delete hi.dataset["wbPrevOutline"];
      hi = null;
    }
  };

  document.addEventListener(
    "mouseover",
    (e) => {
      if (!inspecting) return;
      const el = e.target as HTMLElement;
      if (!el || el === hi) return;
      clearHi();
      hi = el;
      el.dataset["wbPrevOutline"] = el.style.outline;
      el.style.outline = "2px solid #3b5bdb";
      post({ type: "hover", selector: selectorFor(el) });
    },
    true,
  );

  document.addEventListener(
    "click",
    (e) => {
      const el = e.target as HTMLElement;
      // Option pick works regardless of inspect mode.
      const opt = el.closest("[data-wb-option]") as HTMLElement | null;
      if (opt) {
        e.preventDefault();
        e.stopPropagation();
        post({
          type: "option",
          option: opt.getAttribute("data-wb-option"),
          selector: selectorFor(opt),
        });
        return;
      }
      if (!inspecting) return;
      e.preventDefault();
      e.stopPropagation();
      const r = el.getBoundingClientRect();
      post({
        type: "select",
        selector: selectorFor(el),
        box: [Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height)].join(","),
        label: (el.textContent || "").trim().slice(0, 40),
      });
    },
    true,
  );

  // A robust-ish selector: prefer #id, else a nth-of-type path up to <body>.
  function selectorFor(el: HTMLElement): string {
    if (el.id) return "#" + CSS.escape(el.id);
    const parts: string[] = [];
    let node: HTMLElement | null = el;
    while (node && node.nodeType === 1 && node.tagName.toLowerCase() !== "html") {
      let part = node.tagName.toLowerCase();
      const parentEl = node.parentElement;
      if (parentEl) {
        const sibs = Array.from(parentEl.children).filter(
          (c) => c.tagName === node!.tagName,
        );
        if (sibs.length > 1) part += ":nth-of-type(" + (sibs.indexOf(node) + 1) + ")";
      }
      parts.unshift(part);
      if (node.id) {
        parts[0] = "#" + CSS.escape(node.id);
        break;
      }
      node = node.parentElement;
    }
    return parts.join(" > ");
  }
}
