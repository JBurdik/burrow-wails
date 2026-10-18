// Mermaid → sanitized SVG string, themed from the active Burrow theme.
// Loaded on first use (mermaid is big), shared by chat diagram cards and
// DiagramModal. Results are cached per theme + source, so a card that is
// re-inserted by v-html (every streamed token) swaps back in synchronously.

import DOMPurify from "dompurify";

type Mermaid = typeof import("mermaid")["default"];
let loading: Promise<Mermaid> | null = null;
const cache = new Map<string, string>();
let seq = 0;

// Mermaid derives shades with khroma, which needs real colors, not var(): let
// the browser resolve each theme var to rgb() through a probe element.
function themeColors() {
  const probe = document.createElement("span");
  document.body.appendChild(probe);
  const read = (v: string, fallback: string) => {
    probe.style.color = fallback;
    probe.style.color = `var(${v}, ${fallback})`;
    return getComputedStyle(probe).color;
  };
  const c = {
    bg: read("--bg-panel", "#18181c"),
    text: read("--text-primary", "#e5e5e5"),
    muted: read("--text-secondary", "#a3a3a3"),
    border: read("--border", "#333"),
    accent: read("--accent", "#ec4899"),
    base: read("--bg-base", "#0f0f11"),
  };
  probe.remove();
  return c;
}

function isDark(rgb: string): boolean {
  const [r, g, b] = (rgb.match(/[\d.]+/g) ?? []).map(Number);
  return 0.299 * r + 0.587 * g + 0.114 * b < 128;
}

export function themeKey(): string {
  return JSON.stringify(themeColors());
}

export function cachedMermaid(src: string): string | undefined {
  return cache.get(themeKey() + src);
}

/** Resolves to a sanitized SVG string; rejects on invalid source. */
export async function renderMermaid(src: string): Promise<string> {
  const c = themeColors();
  const key = JSON.stringify(c) + src;
  const hit = cache.get(key);
  if (hit) return hit;
  loading ??= import("mermaid").then((m) => m.default);
  const mermaid = await loading;
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: "base",
    // Plain SVG <text> labels: HTML labels live in <foreignObject>, which the
    // SVG sanitizer below would strip.
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    fontFamily: getComputedStyle(document.body).fontFamily,
    themeVariables: {
      darkMode: isDark(c.bg),
      background: c.bg,
      primaryColor: c.base,
      primaryTextColor: c.text,
      primaryBorderColor: c.accent,
      secondaryColor: c.bg,
      tertiaryColor: c.bg,
      lineColor: c.muted,
      textColor: c.text,
      mainBkg: c.base,
      nodeBorder: c.accent,
      clusterBkg: c.bg,
      clusterBorder: c.border,
      edgeLabelBackground: c.bg,
      noteBkgColor: c.bg,
      noteTextColor: c.text,
      noteBorderColor: c.border,
      actorBkg: c.base,
      actorBorder: c.accent,
      actorTextColor: c.text,
      signalColor: c.muted,
      signalTextColor: c.text,
      labelTextColor: c.text,
      fontSize: "13px",
    },
  });
  const id = `burrow-mermaid-${++seq}`;
  try {
    const { svg } = await mermaid.render(id, src);
    const clean = DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true } });
    cache.set(key, clean);
    return clean;
  } finally {
    // A failed render can leave its scratch container behind in <body>.
    document.getElementById(`d${id}`)?.remove();
  }
}
