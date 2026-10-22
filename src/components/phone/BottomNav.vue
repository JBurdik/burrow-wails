<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { PhChatsCircle, PhTray, PhGitBranch, PhDotsThreeOutline } from "@phosphor-icons/vue";
import { router } from "@/router";
import { phoneScreen } from "@/lib/phoneNav";
import { useInbox } from "./useInbox";

const route = useRoute();
const { needsCount } = useInbox();

const items = [
  { screen: "threads", path: "/", label: "Threads", icon: PhChatsCircle },
  { screen: "inbox", path: "/inbox", label: "Inbox", icon: PhTray },
  { screen: "project", path: "/project", label: "Project", icon: PhGitBranch },
  { screen: "more", path: "/more", label: "More", icon: PhDotsThreeOutline },
] as const;

const current = computed(() => phoneScreen(route.name));
// A tab switch is not a step anyone wants to go "back" through.
const go = (path: string) => { if (route.path !== path) void router.replace(path); };
</script>

<template>
  <nav class="phone-nav flex shrink-0 border-t border-border bg-panel" aria-label="Main">
    <button
      v-for="it in items"
      :key="it.screen"
      class="relative flex h-14 flex-1 flex-col items-center justify-center gap-0.5 text-[10.5px] font-medium"
      :class="current === it.screen ? 'text-accent' : 'text-muted-foreground active:text-foreground'"
      :aria-current="current === it.screen ? 'page' : undefined"
      @click="go(it.path)"
    >
      <component :is="it.icon" :size="23" :weight="current === it.screen ? 'fill' : 'regular'" />
      {{ it.label }}
      <span
        v-if="it.screen === 'inbox' && needsCount"
        class="absolute left-1/2 top-1.5 ml-2 min-w-[17px] rounded-full bg-destructive px-1 text-center text-[10px] font-semibold leading-[17px] text-white"
      >{{ needsCount }}</span>
    </button>
  </nav>
</template>

<style scoped>
.phone-nav { padding-bottom: env(safe-area-inset-bottom, 0px); }
</style>
