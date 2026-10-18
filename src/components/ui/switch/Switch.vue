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
        'peer inline-flex h-[22px] w-[38px] shrink-0 cursor-pointer appearance-none items-center rounded-full border transition-colors disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:border-transparent data-[state=checked]:bg-success data-[state=unchecked]:border-border data-[state=unchecked]:bg-hover',
        props.class,
      )
    "
  >
    <SwitchThumb
      class="pointer-events-none block h-4 w-4 translate-x-[2px] rounded-full bg-white shadow-[0_1px_2px_rgba(0,0,0,0.25)] transition-transform data-[state=checked]:translate-x-[18px]"
    />
  </SwitchRoot>
</template>
