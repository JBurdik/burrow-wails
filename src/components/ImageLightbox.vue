<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/70" @click.self="close">
      <button class="absolute right-2.5 top-2.5 flex items-center rounded p-1 text-white/80 hover:bg-white/10 hover:text-white" @click="close"><PhX :size="20" /></button>
      <img :src="lightboxSrc!" class="max-h-[90vh] max-w-[90vw] rounded object-contain shadow-[0_24px_64px_rgba(0,0,0,0.5)]" alt="" />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onMounted, onBeforeUnmount } from "vue";
import { PhX } from "@phosphor-icons/vue";
import { useImageLightbox } from "@/composables/useImageLightbox";

const { lightboxSrc, closeImage } = useImageLightbox();

function close() {
  closeImage();
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") close();
}
onMounted(() => window.addEventListener("keydown", onKey));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>
