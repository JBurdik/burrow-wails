<script setup lang="ts">
import { PhFolder, PhGitBranch, PhCaretDown } from "@phosphor-icons/vue";
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuRoot, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";

type TargetMode = "current" | "new";
type TargetAppearance = "attached" | "inline";

const props = defineProps<{
  mode: TargetMode;
  currentBranch: string;
  detail?: string;
  baseBranch?: string;
  appearance?: TargetAppearance;
  wide?: boolean;
  readonly?: boolean;
  disabled?: boolean;
  error?: string;
}>();

const emit = defineEmits<{
  selectMode: [mode: TargetMode];
}>();

function selectMode(mode: TargetMode) {
  emit("selectMode", mode);
}
</script>

<template>
  <div
    class="flex min-h-7 flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground"
    :class="[
      props.appearance === 'inline'
        ? 'mx-auto mb-[3px] min-h-[27px] w-[calc(100%-18px)] rounded-b-[7px] border border-t-0 border-border bg-[color-mix(in_srgb,var(--chat-surface,var(--bg-panel))_84%,var(--bg-base))] px-[9px] py-0.5'
        : [
            'relative z-0 mx-auto -mt-[18px] min-h-[34px] w-[calc(100%-30px)] rounded-b-[10px] border border-t-0 border-border bg-panel px-2.5 pb-[3px] pt-[15px]',
            props.wide ? 'max-w-full' : 'max-w-[530px]',
          ],
      { 'opacity-[0.55]': props.disabled },
    ]"
  >
    <DropdownMenuRoot v-if="!props.readonly">
      <DropdownMenuTrigger as-child>
        <button
          class="inline-flex items-center gap-1.5 rounded-[5px] border-0 bg-transparent px-[5px] py-1 font-inherit text-inherit hover:bg-hover hover:text-secondary-foreground focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent focus-visible:outline-offset-1 disabled:cursor-default"
          type="button"
          :disabled="props.disabled"
        >
          <PhFolder v-if="props.mode === 'current'" :size="12" weight="fill" />
          <PhGitBranch v-else :size="12" weight="bold" />
          <span>{{ props.mode === "current" ? "Current checkout" : "New worktree" }}</span>
          <PhCaretDown :size="9" weight="bold" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" class="min-w-[188px]">
        <p class="px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">Workspace</p>
        <DropdownMenuItem class="text-[11.5px]" :class="{ 'bg-accent/10 text-foreground': props.mode === 'current' }" @select="selectMode('current')">
          <PhFolder :size="12" weight="fill" class="mr-1.5 shrink-0" />
          Current checkout
        </DropdownMenuItem>
        <DropdownMenuItem class="text-[11.5px]" :class="{ 'bg-accent/10 text-foreground': props.mode === 'new' }" @select="selectMode('new')">
          <PhGitBranch :size="12" weight="bold" class="mr-1.5 shrink-0" />
          New worktree
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuRoot>
    <span v-else class="inline-flex cursor-default items-center gap-1.5 rounded-[5px] px-[5px] py-1">
      <PhFolder v-if="props.mode === 'current'" :size="12" weight="fill" />
      <PhGitBranch v-else :size="12" weight="bold" />
      <span>{{ props.mode === "current" ? "Current checkout" : "New worktree" }}</span>
    </span>

    <template v-if="props.mode === 'current'">
      <span class="flex-1" aria-hidden="true" />
      <span class="h-[13px] w-px bg-border" aria-hidden="true" />
      <PhGitBranch :size="11" weight="bold" class="shrink-0" />
      <span class="font-mono text-secondary-foreground">{{ props.currentBranch || "HEAD" }}</span>
    </template>
    <template v-else>
      <span class="text-secondary-foreground">{{ props.detail || "A new isolated checkout" }}</span>
      <span v-if="props.baseBranch" class="ml-auto whitespace-nowrap">From {{ props.baseBranch }}</span>
    </template>
    <p v-if="props.error" class="m-0 basis-full text-[10px] text-destructive" role="alert">{{ props.error }}</p>
  </div>
</template>
