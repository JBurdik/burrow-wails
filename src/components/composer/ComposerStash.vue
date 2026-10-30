<template>
  <DropdownMenuRoot>
    <DropdownMenuTrigger as-child>
      <button type="button" class="composer-pill" :title="`Prompt stash (${stashEntries.length})`">
        <PhStack :size="14" weight="bold" />
        <span v-if="stashEntries.length" class="composer-pill-label">{{ stashEntries.length }}</span>
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent side="top" align="start" class="composer-menu w-[340px]">
      <DropdownMenuItem class="composer-menu-item" :disabled="isEmptyContent({ text, images })" @select="stash">
        Stash current prompt
        <span class="composer-menu-hint">⌘⇧S</span>
      </DropdownMenuItem>
      <div v-if="!stashEntries.length" class="composer-stash-empty">Nothing stashed</div>
      <div v-else class="composer-stash-list">
        <DropdownMenuItem
          v-for="entry in stashEntries"
          :key="entry.id"
          class="composer-menu-item-detailed composer-stash-item"
          :title="entry.text"
          @select="restore(entry.id)"
        >
          <span class="composer-menu-copy">
            <span class="composer-stash-preview">{{ entry.text.trim() || "(images only)" }}</span>
            <span>
              {{ timeAgo(entry.createdAt) }}<template v-if="entry.images.length"> · {{ entry.images.length }} image{{ entry.images.length === 1 ? "" : "s" }}</template>
              <template v-if="entry.droppedImages.length"> · dropped {{ entry.droppedImages.join(", ") }}</template>
            </span>
          </span>
          <button
            type="button"
            class="composer-stash-delete"
            aria-label="Delete stashed prompt"
            title="Delete"
            @pointerdown.stop
            @pointerup.stop
            @click.stop.prevent="commitStash(stashEntries.filter((e) => e.id !== entry.id))"
          >
            <PhTrash :size="12" />
          </button>
        </DropdownMenuItem>
      </div>
    </DropdownMenuContent>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { PhStack, PhTrash } from "@phosphor-icons/vue";
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { stashEntries, commitStash, isEmptyContent, makeEntry, pushEntry, restoreEntry } from "@/lib/promptStash";
import { timeAgo } from "@/components/phone/timeAgo";

// The host still owns the composer: the stash reads and replaces its text and
// images through these models.
const text = defineModel<string>("text", { required: true });
const images = defineModel<string[]>("images", { required: true });

/** Park the composer's content and clear it. Bound to ⌘⇧S by the host's keydown. */
function stash() {
  const current = { text: text.value, images: images.value };
  if (isEmptyContent(current)) return;
  // A failed write (quota) keeps the composer as it was rather than losing it.
  if (!commitStash(pushEntry(stashEntries.value, makeEntry(current)))) return;
  text.value = "";
  images.value = [];
}

function restore(id: string) {
  const { entries, restored } = restoreEntry(stashEntries.value, id, { text: text.value, images: images.value });
  if (!restored || !commitStash(entries)) return;
  text.value = restored.text;
  images.value = [...restored.images];
}

defineExpose({ stash });
</script>

<style scoped>
.composer-stash-list { max-height: 320px; overflow-y: auto; }
.composer-stash-item { min-height: 0; align-items: center; }
.composer-stash-item .composer-menu-copy { flex: 1; }
.composer-stash-preview {
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  font-size: 12.5px;
  font-weight: 500;
}
.composer-stash-empty { padding: 7px 10px; color: var(--text-muted); font-size: 12px; }
.composer-stash-delete {
  flex: 0 0 auto;
  padding: 4px;
  border-radius: 6px;
  color: var(--text-muted);
}
.composer-stash-delete:hover { color: var(--red, #ef4444); background: var(--bg-hover); }
</style>
