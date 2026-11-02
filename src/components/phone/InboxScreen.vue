<script setup lang="ts">
import { router } from "@/router";
import { agentIconComp } from "@/lib/agentIcons";
import PhoneHeader from "./PhoneHeader.vue";
import { useInbox } from "./useInbox";
import { timeAgo } from "./timeAgo";

const { sections } = useInbox();
const dot: Record<string, string> = {
  "needs-input": "bg-[var(--yellow)]",
  error: "bg-destructive",
  "done-unread": "bg-[var(--blue)]",
  working: "bg-accent animate-pulse",
};
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-base">
    <PhoneHeader title="Inbox" />
    <div class="flex-1 overflow-y-auto">
      <div v-if="!sections.length" class="px-6 py-16 text-center text-[14px] text-muted-foreground">Nothing needs you</div>
      <section v-for="s in sections" :key="s.key">
        <h2 class="px-4 pb-1 pt-4 text-[12px] font-semibold text-muted-foreground">{{ s.label }}</h2>
        <button
          v-for="r in s.rows"
          :key="`${r.wsId}:${r.tabId}`"
          class="flex min-h-[60px] w-full items-center gap-3 border-b border-border/60 px-4 py-2 text-left active:bg-hover"
          @click="router.push(`/ws/${r.wsId}/tab/${r.tabId}`)"
        >
          <component :is="agentIconComp(r.agentIcon)" :size="20" class="shrink-0 text-secondary-foreground" />
          <span class="flex min-w-0 flex-1 flex-col">
            <span class="truncate text-[14.5px] text-foreground">{{ r.title }}</span>
            <span class="truncate text-[12px] text-muted-foreground">{{ r.wsName }}</span>
          </span>
          <span class="shrink-0 text-[11.5px] text-muted-foreground">{{ timeAgo(r.ts) }}</span>
          <span class="h-2 w-2 shrink-0 rounded-full" :class="dot[r.state]" />
        </button>
      </section>
    </div>
  </div>
</template>
