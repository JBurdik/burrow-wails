<script setup lang="ts">
// Connection indicator for a paired device (phone header, Sidebar foot on a
// wide paired browser). Shows nothing while connected or during a short blip
// (src/runtime/linkStatus.ts debounces); tap = try to reconnect now.
import { onMounted } from "vue";
import { PhArrowsClockwise, PhWifiSlash } from "@phosphor-icons/vue";
import { heldCount, linkState, startOffline, wakeLink } from "@/lib/offline";

onMounted(startOffline);
</script>

<template>
  <button
    v-if="linkState !== 'up'"
    class="link-status flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-hover"
    :title="linkState === 'offline' ? 'Offline. Tap to reconnect.' : 'Reconnecting… Tap to retry now.'"
    @click="wakeLink"
  >
    <PhWifiSlash v-if="linkState === 'offline'" :size="13" />
    <PhArrowsClockwise v-else :size="13" class="animate-spin" />
    <span>{{ linkState === "offline" ? "Offline" : "Reconnecting" }}</span>
    <span v-if="heldCount" class="opacity-70">· {{ heldCount }} waiting</span>
  </button>
</template>
