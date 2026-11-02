// "What's new" after an update: the release blocks of docs/changelog.html
// (bundled at build time) newer than the last version this device launched.
import changelog from "../../docs/changelog.html?raw";
import { configReady, getConfig, setConfig } from "@/lib/config";
import { invoke } from "@tauri-apps/api/core";

const SEEN_KEY = "lastLaunchedVersion";
const RELEASE = /(?=<div class="release">)/;

// The changelog trimmed to the releases in (seen, current], newest first —
// or "" when there is nothing to show. Pure string work: vitest has no DOM.
export function releaseNotesSince(html: string, current: string, seen: string): string {
  const [head, ...releases] = html.split(RELEASE);
  const picked: string[] = [];
  let on = false;
  for (const r of releases) {
    const v = /class="version">v?([^<]+)</.exec(r)?.[1].trim();
    if (v === seen) break;
    if (v === current) on = true;
    if (on) picked.push(r);
  }
  if (!picked.length) return "";
  const wrapAt = head.indexOf('<div class="wrap">');
  // Trailing closers of the last block are harmless if repeated.
  return head.slice(0, wrapAt) + '<div class="wrap">' + picked.join("") + "</div></body></html>";
}

// Desktop only: config.json is shared with paired phones, and a phone opening
// first would mark the version seen and swallow the dialog.
export async function pendingReleaseNotes(): Promise<{ version: string; html: string } | null> {
  if (!(window as any).runtime) return null;
  await configReady;
  const current = await invoke<string>("get_app_version");
  const seen = getConfig<string>(SEEN_KEY, "");
  if (seen === current) return null;
  setConfig(SEEN_KEY, current);
  if (!seen) return null; // fresh install: nothing to compare against
  const html = releaseNotesSince(changelog, current, seen);
  return html ? { version: current, html } : null;
}

// Help → What's New: this version and every older one (the whole changelog
// when this build has no entry of its own, e.g. a dev build).
export async function allReleaseNotes(): Promise<{ version: string; html: string }> {
  const version = await invoke<string>("get_app_version");
  return { version, html: releaseNotesSince(changelog, version, "") || changelog };
}
