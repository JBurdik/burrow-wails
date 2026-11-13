<template>
  <!-- The last turn's end_turn (src/lib/turnNeeds.ts), above the composer once
       the turn has settled: Needs you = answer buttons, Done = Archive + the
       follow-ups the agent suggested + what Burrow found still open. -->
  <div class="needs-card mb-2" :class="isYou ? 'is-you' : 'is-done'">
    <div class="needs-head">
      <component :is="isYou ? PhHandPalm : PhCheckCircle" :size="15" weight="fill" class="shrink-0" :class="isYou ? 'text-[var(--chat-warn)]' : 'text-[var(--chat-success)]'" />
      <span class="font-semibold text-foreground">{{ heading }}</span>
      <span v-if="isYou && asks.length > 1" class="flex items-center gap-0.5 text-muted-foreground">
        <button class="needs-icon" :disabled="current === 0" title="Previous" @click="current--"><PhCaretLeft :size="11" /></button>
        <span class="tabular-nums">{{ current + 1 }} of {{ asks.length }}</span>
        <button class="needs-icon" :disabled="current === asks.length - 1" title="Next" @click="current++"><PhCaretRight :size="11" /></button>
      </span>
      <span class="ml-auto flex items-center gap-1">
        <button v-if="!isYou && !archived" class="needs-btn" @click="emit('archive')"><PhArchive :size="12" /> Archive</button>
        <button class="needs-icon" :title="collapsed ? 'Expand' : 'Collapse'" @click="collapsed = !collapsed">
          <PhCaretDown :size="11" class="transition-transform" :class="{ '-rotate-90': collapsed }" />
        </button>
      </span>
    </div>

    <template v-if="!collapsed">
      <!-- Needs you: one ask at a time -->
      <div v-if="isYou && ask" class="needs-body">
        <p class="whitespace-pre-wrap text-[13px] leading-[1.5] text-foreground">{{ ask.question }}</p>
        <p v-if="ask.multi" class="mt-0.5 text-[11.5px] text-muted-foreground">Pick any that apply.</p>
        <div v-if="ask.run" class="needs-run mt-2">
          <div class="flex items-center gap-2 px-2.5 pt-1.5 text-[11px] text-muted-foreground">
            <PhTerminalWindow :size="12" class="shrink-0" />
            <span class="min-w-0 flex-1 truncate font-mono" :title="cwd">{{ cwd || "chat folder" }}</span>
            <span v-if="ask.run.interrupted && !ask.run.state" class="text-[var(--chat-warn)]">Interrupted by a restart</span>
            <span v-if="ask.run.state === 'done'" :class="ask.run.exit ? 'text-[var(--red)]' : 'text-[var(--chat-success)]'">exit {{ ask.run.exit ?? 0 }} · {{ ask.run.reply ? "goes with your answers" : "result sent" }}</span>
            <button v-else class="needs-btn is-primary !h-6" :disabled="ask.run.state === 'running' || running" @click="runIt">
              <PhPlay :size="11" weight="fill" /> {{ ask.run.state === "running" || running ? "Running…" : "Run" }}
            </button>
          </div>
          <pre class="needs-run-cmd">{{ ask.run.command }}</pre>
          <p v-if="runError" class="px-2.5 pb-1.5 text-[11.5px] text-[var(--red)]">{{ runError }}</p>
        </div>
        <!-- With descriptions the options stack as rows (label + what it leads to);
             bare labels stay compact buttons. -->
        <div v-if="ask.options?.length" class="mt-2 flex gap-1.5" :class="ask.details?.length ? 'flex-col' : 'flex-wrap'">
          <button
            v-for="(o, i) in ask.options"
            :key="i"
            class="needs-option"
            :class="{ picked: isPicked(o), 'is-row': !!ask.details?.length }"
            :title="i === ask.suggested ? 'Recommended' : undefined"
            @click="pick(o)"
          >
            <span class="flex min-w-0 items-center gap-1.5">
              <component :is="isPicked(o) ? PhCheckSquare : PhSquare" v-if="ask.multi" :size="13" class="shrink-0" />
              <PhStar v-if="i === ask.suggested" :size="11" weight="fill" class="shrink-0 text-[var(--chat-warn)]" />
              <span :class="ask.details?.length ? 'font-medium' : ''">{{ o }}</span>
              <span v-if="i === ask.suggested && ask.details?.length" class="needs-rec">Recommended</span>
            </span>
            <span v-if="ask.details?.[i]" class="needs-detail">{{ ask.details[i] }}</span>
          </button>
          <button v-if="!otherOpen[current]" class="needs-option is-other" @click="openOther">Something else…</button>
        </div>
        <input
          v-if="!ask.options?.length || otherOpen[current]"
          ref="otherEl"
          v-model="other[current]"
          class="needs-input mt-2"
          :placeholder="ask.options?.length ? 'Your answer' : 'Answer'"
          @keydown.enter.exact.prevent="commitOther"
          @keydown.meta.enter.prevent="send"
        />
        <div class="mt-2 flex items-center gap-2">
          <span class="text-[11.5px] text-muted-foreground">{{ answeredCount }} of {{ asks.length }} answered<template v-if="composerText.trim()"> · your message goes with it</template></span>
          <button class="needs-btn is-primary ml-auto" :disabled="!answeredCount" @click="send">Send {{ asks.length > 1 ? 'answers' : 'answer' }}</button>
        </div>
      </div>

      <!-- Done: follow-ups + loose ends -->
      <div v-else-if="!isYou" class="needs-body">
        <template v-if="next.length">
          <div class="needs-label">Or carry on</div>
          <button v-for="(s, i) in next" :key="i" class="needs-next" :class="{ picked: picked[i] }" @click="picked[i] = !picked[i]">
            <component :is="picked[i] ? PhCheckSquare : PhSquare" :size="14" class="mt-px shrink-0" />
            <span class="text-left">{{ s }}</span>
          </button>
          <div v-if="pickedNext.length" class="mt-1.5 flex justify-end">
            <button class="needs-btn is-primary" @click="sendNext">Send {{ pickedNext.length > 1 ? `${pickedNext.length} follow-ups` : 'follow-up' }}</button>
          </div>
        </template>
        <div class="needs-label" :class="{ 'mt-2': next.length }">Still open</div>
        <p v-if="loose === null" class="text-[12px] text-muted-foreground">Checking…</p>
        <p v-else-if="!loose.length" class="text-[12px] text-muted-foreground">Nothing left open — safe to archive.</p>
        <template v-else>
          <component
            :is="l.chat_id ? 'button' : 'div'"
            v-for="(l, i) in loose"
            :key="i"
            class="needs-loose"
            :class="{ 'is-link': !!l.chat_id }"
            @click="l.chat_id && emit('openChat', l.chat_id)"
          >
            <component :is="LOOSE_ICONS[l.kind] ?? PhDotOutline" :size="13" class="shrink-0 text-[var(--chat-warn)]" />
            <span>{{ l.text }}</span>
          </component>
        </template>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhArchive, PhCaretDown, PhCaretLeft, PhCaretRight, PhCheckCircle, PhCheckSquare, PhClock, PhDotOutline,
  PhGitBranch, PhGitPullRequest, PhHandPalm, PhPencilSimple, PhPlay, PhSquare, PhStar, PhTerminalWindow, PhUsersThree,
} from "@phosphor-icons/vue";
import { composeAnswers, composeNext, type LooseEnd, type TurnNeeds } from "@/lib/turnNeeds";

const props = defineProps<{ needs: TurnNeeds; chatId: number; archived: boolean; composerText: string; cwd?: string }>();
const emit = defineEmits<{ send: [text: string]; archive: []; openChat: [id: number] }>();

const LOOSE_ICONS: Record<string, unknown> = {
  uncommitted: PhPencilSimple,
  unpushed: PhGitBranch,
  pr: PhGitPullRequest,
  subagent: PhUsersThree,
  schedule: PhClock,
};

const isYou = computed(() => props.needs.needs === "you");
const asks = computed(() => props.needs.asks ?? []);
const next = computed(() => props.needs.next ?? []);
const heading = computed(() => {
  if (!isYou.value) return "Done";
  return asks.value.length > 0 && asks.value.every((a) => a.options?.length) ? "Decisions needed" : "Needs you";
});

const collapsed = ref(false);
const current = ref(0);
const answers = ref<(string | undefined)[]>([]);
const other = ref<string[]>([]);
const otherOpen = ref<boolean[]>([]);
const otherEl = ref<HTMLInputElement | null>(null);
const picked = ref<boolean[]>([]);
const loose = ref<LooseEnd[] | null>(null);
const running = ref(false);
const runError = ref("");

const ask = computed(() => asks.value[current.value]);
// A run whose result waits on the card counts as answered: it goes with the rest.
const answeredCount = computed(() => asks.value.filter((a, i) => answers.value[i]?.trim() || a.run?.reply).length);
const pickedNext = computed(() => next.value.filter((_, i) => picked.value[i]));

// A new end_turn (or another chat) starts clean. Keyed on the questions, not
// the whole card: a run finishing rewrites the card's JSON (state, exit,
// reply) and must not throw away the answers already picked.
watch(
  () => [props.chatId, props.needs.needs, JSON.stringify((props.needs.asks ?? []).map((a) => [a.question, a.options])), JSON.stringify(props.needs.next ?? [])],
  () => {
    collapsed.value = false;
    current.value = 0;
    answers.value = [];
    other.value = [];
    otherOpen.value = [];
    picked.value = [];
    loose.value = null;
    running.value = false;
    runError.value = "";
    if (!isYou.value) void loadLoose();
  },
  { immediate: true },
);

async function loadLoose() {
  const id = props.chatId;
  try {
    const items = await invoke<LooseEnd[]>("chat_loose_ends", { chatId: id });
    if (id === props.chatId) loose.value = items ?? [];
  } catch {
    if (id === props.chatId) loose.value = [];
  }
}

function advance() {
  const nextOpen = asks.value.findIndex((_, i) => i > current.value && !answers.value[i]?.trim());
  const anyOpen = nextOpen >= 0 ? nextOpen : asks.value.findIndex((_, i) => !answers.value[i]?.trim());
  if (anyOpen >= 0) current.value = anyOpen;
}

function isPicked(o: string): boolean {
  const a = answers.value[current.value];
  return ask.value?.multi ? (a ?? "").split(", ").includes(o) : a === o;
}

function pick(o: string) {
  if (ask.value?.multi) {
    // Several may be picked: toggle this one, stay on the question.
    const picked = new Set((answers.value[current.value] ?? "").split(", ").filter(Boolean));
    if (picked.has(o)) picked.delete(o);
    else picked.add(o);
    const order = ask.value.options ?? [];
    answers.value[current.value] = order.filter((x) => picked.has(x)).join(", ") || undefined;
    return;
  }
  answers.value[current.value] = answers.value[current.value] === o ? undefined : o;
  otherOpen.value[current.value] = false;
  if (answers.value[current.value]) advance();
}

async function openOther() {
  otherOpen.value[current.value] = true;
  answers.value[current.value] = other.value[current.value] || undefined;
  await nextTick();
  otherEl.value?.focus();
}

function commitOther() {
  answers.value[current.value] = other.value[current.value]?.trim() || undefined;
  // The last answer sends the lot.
  if (answeredCount.value === asks.value.length) return send();
  advance();
}

// Go runs the command stored on the card (not this text) once, in the chat's
// folder, and queues its output back to the agent; the card shows the exit.
async function runIt() {
  running.value = true;
  runError.value = "";
  try {
    await invoke("chat_run_ask", { chatId: props.chatId, index: current.value });
  } catch (e) {
    runError.value = String(e);
  } finally {
    running.value = false;
  }
}

function send() {
  if (!answeredCount.value) return;
  emit("send", composeAnswers(asks.value, answers.value, props.composerText));
}

function sendNext() {
  if (pickedNext.value.length) emit("send", composeNext(pickedNext.value, props.composerText));
}
</script>

<style scoped>
.needs-card { border: 1px solid var(--chat-border); border-radius: 14px; overflow: hidden; background: color-mix(in srgb, var(--chat-text) 2%, var(--chat-bg)); }
.needs-card.is-you { border-color: color-mix(in srgb, var(--chat-warn) 45%, var(--chat-border)); }
.needs-head { display: flex; align-items: center; gap: 8px; padding: 8px 10px 8px 14px; font-size: 12.5px; }
.needs-body { padding: 2px 14px 12px; }
.needs-label { margin-bottom: 4px; font-size: 11px; font-weight: 600; color: var(--chat-text-secondary); }
.needs-icon { display: inline-flex; align-items: center; justify-content: center; width: 20px; height: 20px; border-radius: 6px; color: var(--chat-text-secondary); }
.needs-icon:hover:not(:disabled) { background: color-mix(in srgb, var(--chat-text) 7%, transparent); color: var(--chat-text); }
.needs-icon:disabled { opacity: 0.35; }
.needs-btn { display: inline-flex; align-items: center; gap: 5px; height: 26px; padding: 0 10px; border-radius: 8px; font-size: 12px; color: var(--chat-text); border: 1px solid var(--chat-border); }
.needs-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--chat-text) 6%, transparent); }
.needs-btn:disabled { opacity: 0.45; }
.needs-btn.is-primary { border-color: transparent; background: var(--chat-text); color: var(--chat-bg); font-weight: 500; }
.needs-btn.is-primary:hover:not(:disabled) { background: var(--chat-text); opacity: 0.88; }
.needs-option { display: inline-flex; align-items: center; gap: 5px; padding: 5px 11px; border-radius: 8px; font-size: 12.5px; text-align: left; color: var(--chat-text); border: 1px solid var(--chat-border); background: var(--chat-bg); }
.needs-option:hover { border-color: color-mix(in srgb, var(--chat-text) 35%, var(--chat-border)); }
.needs-option.picked { border-color: var(--chat-info); background: color-mix(in srgb, var(--chat-info) 14%, var(--chat-bg)); }
.needs-option.is-other { color: var(--chat-text-secondary); border-style: dashed; }
.needs-option.is-row { display: flex; flex-direction: column; align-items: stretch; gap: 2px; padding: 7px 11px; width: 100%; }
.needs-option.is-other:only-child, .flex-col > .needs-option.is-other { align-self: flex-start; }
.needs-detail { font-size: 12px; line-height: 1.4; color: var(--chat-text-secondary); }
.needs-rec { padding: 0 6px; border-radius: 5px; font-size: 10.5px; font-weight: 500; color: var(--chat-warn); background: color-mix(in srgb, var(--chat-warn) 14%, transparent); }
.needs-input { width: 100%; height: 30px; padding: 0 10px; border-radius: 8px; font-size: 12.5px; color: var(--chat-text); border: 1px solid var(--chat-border); background: var(--chat-bg); outline: none; }
.needs-input:focus { border-color: var(--chat-info); }
.needs-next { display: flex; width: 100%; align-items: flex-start; gap: 8px; padding: 4px 0; font-size: 12.5px; line-height: 1.45; color: var(--chat-text-secondary); }
.needs-next:hover, .needs-next.picked { color: var(--chat-text); }
.needs-run { border: 1px solid var(--chat-border); border-radius: 10px; background: var(--chat-bg); overflow: hidden; }
.needs-run-cmd { margin: 0; padding: 6px 10px 8px; max-height: 160px; overflow: auto; font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all; color: var(--chat-text); }
.needs-loose { display: flex; width: 100%; align-items: center; gap: 7px; padding: 2px 0; font-size: 12.5px; color: var(--chat-text); text-align: left; }
.needs-loose.is-link:hover { text-decoration: underline; }
</style>
