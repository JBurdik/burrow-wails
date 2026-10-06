<template>
  <div
    v-if="queue"
    class="flex shrink-0 items-center gap-2 border-b border-border bg-panel px-3 py-1.5 text-xs"
    role="alert"
    @mousedown.stop
  >
    <span class="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-amber-500" />
    <span class="shrink-0 font-semibold text-foreground">{{ queue.head.tool_name }}</span>
    <span class="min-w-0 flex-1 truncate text-muted-foreground" :title="describePermission(queue.head)">
      {{ describePermission(queue.head) }}
    </span>
    <span v-if="queue.more" class="shrink-0 text-muted-foreground">+{{ queue.more }}</span>
    <PtyPermissionActions :request="queue.head" />
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import PtyPermissionActions from "@/components/PtyPermissionActions.vue";
import { describePermission, headOfQueue, usePtyPermissionsStore } from "@/stores/ptyPermissions";

const props = defineProps<{ ptyId: number | string }>();
const store = usePtyPermissionsStore();
const queue = computed(() => headOfQueue(store.byPty[String(props.ptyId)] ?? []));
</script>
