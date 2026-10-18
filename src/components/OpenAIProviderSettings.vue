<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import SettingField from "@/components/SettingField.vue";
import { Button } from "@/components/ui/button";
import { learnModels } from "@/lib/chatModels";
import type { OpenAIConfig, ProviderInstance } from "@/lib/providers";

const props = defineProps<{ instance: ProviderInstance }>();
const emit = defineEmits<{ update: [config: OpenAIConfig] }>();
const config = computed<OpenAIConfig>(() => props.instance.openai ?? {
  baseUrl: "http://127.0.0.1:8080/v1", apiKey: "", model: "", contextWindow: 32768, maxOutputTokens: 4096,
});
const busy = ref(false);
const error = ref("");
const result = ref("");
const modelIds = ref<string[]>([]);
function update(patch: Partial<OpenAIConfig>) {
  emit("update", { ...config.value, ...patch });
  error.value = "";
  result.value = "";
}
watch(() => config.value.model, (id) => {
  if (id.trim()) learnModels(props.instance.id, [...new Set([id.trim(), ...modelIds.value])].map((id) => ({ id, label: id })));
}, { immediate: true });
async function testConnection() {
  const snapshot = { ...config.value };
  busy.value = true;
  error.value = "";
  result.value = "";
  try {
    const models = await invoke<{ id: string; label: string }[]>("openai_list_models", { config: snapshot });
    // A slow response for an old URL/key must not overwrite new settings.
    if (snapshot.baseUrl !== config.value.baseUrl || snapshot.apiKey !== config.value.apiKey) return;
    modelIds.value = models.map((m) => m.id);
    learnModels(props.instance.id, models);
    if (!config.value.model && models[0]) emit("update", { ...config.value, model: models[0].id });
    result.value = `Connected · ${models.length} model${models.length === 1 ? "" : "s"} available`;
  } catch (e) {
    error.value = String(e);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <div class="flex flex-col gap-4 rounded-lg border border-border bg-panel/40 p-3">
    <p class="text-xs text-muted-foreground">Built-in coding agent with file tools, shell commands, approvals, and resumable history. Connect llama.cpp, LM Studio, Ollama, or another OpenAI-compatible server. The model must support tool calling.</p>
    <SettingField label="Base URL" hint="OpenAI-compatible API root, including /v1. The server must be reachable from the machine running Burrow.">
      <input :value="config.baseUrl" type="url" placeholder="http://127.0.0.1:8080/v1" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground" @input="update({ baseUrl: ($event.target as HTMLInputElement).value })" />
    </SettingField>
    <SettingField label="API key" hint="Optional for local servers. Saved with this provider's settings.">
      <input :value="config.apiKey" type="password" autocomplete="off" placeholder="Optional" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground" @input="update({ apiKey: ($event.target as HTMLInputElement).value })" />
    </SettingField>
    <SettingField label="Default model" hint="Exact server model ID. Test the connection to discover models, or enter an ID manually.">
      <input :value="config.model" :list="`openai-models-${instance.id}`" type="text" placeholder="Model ID" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 font-mono text-xs text-foreground" @input="update({ model: ($event.target as HTMLInputElement).value })" />
      <datalist :id="`openai-models-${instance.id}`"><option v-for="id in modelIds" :key="id" :value="id" /></datalist>
    </SettingField>
    <div class="grid grid-cols-2 gap-3">
      <SettingField label="Context window" hint="Tokens configured on the server. Earlier turns are summarized when needed.">
        <input :value="config.contextWindow" type="number" min="2048" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground" @input="update({ contextWindow: Number(($event.target as HTMLInputElement).value) })" />
      </SettingField>
      <SettingField label="Maximum output tokens" hint="Reserved for each response; at most half the context window.">
        <input :value="config.maxOutputTokens" type="number" min="128" :max="config.contextWindow / 2" class="w-full rounded-lg border border-border bg-base px-2.5 py-1.5 text-xs text-foreground" @input="update({ maxOutputTokens: Number(($event.target as HTMLInputElement).value) })" />
      </SettingField>
    </div>
    <div class="flex items-center gap-2">
      <Button variant="outline" size="sm" :disabled="busy" @click="testConnection">{{ busy ? "Connecting…" : "Test connection & load models" }}</Button>
      <span v-if="result" role="status" class="text-xs text-success">{{ result }}</span>
    </div>
    <p v-if="error" role="alert" class="break-words text-xs text-destructive">{{ error }}</p>
  </div>
</template>
