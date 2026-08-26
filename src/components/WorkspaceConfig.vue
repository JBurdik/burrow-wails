<template>
  <div class="fixed inset-0 z-[800] flex items-center justify-center bg-black/55" @click.self="$emit('close')">
    <div class="flex max-h-[80vh] w-[640px] flex-col overflow-hidden rounded-[10px] border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.6)]" @keydown.esc.stop="$emit('close')">
      <!-- Header -->
      <div class="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
        <span class="text-[13px] font-semibold text-foreground">{{ workspaceName }} — Project Config</span>
        <button class="flex rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="$emit('close')">
          <PhX :size="14" />
        </button>
      </div>

      <!-- Tabs -->
      <div class="flex shrink-0 border-b border-border px-4">
        <button
          class="-mb-px border-b-2 border-transparent px-3 pb-2 pt-2.5 text-xs text-muted-foreground hover:text-foreground"
          :class="{ 'border-b-accent text-foreground': tab === 'prompt' }"
          @click="tab = 'prompt'"
        >Manager Prompt</button>
        <button
          class="-mb-px border-b-2 border-transparent px-3 pb-2 pt-2.5 text-xs text-muted-foreground hover:text-foreground"
          :class="{ 'border-b-accent text-foreground': tab === 'scripts' }"
          @click="tab = 'scripts'"
        >Scripts</button>
      </div>

      <!-- Tab: Manager Prompt -->
      <div v-if="tab === 'prompt'" class="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
        <p class="m-0 text-[11px] text-muted-foreground">
          Saved to <code class="font-mono text-secondary-foreground">{{ workspacePath }}/.burrow/manager.md</code>. This is the full Manager system prompt for this project — edit or extend it as needed.
        </p>
        <textarea
          v-model="promptContent"
          class="min-h-[280px] flex-1 resize-y rounded-md border border-border bg-base p-2.5 font-mono text-xs leading-relaxed text-foreground outline-none focus:border-accent"
          placeholder="# Project-specific manager instructions&#10;&#10;Describe the project, conventions, team norms, or anything the Manager should know..."
          spellcheck="false"
        />
        <div class="flex shrink-0 items-center justify-end gap-2.5">
          <span v-if="saveState === 'ok'" class="text-xs text-success">Saved</span>
          <span v-else-if="saveState === 'err'" class="text-xs text-destructive">Save failed</span>
          <button
            class="flex items-center gap-1.5 rounded-[5px] border border-accent bg-accent px-3.5 py-1.5 text-xs text-white hover:opacity-85 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="saving"
            @click="savePrompt"
          >{{ saving ? 'Saving…' : 'Save' }}</button>
        </div>
      </div>

      <!-- Tab: Scripts -->
      <div v-else class="flex flex-1 flex-col gap-2.5 overflow-y-auto p-4">
        <div class="flex flex-col gap-2.5">
          <div v-for="s in scripts" :key="s.id" class="flex flex-col gap-2 rounded-[7px] border border-border bg-base p-2.5">
            <div class="flex items-center gap-2">
              <span class="h-2.5 w-2.5 shrink-0 rounded-full" :style="{ background: s.color || '#60a5fa' }" />
              <input
                class="flex-1 border-0 border-b border-transparent bg-transparent py-px text-[13px] font-medium text-foreground outline-none focus:border-b-accent"
                :value="s.name"
                @change="patch(s.id, { name: ($event.target as HTMLInputElement).value })"
              />
              <button class="flex rounded p-0.5 text-muted-foreground hover:text-destructive" title="Delete script" @click="scriptsStore.removeScript(workspacePath, s.id)">
                <PhTrash :size="13" />
              </button>
            </div>
            <textarea
              class="box-border w-full resize-y rounded-md border border-border bg-panel p-1.5 font-mono text-[11px] leading-relaxed text-foreground outline-none focus:border-accent"
              :value="s.steps.join('\n')"
              placeholder="One shell command per line"
              rows="3"
              @change="patch(s.id, { steps: splitSteps(($event.target as HTMLTextAreaElement).value) })"
            />
            <label class="flex cursor-pointer select-none items-center gap-1.5 text-[11px] text-muted-foreground">
              <input
                type="checkbox"
                class="cursor-pointer accent-accent"
                :checked="s.continueOnError"
                @change="patch(s.id, { continueOnError: ($event.target as HTMLInputElement).checked })"
              />
              <span>Continue on error</span>
            </label>
          </div>
        </div>
        <button
          class="mt-1 flex items-center gap-1.5 self-start rounded-[5px] border border-border bg-hover px-3.5 py-1.5 text-xs text-foreground hover:border-accent hover:bg-panel"
          @click="scriptsStore.addScript(workspacePath)"
        ><PhPlus :size="13" /> Add script</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { invoke } from '@tauri-apps/api/core'
import { PhX, PhPlus, PhTrash } from '@phosphor-icons/vue'
import { useScriptsStore } from '@/stores/scripts'
import { getDefaultManagerPrimer } from '@/utils/managerPrimer'

const props = defineProps<{
  workspacePath: string
  workspaceName: string
}>()
const emit = defineEmits<{ close: [] }>()

const tab = ref<'prompt' | 'scripts'>('prompt')

// ── Manager Prompt ─────────────────────────────────────────────────────────
const promptContent = ref('')
const saving = ref(false)
const saveState = ref<'idle' | 'ok' | 'err'>('idle')
let saveTimer: ReturnType<typeof setTimeout> | null = null

async function loadPrompt() {
  try {
    const content = await invoke<string>('read_text_file', {
      path: props.workspacePath + '/.burrow/manager.md',
    })
    const stripped = content.replace(/<!--[\s\S]*?-->/g, '').trim()
    const isPlaceholder = stripped === '# Project-specific Manager instructions' || stripped === ''
    promptContent.value = isPlaceholder ? getDefaultManagerPrimer(false) : stripped
  } catch {
    promptContent.value = getDefaultManagerPrimer(false)
  }
}

async function savePrompt() {
  saving.value = true
  try {
    await invoke('write_text_file', {
      path: props.workspacePath + '/.burrow/manager.md',
      content: promptContent.value,
    })
    saveState.value = 'ok'
  } catch {
    saveState.value = 'err'
  } finally {
    saving.value = false
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => { saveState.value = 'idle' }, 2500)
  }
}

// ── Scripts ────────────────────────────────────────────────────────────────
const scriptsStore = useScriptsStore()

const scripts = computed(() => scriptsStore.scriptsFor(props.workspacePath))

function patch(id: string, p: Parameters<typeof scriptsStore.updateScript>[2]) {
  scriptsStore.updateScript(props.workspacePath, id, p)
}

function splitSteps(raw: string): string[] {
  return raw.split('\n').map((l) => l.trimEnd()).filter((l) => l.length > 0)
}

// ── Keyboard ───────────────────────────────────────────────────────────────
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => {
  loadPrompt()
  scriptsStore.loadForPath(props.workspacePath)
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  if (saveTimer) clearTimeout(saveTimer)
})
</script>
