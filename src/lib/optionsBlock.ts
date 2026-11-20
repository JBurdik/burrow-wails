// ```options: the agent offers 2–N looks for the user to compare.
//
//   {"question"?: "…", "options": [{"label": "…", "note"?: "…", "media": ["a.png", "b.mp4", "c.html"]}],
//    "current"?: ["today.png"]}
//
// Paths are relative to the chat's cwd or absolute. In the reply it renders as
// a card (thumbnails + Compare); Compare opens OptionsCompare.vue in its own
// window (phone: a full-screen overlay). The spec rides on the card as
// URI-encoded JSON, so the window needs nothing but the chat id and cwd.

// Only functions from chatBlocks (it imports this module back).
import { escapeHtml } from "@/lib/chatBlocks";

export type MediaKind = "image" | "video" | "html";
export interface OptionItem { label: string; note?: string; media: string[] }
export interface OptionsSpec { question?: string; options: OptionItem[]; current?: string[] }

const VIDEO_MIME: Record<string, string> = { mp4: "video/mp4", m4v: "video/mp4", webm: "video/webm", mov: "video/quicktime" };
export const MEDIA_IMAGE_MIME: Record<string, string> = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", svg: "image/svg+xml", avif: "image/avif" };

export function mediaKind(path: string): MediaKind | null {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  if (MEDIA_IMAGE_MIME[ext]) return "image";
  if (VIDEO_MIME[ext]) return "video";
  if (ext === "html" || ext === "htm") return "html";
  return null;
}

export function mediaMime(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  return MEDIA_IMAGE_MIME[ext] ?? VIDEO_MIME[ext] ?? "text/html";
}

function paths(value: unknown): string[] {
  const list = typeof value === "string" ? [value] : Array.isArray(value) ? value : [];
  return list.filter((p): p is string => typeof p === "string" && !!p.trim() && !!mediaKind(p.trim())).map((p) => p.trim());
}

/** null = not a usable block (the fence then stays a code block). */
export function parseOptions(text: string): OptionsSpec | null {
  let raw: unknown;
  try { raw = JSON.parse(text); } catch { return null; }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const spec = raw as Record<string, unknown>;
  const options = (Array.isArray(spec.options) ? spec.options : []).flatMap((o): OptionItem[] => {
    if (!o || typeof o !== "object" || typeof (o as Record<string, unknown>).label !== "string") return [];
    const p = o as Record<string, unknown>;
    const media = paths(p.media);
    if (!media.length || !String(p.label).trim()) return [];
    return [{ label: String(p.label).trim(), ...(typeof p.note === "string" && p.note.trim() ? { note: p.note.trim() } : {}), media }];
  });
  const current = paths(spec.current);
  // One option is fine only against "current" — otherwise there's nothing to compare.
  if (options.length < (current.length ? 1 : 2)) return null;
  return {
    ...(typeof spec.question === "string" && spec.question.trim() ? { question: spec.question.trim() } : {}),
    options,
    ...(current.length ? { current } : {}),
  };
}

/** A, B, … Z, AA … */
export function optionLetter(i: number): string {
  return i < 26 ? String.fromCharCode(65 + i) : optionLetter(Math.floor(i / 26) - 1) + optionLetter(i % 26);
}

/** The message Send posts into the thread. */
export function formatOptionsReply(spec: OptionsSpec, picked: number[], note: string): string {
  const n = note.trim();
  if (!picked.length) return n ? `None of them: ${n}` : "None of them.";
  const names = [...picked].sort((a, b) => a - b).map((i) => `${optionLetter(i)} (${spec.options[i]?.label ?? "?"})`);
  return `Picked: ${names.join(", ")}${n ? `\n\n${n}` : ""}`;
}

// Image thumbnails reuse AgentChat's `img[data-cv-image]` hydration; a video /
// page gets a labelled tile. A click on a thumbnail opens the compare view on
// that option (data-co-open), Compare on option 1.
export function renderOptions(text: string, answered: boolean): string | null {
  const spec = parseOptions(text);
  if (!spec) return null;
  const tiles = spec.options.map((o, i) => {
    const first = o.media[0];
    const kind = mediaKind(first);
    // svg / avif: AgentChat's hydration only knows IMAGE_MIME, so they get a tile.
    const thumb = kind === "image" && !/\.(svg|avif)$/i.test(first)
      ? `<img class="co-thumb-img" data-cv-image="${escapeHtml(first)}" alt="">`
      : `<span class="co-thumb-kind">${kind === "video" ? "Video" : kind === "html" ? "Page" : "Image"}</span>`;
    const more = o.media.length > 1 ? `<span class="co-more">+${o.media.length - 1}</span>` : "";
    return `<button type="button" class="co-opt" data-co-open="${i}" title="${escapeHtml(o.note ?? o.label)}">`
      + `<span class="co-thumb">${thumb}${more}</span><span class="co-label"><b>${optionLetter(i)}</b> ${escapeHtml(o.label)}</span></button>`;
  });
  const q = spec.question ? `<div class="co-question">${escapeHtml(spec.question)}</div>` : "";
  const done = answered ? `<span class="co-done">Answered</span>` : "";
  return `<div class="co" data-chat-options="${encodeURIComponent(JSON.stringify(spec))}"${answered ? ' data-co-answered="1"' : ""}>${q}`
    + `<div class="co-opts">${tiles.join("")}</div>`
    + `<div class="co-actions">${done}<button type="button" class="co-compare" data-co-open="0">Compare</button></div></div>`;
}
