<template>
  <span
    class="composer-inline-chip"
    data-chip="paste"
    role="button"
    tabindex="0"
    title="Click to view full text"
    @click="dialogText = text"
    @keydown.enter="dialogText = text"
    @keydown.space.prevent="dialogText = text"
  ><svg class="composer-chip-icon" viewBox="0 0 256 256" aria-hidden="true"><path :d="PASTE_CHIP_ICON_PATH" /></svg>{{ label }}</span>
  <PasteTextDialog v-model="dialogText" />
</template>

<script setup lang="ts">
// The transcript's read-only counterpart to the composer's own paste chip
// (ComposerTextInput.vue): a sent message that still carries a `wrapPaste`
// block collapses it here instead of dumping the raw pasted text into the
// bubble, which is what used to overflow the chat layout — the wire payload
// is stripped of markers before it reaches the agent (AgentChat.vue's
// sendMessage), but the DISPLAY copy of the user's own message keeps them so
// the transcript can still tell "this was a paste" after the fact.
import { computed, ref } from "vue";
import { PASTE_CHIP_ICON_PATH } from "@/lib/composerDom";
import PasteTextDialog from "@/components/composer/PasteTextDialog.vue";

const props = defineProps<{ text: string }>();

const dialogText = ref<string | null>(null);
const label = computed(() => `Pasted text (${props.text.split("\n").length} lines)`);
</script>
