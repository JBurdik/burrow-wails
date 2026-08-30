<template>
  <div class="fixed inset-0 z-[800] flex items-center justify-center bg-black/55" @click.self="$emit('close')">
    <div class="flex max-h-[80vh] w-[640px] flex-col overflow-hidden rounded-[10px] border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.6)]" @keydown.esc.stop="$emit('close')">
      <!-- Header -->
      <div class="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
        <span class="text-[13px] font-semibold text-foreground">{{ ws?.name ?? 'Project' }} — Project Config</span>
        <button class="flex rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="$emit('close')">
          <PhX :size="14" />
        </button>
      </div>

      <!-- Tabs -->
      <div class="flex shrink-0 border-b border-border px-4">
        <button
          class="-mb-px border-b-2 border-transparent px-3 pb-2 pt-2.5 text-xs text-muted-foreground hover:text-foreground"
          :class="{ 'border-b-accent text-foreground': tab === 'general' }"
          @click="tab = 'general'"
        >General</button>
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

      <!-- Tab: General -->
      <div v-if="tab === 'general'" class="flex flex-1 flex-col gap-4 overflow-y-auto p-4">
        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] font-semibold text-secondary-foreground">Display name</span>
          <input
            v-model="nameDraft"
            class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
            @change="commitName"
            @keydown.enter="commitName"
          />
        </label>

        <div class="flex flex-col gap-1.5">
          <span class="text-[11px] font-semibold text-secondary-foreground">Icon</span>
          <div class="flex items-center gap-2.5">
            <img v-if="icon" :src="icon" class="h-9 w-9 rounded-md object-cover" />
            <PhFolder v-else :size="24" weight="fill" class="text-accent" />
            <button class="rounded-[5px] border border-border bg-hover px-3 py-1.5 text-xs text-foreground hover:border-accent" @click="pickIcon">Choose…</button>
            <button v-if="icon" class="rounded-[5px] border border-border bg-transparent px-3 py-1.5 text-xs text-muted-foreground hover:text-destructive" @click="wsStore.clearIcon(workspaceId)">Remove</button>
          </div>
        </div>

        <div class="flex flex-col gap-1.5">
          <span class="text-[11px] font-semibold text-secondary-foreground">Composer default</span>
          <p class="m-0 text-[11px] text-muted-foreground">Which agent and model a new thread in this project starts on. Empty = the app-wide default.</p>
          <div class="flex gap-2">
            <select v-model="agentDraft" class="flex-1 rounded-md border border-border bg-base px-2 py-[7px] text-[13px] text-foreground outline-none focus:border-accent" @change="commitDefaults">
              <option value="">Default ({{ chatAgents.byId(ui.defaultChatAgent).name }})</option>
              <option v-for="a in chatAgents.agents" :key="a.id" :value="a.id">{{ a.name }}</option>
            </select>
            <select v-model="modelDraft" class="flex-1 rounded-md border border-border bg-base px-2 py-[7px] text-[13px] text-foreground outline-none focus:border-accent" @change="commitDefaults">
              <option value="">Default model</option>
              <option v-for="m in modelOptions" :key="m.id" :value="m.id">{{ m.label }}</option>
            </select>
          </div>
        </div>

        <label class="flex flex-col gap-1.5">
          <span class="text-[11px] font-semibold text-secondary-foreground">Worktrees directory</span>
          <div class="flex gap-2">
            <input
              v-model="worktreesDraft"
              class="min-w-0 flex-1 rounded-md border border-border bg-base px-2.5 py-[7px] font-mono text-[11.5px] text-foreground outline-none focus:border-accent"
              :placeholder="ui.worktreesDir"
              spellcheck="false"
              @change="commitDefaults"
            />
            <button class="shrink-0 rounded-[5px] border border-border bg-hover px-3 py-1.5 text-xs text-foreground hover:border-accent" @click="pickWorktreesDir">Browse…</button>
          </div>
        </label>

        <div class="mt-auto flex flex-col gap-2 border-t border-border pt-3">
          <div class="flex flex-wrap items-center gap-2">
            <button class="rounded-[5px] border border-border bg-hover px-3 py-1.5 text-xs text-foreground hover:border-accent" @click="togglePin(workspaceId)">
              {{ isPinned(workspaceId) ? 'Unpin from top' : 'Pin to top' }}
            </button>
            <button class="rounded-[5px] border border-border bg-hover px-3 py-1.5 text-xs text-foreground hover:border-accent" @click="toggleArchived(workspaceId)">
              {{ isArchived(workspaceId) ? 'Unarchive' : 'Archive' }}
            </button>
            <button v-if="!confirmDelete" class="ml-auto rounded-[5px] border border-destructive/40 bg-transparent px-3 py-1.5 text-xs text-destructive hover:bg-destructive/10" @click="confirmDelete = true">Delete project…</button>
            <template v-else>
              <span class="ml-auto text-[11px] text-secondary-foreground">Removes it from Burrow. Your folder stays on disk.</span>
              <button class="rounded-[5px] border border-border bg-hover px-3 py-1.5 text-xs text-foreground" @click="confirmDelete = false">Cancel</button>
              <button class="rounded-[5px] border-0 bg-destructive px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90" @click="doDelete">Delete</button>
            </template>
          </div>
        </div>
      </div>

      <!-- Tab: Manager Prompt -->
      <div v-else-if="tab === 'prompt'" class="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
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
import { open as openDialog } from '@tauri-apps/plugin-dialog'
import { PhX, PhPlus, PhTrash, PhFolder } from '@phosphor-icons/vue'
import { useScriptsStore } from '@/stores/scripts'
import { useWorkspaceStore } from '@/stores/workspace'
import { useChatAgentsStore } from '@/stores/chatAgents'
import { useUIStore } from '@/stores/ui'
import { modelsFor } from '@/lib/chatModels'
import { getProjectSettings, setProjectSettings } from '@/lib/projectSettings'
import { isPinned, togglePin } from '@/lib/pinnedWorkspaces'
import { isArchived, toggleArchived } from '@/lib/archivedWorkspaces'
import { getDefaultManagerPrimer } from '@/utils/managerPrimer'

const props = defineProps<{ workspaceId: number }>()
const emit = defineEmits<{ close: [] }>()

const tab = ref<'general' | 'prompt' | 'scripts'>('general')

// ── General ────────────────────────────────────────────────────────────────
const wsStore = useWorkspaceStore()
const chatAgents = useChatAgentsStore()
const ui = useUIStore()

const ws = computed(() => wsStore.workspaces.find((w) => w.id === props.workspaceId) ?? null)
const workspacePath = computed(() => ws.value?.path ?? '')
const icon = computed(() => wsStore.icons[props.workspaceId])

const saved = getProjectSettings(props.workspaceId)
const nameDraft = ref(ws.value?.name ?? '')
const agentDraft = ref(saved.agentId ?? '')
const modelDraft = ref(saved.modelId ?? '')
const worktreesDraft = ref(saved.worktreesDir ?? '')
const confirmDelete = ref(false)

const modelOptions = computed(() => modelsFor(chatAgents.byId(agentDraft.value || ui.defaultChatAgent).kind))

function commitName() {
  const name = nameDraft.value.trim()
  if (name && name !== ws.value?.name) wsStore.rename(props.workspaceId, name)
}

function commitDefaults() {
  setProjectSettings(props.workspaceId, {
    agentId: agentDraft.value,
    modelId: modelDraft.value,
    worktreesDir: worktreesDraft.value.trim(),
  })
}

function mimeForPath(path: string): string {
  const ext = path.split('.').pop()?.toLowerCase() ?? ''
  if (ext === 'svg') return 'image/svg+xml'
  if (ext === 'ico') return 'image/x-icon'
  if (ext === 'jpg' || ext === 'jpeg') return 'image/jpeg'
  return 'image/png'
}

async function pickIcon() {
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: 'Image', extensions: ['png', 'jpg', 'jpeg', 'svg', 'ico'] }],
  })
  if (typeof selected !== 'string') return
  const b64 = await invoke<string>('read_file_base64', { path: selected })
  wsStore.setIcon(props.workspaceId, `data:${mimeForPath(selected)};base64,${b64}`)
}

async function pickWorktreesDir() {
  const selected = await openDialog({ directory: true, multiple: false })
  if (typeof selected !== 'string') return
  worktreesDraft.value = selected
  commitDefaults()
}

async function doDelete() {
  await wsStore.remove(props.workspaceId)
  emit('close')
}

// ── Manager Prompt ─────────────────────────────────────────────────────────
const promptContent = ref('')
const saving = ref(false)
const saveState = ref<'idle' | 'ok' | 'err'>('idle')
let saveTimer: ReturnType<typeof setTimeout> | null = null

async function loadPrompt() {
  try {
    const content = await invoke<string>('read_text_file', {
      path: workspacePath.value + '/.burrow/manager.md',
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
      path: workspacePath.value + '/.burrow/manager.md',
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

const scripts = computed(() => scriptsStore.scriptsFor(workspacePath.value))

function patch(id: string, p: Parameters<typeof scriptsStore.updateScript>[2]) {
  scriptsStore.updateScript(workspacePath.value, id, p)
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
  scriptsStore.loadForPath(workspacePath.value)
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  if (saveTimer) clearTimeout(saveTimer)
})
</script>
