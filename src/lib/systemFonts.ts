import { invoke } from "@tauri-apps/api/core";
import type { FontPreset } from "@/stores/ui";

// The Settings font pickers list what is actually installed, not just the
// handful of presets. The backend enumerates the macOS font directories; the
// monospace split happens here, where a canvas can just measure the font.

let pending: Promise<string[]> | null = null;

export function loadSystemFonts(): Promise<string[]> {
  pending ??= invoke<string[]>("list_fonts").catch(() => [] as string[]);
  return pending;
}

/** A family is monospace when every glyph advances the same width. */
export function isMonospace(family: string): boolean {
  const ctx = measureCtx();
  if (!ctx) return false;
  ctx.font = `16px "${family}"`;
  return ctx.measureText("iiiiiiiiii").width === ctx.measureText("WWWWWWWWWW").width;
}

let ctx: CanvasRenderingContext2D | null | undefined;
function measureCtx() {
  ctx ??= document.createElement("canvas").getContext("2d");
  return ctx;
}

/** Wrap an installed family name in a font-family stack the app can apply. */
export function toPreset(family: string, fallback: string): FontPreset {
  return { label: family, value: `"${family}", ${fallback}` };
}
