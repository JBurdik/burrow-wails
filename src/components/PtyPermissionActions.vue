<template>
  <div class="flex shrink-0 items-center gap-1.5">
    <Button size="sm" @click="store.answer(request, 'allow')">Allow</Button>
    <Button size="sm" variant="secondary" title="Allow for the rest of this session" @click="store.answer(request, 'always')">Always</Button>
    <Button size="sm" variant="outline" @click="store.answer(request, 'deny')">Deny</Button>
  </div>
</template>

<script setup lang="ts">
import { Button } from "@/components/ui/button";
import { usePtyPermissionsStore, type PtyPermission } from "@/stores/ptyPermissions";

defineProps<{ request: PtyPermission }>();
const store = usePtyPermissionsStore();
</script>
