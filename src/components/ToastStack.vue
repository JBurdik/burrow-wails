<template>
  <Teleport to="body">
    <div class="toast-stack fixed z-[9999] flex flex-col gap-2 pointer-events-none" :class="`toast-stack--${ui.toastPosition}`">
      <TransitionGroup name="toast">
        <div
          v-for="toast in store.toasts"
          :key="toast.id"
          class="flex min-w-[220px] max-w-[320px] cursor-pointer items-start gap-2.5 rounded-lg border border-border bg-panel px-3.5 py-2.5 pointer-events-auto shadow-[0_4px_20px_rgba(0,0,0,0.4)] backdrop-blur-sm"
          @click="store.dismiss(toast.id)"
        >
          <span
            class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full"
            :class="{
              'bg-lime-500': toast.type === 'done',
              'bg-blue-500': toast.type === 'info',
              'bg-destructive': toast.type === 'error',
            }"
          />
          <div class="flex min-w-0 flex-col gap-0.5">
            <div class="truncate text-xs font-semibold text-foreground">{{ toast.title }}</div>
            <div v-if="toast.body" class="truncate text-[11px] text-muted-foreground">{{ toast.body }}</div>
          </div>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { useNotificationsStore } from "@/stores/notifications";
import { useUIStore } from "@/stores/ui";
const store = useNotificationsStore();
const ui = useUIStore();
</script>

<style scoped>
/* Positional anchors + enter/leave transition directions are combinatorial
   (4 corners × 2 phases) and Vue's TransitionGroup appends its own classes
   at runtime — cleaner as real CSS than as conditional Tailwind classes. */
.toast-stack--top-left,
.toast-stack--top-center,
.toast-stack--top-right    { top: 20px; }
.toast-stack--bottom-left,
.toast-stack--bottom-center,
.toast-stack--bottom-right { bottom: 20px; }

.toast-stack--top-left,
.toast-stack--bottom-left    { left: 20px; align-items: flex-start; }
.toast-stack--top-right,
.toast-stack--bottom-right   { right: 20px; align-items: flex-end; }
.toast-stack--top-center,
.toast-stack--bottom-center  { left: 50%; transform: translateX(-50%); align-items: center; }

.toast-enter-active { transition: all 0.2s ease; }
.toast-leave-active { transition: all 0.18s ease; }
.toast-enter-from   { opacity: 0; transform: translateY(12px); }
.toast-stack--bottom-left   .toast-leave-to,
.toast-stack--top-left      .toast-leave-to   { opacity: 0; transform: translateX(-24px); }
.toast-stack--bottom-right  .toast-leave-to,
.toast-stack--top-right     .toast-leave-to   { opacity: 0; transform: translateX(24px); }
.toast-stack--bottom-center .toast-leave-to   { opacity: 0; transform: translateY(24px); }
.toast-stack--top-center    .toast-leave-to   { opacity: 0; transform: translateY(-24px); }
</style>
