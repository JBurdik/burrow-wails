<template>
  <!-- Every thread waiting on an answer (end_turn needs "you"), as its card:
       answer here and it goes to that thread exactly as if typed there —
       queued in Go (chat_queue_add), delivered at once since the turn has
       settled. Nothing runs or is relayed besides that one message. -->
  <section v-if="items.length" class="decisions">
    <div class="decisions-head">
      <PhHandPalm :size="12" weight="fill" class="text-[var(--chat-warn)]" />
      Decisions <span class="opacity-60">{{ items.length }}</span>
    </div>
    <div v-for="s in items" :key="s.id" class="decision">
      <button type="button" class="decision-title" :title="`Open “${s.title}”`" @click="emit('open', s)">
        <span class="truncate font-medium text-foreground">{{ s.title || "Chat" }}</span>
        <span class="shrink-0 truncate text-muted-foreground">{{ wsName(s.workspaceId) }}</span>
        <PhArrowSquareOut :size="11" class="shrink-0 text-muted-foreground" />
      </button>
      <p v-if="sending.has(s.id)" class="px-1 pb-2 text-[12px] text-muted-foreground">Sending…</p>
      <TurnNeedsCard
        v-else
        :needs="s.turnNeeds!"
        :chat-id="s.id"
        :archived="false"
        composer-text=""
        @send="(text) => answer(s.id, text)"
        @open-chat="(id) => emit('open', s, id)"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhArrowSquareOut, PhHandPalm } from "@phosphor-icons/vue";
import TurnNeedsCard from "@/components/TurnNeedsCard.vue";
import { useClaudeChatsStore, type ClaudeSession } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";

/** open(session, subagentId?): the host shows the thread (or that sub-agent). */
const emit = defineEmits<{ open: [session: ClaudeSession, subagentId?: number] }>();
const props = defineProps<{ repoId?: number | null }>();

const chats = useClaudeChatsStore();
const ws = useWorkspaceStore();

const sending = reactive(new Set<number>());
const items = computed(() =>
  chats.sessions
    .filter((s) => s.turnNeeds?.needs === "you" && !s.archivedAt && s.kind !== "side" && s.status !== "running")
    .filter((s) => props.repoId == null || repoOf(s.workspaceId) === props.repoId)
    .sort((a, b) => (b.lastActivityAt ?? 0) - (a.lastActivityAt ?? 0)),
);

function repoOf(wsId: number): number {
  const w = ws.workspaces.find((x) => x.id === wsId);
  return w?.parent_id ?? wsId;
}
function wsName(wsId: number): string {
  return ws.workspaces.find((x) => x.id === wsId)?.name ?? "";
}

async function answer(chatId: number, text: string) {
  sending.add(chatId);
  try {
    await invoke("chat_queue_add", { chatId, text, images: [] });
  } catch {
    sending.delete(chatId); // the card comes back to retry
    return;
  }
  // The new turn clears turn_needs (Go) and the card drops out; until then
  // "Sending…" holds its place.
  setTimeout(() => sending.delete(chatId), 15_000);
}
</script>

<style scoped>
.decisions {
  --chat-bg: var(--bg-base, #0f0f11);
  --chat-border: var(--border, rgba(255, 255, 255, 0.08));
  --chat-text: var(--text-primary, rgba(255, 255, 255, 0.88));
  --chat-text-secondary: var(--text-secondary, rgba(255, 255, 255, 0.6));
  --chat-warn: var(--yellow, #f59e0b);
  --chat-success: var(--green, #10b981);
  --chat-info: var(--blue, #60a5fa);
}
.decisions-head { display: flex; align-items: center; gap: 6px; padding: 10px 8px 4px; font-size: 11px; font-weight: 500; color: var(--text-muted); }
.decision { padding: 0 2px; }
.decision-title { display: flex; width: 100%; align-items: center; gap: 6px; padding: 4px 6px; font-size: 12px; text-align: left; border-radius: 6px; }
.decision-title:hover { background: var(--bg-hover); }
</style>
