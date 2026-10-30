<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { useProvidersStore } from "@/stores/providers";

// Settings → Scheduled: every chat's scheduled prompts and recurring tasks
// (chatschedule.go). Go owns the rows, the next-run math and delivery; this
// only edits them.

interface Rule { kind: "once" | "interval" | "weekly"; every_min?: number; weekdays?: number[]; time_of_day?: string }
interface Scheduled {
  id: number; chat_id: number; name: string; text: string; due_at: string; rule: Rule; recurs: string;
  new_chat: boolean; workspace_id?: number; agent?: string; paused: boolean; last_run_at?: string; last_error?: string;
}

const chats = useClaudeChatsStore();
const workspaces = useWorkspaceStore();
const providers = useProvidersStore();
const items = ref<Scheduled[]>([]);
const error = ref("");
const draft = ref<Scheduled | null>(null);
const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

const openChats = computed(() => chats.sessions.filter((s) => !s.archivedAt));

async function load() {
  items.value = await invoke<Scheduled[]>("schedule_list_all").catch(() => items.value);
}
let timer: number | undefined;
onMounted(() => { void load(); timer = window.setInterval(load, 15000); });
onUnmounted(() => window.clearInterval(timer));

async function act(cmd: string, args: Record<string, unknown>) {
  error.value = "";
  try {
    await invoke(cmd, args);
  } catch (e) {
    error.value = String(e);
  }
  await load();
}

function targetLabel(s: Scheduled): string {
  if (s.new_chat) {
    const ws = workspaces.workspaces.find((w) => w.id === s.workspace_id)?.name ?? `workspace ${s.workspace_id}`;
    return `New chat in ${ws}${s.agent ? ` · ${providers.byId(s.agent)?.name ?? s.agent}` : ""}`;
  }
  return chats.sessions.find((c) => c.id === s.chat_id)?.title || `Chat ${s.chat_id}`;
}
function when(iso?: string): string {
  return iso ? new Date(iso).toLocaleString([], { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) : "";
}
// <input type="datetime-local"> wants local "YYYY-MM-DDTHH:MM".
function localInput(iso: string): string {
  const d = new Date(iso);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

function create() {
  draft.value = {
    id: 0, chat_id: openChats.value[0]?.id ?? 0, name: "", text: "", due_at: localInput(new Date(Date.now() + 3600000).toISOString()),
    rule: { kind: "weekly", weekdays: [1, 2, 3, 4, 5], time_of_day: "09:00", every_min: 60 }, recurs: "",
    new_chat: false, workspace_id: workspaces.workspaces[0]?.id, agent: "", paused: false,
  };
}
function edit(s: Scheduled) {
  draft.value = { ...s, due_at: localInput(s.due_at), rule: { every_min: 60, weekdays: [], time_of_day: "09:00", ...s.rule } };
}
function toggleDay(d: number) {
  const r = draft.value!.rule;
  r.weekdays = r.weekdays?.includes(d) ? r.weekdays.filter((x) => x !== d) : [...(r.weekdays ?? []), d].sort();
}
async function save() {
  const d = draft.value!;
  const r = d.rule;
  const rule: Rule = r.kind === "interval" ? { kind: "interval", every_min: Number(r.every_min) }
    : r.kind === "weekly" ? { kind: "weekly", weekdays: r.weekdays, time_of_day: r.time_of_day } : { kind: "once" };
  const schedule = {
    id: d.id, chat_id: d.new_chat ? 0 : d.chat_id, name: d.name, text: d.text, rule, new_chat: d.new_chat,
    workspace_id: d.new_chat ? d.workspace_id : 0, agent: d.new_chat ? d.agent : "", paused: d.paused,
    // A recurring item's next run is Go's (NextDue); only a one-shot names it.
    due_at: r.kind === "once" ? new Date(d.due_at).toISOString() : "0001-01-01T00:00:00Z",
  };
  error.value = "";
  try {
    await invoke("schedule_save", { schedule });
    draft.value = null;
  } catch (e) {
    error.value = String(e);
  }
  await load();
}

const field = "h-8 min-w-0 rounded-[var(--radius-input)] border border-border bg-base px-2 text-xs text-foreground focus:outline-accent";
const btn = "rounded-[var(--radius-button)] border border-border px-2.5 py-1 text-xs text-secondary-foreground hover:bg-hover disabled:opacity-40";
</script>

<template>
  <div class="flex max-w-[760px] flex-col gap-4">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h3 class="m-0 text-sm font-semibold text-foreground">Scheduled</h3>
        <p class="m-0 mt-1 text-xs leading-relaxed text-muted-foreground">Prompts sent later, once or on a schedule. A run into a chat waits until its turn has finished; a run missed while Burrow was closed fires once on the next launch.</p>
      </div>
      <button v-if="!draft" type="button" class="shrink-0 rounded-[var(--radius-button)] bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground" @click="create">New task</button>
    </div>

    <p v-if="error" role="alert" class="m-0 text-xs text-destructive">{{ error }}</p>

    <!-- Editor -->
    <div v-if="draft" class="flex flex-col gap-3 rounded-lg border border-border p-3">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="grid gap-1 text-xs text-secondary-foreground">Name
          <input v-model="draft.name" :class="field" placeholder="Defaults to the prompt's first line" />
        </label>
        <label class="grid gap-1 text-xs text-secondary-foreground">Target
          <select v-model="draft.new_chat" :class="field">
            <option :value="false">Post into a chat</option>
            <option :value="true">Start a new chat each run</option>
          </select>
        </label>
        <label v-if="!draft.new_chat" class="grid gap-1 text-xs text-secondary-foreground sm:col-span-2">Chat
          <select v-model="draft.chat_id" :class="field">
            <option v-for="c in openChats" :key="c.id" :value="c.id">{{ c.title || `Chat ${c.id}` }}</option>
          </select>
        </label>
        <template v-else>
          <label class="grid gap-1 text-xs text-secondary-foreground">Workspace
            <select v-model="draft.workspace_id" :class="field">
              <option v-for="w in workspaces.workspaces" :key="w.id" :value="w.id">{{ w.name }}</option>
            </select>
          </label>
          <label class="grid gap-1 text-xs text-secondary-foreground">Agent
            <select v-model="draft.agent" :class="field">
              <option value="">Default chat agent</option>
              <option v-for="a in providers.chatAgents" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
          </label>
        </template>
      </div>
      <label class="grid gap-1 text-xs text-secondary-foreground">Prompt
        <textarea v-model="draft.text" rows="4" class="w-full resize-y rounded-[var(--radius-input)] border border-border bg-base p-2 text-xs leading-relaxed text-foreground focus:outline-accent" />
      </label>
      <div class="flex flex-wrap items-end gap-3">
        <label class="grid gap-1 text-xs text-secondary-foreground">Repeat
          <select v-model="draft.rule.kind" :class="field">
            <option value="once">Once</option>
            <option value="interval">Every…</option>
            <option value="weekly">Weekly</option>
          </select>
        </label>
        <label v-if="draft.rule.kind === 'once'" class="grid gap-1 text-xs text-secondary-foreground">At
          <input v-model="draft.due_at" type="datetime-local" :class="field" />
        </label>
        <label v-else-if="draft.rule.kind === 'interval'" class="grid gap-1 text-xs text-secondary-foreground">Minutes (15 or more)
          <input v-model.number="draft.rule.every_min" type="number" min="15" step="5" :class="[field, 'w-28']" />
        </label>
        <template v-else>
          <div class="flex gap-1" role="group" aria-label="Weekdays">
            <button v-for="(name, d) in DAYS" :key="d" type="button" :aria-pressed="draft.rule.weekdays?.includes(d)"
              class="h-8 w-10 rounded-[var(--radius-button)] border text-xs"
              :class="draft.rule.weekdays?.includes(d) ? 'border-accent bg-selected text-foreground' : 'border-border text-muted-foreground hover:bg-hover'"
              @click="toggleDay(d)">{{ name }}</button>
          </div>
          <label class="grid gap-1 text-xs text-secondary-foreground">At
            <input v-model="draft.rule.time_of_day" type="time" :class="field" />
          </label>
        </template>
      </div>
      <p v-if="draft.rule.kind === 'weekly' && !draft.rule.weekdays?.length" class="m-0 text-[11px] text-muted-foreground">No day picked: runs every day.</p>
      <div class="flex items-center gap-2">
        <button type="button" :disabled="!draft.text.trim()" class="rounded-[var(--radius-button)] bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground disabled:opacity-40" @click="save">{{ draft.id ? "Save" : "Create" }}</button>
        <button type="button" :class="btn" @click="draft = null">Cancel</button>
      </div>
    </div>

    <!-- List -->
    <p v-if="!items.length && !draft" class="m-0 text-xs text-muted-foreground">Nothing scheduled. Agents schedule with <code>burrow schedule</code>; create one here with New task.</p>
    <div v-for="s in items" :key="s.id" class="flex items-start gap-3 rounded-lg border border-border px-3 py-2.5" :class="s.paused && 'opacity-60'">
      <div class="min-w-0 flex-1">
        <div class="truncate text-[13px] text-foreground" :title="s.text">{{ s.name || s.text }}</div>
        <div class="mt-0.5 truncate text-[11px] text-muted-foreground">
          {{ targetLabel(s) }} · {{ s.recurs }} · {{ s.paused ? "Paused" : `next ${when(s.due_at)}` }}<template v-if="s.last_run_at"> · last {{ when(s.last_run_at) }}</template>
        </div>
        <div v-if="s.last_error" class="mt-0.5 truncate text-[11px] text-warning" :title="s.last_error">{{ s.last_error }}</div>
      </div>
      <div class="flex shrink-0 gap-1">
        <button type="button" :class="btn" @click="edit(s)">Edit</button>
        <button type="button" :class="btn" @click="act('schedule_set_paused', { id: s.id, paused: !s.paused })">{{ s.paused ? "Resume" : "Pause" }}</button>
        <button type="button" :class="btn" :disabled="s.paused" @click="act('schedule_run_now', { id: s.id })">Run now</button>
        <button type="button" :class="btn" @click="act('chat_schedule_cancel', { id: s.id })">Delete</button>
      </div>
    </div>
  </div>
</template>
