<template>
  <!-- No visible chrome of its own: each child renders through AgentChat,
       either hidden here or teleported into the Right Panel's slot.
       `compact` for the same reason ManagerPanel uses it — the panel is a
       narrow column, and a chat's changes panel and diff sidebar inside it
       are a second sidebar nested in a sidebar. The surface is a list, one
       child's stream, and the way back. -->

  <!-- Hidden. The child the panel is SHOWING is rendered by the panel itself;
       this host only keeps the others alive. There is no <Teleport> any more:
       Vue resolves a Teleport's target once at mount and caches it, and these
       mount the moment a child chat exists — so the target was routinely
       cached as "not found" and the chat rendered here, beside the panel,
       while the panel showed an empty body. Handing the panel its own
       instance has no such ordering to get wrong. -->
  <div class="hidden">
    <AgentChat
      v-for="child in hostedChildren"
      :key="child.id"
      compact
      :chat-id="child.id"
      :workspace-id="child.workspaceId"
      :cwd="cwdOf(child.workspaceId)"
      :agent-kind="child.agentKind"
      :is-watching="false"
      :initial-prompt="initialPrompts[child.id]"
      @prompt-sent="initialPrompts[child.id] = undefined"
    />
  </div>
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
import { computed, onMounted, reactive, watch } from "vue";
import AgentChat from "@/components/AgentChat.vue";
import { useClaudeChatsStore, takePendingSubagentPrompt, isLocallyCreatedSubagent } from "@/stores/claudeChats";
import { useWorkspaceStore } from "@/stores/workspace";
import { subAgentViewTarget } from "@/lib/subAgentView";

const chats = useClaudeChatsStore();
const workspace = useWorkspaceStore();

const openWsIds = computed(() => new Set(workspace.opened.map((w) => w.id)));

/** Every sub-agent chat (`parentChatId` set) belonging to a workspace that is
 *  currently open. A closed workspace's children stay unmounted along with
 *  its Terminal — there is nowhere for their PTYs/processes to live either.
 *
 *  Also excludes archived children: archive(parent) already stopped their
 *  CLI process (claudeChats.ts's archive()), so keeping one mounted here
 *  would restart it the moment AgentChat.vue mounts — undoing the archive
 *  the user just did. */
const children = computed(() =>
  chats.sessions.filter((s) => s.parentChatId && !s.archivedAt && openWsIds.value.has(s.workspaceId)),
);

/** The children this host renders: everything except the one the Right Panel
 *  is currently showing, which mounts its own instance there.
 *
 *  Two instances of the same chat id would install their reducers over each
 *  other in the shared chat-session registry, so exactly one of us may hold a
 *  given child at a time. Handing it over remounts the view, which is cheap
 *  and safe: the session keeps streaming while unmounted (it is only evicted
 *  when idle) and the transcript replays from `chat_stream`.
 *
 *  ponytail: a child opened in the very first moments after `spawn` — before
 *  its initial prompt has been sent — hands over with the prompt still queued
 *  here, and the prompt is lost; `chat_send` recovers it. Narrow enough to
 *  leave rather than add a second piece of handover state. */
const hostedChildren = computed(() => children.value.filter((c) => c.id !== subAgentViewTarget.value));

function cwdOf(workspaceId: number): string {
  return workspace.workspaces.find((w) => w.id === workspaceId)?.path ?? "";
}

// `takePendingSubagentPrompt` is a one-shot read from claudeChats' queue —
// calling it again would return undefined, so the value has to be captured
// into local state the first time a child is seen, not re-read on every
// render from the template.
//
// Caching `undefined` is only safe once we know it is the FINAL answer, not
// a read that outran `create()` filling the queue (the bug this used to
// have — see pendingSubagentPrompts' comment in claudeChats.ts for the fix
// on the writer's side). `isLocallyCreatedSubagent` tells the two cases
// apart: a child this process's own `create()` produced has its queue entry
// (or deliberate absence) already final by the time it can appear in
// `children` here, so any read — defined or not — is safe to cache. A child
// that showed up some other way (ListChats/a shell snapshot: another client,
// or this one after a restart) was never going to have a local handoff
// queued for it, so `undefined` there is correct by design, not a race — also
// safe to cache. What's deliberately NOT cached is anything in between.
// DEBUG(subagent): remove once the start path is confirmed.
onMounted(() => console.log("[subagent] host mounted"));
watch(
  [children, openWsIds],
  ([list, ws]) => {
    console.log("[subagent] openWsIds", [...ws], "children", list.map((c) => ({ id: c.id, ws: c.workspaceId, parent: c.parentChatId, archivedAt: c.archivedAt })));
    console.log("[subagent] all sessions", chats.sessions.map((s) => ({ id: s.id, ws: s.workspaceId, parent: s.parentChatId })));
  },
  { immediate: true, deep: true },
);

const initialPrompts = reactive<Record<number, string | undefined>>({});
watch(
  children,
  (list) => {
    for (const c of list) {
      if (c.id in initialPrompts) continue;
      if (isLocallyCreatedSubagent(c.id)) {
        initialPrompts[c.id] = takePendingSubagentPrompt(c.id);
      } else {
        initialPrompts[c.id] = undefined;
      }
      console.log("[subagent] prompt for", c.id, "=", JSON.stringify(initialPrompts[c.id]));
    }
  },
  { immediate: true },
);
</script>
