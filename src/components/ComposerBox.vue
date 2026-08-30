<template>
  <div v-bind="$attrs" class="composer-box">
    <slot />
    <div v-if="$slots.toolbar" class="composer-box-toolbar">
      <slot name="toolbar" />
    </div>
  </div>
</template>

<script setup lang="ts">
defineOptions({ inheritAttrs: false });
</script>
