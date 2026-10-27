<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhCaretDown, PhCaretRight, PhCheckCircle, PhCircleNotch, PhMinusCircle, PhStop, PhTerminalWindow, PhXCircle,
} from "@phosphor-icons/vue";
import {
  applyBgTaskLine, emptyBgTaskState, listBgTasks, mergeCodexTerminals,
  type BgTask, type BgTaskState, type CodexBgTerminal,
} from "@/lib/backgroundTasks";

// Background shell tasks the active chat's agent launched (Bash
// run_in_background, or a Bash the CLI moved to the background). Derived from
// the chat's stream (src/lib/backgroundTasks.ts); output is the tail of the
// file the CLI writes each task's output to; stop is the CLI's own stop_task.
// A Codex chat's rows are the app-server's background terminals instead
// (`chat_list_bg_terminals`, polled — no stream event marks them); stop goes
// through the same `chat_stop_task`, without output (Codex lists none).
const props = defineProps<{ chatId: number | null }>();

interface StreamLine { ord: number; line: string }

const POLL_MS = 2000;
const TAIL_BYTES = 32 << 10;

let state: BgTaskState = emptyBgTaskState();
let codexRows: BgTask[] = [];
const tasks = shallowRef<BgTask[]>([]);
const expanded = ref<string | null>(null);
const output = reactive<Record<string, string>>({});
const errors = reactive<Record<string, string>>({});
const stopping = reactive<Record<string, boolean>>({});
const now = ref(Date.now());
const outputEl = ref<HTMLElement | null>(null);
function setOutputEl(el: unknown) {
  outputEl.value = (el as HTMLElement | null) ?? null;
}
let timer: ReturnType<typeof setInterval> | undefined;
let polling = false;
let gen = 0;

async function pollLines() {
  const id = props.chatId;
  if (!id || polling) return;
  polling = true;
  const mine = gen;
  try {
    const [lines, live] = await Promise.all([
      invoke<StreamLine[]>("load_chat_task_lines_since", { chatId: id, since: state.lastOrd + 1 }),
      // A failed list (Codex busy past the timeout) must not end live rows.
      invoke<CodexBgTerminal[]>("chat_list_bg_terminals", { chatId: id }).catch(() => null),
    ]);
    if (mine !== gen) return;
    for (const l of lines) applyBgTaskLine(state, l.line, l.ord);
    const codexChanged = live != null && (live.length > 0 || codexRows.some((t) => t.status === "running"));
    if (codexChanged) codexRows = mergeCodexTerminals(codexRows, live ?? []);
    if (lines.length || codexChanged) tasks.value = [...listBgTasks(state), ...codexRows];
  } catch {
    // transient — the next tick retries
  } finally {
    polling = false;
  }
}

async function pollOutput(force = false) {
  const t = tasks.value.find((x) => x.id === expanded.value);
  if (!t?.outputFile) return;
  if (!force && t.status !== "running" && output[t.id] !== undefined) return;
  try {
    const text = stripAnsi(await invoke<string>("read_file_tail", { path: t.outputFile, maxBytes: TAIL_BYTES }));
    const el = outputEl.value;
    const atBottom = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 24;
    output[t.id] = text;
    delete errors[t.id];
    if (atBottom) {
      await nextTick();
      if (outputEl.value) outputEl.value.scrollTop = outputEl.value.scrollHeight;
    }
  } catch (e) {
    errors[t.id] = String(e).includes("no such file") ? "Output file is gone." : String(e);
  }
}

function stripAnsi(s: string): string {
  // eslint-disable-next-line no-control-regex
  return s.replace(/\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07/g, "").replace(/\r(?!\n)/g, "\n");
}

async function tick() {
  now.value = Date.now();
  await pollLines();
  await pollOutput();
}

function toggle(id: string) {
  expanded.value = expanded.value === id ? null : id;
  if (expanded.value) void pollOutput(true);
}

async function stop(t: BgTask) {
  if (!props.chatId) return;
  stopping[t.id] = true;
  delete errors[t.id];
  try {
    await invoke("chat_stop_task", { chatId: props.chatId, taskId: t.id });
    await pollLines();
  } catch (e) {
    errors[t.id] = String(e);
  } finally {
    delete stopping[t.id];
  }
}

function elapsed(t: BgTask): string {
  if (t.startedAt == null) return "";
  const end = t.status === "running" ? now.value : t.endedAt;
  if (end == null) return "";
  const s = Math.max(0, Math.round((end - t.startedAt) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

function statusLabel(t: BgTask): string {
  switch (t.status) {
    case "running": return "running";
    case "completed": return "finished";
    case "failed": return t.exitCode != null ? `exit ${t.exitCode}` : "failed";
    case "killed": return "killed";
    default: return "ended";
  }
}

watch(() => props.chatId, () => {
  gen++;
  state = emptyBgTaskState();
  codexRows = [];
  tasks.value = [];
  expanded.value = null;
  for (const k of Object.keys(output)) delete output[k];
  for (const k of Object.keys(errors)) delete errors[k];
  void pollLines();
}, { immediate: true });
onMounted(() => { timer = setInterval(tick, POLL_MS); });
onBeforeUnmount(() => clearInterval(timer));
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <header class="flex items-center justify-between border-b border-border p-2">
      <span class="flex items-center gap-1.5 font-semibold text-foreground"><PhTerminalWindow :size="14" /> Background</span>
      <span v-if="tasks.length" class="text-muted-foreground">{{ tasks.filter((t) => t.status === "running").length }} running</span>
    </header>
    <div v-if="!props.chatId" class="p-4 text-center leading-relaxed text-muted-foreground">Open a chat.</div>
    <div v-else-if="tasks.length === 0" class="p-4 text-center leading-relaxed text-muted-foreground">Background tasks appear here.</div>
    <div v-else class="flex-1 overflow-auto">
      <div v-for="t in tasks" :key="t.id" class="border-b border-border/65">
        <div class="flex cursor-pointer items-center gap-2 px-2 py-1.5 hover:bg-hover" @click="toggle(t.id)">
          <PhCaretDown v-if="expanded === t.id" :size="11" class="shrink-0 text-muted-foreground" />
          <PhCaretRight v-else :size="11" class="shrink-0 text-muted-foreground" />
          <PhCircleNotch v-if="t.status === 'running'" :size="13" class="shrink-0 animate-spin text-accent" />
          <PhCheckCircle v-else-if="t.status === 'completed'" :size="13" weight="fill" class="shrink-0 text-success" />
          <PhXCircle v-else-if="t.status === 'failed'" :size="13" weight="fill" class="shrink-0 text-destructive" />
          <PhMinusCircle v-else :size="13" class="shrink-0 text-muted-foreground" />
          <span class="min-w-0 flex-1">
            <span class="block truncate text-foreground" :title="t.description || t.command">{{ t.description || t.command || t.id }}</span>
            <span v-if="t.command && t.command !== t.description" class="block truncate font-mono text-muted-foreground" :title="t.command">{{ t.command }}</span>
          </span>
          <span class="shrink-0 text-muted-foreground">{{ statusLabel(t) }}<template v-if="elapsed(t)"> · {{ elapsed(t) }}</template></span>
          <button
            v-if="t.status === 'running'"
            class="inline-flex shrink-0 items-center gap-1 rounded border border-border px-1.5 py-0.5 text-destructive hover:bg-hover disabled:opacity-40"
            :disabled="stopping[t.id]"
            title="Stop this task"
            @click.stop="stop(t)"
          >
            <PhStop :size="11" /> Stop
          </button>
        </div>
        <div v-if="errors[t.id]" class="px-2 pb-1.5 text-destructive">{{ errors[t.id] }}</div>
        <template v-if="expanded === t.id">
          <div v-if="t.cwd != null" class="px-2 pb-2 text-muted-foreground">
            Codex does not expose this process's output.
            <span class="block truncate font-mono" :title="t.cwd">{{ t.cwd }}</span>
          </div>
          <div v-else-if="!t.outputFile" class="px-2 pb-2 text-muted-foreground">No output file reported.</div>
          <pre
            v-else
            :ref="setOutputEl"
            class="mx-2 mb-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded border border-border/65 bg-background p-1.5 font-mono text-[10.5px] leading-snug text-secondary-foreground"
          >{{ output[t.id] || (output[t.id] === "" ? "(no output yet)" : "Loading…") }}</pre>
        </template>
      </div>
    </div>
  </section>
</template>
