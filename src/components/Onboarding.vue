<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { PhArrowLeft, PhArrowRight, PhCheck, PhCommand, PhPaintBrush, PhRobot } from "@phosphor-icons/vue";
import { THEME_FAMILIES, findTheme } from "@/themes";
import { useUIStore } from "@/stores/ui";
import { useProvidersStore } from "@/stores/providers";

const emit = defineEmits<{ complete: []; skip: [] }>();
const ui = useUIStore();
const providers = useProvidersStore();
const step = shallowRef(0);
const selectedAgent = shallowRef(ui.defaultChatAgent);
const steps = ["Welcome", "Default agent", "Appearance", "Ready"];
// One entry per theme family (the meme one stays out of onboarding). Picking
// here sets the family for both light and dark — the pair is the product.
const availableThemes = computed(() =>
  THEME_FAMILIES.filter((f) => f.key !== "stonks").map((f) => ({
    key: f.key,
    label: f.label,
    light: findTheme(f.light),
    dark: findTheme(f.dark),
  })),
);

function next() {
  if (step.value < steps.length - 1) step.value++;
  else finish();
}
function back() { if (step.value > 0) step.value--; }
function finish() {
  ui.defaultChatAgent = selectedAgent.value;
  emit("complete");
}
</script>

<template>
  <div class="onboarding" role="dialog" aria-modal="true" aria-label="Welcome to Burrow">
    <aside class="onboarding-rail">
      <div class="onboarding-wordmark"><PhCommand :size="17" weight="bold" /> Burrow</div>
      <ol class="onboarding-steps">
        <li v-for="(label, index) in steps" :key="label" :class="{ active: step === index, done: step > index }">
          <span>{{ step > index ? "✓" : index + 1 }}</span>{{ label }}
        </li>
      </ol>
      <button class="onboarding-skip" type="button" @click="emit('skip')">Skip for now</button>
    </aside>

    <main class="onboarding-main">
      <section v-if="step === 0" class="onboarding-page onboarding-welcome">
        <div class="onboarding-mark"><PhCommand :size="28" weight="bold" /></div>
        <p class="onboarding-kicker">A focused workspace for agentic work</p>
        <h1>Welcome to Burrow</h1>
        <p class="onboarding-copy">Run AI coding agents in real terminals, keep their work separated, and always know what needs your attention.</p>
        <div class="onboarding-facts">
          <span><PhRobot :size="16" /> Manage agents in parallel</span>
          <span><PhCommand :size="16" /> Keep keyboard flow</span>
        </div>
      </section>

      <section v-else-if="step === 1" class="onboarding-page">
        <div class="onboarding-icon"><PhRobot :size="22" /></div>
        <p class="onboarding-kicker">Your starting point</p>
        <h1>Choose a default agent</h1>
        <p class="onboarding-copy">Used for new chats. You can add, remove, or configure agents later in Settings.</p>
        <div class="onboarding-options">
          <button v-for="agent in providers.chatAgents" :key="agent.id" class="onboarding-option" :class="{ selected: selectedAgent === agent.id }" type="button" @click="selectedAgent = agent.id">
            <span class="option-dot"><PhRobot :size="16" /></span>
            <span><strong>{{ agent.name }}</strong><small>{{ agent.providerId }}</small></span>
            <PhCheck v-if="selectedAgent === agent.id" :size="18" class="option-check" weight="bold" />
          </button>
          <p v-if="!providers.chatAgents.length" class="onboarding-empty">No chat agent is configured yet. You can add one in Settings.</p>
        </div>
      </section>

      <section v-else-if="step === 2" class="onboarding-page">
        <div class="onboarding-icon"><PhPaintBrush :size="22" /></div>
        <p class="onboarding-kicker">Make it yours</p>
        <h1>Pick a theme</h1>
        <p class="onboarding-copy">This changes the entire workspace, including terminals and diffs.</p>
        <div class="theme-grid">
          <button v-for="theme in availableThemes" :key="theme.key" class="theme-option" :class="{ selected: ui.activeFamily.key === theme.key }" type="button" @click="ui.setThemeFamily(theme.key)">
            <span class="theme-pair">
              <span class="theme-swatch" :style="{ background: theme.light.vars['bg-panel'], borderColor: theme.light.vars.border }"><i :style="{ background: theme.light.vars.accent }" /></span>
              <span class="theme-swatch" :style="{ background: theme.dark.vars['bg-panel'], borderColor: theme.dark.vars.border }"><i :style="{ background: theme.dark.vars.accent }" /></span>
            </span>
            {{ theme.label }}
          </button>
        </div>
      </section>

      <section v-else class="onboarding-page onboarding-ready">
        <div class="onboarding-icon success"><PhCheck :size="23" weight="bold" /></div>
        <p class="onboarding-kicker">You are set</p>
        <h1>Ready when you are</h1>
        <p class="onboarding-copy">Open a project, start a thread, and let Burrow keep the moving parts in view.</p>
      </section>

      <footer class="onboarding-footer">
        <button class="onboarding-back" type="button" :disabled="step === 0" @click="back"><PhArrowLeft :size="15" /> Back</button>
        <button class="onboarding-next" type="button" @click="next">{{ step === steps.length - 1 ? "Start using Burrow" : "Continue" }} <PhArrowRight :size="15" /></button>
      </footer>
    </main>
  </div>
</template>

<style scoped>
.onboarding { position: fixed; inset: 0; z-index: 1100; display: grid; grid-template-columns: 220px minmax(0, 1fr); background: var(--bg-base); color: var(--text-primary); font-family: var(--font-ui); }
.onboarding-rail { display: flex; flex-direction: column; border-right: 1px solid var(--border); background: var(--bg-panel); padding: 24px 16px 18px; }
.onboarding-wordmark { display: flex; align-items: center; gap: 8px; font-size: 14px; font-weight: 700; letter-spacing: -0.02em; }
.onboarding-steps { display: grid; gap: 4px; margin: 52px 0; padding: 0; list-style: none; color: var(--text-muted); font-size: 12px; }
.onboarding-steps li { display: flex; align-items: center; gap: 10px; height: 32px; border-radius: 5px; padding: 0 8px; }
.onboarding-steps span { display: grid; width: 17px; height: 17px; place-items: center; border: 1px solid var(--border); border-radius: 50%; font-size: 10px; }
.onboarding-steps .active { background: var(--bg-hover); color: var(--text-primary); font-weight: 600; }.onboarding-steps .active span, .onboarding-steps .done span { border-color: var(--accent); color: var(--accent); }.onboarding-steps .done { color: var(--text-secondary); }
.onboarding-skip { margin-top: auto; border: 0; background: none; color: var(--text-muted); cursor: pointer; font: inherit; font-size: 12px; text-align: left; padding: 8px; }.onboarding-skip:hover { color: var(--text-primary); }
.onboarding-main { display: grid; grid-template-rows: 1fr auto; min-width: 0; }.onboarding-page { width: min(620px, calc(100% - 64px)); align-self: center; margin: auto; }.onboarding-welcome { width: min(680px, calc(100% - 64px)); }
.onboarding-mark, .onboarding-icon { display: grid; width: 48px; height: 48px; place-items: center; margin-bottom: 28px; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-panel); color: var(--accent); }.onboarding-icon.success { color: var(--green); }
.onboarding-kicker { margin: 0 0 9px; color: var(--accent); font-size: 12px; font-weight: 600; }.onboarding-page h1 { margin: 0; font-size: 30px; letter-spacing: -0.04em; }.onboarding-copy { max-width: 58ch; margin: 14px 0 0; color: var(--text-secondary); font-size: 14px; line-height: 1.6; }
.onboarding-facts { display: flex; gap: 30px; margin-top: 34px; color: var(--text-secondary); font-size: 12px; }.onboarding-facts span { display: flex; align-items: center; gap: 8px; }
.onboarding-options { display: grid; gap: 8px; margin-top: 28px; }.onboarding-option { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 60px; border: 1px solid var(--border); border-radius: 7px; background: var(--bg-panel); color: var(--text-primary); cursor: pointer; padding: 10px 12px; text-align: left; }.onboarding-option:hover { background: var(--bg-hover); }.onboarding-option.selected { border-color: var(--accent); }.option-dot { display: grid; width: 31px; height: 31px; place-items: center; border-radius: 6px; background: var(--bg-hover); color: var(--accent); }.onboarding-option strong, .onboarding-option small { display: block; }.onboarding-option strong { font-size: 13px; }.onboarding-option small { margin-top: 2px; color: var(--text-muted); font-size: 11px; }.option-check { margin-left: auto; color: var(--accent); }.onboarding-empty { color: var(--text-muted); font-size: 13px; }
.theme-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin-top: 28px; }.theme-option { display: grid; gap: 9px; border: 1px solid var(--border); border-radius: 7px; background: var(--bg-panel); color: var(--text-secondary); cursor: pointer; padding: 10px; font: inherit; font-size: 12px; text-align: left; }.theme-option:hover { background: var(--bg-hover); }.theme-option.selected { border-color: var(--accent); color: var(--text-primary); }.theme-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 4px; }.theme-swatch { position: relative; display: block; height: 38px; border: 1px solid; border-radius: 4px; }.theme-swatch i { position: absolute; right: 7px; bottom: 7px; width: 10px; height: 10px; border-radius: 50%; }
.onboarding-footer { display: flex; justify-content: space-between; border-top: 1px solid var(--border); padding: 18px 28px; }.onboarding-footer button { display: inline-flex; align-items: center; gap: 7px; border-radius: 6px; padding: 8px 12px; font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; }.onboarding-back { border: 1px solid var(--border); background: transparent; color: var(--text-secondary); }.onboarding-back:disabled { opacity: 0; pointer-events: none; }.onboarding-next { border: 1px solid var(--accent); background: var(--accent); color: var(--bg-base); }.onboarding-next:hover { filter: brightness(1.08); }
@media (max-width: 680px) { .onboarding { grid-template-columns: 1fr; }.onboarding-rail { display: none; }.theme-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
</style>
