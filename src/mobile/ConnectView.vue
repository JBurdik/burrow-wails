<template>
  <div class="flex min-h-dvh flex-col items-center justify-center bg-base px-6 pb-[calc(env(safe-area-inset-bottom)+32px)] pt-8 text-foreground" style="font-family: Inter, -apple-system, system-ui, sans-serif">
    <div v-if="machines.length" class="mb-8 flex w-full max-w-[340px] flex-col gap-2">
      <h1 class="text-[17px] font-semibold">Your machines</h1>
      <div class="overflow-hidden rounded-[10px] border border-border">
        <button
          v-for="m in machines"
          :key="m.environment_id"
          type="button"
          class="flex w-full items-center gap-3 border-border bg-panel px-3 py-3 text-left [&+&]:border-t active:bg-hover"
          @click="useMachine(m.environment_id)"
        >
          <span class="flex min-w-0 flex-1 flex-col"><span class="truncate text-[14px] text-foreground">{{ m.name }}</span><span class="truncate text-[12px] text-muted-foreground">{{ hostLabel(m.baseUrl) }}</span></span>
          <PhCaretRight :size="15" class="text-muted-foreground" />
        </button>
      </div>
    </div>

    <form class="flex w-full max-w-[340px] flex-col gap-5" @submit.prevent="tryPair">
      <div class="flex flex-col gap-1">
        <h1 class="text-[17px] font-semibold">{{ machines.length ? "Add a machine" : "Connect to Burrow" }}</h1>
        <input v-model="url" type="url" autocapitalize="off" autocorrect="off" spellcheck="false" :disabled="busy" class="rounded-[8px] border border-border bg-panel px-2.5 py-2 text-[13px] text-foreground outline-none focus:border-accent" />
      </div>

      <div class="flex flex-col gap-2">
        <span class="text-[12px] font-medium text-secondary-foreground">Pairing code</span>
        <PinInputRoot v-model="digits" type="number" placeholder="•" :disabled="busy" class="flex gap-2" @complete="tryPair">
          <PinInputInput
            v-for="i in 6"
            :key="i"
            :index="i - 1"
            class="h-12 w-0 flex-1 rounded-[10px] border border-border bg-panel text-center font-mono text-[20px] text-foreground outline-none focus:border-accent"
          />
        </PinInputRoot>
      </div>

      <p v-if="err" class="text-[12px] text-destructive" role="alert">{{ err }}</p>

      <Button type="submit" :disabled="busy || digits.length < 6">{{ busy ? "Connecting…" : "Connect" }}</Button>

      <p class="text-[12px] leading-relaxed text-muted-foreground">
        Find the six-digit code on your Mac in Settings → Remote access. It works once; after pairing this device reconnects on its own.
      </p>
    </form>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import { PinInputRoot, PinInputInput } from "reka-ui";
import { Button } from "@/components/ui/button";
import { PhCaretRight } from "@phosphor-icons/vue";
import { listEnvironments, pairDevice, setActiveEnvironment } from "@/runtime/remoteEndpoint";

// Served from a paired Mac itself (directly or under Tailscale Serve's
// /burrow prefix), so the page's own location is the default address. Editable
// to add another machine: /v2/pair and /v2/ws-ticket answer CORS, /v2/ws takes
// any origin.
const url = ref(`${location.origin}${location.pathname.replace(/\/(?:mobile\.html)?$/, "")}`);
// Shown when no machine is active: after "Add machine", a revoke, or a QR scan.
const machines = listEnvironments();
const hostLabel = (u: string) => u.replace(/^https?:\/\//, "");
function useMachine(id: string) {
  setActiveEnvironment(id);
  location.reload();
}
// Set when the page was opened from the Settings QR (main.ts strips it from the URL).
const props = defineProps<{ code?: string }>();
const digits = ref<number[]>(props.code ? props.code.split("").map(Number) : []);
const busy = ref(false);
const err = ref("");

function deviceInfo(): [name: string, kind: string] {
  const ua = navigator.userAgent;
  // iPadOS reports itself as a Mac; touch points tell them apart.
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return ["iPad", "tablet"];
  if (/iPhone/.test(ua)) return ["iPhone", "phone"];
  if (/Android/.test(ua)) return ["Android phone", "phone"];
  return ["Browser", "browser"];
}

onMounted(() => { if (props.code) void tryPair(); });

async function tryPair() {
  const code = digits.value.join("");
  if (busy.value || code.length < 6) return;
  busy.value = true;
  err.value = "";
  try {
    await pairDevice(url.value, code, ...deviceInfo()); // adds to the catalog, makes it active
    location.reload(); // main.ts boots the app now that they exist
  } catch (e: any) {
    // Told apart by type: a refused code and an unreachable host are fixed differently.
    err.value = e?.name === "PairingRefusedError"
      ? "Wrong or expired code. Generate a new one in Settings on your Mac."
      : e?.message ?? String(e);
    digits.value = [];
  } finally {
    busy.value = false;
  }
}
</script>
