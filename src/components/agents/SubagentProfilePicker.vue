<script setup lang="ts">
import { computed } from "vue";
import { useSubagentProfilesStore } from "@/stores/subagentProfiles";
import { useProvidersStore } from "@/stores/providers";
import { PROFILE_PERMISSION_LABELS } from "@/lib/subagentProfiles";
const selected = defineModel<string>({ required: true });
const profiles = useSubagentProfilesStore();
const providers = useProvidersStore();
const profile = computed(() => profiles.find(selected.value));
defineEmits<{ configure: [] }>();
</script>

<template>
  <div class="grid gap-1.5">
    <div class="flex items-center gap-2">
      <label class="min-w-0 flex-1 text-xs text-secondary-foreground">Role
        <select v-model="selected" aria-label="Sub-agent profile" class="ml-2 rounded border border-border bg-base px-2 py-1 text-xs text-foreground focus:outline-accent">
          <option value="">No profile</option>
          <option v-for="row in profiles.profiles" :key="row.id" :value="row.id">{{ row.name }}</option>
        </select>
      </label>
      <button type="button" class="text-[11px] text-muted-foreground hover:text-foreground" @click="$emit('configure')">Configure profiles</button>
    </div>
    <p v-if="profile" class="m-0 text-[11px] leading-relaxed text-muted-foreground">{{ profile.agentId ? providers.byId(profile.agentId)?.name || 'Unavailable provider' : 'Default chat provider' }} · {{ profile.model || 'Default model' }} · {{ PROFILE_PERMISSION_LABELS[profile.permissionMode] }}</p>
    <p v-if="profile" class="m-0 max-h-20 overflow-auto text-[11px] leading-relaxed text-secondary-foreground">{{ profile.instructions }}</p>
  </div>
</template>
