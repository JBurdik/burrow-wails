<script setup lang="ts">
import { ChevronDown } from "lucide-vue-next";
import { SelectContent, SelectIcon, SelectItem, SelectItemText, SelectPortal, SelectRoot, SelectTrigger, SelectValue, SelectViewport } from "reka-ui";
import { cn } from "@/lib/utils";

defineProps<{ options: { value: string; label: string }[]; placeholder?: string; class?: string; ariaLabel?: string; title?: string; triggerStyle?: Record<string, string> }>();
const model = defineModel<string | undefined>();
</script>

<template>
  <SelectRoot v-model="model">
    <SelectTrigger
      :aria-label="ariaLabel"
      :title="title"
      :style="triggerStyle"
      :class="
        cn(
          'flex h-8 items-center justify-between gap-2 rounded-md border border-border bg-panel px-2.5 text-sm text-foreground focus:outline-none focus-visible:ring-1 focus-visible:ring-accent',
          $props.class,
        )
      "
    >
      <SelectValue :placeholder="placeholder" />
      <SelectIcon><ChevronDown class="h-3.5 w-3.5 text-muted-foreground" /></SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent
        class="z-[1100] overflow-hidden rounded-md border border-border bg-popover text-popover-foreground shadow-md"
        position="popper"
        :side-offset="4"
      >
        <SelectViewport class="p-1">
          <SelectItem
            v-for="opt in options"
            :key="opt.value"
            :value="opt.value"
            class="relative flex cursor-pointer select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-hover"
          >
            <SelectItemText>{{ opt.label }}</SelectItemText>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
