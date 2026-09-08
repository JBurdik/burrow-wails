<script setup lang="ts">
import { ref, computed, watch } from "vue";
import { PhFolder, PhGitBranch, PhCaretDown, PhPlus } from "@phosphor-icons/vue";
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuRoot, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { PopoverContent, PopoverRoot, PopoverTrigger } from "@/components/ui/popover";

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
  /** Branches to offer; when given, the shown branch becomes a switch/create picker. */
  branches?: string[];
  disabled?: boolean;
  error?: string;
}>();

const emit = defineEmits<{
  selectMode: [mode: TargetMode];
  switchBranch: [name: string];
  createBranch: [name: string];
}>();

// Branch switcher, moved here from the title bar so the branch lives in one
// place. A Popover rather than a DropdownMenu because it holds a text input and
// the menu's typeahead eats keystrokes — but reka-ui's Popover, not the
// hand-rolled Teleport this used to be: that one measured the trigger's rect
// itself, so it had no collision handling and stayed pinned to a stale position
// after a scroll or resize, and its outside-click guard was a window listener
// that every ancestor had to remember to `@click.stop` around.
const pickerOpen = ref(false);
const filter = ref("");
const filtered = computed(() => {
  const q = filter.value.trim().toLowerCase();
  const all = props.branches ?? [];
  return q ? all.filter((b) => b.toLowerCase().includes(q)) : all;
});
const showCreate = computed(() => {
  const q = filter.value.trim();
  return !!q && !(props.branches ?? []).includes(q);
});

watch(pickerOpen, (open) => { if (open) filter.value = ""; });

function switchBranch(name: string) {
  pickerOpen.value = false;
  emit("switchBranch", name);
}
function createBranch(name: string) {
  if (!name) return;
  pickerOpen.value = false;
  emit("createBranch", name);
}
function onEnter() {
  if (filtered.value.length === 1) { switchBranch(filtered.value[0]); return; }
  if (showCreate.value) createBranch(filter.value.trim());
}

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
      <PopoverRoot v-if="props.branches" v-model:open="pickerOpen">
        <PopoverTrigger as-child>
          <button
            class="rounded-[5px] border-0 bg-transparent px-1 py-0.5 font-mono text-inherit text-secondary-foreground hover:bg-hover hover:text-foreground disabled:cursor-default"
            type="button"
            :disabled="props.disabled"
            :title="`Branch: ${props.currentBranch || 'HEAD'} — click to switch`"
          >{{ props.currentBranch || "HEAD" }}</button>
        </PopoverTrigger>
        <PopoverContent align="end" side="top" class="w-[220px]">
          <!-- The input is the first focusable child, so Popover's own
               open-autofocus lands on it — no nextTick().focus() dance. -->
          <input
            v-model="filter"
            class="box-border w-full border-0 border-b border-border bg-transparent px-[9px] py-[7px] font-mono text-[11px] text-foreground outline-none placeholder:text-muted-foreground"
            placeholder="Switch or create branch…"
            @keydown.enter="onEnter"
          />
          <div class="max-h-[180px] overflow-y-auto">
            <div
              v-for="b in filtered"
              :key="b"
              class="flex cursor-pointer items-center gap-1.5 px-[9px] py-[5px] font-mono text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground"
              :class="b === props.currentBranch && 'text-accent'"
              @click="switchBranch(b)"
            >
              <PhGitBranch :size="10" />
              <span>{{ b }}</span>
              <span v-if="b === props.currentBranch" class="ml-auto not-italic text-accent">✓</span>
            </div>
            <div
              v-if="showCreate"
              class="flex cursor-pointer items-center gap-1.5 px-[9px] py-[5px] font-mono text-[11px] italic text-muted-foreground hover:bg-hover hover:text-foreground"
              @click="createBranch(filter.trim())"
            >
              <PhPlus :size="10" />
              <span>Create "{{ filter.trim() }}"</span>
            </div>
            <div v-if="filtered.length === 0 && !showCreate" class="px-2.5 py-2.5 text-center text-[10px] text-muted-foreground">
              No branches found
            </div>
          </div>
        </PopoverContent>
      </PopoverRoot>
      <span v-else class="font-mono text-secondary-foreground">{{ props.currentBranch || "HEAD" }}</span>
    </template>
    <template v-else>
      <span class="text-secondary-foreground">{{ props.detail || "A new isolated checkout" }}</span>
      <span v-if="props.baseBranch" class="ml-auto whitespace-nowrap">From {{ props.baseBranch }}</span>
    </template>
    <p v-if="props.error" class="m-0 basis-full text-[10px] text-destructive" role="alert">{{ props.error }}</p>
  </div>
</template>
