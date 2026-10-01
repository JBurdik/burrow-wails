<script setup lang="ts">
import { computed, nextTick, shallowRef, watch } from "vue";
import { useSubagentProfilesStore } from "@/stores/subagentProfiles";
import { useProvidersStore } from "@/stores/providers";
import { useUIStore } from "@/stores/ui";
import { ensureModels, visibleModelsFor } from "@/lib/chatModels";
import { PROFILE_PERMISSION_LABELS, type SubagentProfile, type ProfilePermission } from "@/lib/subagentProfiles";

const profiles = useSubagentProfilesStore();
const providers = useProvidersStore();
const ui = useUIStore();
const selected = shallowRef("scout");
const draft = shallowRef<SubagentProfile>({ ...profiles.profiles[0] });
const saved = shallowRef(false);
const supported = computed(() => providers.chatAgents.filter((agent) => ["claude-cli", "codex-app-server"].includes(agent.transport)));
const agent = computed(() => draft.value.agentId ? providers.byId(draft.value.agentId) : providers.byId(ui.defaultChatAgent));
const models = computed(() => agent.value ? visibleModelsFor(agent.value.transport === "claude-cli" ? "claude" : agent.value.id) : []);
const validAgent = computed(() => !!agent.value?.enabled && ["claude-cli", "codex-app-server"].includes(agent.value?.transport ?? ""));
const permissions = Object.entries(PROFILE_PERMISSION_LABELS) as [ProfilePermission, string][];
watch([selected, () => profiles.profiles], () => {
  draft.value = { ...profiles.find(selected.value)! };
  saved.value = false;
}, { immediate: true });
watch(agent, (value) => { if (value) void ensureModels(value.id, value.kind, ""); }, { immediate: true });
function update(patch: Partial<SubagentProfile>) { draft.value = { ...draft.value, ...patch }; saved.value = false; }
async function save() { profiles.save(draft.value); await nextTick(); saved.value = true; }
</script>

<template>
  <div class="flex max-w-[760px] flex-col gap-4">
    <div>
      <h3 class="m-0 text-sm font-semibold text-foreground">Sub-agent profiles</h3>
      <p class="m-0 mt-1 text-xs leading-relaxed text-muted-foreground">Reusable roles for delegation. Changes apply to newly spawned agents. Available with Claude CLI and Codex app-server.</p>
    </div>
    <div class="flex gap-1 border-b border-border" role="tablist" aria-label="Sub-agent role">
      <button v-for="profile in profiles.profiles" :key="profile.id" type="button" role="tab" :aria-selected="selected === profile.id" class="rounded-t px-3 py-2 text-xs hover:bg-hover focus-visible:outline focus-visible:outline-accent" :class="selected === profile.id ? 'bg-hover font-semibold text-foreground' : 'text-muted-foreground'" @click="selected = profile.id">{{ profile.name }}</button>
    </div>
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <label class="grid gap-1 text-xs text-secondary-foreground">Provider
        <select :value="draft.agentId" aria-label="Profile provider" class="h-8 min-w-0 rounded-[var(--radius-input)] border border-border bg-base px-2 text-xs text-foreground focus:outline-accent" @change="update({agentId: ($event.target as HTMLSelectElement).value, model: ''})">
          <option value="">Default chat provider</option>
          <option v-if="draft.agentId && !supported.some((row) => row.id === draft.agentId)" :value="draft.agentId">Unavailable: {{ draft.agentId }}</option>
          <option v-for="row in supported" :key="row.id" :value="row.id">{{ row.name }}</option>
        </select>
      </label>
      <label class="grid gap-1 text-xs text-secondary-foreground">Model
        <select :value="draft.model" aria-label="Profile model" class="h-8 min-w-0 rounded-[var(--radius-input)] border border-border bg-base px-2 text-xs text-foreground focus:outline-accent" @change="update({model: ($event.target as HTMLSelectElement).value})">
          <option value="">Provider default</option>
          <option v-if="draft.model && !models.some((row) => row.id === draft.model)" :value="draft.model">{{ draft.model }} (saved)</option>
          <option v-for="row in models.filter((model) => model.id)" :key="row.id" :value="row.id">{{ row.label }}</option>
        </select>
      </label>
    </div>
    <p v-if="!validAgent" role="alert" class="m-0 text-xs text-warning">Choose an enabled Claude or Codex provider to use this profile.</p>
    <label class="grid gap-1 text-xs text-secondary-foreground">Permissions
      <select :value="draft.permissionMode" aria-label="Profile permissions" class="h-8 max-w-[320px] rounded-[var(--radius-input)] border border-border bg-base px-2 text-xs text-foreground focus:outline-accent" @change="update({permissionMode: ($event.target as HTMLSelectElement).value as ProfilePermission})">
        <option v-for="[value, label] in permissions" :key="value" :value="value">{{ label }}</option>
      </select>
      <span class="text-[11px] leading-relaxed text-muted-foreground">{{ draft.permissionMode === 'plan' ? 'Investigate and plan without workspace writes.' : draft.permissionMode === 'auto' ? 'The provider reviews routine actions automatically; risky actions still ask.' : 'Commands and file changes remain subject to approval.' }}</span>
    </label>
    <label class="grid gap-1 text-xs text-secondary-foreground">Role instructions
      <textarea :value="draft.instructions" aria-label="Role instructions" rows="6" class="w-full resize-y rounded-[var(--radius-input)] border border-border bg-base p-2 text-xs leading-relaxed text-foreground focus:outline-accent" @input="update({instructions: ($event.target as HTMLTextAreaElement).value})" />
    </label>
    <div class="flex flex-wrap items-center gap-2">
      <button type="button" :disabled="!profiles.ready || !validAgent" class="rounded-[var(--radius-button)] bg-accent px-3 py-1.5 text-xs font-medium text-accent-foreground disabled:opacity-40 focus-visible:outline focus-visible:outline-accent" @click="save">Save {{ draft.name }}</button>
      <button type="button" :disabled="!profiles.ready" class="rounded border border-border px-3 py-1.5 text-xs text-secondary-foreground hover:bg-hover disabled:opacity-40" @click="profiles.reset(selected)">Reset role</button>
      <span v-if="saved" role="status" class="text-xs text-muted-foreground">Saved for future delegations.</span>
    </div>
    <p class="m-0 text-[11px] text-muted-foreground">From an agent: <code>burrow spawn "task" --profile {{ selected }}</code>. An explicit agent or model overrides the profile's choice.</p>
  </div>
</template>
