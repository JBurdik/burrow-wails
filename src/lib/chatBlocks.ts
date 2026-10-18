// Rich blocks an agent can put in a chat reply as a fenced code block:
//
//   ```visual     charts (a Vega-Lite subset), stat tiles, tables, text, columns
//   ```questions  a form: every question at once, clickable choices, one Send
//
// Both are plain JSON in the fence, so they work for every provider (Claude,
// Codex, ACP) with no protocol support — the agent learns the format from the
// `burrow-visuals` skill. Anything that does not parse (including a block that
// is still streaming in) falls back to the ordinary code block, so a half-sent
// fence never breaks the message. Idea and format from maddada/Ghostex (MIT).
//
// Everything here returns an HTML string that AgentChat sanitizes with
// DOMPurify like any other markdown, so every value is escaped on the way in.

import { Marked, type Tokens } from "marked";

const plainMarked = new Marked({ gfm: true });

export function escapeHtml(value: unknown): string {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function parseJson(text: string): Record<string, unknown> | null {
  try {
    const value = JSON.parse(text);
    return value && typeof value === "object" && !Array.isArray(value) ? value : null;
  } catch {
    return null;
  }
}

function fmtNum(value: unknown): string {
  return typeof value === "number" ? value.toLocaleString(undefined, { maximumFractionDigits: 2 }) : escapeHtml(value);
}

// ── visual ──────────────────────────────────────────────────────────────────

type Part = Record<string, unknown>;
const PALETTE = ["var(--chat-accent)", "var(--chat-info)", "var(--chat-success)", "var(--chat-warn)", "#a78bfa", "#f472b6"];

export function renderVisual(text: string): string | null {
  const spec = parseJson(text);
  if (!spec) return null;
  if (spec.mark) {
    const chart = chartPart(spec);
    return chart ? `<div class="cv">${chart}</div>` : null;
  }
  const rows = Array.isArray(spec.rows) ? (spec.rows as Part[]) : isPart(spec) ? [spec] : null;
  if (!rows) return null;
  const title = spec.rows && spec.title ? `<div class="cv-title">${escapeHtml(spec.title)}</div>` : "";
  return `<div class="cv">${title}${rows.map(renderPart).join("")}</div>`;
}

function isPart(p: Part): boolean {
  return ["chart", "stats", "table", "text", "columns"].some((k) => k in p);
}

function renderPart(part: Part): string {
  if (!part || typeof part !== "object") return "";
  const title = part.title && !part.chart ? `<div class="cv-part-title">${escapeHtml(part.title)}</div>` : "";
  let body = "";
  if (part.chart && typeof part.chart === "object") body = chartPart({ title: part.title, ...(part.chart as Part) });
  else if (Array.isArray(part.stats)) body = statsPart(part.stats as Part[]);
  else if (part.table && typeof part.table === "object") body = tablePart(part.table as Part);
  else if (typeof part.text === "string") body = `<div class="cv-text">${plainMarked.parse(part.text) as string}</div>`;
  else if (Array.isArray(part.columns)) body = `<div class="cv-columns">${(part.columns as Part[]).map((c) => `<div class="cv-col">${renderPart(c)}</div>`).join("")}</div>`;
  return `<div class="cv-part">${title}${body}</div>`;
}

function statsPart(stats: Part[]): string {
  const tiles = stats.map((s) => {
    const tone = s.tone === "good" || s.tone === "bad" ? ` cv-${s.tone}` : "";
    const note = s.note != null ? `<div class="cv-stat-note">${escapeHtml(s.note)}</div>` : "";
    return `<div class="cv-stat${tone}"><div class="cv-stat-label">${escapeHtml(s.label)}</div><div class="cv-stat-value">${fmtNum(s.value)}</div>${note}</div>`;
  });
  return `<div class="cv-stats">${tiles.join("")}</div>`;
}

function tablePart(table: Part): string {
  const rows = Array.isArray(table.rows) ? (table.rows as unknown[]) : [];
  let columns = Array.isArray(table.columns) ? (table.columns as unknown[]).map(String) : [];
  if (!columns.length) columns = [...new Set(rows.flatMap((r) => (r && typeof r === "object" && !Array.isArray(r) ? Object.keys(r) : [])))];
  const cells = (row: unknown) =>
    Array.isArray(row) ? row : columns.map((c) => (row && typeof row === "object" ? (row as Part)[c] : ""));
  const head = columns.length ? `<thead><tr>${columns.map((c) => `<th>${escapeHtml(c)}</th>`).join("")}</tr></thead>` : "";
  const body = rows.map((r) => `<tr>${cells(r).map((v) => `<td${typeof v === "number" ? ' class="cv-num"' : ""}>${fmtNum(v)}</td>`).join("")}</tr>`).join("");
  return `<div class="cv-table-wrap"><table class="cv-table">${head}<tbody>${body}</tbody></table></div>`;
}

// A Vega-Lite subset: mark bar | line | area | point (circle, square) | arc,
// channels x / y / color / theta, inline `data.values`.
// ponytail: x is always ordinal (one band per distinct value, in data order),
// so a numeric or temporal x is evenly spaced, not to scale. Proper scales when
// an agent actually needs a scatter plot.
function chartPart(spec: Part): string {
  const markRaw = spec.mark;
  const markObj = (markRaw && typeof markRaw === "object" ? markRaw : { type: markRaw }) as Part;
  const mark = String(markObj.type ?? "");
  const values = (((spec.data as Part | undefined)?.values ?? []) as Part[]).filter((v) => v && typeof v === "object");
  const enc = (spec.encoding ?? {}) as Record<string, Part | undefined>;
  const title = spec.title ? `<div class="cv-part-title">${escapeHtml(typeof spec.title === "object" ? (spec.title as Part).text : spec.title)}</div>` : "";
  const height = typeof spec.height === "number" ? Math.min(Math.max(spec.height, 80), 600) : 220;
  const svg = mark === "arc" ? arcChart(values, enc, markObj, height) : xyChart(mark, values, enc, markObj, height);
  return svg ? `${title}${svg}` : "";
}

function field(ch: Part | undefined): string | undefined {
  return typeof ch?.field === "string" ? ch.field : undefined;
}

function legend(series: string[]): string {
  if (series.length < 2) return "";
  return `<div class="cv-legend">${series.map((s, i) => `<span><i style="background:${PALETTE[i % PALETTE.length]}"></i>${escapeHtml(s)}</span>`).join("")}</div>`;
}

function xyChart(mark: string, values: Part[], enc: Record<string, Part | undefined>, markObj: Part, H: number): string {
  const xf = field(enc.x), yf = field(enc.y), cf = field(enc.color);
  if (!xf || !yf || !["bar", "line", "area", "point", "circle", "square"].includes(mark)) return "";
  const cats = [...new Set(values.map((v) => String(v[xf])))];
  const series = cf ? [...new Set(values.map((v) => String(v[cf])))] : [""];
  const ys = values.map((v) => Number(v[yf])).filter(Number.isFinite);
  if (!cats.length || !ys.length) return "";
  const W = 600, L = 48, R = 12, T = 10, B = 28;
  const lo = Math.min(0, ...ys), hi = Math.max(0, ...ys) || 1;
  const sy = (v: number) => T + (H - T - B) * (1 - (v - lo) / (hi - lo));
  const band = (W - L - R) / cats.length;
  const cx = (i: number) => L + band * (i + 0.5);
  const out: string[] = [];
  for (let t = 0; t <= 4; t++) {
    const v = lo + ((hi - lo) * t) / 4, y = sy(v);
    out.push(`<line x1="${L}" x2="${W - R}" y1="${y}" y2="${y}" class="cv-grid"/><text x="${L - 6}" y="${y + 3}" text-anchor="end" class="cv-axis">${fmtNum(+v.toPrecision(3))}</text>`);
  }
  const every = Math.ceil(cats.length / 12);
  cats.forEach((c, i) => { if (i % every === 0) out.push(`<text x="${cx(i)}" y="${H - 10}" text-anchor="middle" class="cv-axis">${escapeHtml(c.length > 14 ? c.slice(0, 13) + "…" : c)}</text>`); });
  series.forEach((s, si) => {
    const color = PALETTE[si % PALETTE.length];
    const pts = cats.flatMap((c, i) => {
      const row = values.find((v) => String(v[xf]) === c && (!cf || String(v[cf]) === s));
      const y = row ? Number(row[yf]) : NaN;
      return Number.isFinite(y) ? [{ i, c, y }] : [];
    });
    const tip = (p: { c: string; y: number }) => `<title>${escapeHtml(s ? `${s} · ` : "")}${escapeHtml(p.c)}: ${fmtNum(p.y)}</title>`;
    if (mark === "bar") {
      const bw = (band * 0.75) / series.length;
      for (const p of pts) {
        const x = L + band * p.i + band * 0.125 + bw * si, y0 = sy(0), y1 = sy(p.y);
        out.push(`<rect x="${x}" y="${Math.min(y0, y1)}" width="${Math.max(bw - 1, 1)}" height="${Math.abs(y0 - y1)}" rx="2" fill="${color}">${tip(p)}</rect>`);
      }
      return;
    }
    const line = pts.map((p) => `${cx(p.i)},${sy(p.y)}`).join(" ");
    if (mark === "area" && pts.length) {
      out.push(`<polygon points="${cx(pts[0].i)},${sy(0)} ${line} ${cx(pts[pts.length - 1].i)},${sy(0)}" fill="${color}" fill-opacity="0.18"/>`);
    }
    if (mark === "line" || mark === "area") out.push(`<polyline points="${line}" fill="none" stroke="${color}" stroke-width="2"/>`);
    if (mark !== "line" || markObj.point) {
      for (const p of pts) out.push(`<circle cx="${cx(p.i)}" cy="${sy(p.y)}" r="${mark === "line" || mark === "area" ? 2.5 : 3.5}" fill="${color}">${tip(p)}</circle>`);
    }
  });
  return `<svg class="cv-chart" viewBox="0 0 ${W} ${H}" preserveAspectRatio="xMidYMid meet">${out.join("")}</svg>${legend(cf ? series : [])}`;
}

function arcChart(values: Part[], enc: Record<string, Part | undefined>, markObj: Part, H: number): string {
  const tf = field(enc.theta), cf = field(enc.color);
  if (!tf) return "";
  const slices = values.map((v, i) => ({ label: cf ? String(v[cf]) : String(i + 1), value: Math.max(Number(v[tf]) || 0, 0) }));
  const total = slices.reduce((a, s) => a + s.value, 0);
  if (!total) return "";
  const r = H / 2 - 6, cx = H / 2, cy = H / 2;
  const inner = Math.min(Math.max(Number(markObj.innerRadius) || 0, 0), r - 4);
  const pt = (a: number, rad: number) => `${cx + rad * Math.sin(a)},${cy - rad * Math.cos(a)}`;
  let a0 = 0;
  const paths = slices.map((s, i) => {
    // A single 100% slice has identical start and end points, so it would draw nothing.
    const a1 = a0 + (Math.min(s.value / total, 0.9999) * Math.PI * 2);
    const big = a1 - a0 > Math.PI ? 1 : 0;
    const d = inner
      ? `M${pt(a0, r)} A${r},${r} 0 ${big} 1 ${pt(a1, r)} L${pt(a1, inner)} A${inner},${inner} 0 ${big} 0 ${pt(a0, inner)} Z`
      : `M${cx},${cy} L${pt(a0, r)} A${r},${r} 0 ${big} 1 ${pt(a1, r)} Z`;
    a0 = a0 + (s.value / total) * Math.PI * 2;
    return `<path d="${d}" fill="${PALETTE[i % PALETTE.length]}"><title>${escapeHtml(s.label)}: ${fmtNum(s.value)} (${Math.round((s.value / total) * 100)}%)</title></path>`;
  });
  return `<svg class="cv-chart cv-pie" viewBox="0 0 ${H} ${H}" style="max-width:${H}px">${paths.join("")}</svg>${legend(slices.map((s) => s.label))}`;
}

// ── questions ───────────────────────────────────────────────────────────────

export interface QuestionOption { label: string; description?: string; recommended?: boolean }
export interface QuestionSpec { question: string; header?: string; multiSelect?: boolean; options: QuestionOption[] }

export function parseQuestions(text: string): QuestionSpec[] | null {
  const spec = parseJson(text);
  const list = Array.isArray(spec?.questions) ? (spec!.questions as Part[]) : null;
  if (!list?.length) return null;
  const questions = list
    .filter((q) => q && typeof q.question === "string" && q.question.trim())
    .map((q) => ({
      question: String(q.question),
      ...(typeof q.header === "string" ? { header: q.header } : {}),
      multiSelect: q.multiSelect === true,
      options: (Array.isArray(q.options) ? (q.options as unknown[]) : []).flatMap((o): QuestionOption[] => {
        if (typeof o === "string") return [{ label: o }];
        if (!o || typeof o !== "object" || typeof (o as Part).label !== "string") return [];
        const p = o as Part;
        return [{ label: String(p.label), ...(typeof p.description === "string" ? { description: p.description } : {}), recommended: p.recommended === true }];
      }),
    }));
  return questions.length ? questions : null;
}

export function renderQuestions(text: string, answered: boolean): string | null {
  const questions = parseQuestions(text);
  if (!questions) return null;
  const dis = answered ? " disabled" : "";
  const items = questions.map((q, qi) => {
    const header = q.header ? `<span class="cq-header">${escapeHtml(q.header)}</span>` : "";
    const opts = q.options.map((o) => {
      const rec = o.recommended ? `<span class="cq-rec">Recommended</span>` : "";
      const desc = o.description ? `<span class="cq-desc">${escapeHtml(o.description)}</span>` : "";
      return `<button type="button" class="cq-opt" data-cq-opt="${escapeHtml(o.label)}" aria-pressed="false"${dis}><span class="cq-label">${escapeHtml(o.label)}${rec}</span>${desc}</button>`;
    });
    const hint = q.multiSelect ? `<div class="cq-hint">Select one or more.</div>` : "";
    return `<div class="cq-q" data-cq-index="${qi}" data-cq-question="${escapeHtml(q.question)}" data-cq-multi="${q.multiSelect ? 1 : 0}">`
      + `<div class="cq-title">${header}<span>${qi + 1}. ${escapeHtml(q.question)}</span></div>${hint}`
      + `<div class="cq-opts">${opts.join("")}</div>`
      + `<input type="text" class="cq-other" placeholder="${q.options.length ? "Other / note…" : "Your answer…"}"${dis}></div>`;
  });
  const notes = questions.length > 1 ? `<textarea class="cq-notes" rows="2" placeholder="Anything else the agent should know (optional)"${dis}></textarea>` : "";
  const actions = answered
    ? `<div class="cq-actions"><span class="cq-done">Answered</span></div>`
    : `<div class="cq-actions"><button type="button" class="cq-skip" data-cq-action="skip">Skip all</button><button type="button" class="cq-send" data-cq-action="send">Send answers</button></div>`;
  return `<div class="cq${answered ? " cq-answered" : ""}" data-chat-questions="1">${items.join("")}${notes}${actions}</div>`;
}

export interface QuestionAnswer { question: string; picked: string[]; other: string }

/** The reply a filled-in form sends, as one user message. Unanswered = skipped. */
export function formatAnswers(answers: QuestionAnswer[], notes = ""): string {
  const lines = answers.map((a, i) => {
    const parts = [...a.picked, ...(a.other.trim() ? [a.other.trim()] : [])];
    return `${i + 1}. ${a.question}\n   → ${parts.length ? parts.join("; ") : "(skipped)"}`;
  });
  const tail = notes.trim() ? `\n\nNotes: ${notes.trim()}` : "";
  return `Answers:\n${lines.join("\n")}${tail}`;
}

// ── markdown ────────────────────────────────────────────────────────────────

// ponytail: module-level flag read by the (synchronous) renderer below, set per
// renderChatMd call. Marked has no per-parse context; a second instance per
// state would be the alternative.
let answeredCtx = false;

const chatMarked = new Marked({
  gfm: true,
  renderer: {
    code(token: Tokens.Code) {
      const lang = (token.lang ?? "").trim().split(/\s+/)[0];
      if (lang === "visual") return renderVisual(token.text) ?? false;
      if (lang === "questions") return renderQuestions(token.text, answeredCtx) ?? false;
      return false;
    },
  },
});

/** Assistant-message markdown with the rich blocks. `answered`: a later user message exists. */
export function renderChatMd(text: string, answered = false): string {
  answeredCtx = answered;
  return chatMarked.parse(text) as string;
}
