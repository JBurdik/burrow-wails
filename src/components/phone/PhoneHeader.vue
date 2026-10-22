<script setup lang="ts">
import { PhCaretLeft } from "@phosphor-icons/vue";
import { router } from "@/router";
import { backTarget } from "@/lib/phoneNav";

// Root screens get a large title; details a compact bar with a back arrow.
const props = defineProps<{ title: string; subtitle?: string; back?: boolean }>();

function goBack() {
  // In-app history → a real back (keeps iOS swipe-back and this arrow in step);
  // a deep link has none, so land on the root of this detail's tab instead.
  if (router.options.history.state.back) router.back();
  else void router.replace(backTarget(router.currentRoute.value.name));
}
</script>

<template>
  <header class="phone-header shrink-0 border-b border-border bg-base" :class="props.back ? 'phone-header--detail' : 'phone-header--root'">
    <div v-if="props.back" class="flex h-12 items-center gap-1 px-1">
      <button class="phone-icon-btn" aria-label="Back" @click="goBack"><PhCaretLeft :size="22" /></button>
      <div class="flex min-w-0 flex-1 flex-col">
        <span class="truncate text-[15px] font-semibold leading-tight text-foreground">{{ props.title }}</span>
        <span v-if="props.subtitle" class="truncate text-[11.5px] leading-tight text-muted-foreground">{{ props.subtitle }}</span>
      </div>
      <slot name="actions" />
    </div>
    <div v-else class="flex items-end gap-1 px-4 pb-2 pt-3">
      <h1 class="min-w-0 flex-1 truncate text-[26px] font-bold leading-tight text-foreground">{{ props.title }}</h1>
      <slot name="actions" />
    </div>
  </header>
</template>

<style scoped>
.phone-header { padding-top: env(safe-area-inset-top, 0px); }
</style>
