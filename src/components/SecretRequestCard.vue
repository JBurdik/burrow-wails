<template>
  <!-- An agent's secret request (src-wails/secretrequests.go). The typed value
       lives only in `value` below until it is sent, then is cleared: no
       localStorage, no draft, no transcript row. The agent gets a file path. -->
  <form
    v-if="req"
    class="secret-card flex flex-col gap-2 border-warning/30 bg-warning/10 px-3.5 py-2.5"
    :class="banner ? 'shrink-0 border-b' : 'mb-2 rounded-xl border'"
    role="alert"
    autocomplete="off"
    @mousedown.stop
    @submit.prevent="submit"
  >
    <div class="flex items-start gap-2">
      <PhKey :size="17" weight="fill" class="mt-px shrink-0 text-warning" />
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="text-[12.5px] font-medium text-foreground">
          Agent requests a secret: <code class="font-mono">{{ req.name }}</code>
          <span v-if="more" class="ml-1.5 rounded-full bg-warning/20 px-2 py-0.5 text-[11px] font-medium text-warning">+{{ more }}</span>
        </span>
        <span v-if="req.reason" class="text-[12px] text-secondary-foreground">{{ req.reason }}</span>
        <span class="text-[11px] text-muted-foreground">The agent only gets a file path; the file is deleted when its turn ends.</span>
      </div>
    </div>

    <div v-if="req.hasSaved" class="flex flex-wrap items-center gap-2">
      <span class="text-[12px] text-secondary-foreground">Saved value available</span>
      <button type="button" class="h-8 rounded-lg bg-warning px-3 text-[12px] font-medium text-black/85 hover:opacity-90 disabled:opacity-50" :disabled="busy" @click="useSaved">Use saved</button>
    </div>

    <div class="flex flex-wrap items-center gap-2">
      <input
        ref="inputEl"
        v-model="value"
        type="password"
        name="burrow-agent-secret"
        autocomplete="new-password"
        data-1p-ignore
        data-lpignore="true"
        data-form-type="other"
        enterkeyhint="send"
        spellcheck="false"
        :placeholder="req.hasSaved ? 'Or enter a new value' : 'Secret value'"
        class="h-8 min-w-0 flex-1 basis-[200px] rounded-lg border border-border bg-[var(--bg-panel)] px-2.5 font-mono text-[12.5px] text-foreground outline-none focus:border-warning"
        @focus="keepVisible"
      />
      <div class="ml-auto flex shrink-0 items-center gap-1">
        <button type="button" class="h-8 rounded-lg px-2.5 text-[12px] text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-50" :disabled="busy" @click="decline">Decline</button>
        <button type="submit" class="h-8 rounded-lg px-3 text-[12px] font-medium disabled:opacity-50" :class="req.hasSaved ? 'border border-warning/50 text-foreground hover:bg-hover' : 'bg-warning text-black/85 hover:opacity-90'" :disabled="busy || !value">Submit</button>
      </div>
    </div>

    <label v-if="req.repo_id > 0" class="flex w-fit cursor-pointer items-center gap-1.5 text-[12px] text-secondary-foreground">
      <input v-model="remember" type="checkbox" class="accent-[var(--warning,currentColor)]" />
      Remember for this project
    </label>
    <span v-if="error" class="text-[12px] text-[var(--red)]">{{ error }}</span>
  </form>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { PhKey } from "@phosphor-icons/vue";
import { useSecretRequestsStore } from "@/stores/secretRequests";

// ownerKey: "chat:<id>" or "pty:<id>". banner = flat strip above a terminal.
const props = defineProps<{ ownerKey: string; banner?: boolean }>();
const store = useSecretRequestsStore();
onMounted(() => store.start());

const queue = computed(() => store.byOwner[props.ownerKey] ?? []);
const req = computed(() => queue.value[0] ?? null);
const more = computed(() => Math.max(0, queue.value.length - 1));

const value = ref("");
const remember = ref(false);
const busy = ref(false);
const error = ref("");
const inputEl = ref<HTMLInputElement | null>(null);

// A new request starts clean; with a saved value, Remember defaults on so a
// newly typed value replaces it.
watch(
  () => req.value?.id,
  () => {
    value.value = "";
    error.value = "";
    remember.value = !!req.value?.hasSaved;
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  value.value = "";
});

async function run(fn: () => Promise<void>) {
  busy.value = true;
  error.value = "";
  try {
    await fn();
  } catch (e) {
    error.value = String(e);
  } finally {
    value.value = "";
    busy.value = false;
  }
}

function submit() {
  const r = req.value;
  if (!r || !value.value || busy.value) return;
  const v = value.value;
  value.value = "";
  void run(() => store.answer(r, v, { remember: remember.value }));
}

function useSaved() {
  const r = req.value;
  if (r) void run(() => store.answer(r, "", { useSaved: true }));
}

function decline() {
  const r = req.value;
  if (r) void run(() => store.decline(r));
}

// Phone: the soft keyboard covers the lower half; keep the card in view.
function keepVisible() {
  setTimeout(() => inputEl.value?.closest("form")?.scrollIntoView({ block: "nearest", behavior: "smooth" }), 300);
}
</script>
