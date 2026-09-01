<template>
  <Teleport to="body">
    <Transition name="pathpick">
      <div v-if="req" class="fixed inset-0 z-[9500] flex justify-center bg-black/60 pt-[140px]" @mousedown.self="cancel">
        <div class="pp-modal flex max-h-[560px] w-[620px] flex-col self-start overflow-hidden rounded-xl border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.6)] [backdrop-filter:var(--blur-overlay,none)]">
          <div class="flex h-14 shrink-0 items-center gap-3 border-b border-border px-4">
            <component :is="fileMode ? PhFileImage : PhFolderOpen" :size="18" class="shrink-0 text-accent" />
            <input
              ref="inputEl"
              v-model="path"
              :placeholder="fileMode ? '~/Pictures/' : '~/code/'"
              class="min-w-0 flex-1 border-0 bg-transparent font-mono text-[15px] text-foreground outline-none placeholder:text-muted-foreground/50 [caret-color:var(--accent)]"
              spellcheck="false"
              autocomplete="off"
              @keydown.esc.prevent="cancel"
              @keydown.enter.prevent="onEnter($event)"
              @keydown.tab.prevent="enterSelected"
              @keydown.up.prevent="move(-1)"
              @keydown.down.prevent="move(1)"
            />
            <button
              class="shrink-0 rounded border border-accent/25 bg-accent/10 px-2 py-0.5 text-[11px] text-accent"
              @click="onEnter()"
            >{{ req.title }} ↵</button>
          </div>

          <div class="flex min-h-0 flex-1">
            <div class="pp-list min-w-0 flex-1 overflow-y-auto py-1.5">
              <div class="flex h-[26px] items-center px-4 text-[10px] font-semibold tracking-[0.05em] text-muted-foreground/60">
                {{ missing ? "DOESN’T EXIST YET" : fileMode ? "FILES & FOLDERS" : "FOLDERS" }}
              </div>
              <div
                v-for="e in listed"
                :key="e.name"
                class="mx-1 flex h-[38px] cursor-pointer items-center gap-3 rounded-md px-3"
                :class="selected === e.name ? 'bg-selected' : 'hover:bg-hover'"
                @mouseenter="selected = e.name"
                @click="e.dir ? descend(e.name) : chooseFile(e.name)"
              >
                <PhArrowUUpLeft v-if="e.name === '..'" :size="14" class="shrink-0 text-muted-foreground" />
                <PhFolder v-else-if="e.dir" :size="14" weight="fill" class="shrink-0 text-accent" />
                <component v-else :is="iconFor(e.name)" :size="14" class="shrink-0 text-success" />
                <span class="truncate font-mono text-[12.5px] text-secondary-foreground">{{ e.name }}</span>
              </div>
              <div v-if="!listed.length" class="px-4 py-3 font-mono text-[11.5px] text-muted-foreground/70">
                {{ missing ? (req.allowCreate ? "⌘↵ creates this folder" : "no such folder") : fileMode ? "nothing matching here" : "no subfolders" }}
              </div>
            </div>

            <!-- Preview pane: only the selected file is read off disk, so browsing
                 a 500-image folder stays one read per keystroke, not 500. -->
            <div v-if="fileMode" class="flex w-[190px] shrink-0 flex-col items-center justify-center gap-3 border-l border-border bg-base p-3">
              <img v-if="preview.kind === 'image'" :src="preview.url" alt="" class="max-h-[150px] max-w-full rounded border border-border object-contain" />
              <button
                v-else-if="preview.kind === 'audio'"
                class="flex h-14 w-14 items-center justify-center rounded-full border border-accent/30 bg-accent/10 text-accent hover:bg-accent/20"
                :title="'Play ' + preview.name"
                @click="playPreview"
              >
                <PhPlay :size="20" weight="fill" />
              </button>
              <PhFile v-else :size="26" class="text-muted-foreground/40" />
              <span class="line-clamp-2 break-all text-center font-mono text-[10.5px] text-muted-foreground/70">{{ preview.name || "no selection" }}</span>
              <span v-if="preview.kind === 'audio'" class="text-[10px] text-muted-foreground/50">⌥↵ plays</span>
            </div>
          </div>

          <div class="flex h-9 shrink-0 items-center gap-4 border-t border-border bg-base px-4 text-[11px] text-muted-foreground/70">
            <span><kbd class="pp-key">↑↓</kbd> navigate</span>
            <span><kbd class="pp-key">⇥</kbd> enter dir</span>
            <span><kbd class="pp-key">↵</kbd> choose</span>
            <span v-if="req.allowCreate"><kbd class="pp-key">⌘↵</kbd> create folder</span>
            <div class="flex-1" />
            <span class="truncate font-mono text-muted-foreground/60">{{ error || expanded }}</span>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { PhFolder, PhFolderOpen, PhArrowUUpLeft, PhFile, PhFileImage, PhFileAudio, PhPlay } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { pending, AUDIO_EXTS, IMAGE_EXTS, extOf, mimeFor } from "@/lib/pickPath";

interface Entry { name: string; dir: boolean }

const req = pending;
const path = ref("~/");
const inputEl = ref<HTMLInputElement | null>(null);
const entries = ref<Entry[]>([]);
const selected = ref("");
const error = ref("");
const missing = ref(false);
let home = "";

const fileMode = computed(() => req.value?.mode === "file");

// The input holds a path; everything after the last "/" filters the listing of
// the dir before it — typing and navigating are the same gesture.
const dir = computed(() => path.value.slice(0, path.value.lastIndexOf("/") + 1) || "~/");
const filter = computed(() => path.value.slice(path.value.lastIndexOf("/") + 1).toLowerCase());
const expanded = computed(() => expand(path.value));

const listed = computed<Entry[]>(() => [
  { name: "..", dir: true },
  ...entries.value.filter((e) => !filter.value || e.name.toLowerCase().includes(filter.value)),
]);

function expand(p: string) {
  return p.startsWith("~") ? home + p.slice(1) : p;
}

function wanted(name: string) {
  const exts = req.value?.extensions ?? [];
  return !exts.length || exts.includes(extOf(name));
}

function iconFor(name: string) {
  return AUDIO_EXTS.includes(extOf(name)) ? PhFileAudio : PhFileImage;
}

watch(req, async (r) => {
  if (!r) return;
  if (!home) {
    home = (await invoke<string>("home_dir").catch(() => "")) ?? "";
  }
  // A start path pointing at a file (e.g. the currently-set wallpaper) opens its
  // folder with that file preselected.
  const start = r.start.replace(/\/$/, "");
  const startsAtFile = r.mode === "file" && extOf(start) !== "";
  path.value = startsAtFile ? start.slice(0, start.lastIndexOf("/") + 1) : start + "/";
  error.value = "";
  await load(dir.value);
  if (startsAtFile) selected.value = start.split("/").pop() ?? "";
  nextTick(() => inputEl.value?.focus());
}, { immediate: true });

watch(dir, (d) => { if (req.value) load(d); });
watch(listed, () => {
  if (!listed.value.some((e) => e.name === selected.value)) selected.value = listed.value[0]?.name ?? "";
});

async function load(d: string) {
  error.value = "";
  try {
    // The Go backend serializes `isDir`; the older Rust payload used `is_dir`.
    const list = await invoke<{ name: string; is_dir?: boolean; isDir?: boolean }[]>("read_dir_shallow", { path: expand(d) });
    entries.value = list
      .filter((e) => !e.name.startsWith("."))
      .map((e) => ({ name: e.name, dir: !!(e.is_dir ?? e.isDir) }))
      .filter((e) => e.dir || (fileMode.value && wanted(e.name)))
      // Folders first, then files, each alphabetical.
      .sort((a, b) => Number(b.dir) - Number(a.dir) || a.name.localeCompare(b.name));
    missing.value = false;
  } catch (e) {
    entries.value = [];
    missing.value = true;
    error.value = String(e);
  }
  selected.value = listed.value[0]?.name ?? "";
}

function move(step: 1 | -1) {
  const names = listed.value.map((e) => e.name);
  const idx = names.indexOf(selected.value);
  const next = Math.max(0, Math.min(names.length - 1, idx + step));
  selected.value = names[next] ?? "";
}

function descend(name: string) {
  path.value = name === ".." ? parentOf(dir.value) : dir.value + name + "/";
  nextTick(() => inputEl.value?.focus());
}

function enterSelected() {
  const e = listed.value.find((x) => x.name === selected.value);
  if (e?.dir) descend(e.name);
  else if (e) path.value = dir.value + e.name; // complete the filename in the input
}

function parentOf(d: string) {
  const trimmed = d.replace(/\/$/, "");
  const cut = trimmed.lastIndexOf("/");
  return cut <= 0 ? "/" : trimmed.slice(0, cut + 1);
}

// --- preview (file mode) ---
// Read the selected file once and hold it as a data URL. Data URL, not a Blob:
// WKWebView needs an explicit MIME for <audio> to accept the source, and a data
// URL carries it inline with no object-URL lifecycle to clean up.
const preview = ref<{ kind: "image" | "audio" | "none"; url: string; name: string }>({ kind: "none", url: "", name: "" });
let previewSeq = 0;

watch([selected, dir, fileMode], async () => {
  const name = selected.value;
  const isFile = fileMode.value && !!name && !listed.value.find((e) => e.name === name)?.dir;
  if (!isFile) return (preview.value = { kind: "none", url: "", name: name === ".." ? "" : name });
  const ext = extOf(name);
  const kind = IMAGE_EXTS.includes(ext) ? "image" : AUDIO_EXTS.includes(ext) ? "audio" : "none";
  if (kind === "none") return (preview.value = { kind, url: "", name });
  const seq = ++previewSeq;
  const b64 = await invoke<string>("read_file_base64", { path: expand(dir.value + name) }).catch(() => "");
  // Drop a slow read whose file the user has already arrowed past.
  if (seq !== previewSeq) return;
  preview.value = { kind, url: b64 ? `data:${mimeFor(name)};base64,${b64}` : "", name };
});

let player: HTMLAudioElement | null = null;
function playPreview() {
  if (preview.value.kind !== "audio" || !preview.value.url) return;
  player ??= new Audio();
  player.src = preview.value.url;
  player.currentTime = 0;
  player.play().catch(() => { /* unsupported codec — the file is still selectable */ });
}

function onEnter(e?: KeyboardEvent) {
  if (e?.altKey && preview.value.kind === "audio") return playPreview();
  if ((e?.metaKey || e?.ctrlKey) && req.value?.allowCreate) return createAndChoose();
  if (fileMode.value) {
    // Enter on a highlighted folder walks into it, which is what "↵" means when
    // the row under the cursor is a folder.
    const sel = listed.value.find((x) => x.name === selected.value);
    if (sel?.dir && filter.value === "") return descend(sel.name);
    return chooseFile(sel && !sel.dir ? sel.name : "");
  }
  chooseDir(path.value);
}

async function createAndChoose() {
  const abs = expand(path.value).replace(/\/$/, "");
  if (!abs) return;
  try {
    await invoke("create_dir", { path: abs });
  } catch (e) {
    error.value = String(e);
    return;
  }
  finish(abs);
}

function chooseFile(name: string) {
  // Either the highlighted row, or whatever full path the user typed.
  const typed = filter.value ? path.value : "";
  const target = name ? dir.value + name : typed;
  if (!target) return void (error.value = "pick a file");
  const abs = expand(target);
  if (req.value?.extensions.length && !wanted(abs)) {
    error.value = `needs one of: ${req.value.extensions.join(", ")}`;
    return;
  }
  finish(abs);
}

function chooseDir(p: string) {
  const abs = expand(p).replace(/\/$/, "");
  if (!abs) return;
  if (missing.value) {
    error.value = req.value?.allowCreate ? "no such folder — ⌘↵ to create it" : "no such folder";
    return;
  }
  finish(abs);
}

function finish(abs: string) {
  const r = req.value;
  pending.value = null;
  r?.resolve(abs);
}

function cancel() {
  const r = req.value;
  pending.value = null;
  r?.resolve(null);
}
</script>

<style scoped>
.pp-key {
  border-radius: 3px;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  padding: 1px 5px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text-secondary);
}
.pp-list::-webkit-scrollbar { width: 4px; }
.pp-list::-webkit-scrollbar-track { background: transparent; }
.pp-list::-webkit-scrollbar-thumb { background: var(--border); border-radius: 2px; }

.pathpick-enter-active,
.pathpick-leave-active { transition: opacity 0.12s ease; }
.pathpick-enter-active .pp-modal,
.pathpick-leave-active .pp-modal { transition: opacity 0.12s ease, transform 0.12s ease; }
.pathpick-enter-from,
.pathpick-leave-to { opacity: 0; }
.pathpick-enter-from .pp-modal,
.pathpick-leave-to .pp-modal { opacity: 0; transform: translateY(-8px) scale(0.98); }
</style>
