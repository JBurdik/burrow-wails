<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from "vue";
import { PhFolder, PhGitBranch, PhCaretDown, PhPlus } from "@phosphor-icons/vue";
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
// place. A plain popover rather than DropdownMenu: it holds a text input, and
// the menu's typeahead eats keystrokes.
const pickerOpen = ref(false);
const filter = ref("");
const filterEl = ref<HTMLInputElement | null>(null);
const triggerEl = ref<HTMLButtonElement | null>(null);
const pickerPos = ref({ left: 0, bottom: 0 });
const filtered = computed(() => {
  const q = filter.value.trim().toLowerCase();
  const all = props.branches ?? [];
  return q ? all.filter((b) => b.toLowerCase().includes(q)) : all;
});
const showCreate = computed(() => {
  const q = filter.value.trim();
  return !!q && !(props.branches ?? []).includes(q);
});

async function togglePicker() {
  pickerOpen.value = !pickerOpen.value;
  if (!pickerOpen.value) return;
  filter.value = "";
  const r = triggerEl.value?.getBoundingClientRect();
  // Anchored by its bottom edge, so the popover's own height stays its business.
  if (r) pickerPos.value = { left: Math.max(8, r.right - 220), bottom: window.innerHeight - r.top + 5 };
  await nextTick();
  filterEl.value?.focus();
}
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
const close = () => { pickerOpen.value = false; };
onMounted(() => window.addEventListener("click", close));
onBeforeUnmount(() => window.removeEventListener("click", close));

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
      <div v-if="props.branches">
        <button
          ref="triggerEl"
          class="rounded-[5px] border-0 bg-transparent px-1 py-0.5 font-mono text-inherit text-secondary-foreground hover:bg-hover hover:text-foreground disabled:cursor-default"
          type="button"
          :disabled="props.disabled"
          :title="`Branch: ${props.currentBranch || 'HEAD'} — click to switch`"
          @click.stop="togglePicker"
        >{{ props.currentBranch || "HEAD" }}</button>
        <Teleport to="body">
        <div
          v-if="pickerOpen"
          class="fixed z-[2000] w-[220px] overflow-hidden rounded-md border border-border bg-panel shadow-[0_8px_24px_rgba(0,0,0,0.45)]"
          :style="{ left: `${pickerPos.left}px`, bottom: `${pickerPos.bottom}px` }"
          @click.stop
        >
          <input
            ref="filterEl"
            v-model="filter"
            class="box-border w-full border-0 border-b border-border bg-transparent px-[9px] py-[7px] font-mono text-[11px] text-foreground outline-none placeholder:text-muted-foreground"
            placeholder="Switch or create branch…"
            @keydown.enter="onEnter"
            @keydown.esc="pickerOpen = false"
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
        </div>
        </Teleport>
      </div>
      <span v-else class="font-mono text-secondary-foreground">{{ props.currentBranch || "HEAD" }}</span>
    </template>
    <template v-else>
      <span class="text-secondary-foreground">{{ props.detail || "A new isolated checkout" }}</span>
      <span v-if="props.baseBranch" class="ml-auto whitespace-nowrap">From {{ props.baseBranch }}</span>
    </template>
    <p v-if="props.error" class="m-0 basis-full text-[10px] text-destructive" role="alert">{{ props.error }}</p>
  </div>
</template>
