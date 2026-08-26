<script setup lang="ts">
import { type HTMLAttributes, computed } from "vue";
import { SwitchRoot, type SwitchRootProps, SwitchThumb, useForwardProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<
  Omit<SwitchRootProps, "modelValue"> & { class?: HTMLAttributes["class"]; checked?: boolean }
>();
const emits = defineEmits<{ "update:checked": [value: boolean] }>();

const delegated = computed(() => {
  const { class: _, checked: __, ...rest } = props;
  return rest;
});
const forwarded = useForwardProps(delegated);
</script>

<template>
  <SwitchRoot
    v-bind="forwarded"
    :model-value="checked"
    @update:model-value="(v) => emits('update:checked', v as boolean)"
    :class="
      cn(
        'peer inline-flex h-5 w-9 shrink-0 cursor-pointer appearance-none items-center rounded-full border border-border transition-colors data-[state=checked]:bg-accent data-[state=unchecked]:bg-hover',
        props.class,
      )
    "
  >
    <SwitchThumb
      class="pointer-events-none block h-3.5 w-3.5 translate-x-0.5 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-[18px]"
    />
  </SwitchRoot>
</template>
