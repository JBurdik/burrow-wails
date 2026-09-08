<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef, watch } from "vue";
import { agentIconComp } from "@/lib/agentIcons";
import { providerFor } from "@/lib/providers";
import { useRemoteStore } from "../store";

const store = useRemoteStore();
const chat = computed(() => store.activeChat);
const provider = computed(() => providerFor(chat.value?.agentKind ?? ""));
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
  <div v-if="store.reconnecting" class="reconnect-banner">Připojuji znovu…</div>
  <div v-if="chat?.pendingPermission" class="perm-banner">
    <span class="perm-text">{{ chat.pendingPermission.toolName }}<template v-if="chat.pendingPermission.detail"> — {{ chat.pendingPermission.detail }}</template></span>
    <div class="perm-actions">
      <button type="button" class="perm-allow" @click="store.respondChatPermission(chat!.id, true)">Povolit</button>
      <button type="button" class="perm-deny" @click="store.respondChatPermission(chat!.id, false)">Zamítnout</button>
    </div>
  </div>
  <main ref="feed" class="chat-feed">
    <article v-for="message in chat?.messages" :key="message.id" :class="['bubble-row', `bubble-row--${message.role}`]">
      <span v-if="message.role === 'assistant'" class="bubble-avatar" :style="{ color: provider.color }" aria-hidden="true"><component :is="agentIconComp(provider.icon)" :size="14" /></span>
      <span v-if="message.role === 'tool'" :class="['tool', { 'tool--failed': message.toolFailed }]">
        <span class="tool-title">{{ message.toolFailed ? '⚠' : '⌘' }} {{ message.text }}</span>
        <!-- The store already parses tool output and the failed flag off the
             domain events; not rendering them was the whole difference between
             this client and the desktop on a tool call that went wrong. -->
        <span v-if="message.toolOutput" class="tool-output">{{ message.toolOutput }}</span>
      </span>
      <p v-else>{{ message.text }}</p>
    </article>
    <div v-if="chat?.busy" class="typing"><i></i><i></i><i></i> Agent pracuje</div>
  </main>
  <form class="chat-composer" @submit.prevent="send"><textarea v-model="draft" :disabled="chat?.busy || !!chat?.pendingPermission" placeholder="Napiš agentovi…" rows="1" enterkeyhint="send" @keydown.enter.exact.prevent="send"/><button type="submit" :disabled="!draft.trim() || chat?.busy || !!chat?.pendingPermission">↑</button></form>
</template>

<style scoped>
.live{color:var(--text-muted);font:700 10px/1 var(--font-mono)}
.live--busy{color:var(--status-running)}
.chat-feed{flex:1;min-height:0;overflow:auto;padding:18px 15px 100px;display:flex;flex-direction:column;gap:12px}
.bubble-row{display:flex;align-items:flex-end;gap:8px}
.bubble-row--user{justify-content:flex-end}
.bubble-row--tool{justify-content:center}

/* small bordered mark before an assistant bubble — the "agent" avatar */
.bubble-avatar{
  flex-shrink:0;
  width:26px;height:26px;
  display:grid;place-items:center;
  border:1px solid var(--border);
  border-radius:8px;
  background:var(--bg-hover);
  color:var(--accent);
  font-size:12px;
}

.bubble-row p{
  max-width:80%;
  margin:0;
  padding:10px 14px;
  border:1px solid var(--border);
  border-radius:16px;
  background:var(--bg-panel);
  color:var(--text-primary);
  white-space:pre-wrap;
  overflow-wrap:anywhere;
  font-size:14px;
  line-height:1.45;
}
.bubble-row--user p{
  background:var(--accent);
  border:none;
  color:#fff;
}
.bubble-row--thinking p{color:var(--text-muted);font-style:italic;background:transparent;border-style:dashed}

.tool{
  display:flex;
  max-width:92%;
  flex-direction:column;
  gap:6px;
  overflow:hidden;
  padding:10px 12px;
  background:var(--bg-hover);
  border:1px solid var(--border);
  border-radius:14px;
}
.tool-title{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px;color:var(--text-secondary)}
.tool--failed .tool-title{color:var(--status-error)}
.tool-output{max-height:9em;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;border-left:2px solid var(--border);padding-left:8px;font:12px var(--font-mono);color:var(--status-running)}
.tool--failed .tool-output{color:var(--status-error)}

.typing{display:flex;align-items:center;gap:5px;padding:6px 4px;color:var(--text-muted);font-size:11px}
.typing i{width:4px;height:4px;border-radius:50%;background:var(--status-running);animation:pulse 1s infinite}
.typing i:nth-child(2){animation-delay:.15s}
.typing i:nth-child(3){animation-delay:.3s}

.chat-composer{position:fixed;right:0;bottom:0;left:0;display:flex;align-items:flex-end;gap:8px;padding:9px 12px calc(9px + var(--safe-bottom));border-top:1px solid var(--border);background:var(--bg-panel)}
.chat-composer textarea{
  flex:1;
  max-height:100px;
  resize:none;
  border:1px solid var(--border);
  border-radius:20px;
  background:var(--bg-panel);
  color:var(--text-primary);
  padding:10px 16px;
  /* Kept at 16px rather than the design's 14px: iOS Safari auto-zooms on
     focus for inputs under 16px, which is a real usability regression, not
     a visual one — see the report for why this one wasn't matched exactly. */
  font:16px/1.3 var(--font-ui);
}
.chat-composer textarea::placeholder{color:var(--text-muted)}
.chat-composer button{
  width:34px;height:34px;
  flex-shrink:0;
  display:grid;place-items:center;
  border:0;
  border-radius:50%;
  background:var(--accent);
  color:#fff;
  font-size:18px;
}
.chat-composer button:disabled{opacity:.42}
@keyframes pulse{50%{opacity:.25;transform:translateY(-2px)}}

.reconnect-banner{padding:6px 15px;background:color-mix(in srgb,var(--yellow) 18%,var(--bg-panel));color:var(--yellow);font:11px/1.3 var(--font-mono);text-align:center}
.perm-banner{display:flex;align-items:center;justify-content:space-between;gap:10px;padding:10px 15px;background:color-mix(in srgb,var(--status-permission) 14%,var(--bg-panel));border-bottom:1px solid var(--border)}
.perm-text{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:12px var(--font-mono);color:var(--text-primary)}
.perm-actions{display:flex;gap:6px;flex-shrink:0}
.perm-allow,.perm-deny{min-height:34px;padding:0 12px;border-radius:10px;border:1px solid var(--border);font-size:12px;font-weight:700}
.perm-allow{background:var(--accent);border-color:var(--accent);color:#fff}
.perm-deny{background:transparent;color:var(--red);border-color:var(--red)}
</style>
