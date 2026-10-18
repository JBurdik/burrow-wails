<template>
  <div
    v-if="queue"
    class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b border-warning/25 bg-warning/10 px-3.5 py-2"
    role="alert"
    @mousedown.stop
  >
    <PhShieldWarning :size="18" weight="fill" class="shrink-0 text-warning" />
    <div class="flex min-w-0 flex-1 basis-[180px] flex-col gap-0.5">
      <span class="truncate text-[12.5px] font-medium text-foreground">Allow {{ queue.head.tool_name }}?</span>
      <code class="truncate font-mono text-[11.5px] text-secondary-foreground" :title="describePermission(queue.head)">{{ describePermission(queue.head) }}</code>
    </div>
    <div class="ml-auto flex shrink-0 items-center gap-1">
      <span v-if="queue.more" class="mr-1.5 rounded-full bg-warning/20 px-2 py-0.5 text-[11px] font-medium text-warning">+{{ queue.more }}</span>
      <button class="h-7 rounded-lg px-2.5 text-[12px] text-secondary-foreground hover:bg-hover hover:text-foreground" @click="store.answer(queue.head, 'deny')">Deny</button>
      <button class="h-7 rounded-lg px-2.5 text-[12px] text-secondary-foreground hover:bg-hover hover:text-foreground" title="Allow for the rest of this session" @click="store.answer(queue.head, 'always')">Always</button>
      <button class="h-7 rounded-lg bg-warning px-3 text-[12px] font-medium text-black/85 hover:opacity-90" @click="store.answer(queue.head, 'allow')">Allow</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { PhShieldWarning } from "@phosphor-icons/vue";
import { describePermission, headOfQueue, usePtyPermissionsStore } from "@/stores/ptyPermissions";

const props = defineProps<{ ptyId: number | string }>();
const store = usePtyPermissionsStore();
const queue = computed(() => headOfQueue(store.byPty[String(props.ptyId)] ?? []));
</script>
