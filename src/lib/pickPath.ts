import { ref } from "vue";

// In-app file/folder picker, replacing the native NSOpenPanel everywhere a path
// is chosen. PathPicker.vue (mounted once in App.vue) renders whatever request
// sits in `pending` and resolves its promise.
//
// ponytail: one module-level ref instead of a provide/inject or event bus —
// only one picker can be open at a time anyway.

export interface PickRequest {
  mode: "dir" | "file";
  title: string;
  start: string;
  /** File mode: lowercase extensions without the dot. Empty = any file. */
  extensions: string[];
  /** Dir mode: ⌘↵ creates the typed folder. */
  allowCreate: boolean;
  resolve: (path: string | null) => void;
}

export const pending = ref<PickRequest | null>(null);

function open(req: Omit<PickRequest, "resolve">): Promise<string | null> {
  // A second call while one is open cancels the first, so no promise dangles.
  pending.value?.resolve(null);
  return new Promise((resolve) => {
    pending.value = { ...req, resolve };
  });
}

export function pickDir(opts: { title?: string; start?: string; allowCreate?: boolean } = {}) {
  return open({
    mode: "dir",
    title: opts.title ?? "Choose folder",
    start: opts.start ?? "~/",
    extensions: [],
    allowCreate: opts.allowCreate ?? true,
  });
}

export function pickFile(opts: { title?: string; start?: string; extensions?: string[] } = {}) {
  return open({
    mode: "file",
    title: opts.title ?? "Choose file",
    start: opts.start ?? "~/",
    extensions: (opts.extensions ?? []).map((e) => e.toLowerCase().replace(/^\./, "")),
    allowCreate: false,
  });
}

export const IMAGE_EXTS = ["png", "jpg", "jpeg", "gif", "webp", "avif", "svg", "ico"];
export const AUDIO_EXTS = ["wav", "mp3", "ogg", "m4a", "aac", "flac"];

export function extOf(path: string): string {
  const name = path.split("/").pop() ?? "";
  return name.includes(".") ? (name.split(".").pop() ?? "").toLowerCase() : "";
}

export function mimeFor(path: string): string {
  switch (extOf(path)) {
    case "svg": return "image/svg+xml";
    case "ico": return "image/x-icon";
    case "jpg":
    case "jpeg": return "image/jpeg";
    case "gif": return "image/gif";
    case "webp": return "image/webp";
    case "avif": return "image/avif";
    case "png": return "image/png";
    case "mp3": return "audio/mpeg";
    case "ogg": return "audio/ogg";
    case "m4a":
    case "aac": return "audio/mp4";
    case "flac": return "audio/flac";
    case "wav": return "audio/wav";
    default: return "application/octet-stream";
  }
}
