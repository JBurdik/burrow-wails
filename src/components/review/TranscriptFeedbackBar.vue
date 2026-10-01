<script setup lang="ts">
defineProps<{ count: number; sending: boolean; busy: boolean; error: string }>();
const emit = defineEmits<{ send: [] }>();
</script>

<template>
  <div v-if="count || error" class="mb-2 flex flex-wrap items-center gap-2 rounded border border-border bg-hover px-3 py-2 text-[11px]" aria-live="polite">
    <span v-if="count" class="flex-1 text-secondary-foreground">{{ count }} pending comment{{ count === 1 ? '' : 's' }}</span>
    <span v-if="error" role="alert" class="w-full text-destructive">{{ error }}</span>
    <span v-if="busy && count" class="text-muted-foreground">Send after the current turn finishes</span>
    <button v-if="count" type="button" class="rounded border border-border bg-base px-2 py-1 text-foreground hover:bg-hover disabled:opacity-40" :disabled="sending || busy" @click="emit('send')">{{ sending ? 'Sending…' : 'Send comments' }}</button>
  </div>
</template>
