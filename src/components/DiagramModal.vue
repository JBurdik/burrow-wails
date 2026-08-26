<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/60" @click.self="close">
      <div class="relative max-h-[85vh] max-w-[90vw] overflow-auto rounded-[10px] bg-panel p-8 shadow-[0_24px_64px_rgba(0,0,0,0.5)]">
        <button class="absolute right-2.5 top-2.5 flex items-center rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" @click="close"><PhX :size="16" /></button>
        <div ref="containerRef" class="diagram-svg" />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { PhX } from "@phosphor-icons/vue";
import mermaid from "mermaid";
import { useDiagram } from "@/composables/useDiagram";

const { diagramContent, closeDiagram } = useDiagram();
const containerRef = ref<HTMLElement | null>(null);

const isDark = window.matchMedia("(prefers-color-scheme: dark)").matches;

mermaid.initialize({
  startOnLoad: false,
  theme: isDark ? "dark" : "default",
});

async function render(content: string) {
  if (!containerRef.value) return;
  try {
    const id = `burrow-diagram-${Date.now()}`;
    const { svg } = await mermaid.render(id, content);
    containerRef.value.innerHTML = svg;
  } catch (e) {
    containerRef.value.innerHTML = `<pre style="color:red;padding:1em">${String(e)}</pre>`;
  }
}

onMounted(() => {
  if (diagramContent.value) render(diagramContent.value);
});

watch(diagramContent, (val) => {
  if (val) render(val);
});

function close() {
  closeDiagram();
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") close();
}
onMounted(() => window.addEventListener("keydown", onKey));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>

<style scoped>
/* mermaid renders raw SVG via innerHTML — :deep() needed to reach it,
   Tailwind classes can't target dynamically-injected markup. */
.diagram-svg :deep(svg) {
  max-width: 80vw;
  max-height: 70vh;
  height: auto;
}
</style>
