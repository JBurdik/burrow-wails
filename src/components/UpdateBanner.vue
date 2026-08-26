<template>
  <Transition name="update-pop">
    <div v-if="show" class="fixed bottom-4 right-4 z-[9998] flex w-80 gap-2.5 rounded-xl border border-border bg-panel p-3.5 shadow-[0_16px_48px_rgba(0,0,0,0.55)]">
      <div class="mt-px shrink-0 text-accent" :class="{ 'text-secondary-foreground': u.downloading }">
        <PhArrowCircleUp v-if="!u.installed" :size="20" weight="fill" />
        <PhCheckCircle v-else :size="20" weight="fill" />
      </div>

      <div class="min-w-0 flex-1">
        <!-- installed → awaiting relaunch -->
        <template v-if="u.installed">
          <div class="text-[12.5px] font-semibold text-foreground">Update ready</div>
          <div class="mt-0.5 text-[11.5px] text-secondary-foreground">Restart Burrow to finish updating to v{{ u.newVersion }}.</div>
        </template>

        <!-- downloading -->
        <template v-else-if="u.downloading">
          <div class="text-[12.5px] font-semibold text-foreground">Downloading v{{ u.newVersion }}…</div>
          <div class="mt-2 h-1 overflow-hidden rounded-full bg-hover">
            <div
              class="h-full rounded-full bg-accent transition-[width] duration-150 ease-out"
              :class="{ 'w-[35%] animate-[update-slide_1.1s_ease-in-out_infinite]': u.progress < 0 }"
              :style="u.progress >= 0 ? { width: Math.round(u.progress * 100) + '%' } : {}"
            />
          </div>
        </template>

        <!-- available -->
        <template v-else>
          <div class="text-[12.5px] font-semibold text-foreground">Update available</div>
          <div class="mt-0.5 text-[11.5px] text-secondary-foreground">
            v{{ u.newVersion }} is ready to install
            <span class="text-muted-foreground">(you have v{{ u.currentVersion }})</span>
          </div>
          <div v-if="u.notes" class="mt-1.5 max-h-16 overflow-y-auto whitespace-pre-wrap text-[11px] leading-snug text-muted-foreground">{{ u.notes }}</div>
        </template>
      </div>

      <div class="flex shrink-0 flex-col gap-1.5 self-center">
        <template v-if="u.installed">
          <button class="whitespace-nowrap rounded-lg border border-transparent bg-accent px-3 py-1 text-[11.5px] font-semibold text-white hover:brightness-110" @click="u.relaunch()">Restart</button>
        </template>
        <template v-else-if="!u.downloading">
          <button class="whitespace-nowrap rounded-lg border border-transparent bg-accent px-3 py-1 text-[11.5px] font-semibold text-white hover:brightness-110" @click="u.downloadAndInstall()">Install</button>
          <button class="whitespace-nowrap rounded-lg border border-border bg-transparent px-3 py-1 text-[11.5px] font-semibold text-secondary-foreground hover:bg-hover" @click="u.dismiss()">Later</button>
        </template>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { PhArrowCircleUp, PhCheckCircle } from "@phosphor-icons/vue";
import { useUpdateStore } from "@/stores/update";

const u = useUpdateStore();
// Stays mounted while installed (restart prompt) even though banner was dismissible.
const show = computed(() => u.bannerVisible || u.downloading || u.installed);
</script>

<style scoped>
@keyframes update-slide {
  0% { margin-left: -35%; }
  100% { margin-left: 100%; }
}

.update-pop-enter-active,
.update-pop-leave-active { transition: all 0.22s cubic-bezier(0.16, 1, 0.3, 1); }
.update-pop-enter-from,
.update-pop-leave-to { opacity: 0; transform: translateY(12px) scale(0.97); }
</style>
