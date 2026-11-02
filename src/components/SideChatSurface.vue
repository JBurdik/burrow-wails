<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { PhChatsCircle, PhPlus, PhArchive } from "@phosphor-icons/vue";
import AgentChat from "./AgentChat.vue";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { canSideChat, sideChatsOf } from "@/stores/chatTree";
import { newSideChat, sideChatSelection } from "@/lib/sideChat";

// Side chats of the active chat: quick questions asked on a fork of its
// current agent session (src-wails/chatside.go), so the chat itself — even a
// running turn — is never disturbed. The view is a compact AgentChat; the
// side chat's process is the Go chat runtime's like any chat's.
const props = defineProps<{ chatId: number | null; cwd: string }>();

const chats = useClaudeChatsStore();
const parent = computed(() => (props.chatId ? chats.sessions.find((s) => s.id === props.chatId) : undefined));
const supported = computed(() => canSideChat(parent.value));
const list = computed(() => (props.chatId ? sideChatsOf(chats.sessions, props.chatId) : []));
const current = computed(() => {
  if (!props.chatId) return undefined;
  const picked = sideChatSelection[props.chatId];
  return list.value.find((c) => c.id === picked) ?? list.value[0];
});
const selectedId = computed({
  get: () => current.value?.id,
  set: (id) => { if (props.chatId && id) sideChatSelection[props.chatId] = id; },
});

const error = ref("");
const creating = ref(false);
watch(() => props.chatId, () => { error.value = ""; });

async function create() {
  if (!props.chatId || creating.value) return;
  creating.value = true;
  error.value = "";
  try {
    await newSideChat(props.chatId);
  } catch (e) {
    error.value = String(e);
  } finally {
    creating.value = false;
  }
}

async function archiveCurrent() {
  if (current.value) await chats.archive(current.value.id);
}
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <header class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
      <PhChatsCircle :size="14" class="shrink-0 text-foreground" />
      <select
        v-if="list.length > 1"
        v-model="selectedId"
        class="min-w-0 flex-1 truncate bg-transparent font-semibold text-foreground outline-none"
        aria-label="Side chats of this chat"
      >
        <option v-for="c in list" :key="c.id" :value="c.id">{{ c.title || `Side chat ${c.id}` }}</option>
      </select>
      <span v-else class="min-w-0 flex-1 truncate font-semibold text-foreground">{{ current?.title || "Side chat" }}</span>
      <button
        v-if="current"
        class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground"
        title="Archive this side chat"
        aria-label="Archive this side chat"
        @click="archiveCurrent"
      ><PhArchive :size="12" /></button>
      <button
        v-if="supported"
        class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground disabled:opacity-50"
        title="New side chat from the chat's current state"
        aria-label="New side chat"
        :disabled="creating"
        @click="create"
      ><PhPlus :size="12" /></button>
    </header>

    <div v-if="error" class="mx-2 mt-2 rounded-[var(--radius-card)] border border-destructive/40 px-3 py-2 text-destructive">{{ error }}</div>

    <div v-if="!parent" class="p-4 text-center leading-relaxed text-muted-foreground">Open a chat to ask a side question about it.</div>
    <div v-else-if="!supported" class="p-4 text-center leading-relaxed text-muted-foreground">Side chats need a Claude or Codex chat.</div>
    <AgentChat
      v-else-if="current"
      :key="current.id"
      compact
      class="min-h-0 flex-1"
      :chat-id="current.id"
      :workspace-id="current.workspaceId"
      :cwd="props.cwd"
      :agent-kind="current.agentKind"
      is-watching
    />
    <div v-else class="flex flex-col items-center gap-3 p-4 text-center leading-relaxed text-muted-foreground">
      <span>Ask a quick question about this chat. The side chat starts from everything the chat knows right now and leaves the chat itself alone, even mid-turn.</span>
      <button
        class="rounded-[var(--radius-nav)] border border-border px-3 py-1.5 text-foreground hover:bg-hover disabled:opacity-50"
        :disabled="creating"
        @click="create"
      >Start a side chat</button>
    </div>
  </section>
</template>
