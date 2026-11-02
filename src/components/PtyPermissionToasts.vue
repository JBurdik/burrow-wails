<template>
  <Teleport to="body">
    <div class="fixed bottom-5 right-5 z-[9998] flex flex-col items-end gap-2 pointer-events-none">
      <div
        v-for="p in store.toasts"
        :key="p.id"
        class="pointer-events-auto flex w-[340px] flex-col gap-2 rounded-lg border border-border bg-panel px-3.5 py-2.5 shadow-[0_4px_20px_rgba(0,0,0,0.4)] backdrop-blur-sm"
        role="alert"
      >
        <div class="flex items-start gap-2.5">
          <span class="mt-1 h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-amber-500" />
          <div class="flex min-w-0 flex-1 flex-col gap-0.5">
            <div class="truncate text-xs font-semibold text-foreground">{{ p.tool_name }} needs permission</div>
            <div class="truncate text-[11px] text-muted-foreground" :title="describePermission(p)">{{ describePermission(p) }}</div>
          </div>
          <button class="shrink-0 text-muted-foreground hover:text-foreground" title="Hide (still pending in the tab)" @click="store.dismissToast(p.id)">
            <PhX :size="10" weight="bold" />
          </button>
        </div>
        <PtyPermissionActions :request="p" class="self-end" />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { PhX } from "@phosphor-icons/vue";
import PtyPermissionActions from "@/components/PtyPermissionActions.vue";
import { describePermission, usePtyPermissionsStore } from "@/stores/ptyPermissions";

const store = usePtyPermissionsStore();
store.start();
</script>
