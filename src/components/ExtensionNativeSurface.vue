<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { PhArrowLeft, PhCheckCircle, PhMagnifyingGlass, PhPlay } from "@phosphor-icons/vue";
import type { NativeSurfaceNode } from "@/composables/useExtensionSurfaces";

const props = defineProps<{ title: string; node: NativeSurfaceNode }>();
const selectedItem = shallowRef<NativeSurfaceNode | null>(null);
const actionMessage = shallowRef("");
const isDetail = computed(() => selectedItem.value?.actions || selectedItem.value?.subtitle);
const visibleNode = computed(() => selectedItem.value ?? props.node);
const listItems = computed(() => (props.node.children ?? []).flatMap((node) => node.type === "section" ? node.children ?? [] : [node]));

function selectItem(item: NativeSurfaceNode) {
  selectedItem.value = item;
  actionMessage.value = "";
}

function selectAction(action: NativeSurfaceNode) {
  actionMessage.value = `${action.title ?? action.id ?? "Action"} selected`;
}

function backToList() {
  selectedItem.value = null;
  actionMessage.value = "";
}
</script>

<template>
  <section class="native-surface">
    <template v-if="visibleNode.type === 'list'">
      <header class="native-header"><div><p class="native-eyebrow">EXTENSION SURFACE</p><h2>{{ visibleNode.title || props.title }}</h2></div></header>
      <label v-if="visibleNode.searchPlaceholder" class="native-search"><PhMagnifyingGlass :size="14" /><input :placeholder="visibleNode.searchPlaceholder" /></label>
      <div class="native-list">
        <button v-for="item in listItems" :key="item.id || item.title" class="native-row" type="button" @click="selectItem(item)">
          <span><strong>{{ item.title }}</strong><small v-if="item.subtitle">{{ item.subtitle }}</small></span>
          <em v-if="item.accessories?.length">{{ item.accessories.join(' · ') }}</em>
        </button>
      </div>
    </template>

    <template v-else-if="isDetail">
      <header class="native-header"><button class="native-back" type="button" @click="backToList"><PhArrowLeft :size="14" /> Back</button><p class="native-eyebrow">{{ props.title }}</p><h2>{{ visibleNode.title }}</h2></header>
      <p v-if="visibleNode.subtitle" class="native-subtitle">{{ visibleNode.subtitle }}</p>
      <pre v-if="visibleNode.markdown" class="native-markdown">{{ visibleNode.markdown }}</pre>
      <div v-if="visibleNode.actions?.children?.length" class="native-actions"><button v-for="action in visibleNode.actions.children" :key="action.id || action.title" type="button" :class="action.style === 'destructive' && 'destructive'" @click="selectAction(action)"><PhPlay :size="13" />{{ action.title }}</button></div>
      <p v-if="actionMessage" class="native-feedback"><PhCheckCircle :size="14" />{{ actionMessage }}</p>
    </template>

    <template v-else-if="visibleNode.type === 'detail'">
      <header class="native-header"><h2>{{ visibleNode.title }}</h2></header><pre class="native-markdown">{{ visibleNode.markdown }}</pre>
    </template>

    <p v-else class="native-empty">This native surface node is reserved for a future renderer.</p>
  </section>
</template>

<style scoped>
.native-surface { display: flex; min-height: 0; flex: 1; flex-direction: column; overflow: auto; color: var(--text-primary); }.native-header { border-bottom: 1px solid var(--border); padding: 16px; }.native-header h2 { margin: 2px 0 0; font-size: 14px; }.native-eyebrow { margin: 0; color: var(--text-muted); font-size: 9px; font-weight: 700; letter-spacing: .09em; }.native-search { display: flex; align-items: center; gap: 7px; margin: 12px; border: 1px solid var(--border); border-radius: 6px; color: var(--text-muted); padding: 7px 8px; }.native-search input { min-width: 0; flex: 1; border: 0; outline: 0; background: transparent; color: inherit; font: inherit; }.native-list { padding: 0 8px 10px; }.native-row { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 10px; border: 0; border-radius: 6px; background: transparent; color: inherit; cursor: pointer; padding: 10px 8px; text-align: left; }.native-row:hover { background: var(--bg-hover); }.native-row span { display: flex; min-width: 0; flex-direction: column; gap: 3px; }.native-row strong { font-size: 12px; font-weight: 600; }.native-row small, .native-row em, .native-subtitle { color: var(--text-secondary); font-size: 11px; font-style: normal; }.native-row em { flex: 0 0 auto; font-family: var(--font-mono); }.native-back { display: inline-flex; align-items: center; gap: 4px; margin: 0 0 10px; border: 0; background: transparent; color: var(--text-secondary); cursor: pointer; padding: 0; font: inherit; font-size: 11px; }.native-markdown { margin: 16px; white-space: pre-wrap; color: var(--text-secondary); font-family: var(--font-mono); font-size: 11px; line-height: 1.65; }.native-subtitle { margin: 16px 16px 0; }.native-actions { display: flex; flex-wrap: wrap; gap: 7px; margin: 16px; }.native-actions button { display: inline-flex; align-items: center; gap: 5px; border: 1px solid var(--border); border-radius: 5px; background: var(--bg-panel); color: var(--text-secondary); cursor: pointer; padding: 6px 8px; font: inherit; font-size: 11px; }.native-actions button:hover { background: var(--bg-hover); color: var(--text-primary); }.native-actions button.destructive { color: var(--red, #ef7373); }.native-feedback { display: flex; align-items: center; gap: 5px; margin: 0 16px; color: var(--green, #4fc68a); font-size: 11px; }.native-empty { margin: 16px; color: var(--text-secondary); font-size: 11px; }
</style>
