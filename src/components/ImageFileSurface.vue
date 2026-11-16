<template>
  <!-- A picture file in the Right Panel (the code editor would show its bytes as text). -->
  <section class="flex min-h-0 flex-1 items-center justify-center overflow-auto bg-[var(--panel)] p-3">
    <p v-if="error" class="text-[12px] text-[var(--red)]">{{ error }}</p>
    <img v-else-if="src" :src="src" :alt="path" class="max-h-full max-w-full object-contain" />
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";

const props = defineProps<{ path: string; cwd: string }>();
const src = ref("");
const error = ref("");

const MIME: Record<string, string> = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", svg: "image/svg+xml" };

function abs(p: string): string {
  return p.startsWith("/") || p.startsWith("~") ? p : `${props.cwd.replace(/\/+$/, "")}/${p}`;
}
watch(
  () => props.path,
  async () => {
    error.value = "";
    src.value = "";
    try {
      const b64 = await invoke<string>("read_file_base64", { path: abs(props.path) });
      const ext = props.path.split(".").pop()?.toLowerCase() ?? "";
      src.value = `data:${MIME[ext] ?? "image/png"};base64,${b64}`;
    } catch (e) {
      error.value = `Could not read ${props.path}: ${e}`;
    }
  },
  { immediate: true },
);
</script>
