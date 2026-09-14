<template>
  <!-- No visible chrome of its own: each child renders through AgentChat,
       either hidden here or teleported into the Right Panel's slot. -->
  <template v-for="child in children" :key="child.id">
    <Teleport to="#subagent-slot" :disabled="subAgentViewTarget !== child.id">
      <AgentChat
        v-show="subAgentViewTarget === child.id"
        :chat-id="child.id"
        :workspace-id="child.workspaceId"
        :cwd="cwdOf(child.workspaceId)"
        :agent-kind="child.agentKind"
        :is-watching="subAgentViewTarget === child.id"
        :initial-prompt="initialPrompts[child.id]"
        @prompt-sent="initialPrompts[child.id] = undefined"
      />
    </Teleport>
  </template>
</template>

<script setup lang="ts">
// Mounted once, always-on, beside RightPanel in App.vue.
//
// A sub-agent's CLI process starts when its AgentChat mounts (claude_start /
// listenClaude / the ACP branch all live in that component's onMounted — see
// AgentChat.vue:2860-3015), so a child chat that nobody mounts never starts
// working. This host is what mounts it: for every child chat belonging to a
// workspace the user has open, it keeps one AgentChat alive for the chat's
// entire life, whether or not the Right Panel (task 9) is currently showing
// it. Closing/reopening the panel therefore never restarts a child — the
// <Teleport> just moves the same instance in and out of the DOM slot the
// panel exposes.
import { computed, reactive, watch } from "vue";
import AgentChat from "@/components/AgentChat.vue";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { takePendingSubagentPrompt } from "@/lib/controlBridge";
import { subAgentViewTarget } from "@/lib/subAgentView";

const chats = useClaudeChatsStore();
const workspace = useWorkspaceStore();

const openWsIds = computed(() => new Set(workspace.opened.map((w) => w.id)));

/** Every sub-agent chat (`parentChatId` set) belonging to a workspace that is
 *  currently open. A closed workspace's children stay unmounted along with
 *  its Terminal — there is nowhere for their PTYs/processes to live either. */
const children = computed(() => chats.sessions.filter((s) => s.parentChatId && openWsIds.value.has(s.workspaceId)));

function cwdOf(workspaceId: number): string {
  return workspace.workspaces.find((w) => w.id === workspaceId)?.path ?? "";
}

// `takePendingSubagentPrompt` is a one-shot read from controlBridge's queue —
// calling it again would return undefined, so the value has to be captured
// into local state the first time a child is seen, not re-read on every
// render from the template.
const initialPrompts = reactive<Record<number, string | undefined>>({});
watch(
  children,
  (list) => {
    for (const c of list) {
      if (!(c.id in initialPrompts)) initialPrompts[c.id] = takePendingSubagentPrompt(c.id);
    }
  },
  { immediate: true },
);
</script>
