<template>
  <!-- The tray toast window (src-wails/desktop.go "Tray toasts"): sized to this
       stack by TrayToastLayout, hidden at zero. Padding leaves room for the shadow. -->
  <div ref="stack" class="flex flex-col gap-2 p-2" @mouseenter="pause(true)" @mouseleave="pause(false)">
    <div
      v-for="t in toasts"
      :key="t.key"
      class="tray-toast group relative flex cursor-pointer items-start gap-2.5 rounded-xl border border-border px-3 py-2.5 text-foreground"
      :class="t.parent && 'tray-toast-sub'"
      @click="open(t)"
    >
      <PhRobot v-if="t.parent" :size="14" weight="fill" class="mt-[2px] shrink-0" :class="t.reason === 'done' ? 'text-[var(--accent)]' : 'text-[var(--yellow)]'" />
      <span v-else class="mt-[5px] size-2 shrink-0 rounded-full" :class="t.reason === 'done' || t.reason === 'snooze' ? 'bg-[var(--blue)]' : 'bg-[var(--yellow)]'" />
      <span class="flex min-w-0 flex-1 flex-col">
        <span class="truncate text-[13px] font-semibold">{{ t.title }}</span>
        <span class="truncate text-[12px] text-secondary-foreground">{{ t.parent ? `Sub-agent · ${t.body}` : t.body }}</span>
        <span v-if="t.parent || t.project" class="truncate text-[11px] text-muted-foreground">{{ t.parent ? `↳ ${t.parent}${t.project ? ` · ${t.project}` : ""}` : t.project }}</span>
      </span>
      <button type="button" class="tray-toast-x invisible shrink-0 group-hover:visible" title="Dismiss" @click.stop="drop(t.key)">
        <PhX :size="12" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Events } from "@wailsio/runtime";
import { PhRobot, PhX } from "@phosphor-icons/vue";
import { TrayOpen, TrayToastLayout, TrayToastReady } from "@/lib/wailsCompat/bindings/burrow/desktopservice";

interface Toast {
  key: string;
  title: string;
  body: string;
  project: string;
  route: string;
  reason: string;
  /** set for a sub-agent: its parent thread's title */
  parent?: string;
}

const MAX = 3;
const LIFETIME_MS = 6000;

const toasts = ref<(Toast & { left: number })[]>([]);
const stack = ref<HTMLElement>();
let paused = false;
let lastTick = Date.now();

function add(t: Toast) {
  // Newest on top; one per chat/tab (a newer notice replaces its older one).
  toasts.value = [{ ...t, left: LIFETIME_MS }, ...toasts.value.filter((x) => x.key !== t.key)].slice(0, MAX);
  void layout();
}

function drop(key: string) {
  toasts.value = toasts.value.filter((t) => t.key !== key);
  void layout();
}

function open(t: Toast) {
  drop(t.key);
  void TrayOpen(t.route);
}

function pause(on: boolean) {
  paused = on;
}

// One clock for the stack: hover pauses every toast, not only the one under the pointer.
const timer = setInterval(() => {
  const now = Date.now();
  const dt = now - lastTick;
  lastTick = now;
  if (paused || !toasts.value.length) return;
  const before = toasts.value.length;
  toasts.value = toasts.value.map((t) => ({ ...t, left: t.left - dt })).filter((t) => t.left > 0);
  if (toasts.value.length !== before) void layout();
}, 250);

async function layout() {
  if (!toasts.value.length) paused = false; // mouseleave never comes once hidden
  await nextTick();
  // CSS px inside #app are scaled by the UI-scale `zoom`; the window is not.
  const zoom = Number(getComputedStyle(document.getElementById("app") ?? document.body).zoom) || 1;
  const h = toasts.value.length && stack.value ? Math.ceil(stack.value.scrollHeight * zoom) : 0;
  await TrayToastLayout(h).catch(() => {});
}

const offs: (() => void)[] = [];
onMounted(() => {
  document.documentElement.style.background = "transparent";
  document.body.style.background = "transparent";
  offs.push(Events.On("tray-toast", (ev) => add(ev.data as Toast)));
  offs.push(Events.On("tray-toast-clear", () => { toasts.value = []; paused = false; }));
  void TrayToastReady();
});
onBeforeUnmount(() => {
  clearInterval(timer);
  offs.forEach((f) => f());
});
</script>

<style scoped>
.tray-toast {
  background: color-mix(in srgb, var(--sidebar-bg) 96%, transparent);
  box-shadow: 0 4px 14px rgb(0 0 0 / 0.28);
}
.tray-toast-sub {
  border-left: 2px solid var(--accent);
}
.tray-toast:hover {
  background: color-mix(in srgb, var(--sidebar-bg) 88%, var(--hover));
}
.tray-toast-x {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 6px;
  color: var(--muted-foreground);
}
.tray-toast-x:hover {
  background: var(--hover);
  color: var(--foreground);
}
</style>
