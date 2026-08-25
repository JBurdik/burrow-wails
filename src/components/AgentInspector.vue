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

function dismissOnEscape(event: KeyboardEvent) {
  if (event.key === "Escape") emit("dismiss");
}
</script>

<template>
  <article
    v-if="open"
    class="agent-inspector"
    :class="`agent-inspector--${mode}`"
    role="dialog"
    :aria-label="`${agent.title} inspector`"
    tabindex="-1"
    @keydown="dismissOnEscape"
  >
    <header class="agent-inspector__header">
      <div class="agent-inspector__heading">
        <span class="agent-inspector__status" :class="`status-${agent.status}`" aria-hidden="true">
          {{ agent.status === "running" ? spinnerFrame : "" }}
        </span>
        <div class="agent-inspector__title-group">
          <h2 class="agent-inspector__title">{{ agent.title }}</h2>
          <span class="agent-inspector__state">{{ statusLabel }}</span>
        </div>
      </div>
      <button class="agent-inspector__icon-button" type="button" title="Dismiss inspector" aria-label="Dismiss inspector" @click="emit('dismiss')">
        <PhX :size="14" weight="bold" />
      </button>
    </header>

    <div class="agent-inspector__content">
      <dl class="agent-inspector__details">
        <div v-if="agent.cwd" class="agent-inspector__detail">
          <dt class="agent-inspector__detail-label"><PhFolder :size="13" /> Working directory</dt>
          <dd class="agent-inspector__detail-value agent-inspector__detail-value--mono" :title="agent.cwd">{{ agent.cwd }}</dd>
        </div>
        <div v-if="terminalLabel" class="agent-inspector__detail">
          <dt class="agent-inspector__detail-label"><PhTerminal :size="13" /> Terminal</dt>
          <dd class="agent-inspector__detail-value agent-inspector__detail-value--mono" :title="terminalLabel">{{ terminalLabel }}</dd>
        </div>
      </dl>

      <section v-if="agent.recentActivity" class="agent-inspector__activity" aria-label="Most recent activity">
        <div class="agent-inspector__activity-meta">
          <span>Latest activity</span>
          <time v-if="agent.recentActivityAt">{{ agent.recentActivityAt }}</time>
        </div>
        <p class="agent-inspector__activity-message">{{ agent.recentActivity }}</p>
      </section>
    </div>

    <footer class="agent-inspector__actions">
      <button class="agent-inspector__button agent-inspector__button--primary" type="button" @click="emit('focus', agent)">
        <PhArrowSquareOut :size="14" /> Focus
      </button>
      <button class="agent-inspector__button" type="button" @click="emit('follow-up', agent)">
        <PhChatTeardropText :size="14" /> Follow up
      </button>
      <button
        v-if="canStop"
        class="agent-inspector__button agent-inspector__button--stop"
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

<style scoped>
.agent-inspector {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  width: min(360px, calc(100vw - 24px));
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-panel);
  box-shadow: 0 14px 32px color-mix(in srgb, var(--bg-base) 68%, transparent);
  color: var(--text-primary);
  font-family: var(--font-ui);
}

.agent-inspector--drawer {
  width: min(400px, 100%);
  min-height: 100%;
  border-radius: 0;
  box-shadow: none;
}

.agent-inspector:focus-visible {
  outline: 1px solid var(--accent);
  outline-offset: 2px;
}

.agent-inspector__header,
.agent-inspector__heading,
.agent-inspector__actions,
.agent-inspector__detail-label,
.agent-inspector__activity-meta,
.agent-inspector__button,
.agent-inspector__icon-button {
  display: flex;
  align-items: center;
}

.agent-inspector__header {
  justify-content: space-between;
  gap: 12px;
  min-height: 42px;
  padding: 8px 9px 8px 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
}

.agent-inspector__heading {
  min-width: 0;
  gap: 8px;
}

.agent-inspector__status {
  width: 7px;
  height: 7px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--text-muted);
}

.agent-inspector__status.status-running {
  width: auto;
  height: auto;
  border-radius: 0;
  background: transparent;
  color: var(--status-running);
  font-family: var(--font-mono);
  font-size: 13px;
  font-weight: 700;
  line-height: 1;
}

.agent-inspector__status.status-waiting { background: var(--status-waiting); }
.agent-inspector__status.status-permission { background: var(--status-permission); }
.agent-inspector__status.status-done,
.agent-inspector__status.status-review { background: var(--status-review); }
.agent-inspector__status.status-error { background: var(--status-error); }
.agent-inspector__status.status-stopped { background: var(--text-muted); }

.agent-inspector__title-group {
  display: grid;
  min-width: 0;
  gap: 1px;
}

.agent-inspector__title {
  overflow: hidden;
  margin: 0;
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-inspector__state {
  color: var(--text-muted);
  font-size: 10px;
  line-height: 1.2;
}

.agent-inspector__icon-button {
  justify-content: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}

.agent-inspector__icon-button:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.agent-inspector__icon-button:focus-visible,
.agent-inspector__button:focus-visible {
  outline: 1px solid var(--accent);
  outline-offset: 1px;
}

.agent-inspector__content {
  display: grid;
  gap: 10px;
  padding: 10px 12px 12px;
}

.agent-inspector__details {
  display: grid;
  gap: 7px;
  margin: 0;
}

.agent-inspector__detail {
  display: grid;
  gap: 3px;
  min-width: 0;
}

.agent-inspector__detail-label {
  gap: 5px;
  color: var(--text-muted);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.agent-inspector__detail-value {
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 10.5px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-inspector__detail-value--mono {
  font-family: var(--font-mono);
}

.agent-inspector__activity {
  display: grid;
  gap: 5px;
  padding: 8px 9px;
  border: 1px solid color-mix(in srgb, var(--border) 65%, transparent);
  border-radius: 5px;
  background: var(--bg-base);
}

.agent-inspector__activity-meta {
  justify-content: space-between;
  gap: 10px;
  color: var(--text-muted);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.agent-inspector__activity-meta time {
  overflow: hidden;
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 400;
  letter-spacing: 0;
  text-overflow: ellipsis;
  text-transform: none;
  white-space: nowrap;
}

.agent-inspector__activity-message {
  display: -webkit-box;
  margin: 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 11px;
  line-height: 1.42;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.agent-inspector__actions {
  gap: 6px;
  padding: 8px 12px;
  border-top: 1px solid color-mix(in srgb, var(--border) 72%, transparent);
  background: color-mix(in srgb, var(--bg-base) 58%, transparent);
}

.agent-inspector__button {
  justify-content: center;
  gap: 5px;
  min-height: 27px;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font: 500 10.5px var(--font-ui);
  transition: background 120ms ease-out, border-color 120ms ease-out, color 120ms ease-out;
}

.agent-inspector__button:hover:not(:disabled) {
  border-color: var(--text-muted);
  background: var(--bg-hover);
  color: var(--text-primary);
}

.agent-inspector__button--primary {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 13%, transparent);
  color: var(--text-primary);
}

.agent-inspector__button--primary:hover:not(:disabled) {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 19%, transparent);
}

.agent-inspector__button--stop {
  margin-left: auto;
  border-color: color-mix(in srgb, var(--red) 48%, var(--border));
  color: var(--red);
}

.agent-inspector__button--stop:hover:not(:disabled) {
  border-color: var(--red);
  background: color-mix(in srgb, var(--red) 12%, transparent);
  color: var(--red);
}

.agent-inspector__button:disabled {
  opacity: 0.45;
  cursor: default;
}

@media (max-width: 420px) {
  .agent-inspector--popover {
    width: 100%;
  }

  .agent-inspector__actions {
    flex-wrap: wrap;
  }
}
</style>
