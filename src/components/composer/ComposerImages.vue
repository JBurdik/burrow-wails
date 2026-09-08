<template>
  <div v-if="images.length > 0" class="composer-images">
    <div v-for="(img, i) in images" :key="i" class="composer-image">
      <img :src="img" :alt="`Attached image ${i + 1}`" />
      <button
        type="button"
        class="composer-image-remove"
        :aria-label="`Remove attached image ${i + 1}`"
        title="Remove"
        @click="images.splice(i, 1)"
      >
        <PhX :size="10" weight="bold" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { PhX } from "@phosphor-icons/vue";

// Data URIs. Mutated in place so removing one is a splice at the call site's
// own ref — the host still owns the list and sends it with the message.
const images = defineModel<string[]>({ required: true });
</script>
