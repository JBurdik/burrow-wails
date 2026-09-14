<template>
  <Transition name="update-pop">
    <div v-if="show" class="fixed bottom-[108px] right-4 z-[9998] flex w-80 gap-2.5 rounded-xl border border-border bg-panel p-3.5 shadow-[0_16px_48px_rgba(0,0,0,0.55)]">
      <div class="mt-px shrink-0 text-amber-500">
        <PhWarning :size="20" weight="fill" />
      </div>

      <div class="min-w-0 flex-1">
        <div class="text-[12.5px] font-semibold text-foreground">
          Updates Available: {{ outdated.length }} provider{{ outdated.length === 1 ? "" : "s" }}
        </div>
        <div class="mt-0.5 text-[11.5px]" :class="error ? 'text-red-400' : 'text-secondary-foreground'">
          {{ error || (updating ? "Installing…" : "Install the update now or review provider settings.") }}
        </div>
      </div>

      <!-- mt, not self-center: centred, the column's top-right corner landed
           underneath the absolutely-positioned dismiss X below. -->
      <div class="mt-3.5 flex shrink-0 flex-col gap-1.5">
        <button class="whitespace-nowrap rounded-lg border border-transparent bg-accent px-3 py-1 text-[11.5px] font-semibold text-white hover:brightness-110 disabled:opacity-60" :disabled="updating" @click="installUpdates">{{ updating ? "Updating…" : "Update" }}</button>
        <button class="whitespace-nowrap rounded-lg border border-border bg-transparent px-3 py-1 text-[11.5px] font-semibold text-secondary-foreground hover:bg-hover" @click="ui.openSettings('providers')">Settings</button>
      </div>

      <button class="absolute right-2 top-2 rounded p-1 text-secondary-foreground hover:bg-hover hover:text-foreground" title="Dismiss" @click="dismiss">
        <PhX :size="14" />
      </button>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed } from "vue";
import { PhWarning, PhX } from "@phosphor-icons/vue";
import { useProvidersStore } from "@/stores/providers";
import { useUIStore } from "@/stores/ui";
import { configReady, getConfig, setConfig } from "@/lib/config";

const DISMISS_KEY = "providerUpdateDismissedSet";

const providers = useProvidersStore();
const ui = useUIStore();

const outdated = computed(() => providers.outdated);
/** Signature of the currently-outdated set — re-nags only once its membership changes. */
const signature = computed(() => outdated.value.map((a) => `${a.providerId}@${providers.latest[a.providerId]?.version}`).sort().join(","));

const dismissed = ref("");
configReady.then(() => { dismissed.value = getConfig<string>(DISMISS_KEY, ""); });

const show = computed(() => outdated.value.length > 0 && signature.value !== dismissed.value);

function dismiss() {
  dismissed.value = signature.value;
  setConfig(DISMISS_KEY, signature.value);
}

const updating = ref(false);
const error = ref("");

/** Runs the install in-process (Go side) and re-probes, instead of typing a
 * command into a terminal tab — that required a mounted workspace and never
 * reflected whether the install actually worked.
 *
 * store.updateProvider re-probes per provider, so one failure no longer
 * strands its neighbours: this used to bail on the first bad result before
 * probing ANYTHING, leaving a provider that updated fine still listed as out
 * of date here and in Settings. Each provider now leaves `outdated` on its
 * own merit, and the two surfaces shrink together because they read the same
 * computed. */
async function installUpdates() {
  updating.value = true;
  error.value = "";
  const providerIds = [...new Set(outdated.value.map((a) => a.providerId))];
  const results = await Promise.all(providerIds.map((id) => providers.updateProvider(id)));
  updating.value = false;
  const failed = results.find((r) => !r.ok);
  if (failed) {
    // Not dismissed: whatever is still listed is genuinely still out of date.
    error.value = failed.error || "Update failed.";
    return;
  }
  dismiss();
}
</script>

<style scoped>
.update-pop-enter-active,
.update-pop-leave-active { transition: all 0.22s cubic-bezier(0.16, 1, 0.3, 1); }
.update-pop-enter-from,
.update-pop-leave-to { opacity: 0; transform: translateY(12px) scale(0.97); }
</style>
