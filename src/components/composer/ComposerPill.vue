<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child>
      <button
        type="button"
        class="composer-pill"
        :class="{ 'composer-pill-danger': danger, 'composer-pill-active': highlight }"
        :title="title"
      >
        <component :is="icon" v-if="icon" :size="14" weight="bold" />
        <span v-if="label" class="composer-pill-label">{{ label }}</span>
        <PhCaretDown :size="9" weight="bold" class="composer-pill-caret" />
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent
      :align="align"
      :side="side"
      class="composer-menu"
      :class="detailed ? 'min-w-[330px]' : 'min-w-[200px]'"
    >
      <DropdownMenuItem
        v-for="item in items"
        :key="item.id"
        :class="[
          detailed ? 'composer-menu-item-detailed' : 'composer-menu-item',
          { 'composer-menu-item-active': item.id === active, 'composer-menu-item-danger': item.danger },
        ]"
        :title="item.title ?? item.description"
        @select="emit('select', item.id)"
      >
        <component :is="item.icon" v-if="detailed && item.icon" :size="17" weight="bold" class="composer-menu-icon" />
        <span v-if="detailed" class="composer-menu-copy">
          <span>{{ item.label }}</span>
          <span v-if="item.description">{{ item.description }}</span>
        </span>
        <template v-else>
          {{ item.label }}
          <span v-if="item.hint" class="composer-menu-hint">{{ item.hint }}</span>
        </template>
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { PhCaretDown } from "@phosphor-icons/vue";
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";

export interface ComposerPillItem {
  id: string;
  label: string;
  /** Second line, in the `detailed` layout. */
  description?: string;
  /** Right-aligned mono note on a plain row (model ids, config dirs). */
  hint?: string;
  icon?: unknown;
  danger?: boolean;
  title?: string;
}

withDefaults(defineProps<{
  items: readonly ComposerPillItem[];
  /** Id of the current selection — gets the accent row. */
  active?: string;
  label?: string;
  icon?: unknown;
  title?: string;
  /** Pill wears the destructive tint (Claude's bypassPermissions). */
  danger?: boolean;
  /** Pill wears the accent tint (acceptEdits, a non-default profile). */
  highlight?: boolean;
  /** Icon + description rows instead of one-line labels. */
  detailed?: boolean;
  align?: "start" | "center" | "end";
  side?: "top" | "bottom" | "left" | "right";
}>(), {
  // The composer lives at the bottom of both surfaces, so menus open upward.
  side: "top",
  align: "start",
});

const emit = defineEmits<{ select: [id: string] }>();
</script>
