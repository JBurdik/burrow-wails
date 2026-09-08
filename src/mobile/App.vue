<template>
  <div class="m-root">
    <ConnectView v-if="store.view === 'connect'" />
    <WelcomeView v-else-if="store.view === 'welcome'" />
    <ChatsView v-else-if="store.view === 'chats'" />
    <ChatView v-else-if="store.view === 'chat'" />
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue';
import { useRemoteStore } from './store';
import ConnectView from './views/ConnectView.vue';
import WelcomeView from './views/WelcomeView.vue';
import ChatsView from './views/ChatsView.vue';
import ChatView from './views/ChatView.vue';

const store = useRemoteStore();

onMounted(() => {
  // A stored device token is all that is needed: the transport asks for a
  // fresh ticket itself, so there is nothing to restore beyond "are we
  // paired". A revoked token surfaces as RevokedError and drops the view back
  // to the pairing screen rather than spinning.
  if (store.credentials) store.connect().catch(() => {});
});
</script>

<style>
/* Dark theme token baseline — same keys as desktop App.vue :root.
   Values below are the pen.dev "Agent List" / "Chat" reference palette;
   --bg-hover is repurposed from a plain hover-flash tone into the design's
   dedicated "elevated" surface (avatars, tool-call blocks, pill backgrounds)
   — same variable name, new role, so every existing var(--bg-hover) still
   resolves correctly without a rename pass. */
:root {
  --bg-base:        #0A0B0F;
  --bg-panel:       #14161C; /* surface: nav bars, cards, bubbles, composer */
  --bg-hover:       #1B1E26; /* elevated: avatars, tool blocks, pill fills, pressed rows */
  --bg-selected:    #2C2D4A; /* accent-tinted elevated, e.g. pressed on-screen keys */
  --border:         #262A33;
  --text-primary:   #F2F3F5;
  --text-secondary: #8B909C;
  --text-muted:     #5C6270;
  --accent:         #7C6FF0;
  --accent-dim:     #5A50B0;
  --green:          #4ADE80;
  --yellow:         #FBBF24;
  --red:            #F87171;

  /* status dot tokens */
  --status-running:    #4ADE80;
  --status-waiting:    #FBBF24;
  --status-permission: #60A5FA;
  --status-done:       #60A5FA;
  --status-review:     #60A5FA;
  --status-error:      #F87171;

  --font-mono: 'JetBrains Mono', 'SF Mono', 'Cascadia Code', 'Menlo', 'Consolas', monospace;
  --font-ui:   'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;

  --safe-top:    env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
}

*, *::before, *::after { box-sizing: border-box; }

html, body {
  margin: 0; padding: 0;
  background: var(--bg-base);
  color: var(--text-primary);
  font-family: var(--font-ui);
  font-size: 14px;
  line-height: 1.4;
  -webkit-font-smoothing: antialiased;
  overscroll-behavior: none;
}

.m-root {
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
  /* Flat throughout: the dark base fill plus borders is what separates
     surfaces, not a decorative glow. */
  background: var(--bg-base);
}

button, input { font: inherit; }
button { -webkit-tap-highlight-color: transparent; }
button:focus-visible, input:focus-visible, [role="link"]:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

/* ── shared nav bar ── */
.m-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: calc(var(--safe-top) + 8px) 16px 8px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  min-height: 56px;
}
.m-nav-title {
  font-family: var(--font-ui);
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.m-nav-back {
  background: none;
  border: none;
  color: var(--accent);
  font-size: 14px;
  font-weight: 600;
  padding: 4px 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

/* ── scrollable body ── */
.m-body {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding-bottom: var(--safe-bottom);
}

.m-btn {
  width: 100%;
  background: var(--accent);
  color: #f8fafc;
  border: none;
  border-radius: 10px;
  font-size: 14px;
  font-weight: 600;
  padding: 12px;
  cursor: pointer;
  transition: opacity 0.15s;
}
.m-btn:active  { opacity: 0.8; }
.m-btn:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
