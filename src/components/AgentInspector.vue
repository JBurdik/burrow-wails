<script setup lang="ts">
import { computed } from "vue";
import {
  PhArrowSquareOut,
  PhChatTeardropText,
  PhFolder,
  PhStop,
  PhTerminal,
  PhX,
} from "@phosphor-icons/vue";
import { spinnerFrame } from "@/lib/spinner";
import type { TermStatus } from "@/lib/terminalStatus";

export type AgentInspectorStatus = TermStatus | "stopped";
export type AgentInspectorMode = "popover" | "drawer";

export interface AgentInspectorTerminalIdentity {
  /** Stable terminal/PTY identifier used by the host to focus or stop it. */
  id: string | number;
  /** Optional human-friendly identity, shown in preference to the generated one. */
  label?: string;
  ptyId?: string | number;
  workspaceId?: string | number;
}

export interface AgentInspectorAgent {
  /** The concise agent or task name shown in the inspector heading. */
  title: string;
  status: AgentInspectorStatus;
  cwd?: string | null;
  /** The latest terminal activity or agent message. */
  recentActivity?: string | null;
  /** Optional companion timestamp, already formatted by the caller. */
  recentActivityAt?: string | null;
  terminal?: AgentInspectorTerminalIdentity | null;
}

const props = withDefaults(defineProps<{
  /** Complete display snapshot. The inspector never mutates this object. */
  agent: AgentInspectorAgent;
  /** Lets a host keep the surface mounted while controlling visibility. */
  open?: boolean;
  /** Popovers suit pointer context; drawers suit narrower, persistent locations. */
  mode?: AgentInspectorMode;
  /** Hosts can hide Stop when an agent cannot be interrupted. */
  canStop?: boolean;
}>(), {
  open: true,
  mode: "popover",
  canStop: true,
});

const emit = defineEmits<{
  focus: [agent: AgentInspectorAgent];
  "follow-up": [agent: AgentInspectorAgent];
  stop: [agent: AgentInspectorAgent];
  dismiss: [];
}>();

const statusLabel = computed(() => {
  const labels: Record<AgentInspectorStatus, string> = {
    idle: "Idle",
    running: "Working",
    waiting: "Waiting",
    permission: "Needs permission",
    done: "Done",
    review: "Ready for review",
    error: "Needs attention",
    stopped: "Stopped",
  };
  return labels[props.agent.status];
});

const terminalLabel = computed(() => {
  const terminal = props.agent.terminal;
  if (!terminal) return null;
  if (terminal.label) return terminal.label;

  const details = [`Terminal ${terminal.id}`];
  if (terminal.ptyId !== undefined) details.push(`PTY ${terminal.ptyId}`);
  if (terminal.workspaceId !== undefined) details.push(`Workspace ${terminal.workspaceId}`);
  return details.join(" · ");
});

const canStopAgent = computed(() => props.canStop && props.agent.status === "running");

const statusDotClass = computed(() => {
  const map: Record<AgentInspectorStatus, string> = {
    idle: "bg-muted-foreground",
    running: "",
    waiting: "bg-[var(--status-waiting)]",
    permission: "bg-[var(--status-permission)]",
    done: "bg-[var(--status-review)]",
    review: "bg-[var(--status-review)]",
    error: "bg-[var(--status-error)]",
    stopped: "bg-muted-foreground",
  };
  return map[props.agent.status];
});

function dismissOnEscape(event: KeyboardEvent) {
  if (event.key === "Escape") emit("dismiss");
}
</script>

<template>
  <article
    v-if="open"
    class="box-border flex w-[min(360px,calc(100vw-24px))] flex-col overflow-hidden rounded-lg border border-border bg-panel font-sans text-foreground shadow-[0_14px_32px_color-mix(in_srgb,var(--bg-base)_68%,transparent)] focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:outline-offset-2"
    :class="mode === 'drawer' && 'min-h-full w-[min(400px,100%)] rounded-none shadow-none'"
    role="dialog"
    :aria-label="`${agent.title} inspector`"
    tabindex="-1"
    @keydown="dismissOnEscape"
  >
    <header class="flex min-h-[42px] items-center justify-between gap-3 border-b border-border/72 py-2 pl-3 pr-2.5">
      <div class="flex min-w-0 items-center gap-2">
        <span
          v-if="agent.status === 'running'"
          class="shrink-0 font-mono text-[13px] font-bold leading-none text-[var(--status-running)]"
          aria-hidden="true"
        >{{ spinnerFrame }}</span>
        <span v-else class="h-[7px] w-[7px] shrink-0 rounded-full" :class="statusDotClass" aria-hidden="true" />
        <div class="grid min-w-0 gap-px">
          <h2 class="m-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold leading-tight text-foreground">{{ agent.title }}</h2>
          <span class="text-[10px] leading-tight text-muted-foreground">{{ statusLabel }}</span>
        </div>
      </div>
      <button
        class="flex h-6 w-6 shrink-0 items-center justify-center rounded border-0 bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:outline-offset-1"
        type="button"
        title="Dismiss inspector"
        aria-label="Dismiss inspector"
        @click="emit('dismiss')"
      >
        <PhX :size="14" weight="bold" />
      </button>
    </header>

    <div class="grid gap-2.5 px-3 pb-3 pt-2.5">
      <dl class="m-0 grid gap-1.5">
        <div v-if="agent.cwd" class="grid min-w-0 gap-0.5">
          <dt class="flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-[0.04em] text-muted-foreground"><PhFolder :size="13" /> Working directory</dt>
          <dd class="m-0 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10.5px] leading-snug text-secondary-foreground" :title="agent.cwd">{{ agent.cwd }}</dd>
        </div>
        <div v-if="terminalLabel" class="grid min-w-0 gap-0.5">
          <dt class="flex items-center gap-1.5 text-[9px] font-semibold uppercase tracking-[0.04em] text-muted-foreground"><PhTerminal :size="13" /> Terminal</dt>
          <dd class="m-0 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10.5px] leading-snug text-secondary-foreground" :title="terminalLabel">{{ terminalLabel }}</dd>
        </div>
      </dl>

      <section v-if="agent.recentActivity" class="grid gap-1 rounded-[5px] border border-border/65 bg-base px-2.5 py-2" aria-label="Most recent activity">
        <div class="flex items-center justify-between gap-2.5 text-[9px] font-semibold uppercase tracking-[0.04em] text-muted-foreground">
          <span>Latest activity</span>
          <time v-if="agent.recentActivityAt" class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[9px] font-normal normal-case tracking-normal">{{ agent.recentActivityAt }}</time>
        </div>
        <p class="m-0 line-clamp-3 text-[11px] leading-[1.42] text-secondary-foreground">{{ agent.recentActivity }}</p>
      </section>
    </div>

    <footer class="flex flex-wrap gap-1.5 border-t border-border/72 bg-base/58 px-3 py-2">
      <button
        class="flex min-h-[27px] items-center justify-center gap-1.5 rounded border px-2 py-1 font-sans text-[10.5px] font-medium text-foreground transition-colors hover:border-accent hover:bg-accent/19"
        style="border-color: color-mix(in srgb, var(--accent) 55%, var(--border)); background: color-mix(in srgb, var(--accent) 13%, transparent);"
        type="button"
        @click="emit('focus', agent)"
      >
        <PhArrowSquareOut :size="14" /> Focus
      </button>
      <button
        class="flex min-h-[27px] items-center justify-center gap-1.5 rounded border border-border bg-transparent px-2 py-1 font-sans text-[10.5px] font-medium text-secondary-foreground transition-colors hover:border-muted-foreground hover:bg-hover hover:text-foreground"
        type="button"
        @click="emit('follow-up', agent)"
      >
        <PhChatTeardropText :size="14" /> Follow up
      </button>
      <button
        v-if="canStop"
        class="ml-auto flex min-h-[27px] items-center justify-center gap-1.5 rounded border px-2 py-1 font-sans text-[10.5px] font-medium text-destructive transition-colors hover:bg-destructive/12 disabled:cursor-default disabled:opacity-45"
        style="border-color: color-mix(in srgb, var(--red) 48%, var(--border));"
        type="button"
        :disabled="!canStopAgent"
        :title="canStopAgent ? 'Stop agent' : 'The agent is not running'"
        @click="emit('stop', agent)"
      >
        <PhStop :size="14" weight="fill" /> Stop
      </button>
    </footer>
  </article>
</template>
