<script setup lang="ts">
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

// A terminal on a phone: the soft keyboard has no Esc, Tab, arrows or Ctrl, and
// typing into xterm's hidden textarea is fiddly. A line to send plus the keys a
// TUI agent asks for. Esc and ^C go out as the single byte App.WritePty reads
// as an interrupt (agentphase.Interrupt).
const props = defineProps<{ ptyId: number }>();
const line = ref("");

const write = (s: string) => invoke("write_pty", { id: props.ptyId, data: Array.from(new TextEncoder().encode(s)) }).catch(() => {});

const keys = [
  { label: "Esc", seq: "\x1b" },
  { label: "Tab", seq: "\t" },
  { label: "↑", seq: "\x1b[A" },
  { label: "↓", seq: "\x1b[B" },
  { label: "←", seq: "\x1b[D" },
  { label: "→", seq: "\x1b[C" },
  { label: "^C", seq: "\x03" },
  { label: "⏎", seq: "\r" },
];

function send() {
  // Text, then Enter on its own: a TUI that sees "text\r" in one read can take
  // it as a paste and keep the newline in its input box.
  if (line.value) void write(line.value);
  void write("\r");
  line.value = "";
}
</script>

<template>
  <div class="phone-keys shrink-0 border-t border-border bg-panel">
    <div class="flex gap-1 overflow-x-auto px-2 pt-2">
      <button v-for="k in keys" :key="k.label" type="button" class="key" @click="write(k.seq)">{{ k.label }}</button>
    </div>
    <form class="flex gap-2 px-2 py-2" @submit.prevent="send">
      <input
        v-model="line"
        class="min-w-0 flex-1 rounded-lg border border-border bg-base px-3 text-[15px] text-foreground outline-none focus:border-accent"
        style="height: 38px"
        placeholder="Type a line…"
        enterkeyhint="send"
        autocapitalize="off"
        autocorrect="off"
        spellcheck="false"
      />
      <button type="submit" class="key key--send">Send</button>
    </form>
  </div>
</template>

<style scoped>
.phone-keys { padding-bottom: env(safe-area-inset-bottom, 0px); }
.key {
  flex-shrink: 0;
  min-width: 44px;
  height: 38px;
  padding: 0 10px;
  border-radius: 8px;
  border: 1px solid var(--border);
  font: 500 14px/1 ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--text-primary);
}
.key:active { background: var(--bg-hover); }
.key--send { font-family: inherit; background: var(--accent); border-color: var(--accent); color: var(--accent-foreground, #fff); }
</style>
