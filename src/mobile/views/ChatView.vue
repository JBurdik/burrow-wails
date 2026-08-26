<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from "vue";
import { useRemoteStore } from "../store";

const store = useRemoteStore();
const chat = computed(() => store.activeChat);
const draft = ref("");
const feed = useTemplateRef<HTMLElement>("feed");

function send() {
  const text = draft.value.trim();
  if (!text) return;
  store.sendChat(text);
  draft.value = "";
}
watch(() => chat.value?.messages.length, () => nextTick(() => { if (feed.value) feed.value.scrollTop = feed.value.scrollHeight; }));
</script>

<template>
  <header class="m-nav"><button class="m-nav-back" type="button" @click="store.closeChat">‹ Chaty</button><span class="m-nav-title">{{ chat?.title }}</span><span :class="['live', { 'live--busy': chat?.busy }]">{{ chat?.busy ? 'LIVE' : 'READY' }}</span></header>
  <main ref="feed" class="chat-feed">
    <article v-for="message in chat?.messages" :key="message.id" :class="['bubble-row', `bubble-row--${message.role}`]">
      <span v-if="message.role === 'tool'" class="tool">⌘ {{ message.text }}</span>
      <p v-else>{{ message.text }}</p>
    </article>
    <div v-if="chat?.busy" class="typing"><i></i><i></i><i></i> Agent pracuje</div>
  </main>
  <form class="chat-composer" @submit.prevent="send"><textarea v-model="draft" :disabled="chat?.busy" placeholder="Napiš agentovi…" rows="1" enterkeyhint="send" @keydown.enter.exact.prevent="send"/><button type="submit" :disabled="!draft.trim() || chat?.busy">↑</button></form>
</template>

<style scoped>
.live{color:var(--text-muted);font:700 10px/1 var(--font-mono)}.live--busy{color:var(--yellow)}.chat-feed{flex:1;min-height:0;overflow:auto;padding:18px 15px 100px;display:flex;flex-direction:column;gap:10px}.bubble-row{display:flex}.bubble-row p{max-width:86%;margin:0;padding:10px 12px;border:1px solid var(--border);border-radius:12px;color:var(--text-primary);white-space:pre-wrap;overflow-wrap:anywhere;font-size:13px;line-height:1.45}.bubble-row--user{justify-content:flex-end}.bubble-row--user p{background:var(--bg-selected);border-color:color-mix(in srgb,var(--accent) 55%,var(--border))}.bubble-row--thinking p{color:var(--text-muted);font-style:italic}.bubble-row--tool{justify-content:center}.tool{max-width:95%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-muted);font:11px/1.2 var(--font-mono)}.typing{display:flex;align-items:center;gap:5px;padding:6px 4px;color:var(--text-muted);font-size:11px}.typing i{width:4px;height:4px;border-radius:50%;background:var(--yellow);animation:pulse 1s infinite}.typing i:nth-child(2){animation-delay:.15s}.typing i:nth-child(3){animation-delay:.3s}.chat-composer{position:fixed;right:0;bottom:0;left:0;display:flex;align-items:flex-end;gap:8px;padding:9px 12px calc(9px + var(--safe-bottom));border-top:1px solid var(--border);background:var(--bg-panel)}.chat-composer textarea{flex:1;max-height:100px;resize:none;border:1px solid var(--border);border-radius:10px;background:var(--bg-hover);color:var(--text-primary);padding:10px 11px;font:16px/1.3 var(--font-ui)}.chat-composer button{width:39px;height:39px;border:0;border-radius:10px;background:var(--accent);color:white;font-size:20px}.chat-composer button:disabled{opacity:.42}@keyframes pulse{50%{opacity:.25;transform:translateY(-2px)}}
</style>
