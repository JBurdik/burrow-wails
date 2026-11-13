<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import {
  PhCaretDown, PhCheckSquare, PhFile, PhGitCommit, PhGitPullRequest, PhGlobe, PhHandPalm, PhPushPin, PhSquare,
  PhSparkle, PhUsersThree, PhX,
} from "@phosphor-icons/vue";
import { chatSession } from "@/lib/chatSession";
import type { ChatMessage } from "@/lib/chatTypes";
import { highlightFiles, highlightLinks, isReviewFile, shortPath } from "@/lib/chatHighlights";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { childrenOf } from "@/stores/chatTree";

// What the open thread made and what needs looking at, in one place (from
// threadbear's Highlights): its open asks, what its agent pinned (`pin` verb),
// files it wrote, links it mentioned, its changes and its sub-agents. Read-only
// projections of the session transcript + the chats store, plus the pins table.
const props = defineProps<{ chatId: number | null; cwd: string }>();
const emit = defineEmits<{ openFile: [path: string]; openUrl: [url: string]; openSurface: [id: string]; openSubagent: [id: number] }>();

interface Pin { id: number; target: string; note: string }

const chats = useClaudeChatsStore();
const session = computed(() => (props.chatId ? chats.sessions.find((s) => s.id === props.chatId) : undefined));
const live = computed(() => (props.chatId ? chatSession(props.chatId).messages.value : []));
// The session holds only the transcript's tail (a long chat opens windowed),
// so the whole stored transcript is read once per thread and merged with the
// live tail — files and links from the start of a long thread stay listed.
// ponytail: one full read when Highlights opens / the thread changes; a Go-side
// index is the upgrade if a huge transcript makes that slow.
const stored = ref<ChatMessage[]>([]);
async function loadStored() {
  const id = props.chatId;
  if (!id) { stored.value = []; return; }
  const raw = await invoke<string | ChatMessage[]>("load_chat_messages", { chatId: id }).catch(() => "[]");
  if (id !== props.chatId) return;
  try { stored.value = (typeof raw === "string" ? JSON.parse(raw || "[]") : raw) ?? []; } catch { stored.value = []; }
}
const messages = computed(() => [...stored.value, ...live.value]);
const asks = computed(() => (session.value?.turnNeeds?.needs === "you" ? session.value.turnNeeds.asks ?? [] : []));
// Files = things to look at (docs, pages, pictures); every edit counts in Changes.
const allFiles = computed(() => highlightFiles(messages.value));
const files = computed(() => allFiles.value.filter((f) => isReviewFile(f.path)));
const links = computed(() => highlightLinks(messages.value));
const children = computed(() => (props.chatId ? childrenOf(chats.sessions, props.chatId) : []));
const pins = ref<Pin[]>([]);

// Ticks are a reading aid for this view only; answering happens on the card.
const ticked = reactive(new Set<string>());

const FOLD_KEY = "burrow.highlights.folded";
const folded = reactive(new Set<string>(JSON.parse(localStorage.getItem(FOLD_KEY) || "[]") as string[]));
function toggleFold(key: string) {
  if (folded.has(key)) folded.delete(key);
  else folded.add(key);
  localStorage.setItem(FOLD_KEY, JSON.stringify([...folded]));
}

async function loadPins() {
  const id = props.chatId;
  if (!id) { pins.value = []; return; }
  const list = await invoke<Pin[]>("chat_pins_list", { chatId: id }).catch(() => []);
  if (id === props.chatId) pins.value = list ?? [];
}
watch(() => props.chatId, () => { ticked.clear(); void loadPins(); void loadStored(); }, { immediate: true });
let off: (() => void) | undefined;
void listen<{ chat_id: number }>("chat-pins-changed", (e) => { if (e.payload?.chat_id === props.chatId) void loadPins(); }).then((u) => (off = u));
onBeforeUnmount(() => off?.());

async function unpin(p: Pin) {
  pins.value = pins.value.filter((x) => x.id !== p.id);
  await invoke("chat_pin_remove", { id: p.id }).catch(() => loadPins());
}

const isUrl = (t: string) => /^https?:\/\//.test(t);
function openTarget(t: string) {
  if (isUrl(t)) emit("openUrl", t);
  else emit("openFile", t);
}
function childLabel(status?: string): string {
  if (status === "running") return "working";
  if (status === "waiting" || status === "permission") return "needs you";
  if (status === "error") return "failed";
  if (status === "review" || status === "done") return "done";
  return "idle";
}

const empty = computed(() => !asks.value.length && !pins.value.length && !allFiles.value.length && !links.value.length && !children.value.length);
</script>

<template>
  <section class="hl flex min-h-0 flex-1 flex-col overflow-hidden text-[12px] text-secondary-foreground">
    <header class="flex items-center gap-1.5 border-b border-border p-2 font-semibold text-foreground">
      <PhSparkle :size="14" /> Highlights
    </header>
    <div v-if="!chatId" class="p-4 text-center text-muted-foreground">Open a thread to see what it made.</div>
    <div v-else-if="empty" class="p-4 text-center leading-relaxed text-muted-foreground">
      What this thread writes, links, pins and asks shows up here as it works.
    </div>
    <div v-else class="flex-1 overflow-auto pb-2">
      <!-- Needs you -->
      <template v-if="asks.length">
        <button class="hl-head" @click="toggleFold('needs')">
          <PhHandPalm :size="12" weight="fill" class="text-[var(--yellow)]" /> Needs you
          <span class="opacity-60">{{ asks.length }}</span>
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('needs') && '-rotate-90'" />
        </button>
        <template v-if="!folded.has('needs')">
          <button v-for="(a, i) in asks" :key="i" class="hl-row items-start" @click="ticked.has(a.question) ? ticked.delete(a.question) : ticked.add(a.question)">
            <component :is="ticked.has(a.question) ? PhCheckSquare : PhSquare" :size="13" class="mt-px shrink-0" />
            <span class="min-w-0 flex-1 leading-snug" :class="ticked.has(a.question) ? 'text-muted-foreground line-through' : 'text-foreground'">{{ a.question }}</span>
          </button>
        </template>
      </template>

      <!-- Pinned -->
      <template v-if="pins.length">
        <button class="hl-head" @click="toggleFold('pins')">
          <PhPushPin :size="12" weight="fill" /> Pinned <span class="opacity-60">{{ pins.length }}</span>
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('pins') && '-rotate-90'" />
        </button>
        <template v-if="!folded.has('pins')">
          <div v-for="p in pins" :key="p.id" class="group hl-row cursor-pointer items-start" :title="p.target" @click="openTarget(p.target)">
            <component :is="isUrl(p.target) ? PhGlobe : PhFile" :size="13" class="mt-px shrink-0" />
            <span class="flex min-w-0 flex-1 flex-col">
              <span class="truncate text-foreground">{{ isUrl(p.target) ? p.target.replace(/^https?:\/\//, "") : shortPath(p.target, cwd) }}</span>
              <span v-if="p.note" class="leading-snug text-muted-foreground">{{ p.note }}</span>
            </span>
            <button class="hl-x invisible group-hover:visible" title="Unpin" @click.stop="unpin(p)"><PhX :size="11" /></button>
          </div>
        </template>
      </template>

      <!-- Files -->
      <template v-if="files.length">
        <button class="hl-head" @click="toggleFold('files')">
          <PhFile :size="12" /> Files <span class="opacity-60">{{ files.length }}</span>
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('files') && '-rotate-90'" />
        </button>
        <template v-if="!folded.has('files')">
          <button v-for="f in files" :key="f.path" class="hl-row" :title="f.path" @click="emit('openFile', f.path)">
            <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ shortPath(f.path, cwd) }}</span>
            <span class="shrink-0 text-[11px] text-muted-foreground">{{ f.op }}</span>
          </button>
        </template>
      </template>

      <!-- Links -->
      <template v-if="links.length">
        <button class="hl-head" @click="toggleFold('links')">
          <PhGlobe :size="12" /> Links <span class="opacity-60">{{ links.length }}</span>
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('links') && '-rotate-90'" />
        </button>
        <template v-if="!folded.has('links')">
          <button v-for="l in links" :key="l.url" class="hl-row" :title="l.url" @click="emit('openUrl', l.url)">
            <component :is="l.pr ? PhGitPullRequest : PhGlobe" :size="13" class="shrink-0" />
            <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ l.url.replace(/^https?:\/\//, "") }}</span>
          </button>
        </template>
      </template>

      <!-- Changes -->
      <template v-if="allFiles.length">
        <button class="hl-head" @click="toggleFold('changes')">
          <PhGitCommit :size="12" /> Changes
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('changes') && '-rotate-90'" />
        </button>
        <button v-if="!folded.has('changes')" class="hl-row" @click="emit('openSurface', 'diff')">
          <span class="flex-1 text-left text-foreground">{{ allFiles.length }} {{ allFiles.length === 1 ? "file" : "files" }} changed</span>
          <span class="shrink-0 text-[11px] text-muted-foreground">Open diff</span>
        </button>
      </template>

      <!-- Sub-agents -->
      <template v-if="children.length">
        <button class="hl-head" @click="toggleFold('agents')">
          <PhUsersThree :size="12" /> Sub-agents <span class="opacity-60">{{ children.length }}</span>
          <PhCaretDown :size="10" class="ml-auto transition-transform" :class="folded.has('agents') && '-rotate-90'" />
        </button>
        <template v-if="!folded.has('agents')">
          <button v-for="c in children" :key="c.id" class="hl-row" :title="c.title" @click="emit('openSubagent', c.id)">
            <span class="min-w-0 flex-1 truncate text-left text-foreground">{{ c.title || `Sub-agent ${c.id}` }}</span>
            <span class="shrink-0 text-[11px]" :class="childLabel(c.status) === 'needs you' ? 'text-[var(--yellow)]' : 'text-muted-foreground'">{{ childLabel(c.status) }}</span>
          </button>
        </template>
      </template>
    </div>
  </section>
</template>

<style scoped>
.hl-head { display: flex; width: 100%; align-items: center; gap: 6px; padding: 10px 10px 3px; font-size: 11px; font-weight: 600; color: var(--text-muted); }
.hl-head:hover { color: var(--text-secondary); }
.hl-row { display: flex; width: 100%; align-items: center; gap: 7px; padding: 4px 10px; text-align: left; border-radius: 6px; }
.hl-row:hover { background: var(--bg-hover); }
.hl-x { display: flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px; color: var(--text-muted); }
.hl-x:hover { background: var(--bg-hover); color: var(--text-primary); }
</style>
