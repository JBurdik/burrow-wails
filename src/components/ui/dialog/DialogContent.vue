<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { X } from "lucide-vue-next";
import { DialogClose, DialogContent, type DialogContentProps, DialogOverlay, DialogPortal } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<DialogContentProps & { class?: HTMLAttributes["class"] }>();
const delegated = computed(() => {
  const { class: _, ...rest } = props;
  return rest;
});
</script>

<template>
  <DialogPortal>
    <DialogOverlay class="fixed inset-0 z-50 bg-black/60" />
    <DialogContent
      v-bind="delegated"
      :class="
        cn(
          'fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-card p-6 text-card-foreground shadow-lg',
          props.class,
        )
      "
    >
      <slot />
      <DialogClose class="absolute right-4 top-4 rounded-sm text-muted-foreground hover:text-foreground">
        <X class="h-4 w-4" />
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>
