<template>
  <div class="fixed inset-0 z-[1000] flex flex-col overflow-hidden bg-base [backdrop-filter:var(--blur-overlay,none)] [-webkit-backdrop-filter:var(--blur-overlay,none)]">
    <!-- Header -->
    <div class="relative flex h-[52px] shrink-0 items-center justify-end border-b border-border bg-panel px-6">
      <div class="absolute left-1/2 flex -translate-x-1/2 items-center gap-2.5">
        <PhGearSix :size="15" class="text-muted-foreground" />
        <span class="text-sm font-semibold text-foreground">Settings</span>
      </div>
      <button class="flex rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="$emit('close')">
        <PhX :size="15" />
      </button>
    </div>

    <div class="flex flex-1 overflow-hidden">
      <!-- Nav -->
      <nav class="flex w-[220px] shrink-0 flex-col gap-px border-r border-border bg-panel py-2.5">
        <template v-for="item in navItems" :key="item.id">
          <div v-if="item.divider" class="my-2 h-px bg-border" />
          <button
            v-else
            class="flex h-[34px] items-center gap-2.5 border-l-2 border-transparent px-4 text-left text-[13px] text-secondary-foreground hover:bg-hover"
            :class="active === item.id && 'border-l-accent bg-hover text-foreground [&_.nav-icon]:text-accent'"
            @click="active = item.id!"
          >
            <component :is="item.icon" :size="14" class="nav-icon shrink-0 text-muted-foreground" />
            <span>{{ item.label }}</span>
          </button>
        </template>
      </nav>

      <!-- Content -->
      <div class="flex-1 overflow-y-auto bg-base px-10 py-8">
        <!-- Providers -->
        <section v-if="active === 'providers'" class="flex flex-col gap-3.5">
          <ProvidersPanel :focus-id="focusId" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Sub-agent delegation</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Max concurrent sub-agents</span>
                <span class="text-[11px] text-muted-foreground">Soft per-workspace cap the <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">/burrow</code> skill respects when it spawns agents (1–20)</span>
              </div>
              <div class="flex items-center gap-1.5">
                <input
                  class="h-8 w-16 cursor-text rounded-md border border-border bg-hover px-2.5 text-center text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent"
                  type="number"
                  min="1"
                  max="20"
                  :value="ui.maxAgents"
                  @input="ui.maxAgents = clampRange(val($event), 1, 20, 3)"
                />
                <span class="text-xs text-muted-foreground/70">agents</span>
              </div>
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">MCP recursion depth</span>
                <span class="text-[11px] text-muted-foreground">How deep MCP-spawned sub-agents may spawn further sub-agents before the depth cap refuses (1–10)</span>
              </div>
              <div class="flex items-center gap-1.5">
                <input
                  class="h-8 w-16 cursor-text rounded-md border border-border bg-hover px-2.5 text-center text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent"
                  type="number"
                  min="1"
                  max="10"
                  :value="ui.mcpMaxDepth"
                  @input="ui.mcpMaxDepth = clampRange(val($event), 1, 10, 3)"
                />
                <span class="text-xs text-muted-foreground/70">levels</span>
              </div>
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Chat</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Default agent</span>
                <span class="text-[11px] text-muted-foreground">Agent used when opening a new chat.</span>
              </div>
              <Select v-model="ui.defaultChatAgent" class="min-w-[200px]" :options="chatAgentOptions" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Spawn sub-agents as</span>
                <span class="text-[11px] text-muted-foreground">How <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">burrow spawn</code> sub-agents open. Chat prefills the task in a new chat's input; Terminal keeps <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">burrow wait</code> result capture.</span>
              </div>
              <Select v-model="ui.spawnMode" class="min-w-[200px]" :options="SPAWN_MODE_OPTIONS" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Text generation model</span>
                <span class="text-[11px] text-muted-foreground">Model used for small background writing jobs — the GitPanel sparkle button's commit message, and chat titles. Kept cheap by default.</span>
              </div>
              <Select v-model="ui.commitMessageModel" class="min-w-[200px]" :options="commitMessageModelOptions" />
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Floating windows</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Snap corner</span>
                <span class="text-[11px] text-muted-foreground">Which screen corner popped-out terminal bubbles snap to and stack at</span>
              </div>
              <Select v-model="ui.floatCorner" class="min-w-[200px]" :options="FLOAT_CORNER_OPTIONS" />
            </div>
          </div>
        </section>

        <!-- Remote access -->
        <section v-else-if="active === 'remote'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5"><div class="flex items-baseline gap-2.5"><h2 class="text-[15px] font-semibold text-foreground">Remote access</h2><span class="text-xs text-muted-foreground">Reach Burrow securely from another device</span></div></div>
          <div class="h-px bg-border" />
          <div class="flex max-w-[880px] flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Burrow Remote</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[13px] font-medium text-foreground">Enable HTTP/WebSocket server</span><span class="text-[11px] text-muted-foreground">Starts a loopback-only, token-protected connection for Burrow Remote. Restart Burrow once after changing this option.</span></div><Switch :checked="httpEnabled" @update:checked="onToggleHttp" /></div>
            <div v-if="httpStatus?.enabled" class="flex items-start gap-4 rounded-md border border-border bg-panel px-4 py-3"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[13px] font-medium text-foreground">Phone pairing code</span><span v-if="httpStatus.pairLocked" class="text-[11px] text-red-400">Too many wrong codes — pairing is locked. Generate a new code to unlock it.</span><span v-else class="text-[11px] text-muted-foreground">Type this into Burrow Remote on your phone. Single use — it changes as soon as a device pairs.</span><code v-if="!httpStatus.pairLocked" class="mt-1.5 block font-mono text-[22px] tracking-[0.3em] text-secondary-foreground">{{ httpStatus.pairCode }}</code></div><Button variant="outline" size="sm" type="button" @click="onRegeneratePairCode">New code</Button></div>
            <details v-if="httpStatus?.enabled" class="rounded-md border border-border bg-panel px-4 py-3"><summary class="cursor-pointer text-[13px] font-medium text-foreground">Access token (for integrations)</summary><div class="mt-2 flex items-start gap-4"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[11px] text-muted-foreground">Phones do not need this — pairing hands it over. Treat it like an SSH key: it is full access to your terminals.</span><code class="mt-1.5 block max-w-[620px] overflow-wrap-anywhere text-[11px] text-secondary-foreground">{{ httpStatus.token }}</code></div><Button variant="outline" size="sm" type="button" @click="copyToClipboard(httpStatus.token, 'token')">{{ copiedLabel === 'token' ? 'Copied' : 'Copy token' }}</Button></div></details>
            <div v-else class="flex items-center gap-4 rounded-md border border-dashed border-border bg-panel px-4 py-3"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[13px] font-medium text-foreground">Not enabled</span><span class="text-[11px] text-muted-foreground">Turn on the server above, then restart Burrow to generate a token and start listening.</span></div></div>
            <span class="mt-3 text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Private tunnel</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[13px] font-medium text-foreground">Tailscale tunnel</span><span class="text-[11px] text-muted-foreground"><template v-if="!tailscaleStatus?.installed">Install Tailscale to securely reach this Mac from your tailnet.</template><template v-else-if="!tailscaleStatus.logged_in">Log in to Tailscale to enable this tunnel.</template><template v-else-if="!httpEnabled">Enable the HTTP/WebSocket server first.</template><template v-else-if="tailscaleStatus.serving">Your private HTTPS address is ready below.</template><template v-else>Publishes Burrow at <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">/burrow</code> through your tailnet, never to the public internet. Existing services at <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">/</code> stay untouched.</template></span></div><Switch :checked="tailscaleStatus?.serving ?? false" :disabled="!httpEnabled || !tailscaleStatus?.installed || !tailscaleStatus?.logged_in" :title="!httpEnabled ? 'Enable the HTTP/WebSocket server first' : (!tailscaleStatus?.installed ? 'Tailscale not installed' : (!tailscaleStatus?.logged_in ? 'Not logged in to Tailscale' : ''))" @update:checked="onToggleTailscale" /></div>
            <div v-if="tailscaleStatus?.serving && tailscaleStatus.serve_url" class="flex items-start gap-4 rounded-md border border-border bg-panel px-4 py-3"><div class="flex flex-1 min-w-0 flex-col gap-0.5"><span class="text-[13px] font-medium text-foreground">Open Burrow Remote</span><code class="block max-w-[620px] overflow-wrap-anywhere text-[11px] text-secondary-foreground">{{ tailscaleStatus.serve_url }}</code><span class="text-[11px] text-muted-foreground">Open this address on your phone, then type the pairing code above.</span></div><Button variant="outline" size="sm" type="button" @click="copyToClipboard(tailscaleStatus.serve_url, 'url')">{{ copiedLabel === 'url' ? 'Copied' : 'Copy URL' }}</Button></div>
          </div>
        </section>

        <!-- Scripts -->
        <section v-else-if="active === 'scripts'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Scripts</h2>
              <span class="text-xs text-muted-foreground">Named, multi-step commands run sequentially in a new terminal tab</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <p class="m-0 text-xs leading-relaxed text-muted-foreground">
            A script is an ordered list of steps. They run one after another —
            with <strong>&amp;&amp;</strong> so each step starts only if the previous
            <em>succeeded</em>, or with <strong>;</strong> (Continue on error) so every
            step runs regardless. Launch from the toolbar's <strong>Scripts</strong> menu
            or by name in <strong>⌘P</strong>.
          </p>

          <!-- Active repo scripts -->
          <div class="flex flex-col gap-2.5">
            <div class="mb-1.5 flex items-center gap-2.5">
              <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">
                {{ activeWsId != null ? `This repo — ${activeWsName}` : "This repo" }}
              </span>
              <Button variant="outline" size="sm" class="ml-auto" :disabled="activeWsPath == null" @click="activeWsPath != null && scriptsStore.addScript(activeWsPath)">
                <PhPlus :size="11" /> Add Script
              </Button>
            </div>
            <p v-if="activeWsPath == null" class="m-0 text-[11.5px] leading-relaxed text-muted-foreground">Open a workspace to add repo-specific scripts.</p>

            <div
              v-for="s in (activeWsPath != null ? scriptsStore.scriptsFor(activeWsPath) : [])"
              :key="s.id"
              class="mb-2.5 rounded-lg border border-border bg-base/50 p-2.5"
            >
              <div class="flex items-center gap-2">
                <label class="group relative flex shrink-0 cursor-pointer items-center" title="Pick color">
                  <span class="h-[7px] w-[7px] shrink-0 rounded-full transition-transform duration-100 group-hover:scale-[1.3]" :style="{ background: s.color || '#34d399' }" />
                  <input type="color" class="absolute inset-0 h-full w-full cursor-pointer border-0 p-0 opacity-0" :value="s.color || '#34d399'" @input="s.color = val($event)" />
                </label>
                <input class="min-w-0 w-[200px] flex-none border-0 bg-transparent text-[13px] font-medium text-foreground outline-none placeholder:text-muted-foreground/50" :value="s.name" placeholder="Script name" @input="s.name = val($event)" />
                <label class="flex cursor-pointer items-center gap-1.5" title="Continue on error — chain steps with ; instead of &&">
                  <Switch :checked="s.continueOnError" @update:checked="(v: boolean) => s.continueOnError = v" />
                  <span class="whitespace-nowrap text-[11px] text-secondary-foreground">Continue on error</span>
                </label>
                <span class="flex-1" />
                <button class="flex rounded p-1.5 text-muted-foreground/40 hover:bg-destructive/12 hover:text-destructive" title="Remove script" @click="activeWsPath != null && scriptsStore.removeScript(activeWsPath, s.id)">
                  <PhTrash :size="13" />
                </button>
              </div>
              <div class="mt-2.5 flex flex-col gap-[5px]">
                <div v-for="(_, i) in s.steps" :key="i" class="flex items-center gap-1.5">
                  <span class="flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded bg-accent/12 text-[10px] text-muted-foreground">{{ i + 1 }}</span>
                  <input class="min-w-0 w-full flex-1 border-0 bg-transparent font-mono text-[13px] text-foreground outline-none placeholder:text-muted-foreground/50" :value="s.steps[i]" placeholder="npm install" @input="setStep(s, i, val($event))" />
                  <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-border bg-transparent text-muted-foreground transition-colors hover:bg-accent/12 hover:text-foreground disabled:opacity-30" title="Move up" :disabled="i === 0" @click="moveStep(s, i, i - 1)"><PhArrowUp :size="12" /></button>
                  <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-border bg-transparent text-muted-foreground transition-colors hover:bg-accent/12 hover:text-foreground disabled:opacity-30" title="Move down" :disabled="i === s.steps.length - 1" @click="moveStep(s, i, i + 1)"><PhArrowDown :size="12" /></button>
                  <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded border border-border bg-transparent text-muted-foreground transition-colors hover:bg-destructive/16 hover:text-destructive disabled:opacity-30" title="Remove step" @click="removeStep(s, i)"><PhX :size="12" /></button>
                </div>
                <Button variant="outline" size="sm" class="mt-1 self-start" @click="addStep(s)"><PhPlus :size="11" /> Add step</Button>
              </div>
              <div class="mt-2.5 flex items-baseline gap-1.5 overflow-hidden text-[11px] text-muted-foreground"><span class="shrink-0">Runs:</span> <code class="truncate font-mono text-secondary-foreground">{{ scriptPreview(s) }}</code></div>
            </div>
          </div>

        </section>

        <!-- General -->
        <section v-else-if="active === 'general'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">General</h2>
              <span class="text-xs text-muted-foreground">Interface &amp; layout</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Interface</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">UI scale</span>
                <span class="text-[11px] text-muted-foreground">Zoom the entire interface</span>
              </div>
              <Select v-model="uiScaleModel" class="min-w-[200px]" :options="UI_SCALE_OPTIONS" />
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Layout</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Swap panel sides</span>
                <span class="text-[11px] text-muted-foreground">Move primary panel to the right</span>
              </div>
              <Switch :checked="ui.swapPanels" @update:checked="(v: boolean) => ui.swapPanels = v" />
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Developer</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Terminal debug overlay</span>
                <span class="text-[11px] text-muted-foreground">Show per-terminal diagnostics (size, bytes, buffer)</span>
              </div>
              <Switch :checked="ui.debugOverlay" @update:checked="(v: boolean) => ui.debugOverlay = v" />
            </div>
          </div>
        </section>

        <!-- Notifications -->
        <section v-else-if="active === 'notifications'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Notifications</h2>
              <span class="text-xs text-muted-foreground">Sounds for agent activity</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Toasts</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Position</span>
                <span class="text-[11px] text-muted-foreground">Where on-screen toast notifications appear</span>
              </div>
              <Select v-model="ui.toastPosition" class="min-w-[200px]" :options="toastPositionOptions" />
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">General</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Enable sounds</span>
                <span class="text-[11px] text-muted-foreground">Master switch for all notification sounds</span>
              </div>
              <Switch :checked="ui.soundEnabled" @update:checked="(v: boolean) => ui.soundEnabled = v" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Volume</span>
                <span class="text-[11px] text-muted-foreground">Playback volume ({{ ui.soundVolume }}%)</span>
              </div>
              <input
                class="w-40 cursor-pointer accent-accent"
                type="range"
                min="0"
                max="100"
                :value="ui.soundVolume"
                @input="ui.soundVolume = clampRange(val($event), 0, 100, 70)"
              />
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Agent finished</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Play when an agent finishes while you're away</span>
                <span class="text-[11px] text-muted-foreground">Fires on the "review" state (another tab/window)</span>
              </div>
              <Switch :checked="ui.soundDoneEnabled" @update:checked="(v: boolean) => ui.soundDoneEnabled = v" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Sound</span>
                <span class="text-[11px] text-muted-foreground">Choose a built-in sound or a custom file</span>
              </div>
              <div class="flex items-center gap-1.5">
                <Select v-model="ui.soundDoneId" class="min-w-[200px]" :options="soundDoneOptions" />
                <button class="flex items-center justify-center rounded border border-border bg-transparent p-1.5 text-muted-foreground hover:border-muted-foreground hover:text-secondary-foreground" title="Test" @click="playSound('done', true)"><PhPlay :size="13" /></button>
              </div>
            </div>
            <div v-if="ui.soundDoneId === 'custom'" class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Custom file</span>
                <span class="text-[11px] text-muted-foreground">{{ ui.soundDoneCustomPath ? soundFileName(ui.soundDoneCustomPath) : "No file selected" }}</span>
              </div>
              <Button variant="outline" size="sm" @click="pickSound('done')"><PhFolderOpen :size="12" /> Choose…</Button>
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Needs input</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Play when an agent is waiting for your input</span>
                <span class="text-[11px] text-muted-foreground">Fires on the "waiting" state</span>
              </div>
              <Switch :checked="ui.soundWaitingEnabled" @update:checked="(v: boolean) => ui.soundWaitingEnabled = v" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Sound</span>
                <span class="text-[11px] text-muted-foreground">Choose a built-in sound or a custom file</span>
              </div>
              <div class="flex items-center gap-1.5">
                <Select v-model="ui.soundWaitingId" class="min-w-[200px]" :options="soundWaitingOptions" />
                <button class="flex items-center justify-center rounded border border-border bg-transparent p-1.5 text-muted-foreground hover:border-muted-foreground hover:text-secondary-foreground" title="Test" @click="playSound('waiting', true)"><PhPlay :size="13" /></button>
              </div>
            </div>
            <div v-if="ui.soundWaitingId === 'custom'" class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Custom file</span>
                <span class="text-[11px] text-muted-foreground">{{ ui.soundWaitingCustomPath ? soundFileName(ui.soundWaitingCustomPath) : "No file selected" }}</span>
              </div>
              <Button variant="outline" size="sm" @click="pickSound('waiting')"><PhFolderOpen :size="12" /> Choose…</Button>
            </div>
          </div>
        </section>

        <!-- Integrations -->
        <section v-else-if="active === 'integrations'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Integrations</h2>
              <span class="text-xs text-muted-foreground">Send agent status to external services</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">ntfy.sh — push notifications</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Enable ntfy</span>
                <span class="text-[11px] text-muted-foreground">Push agent events to your phone/desktop via <a class="text-accent hover:underline" href="https://ntfy.sh" target="_blank" rel="noopener">ntfy.sh</a></span>
              </div>
              <Switch :checked="ui.ntfyEnabled" @update:checked="(v: boolean) => ui.ntfyEnabled = v" />
            </div>

            <template v-if="ui.ntfyEnabled">
              <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
                <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                  <span class="text-[13px] font-medium text-foreground">Server</span>
                  <span class="text-[11px] text-muted-foreground">Base URL of your ntfy server</span>
                </div>
                <input v-model="ui.ntfyServer" class="h-8 min-w-[200px] rounded-md border border-border bg-hover px-2.5 text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent" placeholder="https://ntfy.sh" spellcheck="false" />
              </div>
              <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
                <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                  <span class="text-[13px] font-medium text-foreground">Topic</span>
                  <span class="text-[11px] text-muted-foreground">Subscribe to this topic in the ntfy app to receive pushes</span>
                </div>
                <input v-model="ui.ntfyTopic" class="h-8 min-w-[200px] rounded-md border border-border bg-hover px-2.5 text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent" placeholder="my-burrow-agents" spellcheck="false" />
              </div>
              <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
                <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                  <span class="text-[13px] font-medium text-foreground">Access token</span>
                  <span class="text-[11px] text-muted-foreground">Optional — only for protected topics (Bearer token)</span>
                </div>
                <input v-model="ui.ntfyToken" type="password" class="h-8 min-w-[200px] rounded-md border border-border bg-hover px-2.5 text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent" placeholder="tk_…" spellcheck="false" autocomplete="off" />
              </div>
            </template>
          </div>

          <div v-if="ui.ntfyEnabled" class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Notify on</span>
            <div v-for="ev in NTFY_EVENTS" :key="ev.id" class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">{{ ev.label }}</span>
              </div>
              <Switch :checked="ui.ntfyEvents.includes(ev.id)" @update:checked="(v: boolean) => toggleNtfyEvent(ev.id, v)" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Only when away</span>
                <span class="text-[11px] text-muted-foreground">Skip pushes while the Burrow window is focused</span>
              </div>
              <Switch :checked="ui.ntfyOnlyWhenAway" @update:checked="(v: boolean) => ui.ntfyOnlyWhenAway = v" />
            </div>
            <div class="mt-2 flex items-center gap-2.5">
              <span v-if="ntfyTestMsg" class="text-xs text-muted-foreground" :class="{ 'text-destructive': ntfyTestErr }">{{ ntfyTestMsg }}</span>
              <Button variant="outline" size="sm" :disabled="!ui.ntfyTopic || ntfyTesting" @click="sendNtfyTest">
                <PhPaperPlaneTilt :size="12" /> {{ ntfyTesting ? "Sending…" : "Send test" }}
              </Button>
            </div>
          </div>
        </section>

        <!-- Plugins -->
        <section v-else-if="active === 'plugins'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Plugins</h2>
              <span class="text-xs text-muted-foreground">Optional fun + experimental add-ons</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">🐾 Terminal Pets</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Enable pets</span>
                <span class="text-[11px] text-muted-foreground">A mixed pixel zoo — cat, mole, slime, ghost, duck — roams the bottom of the window. One critter per active agent; it struts while the agent works, bounces when it needs input, hops when a turn finishes, and shakes red on error.</span>
              </div>
              <Switch :checked="ui.petsEnabled" @update:checked="(v: boolean) => ui.petsEnabled = v" />
            </div>

            <template v-if="ui.petsEnabled">
              <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
                <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                  <span class="text-[13px] font-medium text-foreground">Speech bubbles</span>
                  <span class="text-[11px] text-muted-foreground">Pets squeak tiny status quips — “working…”, “need input!”, “done!”</span>
                </div>
                <Switch :checked="ui.petsSpeech" @update:checked="(v: boolean) => ui.petsSpeech = v" />
              </div>
              <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
                <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                  <span class="text-[13px] font-medium text-foreground">Leveling &amp; crowns</span>
                  <span class="text-[11px] text-muted-foreground">Pets level up as their agent finishes turns and earn a ♛ crown once they hit veteran status.</span>
                </div>
                <Switch :checked="ui.petsLeveling" @update:checked="(v: boolean) => ui.petsLeveling = v" />
              </div>
              <div class="mt-2 flex items-center gap-2.5">
                <span class="text-xs text-muted-foreground">Tip: click a pet to give it a poke.</span>
              </div>
            </template>
          </div>
        </section>

        <!-- Keybindings -->
        <section v-else-if="active === 'keybindings'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Keyboard Shortcuts</h2>
              <span class="text-xs text-muted-foreground">Click a shortcut to rebind · ⌫ clears · stored in config.json</span>
            </div>
            <div class="flex-1" />
            <Button variant="outline" size="sm" @click="openConfigFile">Edit config.json</Button>
            <Button variant="outline" size="sm" @click="keys.resetAll()">Reset all</Button>
          </div>
          <div class="h-px bg-border" />

          <div v-for="group in keys.groups" :key="group.label" class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">{{ group.label }}</span>
            <div v-for="c in group.commands" :key="c.id" class="flex items-center justify-between gap-3 border-b border-border/40 py-1.5 last:border-b-0">
              <span class="text-xs text-muted-foreground">{{ c.label }}</span>
              <span class="flex shrink-0 items-center gap-2">
                <button
                  class="min-w-[86px] rounded border px-2 py-0.5 font-mono text-[11px] leading-snug"
                  :class="kbRecording === c.id
                    ? 'border-accent bg-accent/10 text-accent'
                    : 'border-border bg-hover text-secondary-foreground hover:border-accent/40'"
                  @click="startKeyRecord(c.id, $event)"
                  @keydown="onKeyRecord(c.id, $event)"
                >{{ kbRecording === c.id ? "press keys…" : (c.keys || "unbound") }}</button>
                <button
                  v-if="c.custom"
                  class="text-[10px] text-muted-foreground hover:text-foreground"
                  title="Reset to default"
                  @click="keys.reset(c.id)"
                >reset</button>
              </span>
            </div>
          </div>

          <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Fixed (not rebindable)</span>
          <div v-for="s in FIXED_SHORTCUTS" :key="s.keys" class="flex items-center justify-between gap-3 border-b border-border/40 py-1.5 last:border-b-0">
            <span class="text-xs text-muted-foreground">{{ s.desc }}</span>
            <span class="flex shrink-0 items-center gap-[3px]">
              <kbd v-for="k in s.keys.split(' ')" :key="k" class="inline-flex items-center justify-center rounded border border-border bg-hover px-1.5 py-0.5 font-mono text-[11px] leading-snug text-secondary-foreground">{{ k }}</kbd>
            </span>
          </div>
        </section>

        <!-- Workspaces -->
        <section v-else-if="active === 'workspaces'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Workspaces</h2>
              <span class="text-xs text-muted-foreground">Customize project icons</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Worktrees directory</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Where new git worktrees are created</span>
                <span class="text-[11px] text-muted-foreground">Worktrees land at &lt;dir&gt;/&lt;repo&gt;/&lt;branch&gt;</span>
              </div>
              <div class="flex items-center gap-1.5">
                <input
                  class="h-8 min-w-[240px] rounded-md border border-border bg-hover px-2.5 font-mono text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent"
                  :value="ui.worktreesDir"
                  @input="ui.worktreesDir = ($event.target as HTMLInputElement).value"
                  spellcheck="false"
                />
                <Button variant="outline" size="sm" @click="pickWorktreesDir"><PhFolderOpen :size="12" /> Browse…</Button>
              </div>
            </div>
          </div>

          <div class="flex flex-col gap-1.5">
            <div v-for="w in wsStore.workspaces" :key="w.id" class="flex items-center gap-3 rounded-lg border border-border bg-hover px-2.5 py-2">
              <button class="group relative flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border bg-base hover:border-accent/40" title="Change icon" @click="pickWsIcon(w.id)">
                <img v-if="wsStore.icons[w.id]" :src="wsStore.icons[w.id]" class="h-full w-full object-cover" />
                <PhFolder v-else :size="18" weight="fill" class="text-[#60a5fa]" />
                <span class="absolute -bottom-px -right-px hidden h-3.5 w-3.5 items-center justify-center rounded-[4px_0_6px_0] bg-accent text-white group-hover:flex"><PhPencilSimple :size="10" /></span>
              </button>
              <div class="flex min-w-0 flex-1 flex-col">
                <span class="text-[13px] font-medium text-foreground">{{ w.name }}</span>
                <span class="truncate text-[11px] text-muted-foreground">{{ w.path }}</span>
              </div>
              <button
                v-if="wsStore.icons[w.id]"
                class="flex h-[26px] w-[26px] shrink-0 items-center justify-center rounded-md border border-border bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground"
                title="Reset to default icon"
                @click="wsStore.clearIcon(w.id)"
              >
                <PhArrowCounterClockwise :size="13" />
              </button>
            </div>

            <div v-if="wsStore.workspaces.length === 0" class="py-5 text-center text-xs text-muted-foreground/50">
              No workspaces yet. Open a folder first.
            </div>
          </div>
        </section>

        <!-- Appearance -->
        <section v-else-if="active === 'appearance'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Appearance</h2>
              <span class="text-xs text-muted-foreground">Color theme &amp; typography</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Color scheme</span>
            <div class="grid grid-cols-3 gap-3">
              <button
                v-for="m in THEME_MODES"
                :key="m.id"
                class="flex flex-col items-center gap-2 rounded-lg border border-border bg-base px-3 py-4 transition-[border-color,box-shadow] duration-[120ms] hover:border-muted-foreground"
                :class="ui.themeMode === m.id && 'border-accent shadow-[0_0_0_1px_var(--accent)]'"
                @click="ui.setThemeMode(m.id)"
              >
                <component :is="m.icon" :size="20" :class="ui.themeMode === m.id ? 'text-accent' : 'text-muted-foreground'" />
                <span class="flex items-center gap-1 text-xs text-foreground">
                  {{ m.label }}
                  <PhCheck v-if="ui.themeMode === m.id" :size="12" class="text-accent" />
                </span>
              </button>
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Theme</span>
            <span class="text-xs text-muted-foreground/70">
              Picking a theme here sets it as the preferred theme for its mode (dark
              or light) — what “Dark”/“System” above, and the ⌘P “Toggle Dark/Light
              Mode” command, resolve to.
            </span>
            <div class="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-3">
              <button
                v-for="t in THEMES"
                :key="t.key"
                class="flex flex-col gap-2 rounded-lg border border-border bg-base p-2 text-left transition-[border-color,box-shadow] duration-[120ms] hover:border-muted-foreground"
                :class="ui.theme === t.key && 'border-accent shadow-[0_0_0_1px_var(--accent)]'"
                @click="ui.setTheme(t.key)"
              >
                <div
                  class="relative h-16 overflow-hidden rounded-md border"
                  :style="{ background: t.vars['bg-base'], borderColor: t.vars.border }"
                >
                  <div class="absolute left-2 top-2 flex h-[calc(100%-16px)] w-[46%] flex-col gap-[5px] rounded p-[7px]" :style="{ background: t.vars['bg-panel'] }">
                    <span class="h-1 w-4/5 rounded-sm opacity-90" :style="{ background: t.vars['text-primary'] }" />
                    <span class="h-1 w-1/2 rounded-sm opacity-60" :style="{ background: t.vars['text-secondary'] }" />
                  </div>
                  <div class="absolute bottom-[9px] right-[9px] flex gap-[5px]">
                    <span class="h-[9px] w-[9px] rounded-full" :style="{ background: t.vars.accent }" />
                    <span class="h-[9px] w-[9px] rounded-full" :style="{ background: t.vars.green }" />
                    <span class="h-[9px] w-[9px] rounded-full" :style="{ background: t.vars.yellow }" />
                    <span class="h-[9px] w-[9px] rounded-full" :style="{ background: t.vars.red }" />
                  </div>
                </div>
                <div class="flex items-center justify-between px-0.5">
                  <span class="text-xs text-foreground">{{ t.label }}</span>
                  <PhCheck v-if="ui.theme === t.key" :size="13" class="text-accent" />
                </div>
              </button>
            </div>
          </div>

          <div class="h-px bg-border" />

          <!-- Typography -->
          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Interface font</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">UI font</span>
                <span class="text-[11px] text-muted-foreground">Font used across the app interface</span>
              </div>
              <Select v-model="ui.uiFont" :options="uiFontOptions" :trigger-style="{ fontFamily: ui.uiFont }" class="min-w-[200px] bg-hover text-xs hover:border-muted-foreground" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">UI font size</span>
                <span class="text-[11px] text-muted-foreground">Base interface text size (10–20)</span>
              </div>
              <div class="flex items-center gap-1.5">
                <input
                  class="h-8 w-16 cursor-text rounded-md border border-border bg-hover px-2.5 text-center text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent"
                  type="number"
                  min="10"
                  max="20"
                  :value="ui.uiFontSize"
                  @input="ui.uiFontSize = clampRange(val($event), 10, 20, 13)"
                />
                <span class="text-xs text-muted-foreground/70">px</span>
              </div>
            </div>
          </div>

          <div class="flex flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Terminal font</span>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Terminal font</span>
                <span class="text-[11px] text-muted-foreground">Monospace font for terminal panes</span>
              </div>
              <Select v-model="ui.terminalFont" :options="terminalFontOptions" :trigger-style="{ fontFamily: ui.terminalFont }" class="min-w-[200px] bg-hover text-xs hover:border-muted-foreground" />
            </div>
            <div class="flex items-center gap-4 rounded-md border border-border bg-panel px-4 py-3">
              <div class="flex flex-1 min-w-0 flex-col gap-0.5">
                <span class="text-[13px] font-medium text-foreground">Terminal font size</span>
                <span class="text-[11px] text-muted-foreground">Size in pixels (8–24)</span>
              </div>
              <div class="flex items-center gap-1.5">
                <input
                  class="h-8 w-16 cursor-text rounded-md border border-border bg-hover px-2.5 text-center text-xs text-foreground outline-none hover:border-muted-foreground focus:border-accent"
                  type="number"
                  min="8"
                  max="24"
                  :value="ui.terminalFontSize"
                  @input="ui.terminalFontSize = clampRange(val($event), 8, 24, 13)"
                />
                <span class="text-xs text-muted-foreground/70">px</span>
              </div>
            </div>
            <div class="rounded-md border border-border bg-base px-3.5 py-3 leading-snug text-foreground/90" :style="{ fontFamily: ui.terminalFont, fontSize: ui.terminalFontSize + 'px' }">
              <span class="text-success">~/agentic-ide $</span> claude --resume
            </div>
          </div>

          <div class="flex items-center gap-2.5">
            <Button variant="outline" size="sm" @click="ui.resetFonts()">
              <PhArrowCounterClockwise :size="12" /> Reset fonts
            </Button>
          </div>

          <div class="h-px bg-border" />

          <!-- Background image -->
          <div class="flex max-w-[560px] flex-col gap-2.5">
            <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">Background</span>

            <!-- Image picker card -->
            <div class="flex items-center gap-3.5 rounded-[10px] border border-border bg-hover p-3">
              <div
                class="group relative flex h-[74px] w-[116px] shrink-0 cursor-pointer flex-col items-center justify-center gap-1 overflow-hidden rounded-lg border border-border bg-base bg-cover bg-center transition-colors duration-[120ms] hover:border-accent"
                :class="{ 'border-dashed text-muted-foreground': !ui.bgImageUrl }"
                :style="ui.bgImageUrl ? { backgroundImage: `url('${ui.bgImageUrl}')` } : {}"
                @click="pickBgImage"
              >
                <template v-if="!ui.bgImageUrl">
                  <PhImage :size="22" weight="thin" />
                  <span class="text-[10px] text-muted-foreground">Click to choose</span>
                </template>
                <div v-else class="absolute inset-0 flex items-center justify-center bg-black/45 text-white opacity-0 transition-opacity duration-[120ms] group-hover:opacity-100"><PhPencilSimple :size="16" weight="bold" /></div>
              </div>
              <div class="flex min-w-0 flex-1 flex-col gap-0.75">
                <span class="truncate text-[13px] font-semibold text-foreground">{{ ui.bgImagePath ? bgFileName(ui.bgImagePath) : "No background image" }}</span>
                <span class="text-[11px] text-muted-foreground">{{ ui.bgImagePath ? "Shown behind the workspace" : "PNG, JPG or WebP" }}</span>
                <div class="mt-2 flex gap-1.5">
                  <button class="rounded-md border border-border bg-base px-3 py-1.25 text-xs text-foreground transition-colors hover:border-accent hover:text-accent" @click="pickBgImage">
                    {{ ui.bgImagePath ? "Replace…" : "Choose image…" }}
                  </button>
                  <button v-if="ui.bgImagePath" class="rounded-md border border-border bg-base px-3 py-1.25 text-xs text-foreground transition-colors hover:border-destructive hover:text-destructive" @click="ui.clearBgImage()">Remove</button>
                </div>
              </div>
            </div>

            <template v-if="ui.bgImagePath">
              <!-- Opacity -->
              <div class="flex flex-col gap-2 rounded-[10px] border border-border bg-hover px-3.5 py-3">
                <div class="flex items-baseline justify-between gap-2.5">
                  <span class="text-xs font-semibold text-foreground">Opacity</span>
                  <span class="text-xs font-semibold tabular-nums text-accent">{{ Math.round(ui.bgOpacity * 100) }}%</span>
                </div>
                <input
                  type="range"
                  min="0.2"
                  max="1"
                  step="0.01"
                  :value="ui.bgOpacity"
                  class="h-1 w-full cursor-pointer accent-accent"
                  @input="ui.bgOpacity = parseFloat(($event.target as HTMLInputElement).value)"
                />
              </div>

              <!-- Backdrop blur -->
              <div class="flex flex-col gap-2 rounded-[10px] border border-border bg-hover px-3.5 py-3">
                <div class="flex items-baseline justify-between gap-2.5">
                  <span class="text-xs font-semibold text-foreground">Backdrop blur</span>
                  <span class="text-[11px] text-muted-foreground">Frosted-glass over the image</span>
                </div>
                <div class="mt-0.5 flex flex-col gap-2.5">
                  <div v-for="b in blurControls" :key="b.key" class="grid grid-cols-[170px_1fr_42px] items-center gap-3">
                    <span class="text-xs text-secondary-foreground">{{ b.label }}</span>
                    <input
                      type="range"
                      min="0"
                      max="40"
                      step="1"
                      :value="(ui as any)[b.key]"
                      class="h-1 w-full cursor-pointer accent-accent"
                      @input="(ui as any)[b.key] = parseInt(($event.target as HTMLInputElement).value)"
                    />
                    <span class="text-right text-[11px] tabular-nums text-accent">{{ (ui as any)[b.key] }}px</span>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </section>

        <!-- About / Updates -->
        <section v-else-if="active === 'about'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">About</h2>
              <span class="text-xs text-muted-foreground">Version &amp; updates</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2.5">
            <div class="flex items-center gap-3.5">
              <div class="flex h-12 w-12 items-center justify-center rounded-xl border border-border bg-hover text-accent"><PhTerminalWindow :size="26" weight="duotone" /></div>
              <div>
                <div class="text-[15px] font-semibold text-foreground">Burrow</div>
                <div class="mt-0.5 text-xs text-secondary-foreground">Version {{ appVersion || "…" }}</div>
              </div>
            </div>

            <div class="mt-4 rounded-lg border border-border bg-panel px-4 py-3.5">
              <div class="flex items-center justify-between gap-4">
                <div class="flex min-w-0 flex-col gap-0.75">
                  <template v-if="update.installed">
                    <span class="text-[12.5px] font-semibold text-foreground">Update installed</span>
                    <span class="text-[11.5px] text-secondary-foreground">Restart to finish updating to v{{ update.newVersion }}.</span>
                  </template>
                  <template v-else-if="update.downloading">
                    <span class="text-[12.5px] font-semibold text-foreground">Downloading v{{ update.newVersion }}…</span>
                    <span class="text-[11.5px] text-secondary-foreground">{{ update.progress >= 0 ? Math.round(update.progress * 100) + "%" : "…" }}</span>
                  </template>
                  <template v-else-if="update.available">
                    <span class="text-[12.5px] font-semibold text-foreground">Update available — v{{ update.newVersion }}</span>
                    <span class="text-[11.5px] text-secondary-foreground">You have v{{ update.currentVersion }}.</span>
                  </template>
                  <template v-else>
                    <span class="text-[12.5px] font-semibold text-foreground">You're up to date</span>
                    <span class="text-[11.5px] text-secondary-foreground">Last checked {{ lastCheckedLabel }}.</span>
                  </template>
                </div>

                <div class="shrink-0">
                  <Button v-if="update.installed" size="sm" @click="update.relaunch()">
                    <PhArrowClockwise :size="12" /> Restart now
                  </Button>
                  <Button
                    v-else-if="update.available && !update.downloading"
                    size="sm"
                    @click="update.downloadAndInstall()"
                  >
                    <PhDownloadSimple :size="12" /> Install v{{ update.newVersion }}
                  </Button>
                  <Button
                    v-else-if="!update.downloading"
                    variant="outline"
                    size="sm"
                    :disabled="update.checking"
                    @click="update.check()"
                  >
                    <PhArrowClockwise :size="12" :class="{ 'animate-spin': update.checking }" />
                    {{ update.checking ? "Checking…" : "Check for updates" }}
                  </Button>
                </div>
              </div>

              <div v-if="update.notes && update.available && !update.installed" class="mt-3 max-h-40 overflow-y-auto whitespace-pre-wrap border-t border-border pt-3 text-[11.5px] leading-snug text-secondary-foreground">
                {{ update.notes }}
              </div>
              <div v-if="update.error && !update.checking" class="mt-2.5 break-words text-[11px] text-destructive">
                Update check failed: {{ update.error }}
              </div>
            </div>

            <div class="mt-4 rounded-lg border border-border bg-panel px-4 py-3.5">
              <div class="flex items-center justify-between gap-4">
                <div class="flex min-w-0 flex-col gap-0.75">
                  <span class="text-[12.5px] font-semibold text-foreground">Agent status hooks</span>
                  <span class="text-[11.5px] text-secondary-foreground">
                    Fix agent status dots if they get stuck or stop updating —
                    re-points revived sessions at the live server and reinstalls
                    the global hooks.
                  </span>
                </div>
                <div class="flex shrink-0 items-center gap-2">
                  <Button variant="outline" size="sm" :disabled="repairing || reinstalling" @click="reinstallStatusHooks">
                    <PhArrowClockwise :size="12" :class="{ 'animate-spin': reinstalling }" />
                    {{ reinstalling ? "Reinstalling…" : "Reinstall hooks" }}
                  </Button>
                  <Button variant="outline" size="sm" :disabled="repairing || reinstalling" @click="repairAgentStatus">
                    <PhArrowClockwise :size="12" :class="{ 'animate-spin': repairing }" />
                    {{ repairing ? "Repairing…" : "Fix agent status" }}
                  </Button>
                </div>
              </div>
              <div v-if="repairMsg" class="mt-3 border-t border-border pt-3 text-[11.5px] leading-snug text-secondary-foreground">{{ repairMsg }}</div>
            </div>
          </div>
        </section>

        <!-- Skills -->
        <section v-else-if="active === 'skills'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Skills</h2>
              <span class="text-xs text-muted-foreground">Agent skills installed in <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">~/.claude/skills</code></span>
            </div>
            <Button variant="outline" size="sm" class="ml-auto" :disabled="skillsLoading" @click="loadSkills">
              <PhArrowClockwise :size="11" :class="{ 'animate-spin': skillsLoading }" /> Refresh
            </Button>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2">
            <div v-for="s in skills" :key="s.dir" class="flex items-start gap-3 rounded-lg border border-border bg-panel px-3.5 py-3" :class="{ 'opacity-55': !s.enabled }">
              <div class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[7px] bg-hover text-secondary-foreground"><PhSparkle :size="15" /></div>
              <div class="flex-1 min-w-0">
                <div class="text-[13px] font-semibold text-foreground">{{ s.name }}</div>
                <div class="mt-0.75 text-[11.5px] leading-snug text-secondary-foreground">{{ s.description || "No description" }}</div>
              </div>
              <div class="flex shrink-0 items-center gap-1">
                <button
                  class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-secondary-foreground hover:bg-hover hover:text-foreground"
                  :title="s.enabled ? 'Disable skill' : 'Enable skill'"
                  @click="toggleSkill(s)"
                >
                  <component :is="s.enabled ? PhToggleRight : PhToggleLeft" :size="20"
                    :class="s.enabled ? 'text-success' : 'text-secondary-foreground'" />
                </button>
                <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-secondary-foreground hover:bg-hover hover:text-foreground" title="Reveal in Finder" @click="revealSkill(s)">
                  <PhArrowSquareOut :size="14" />
                </button>
                <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-secondary-foreground hover:bg-hover hover:text-destructive" title="Delete skill" @click="deleteSkill(s)">
                  <PhTrash :size="14" />
                </button>
              </div>
            </div>
            <div v-if="!skillsLoading && skills.length === 0" class="py-5 text-center text-xs text-muted-foreground/50">
              No skills found. Install one via Claude Code or the skill marketplace.
            </div>
          </div>
        </section>

        <!-- MCP Servers -->
        <section v-else-if="active === 'mcp'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">MCP Servers</h2>
              <span class="text-xs text-muted-foreground">Model Context Protocol servers in <code class="rounded bg-hover px-1 font-mono text-[10px] text-secondary-foreground">~/.claude.json</code></span>
            </div>
            <Button variant="outline" size="sm" class="ml-auto" @click="startAddMcp">
              <PhPlus :size="11" /> Add Server
            </Button>
          </div>
          <div class="h-px bg-border" />

          <!-- Add / edit form -->
          <div v-if="mcpFormOpen" class="mb-3.5 flex flex-col gap-2 rounded-lg border border-border bg-panel px-3.5 py-3">
            <div class="flex items-center">
              <span class="text-[11px] font-semibold uppercase tracking-[0.06em] text-muted-foreground">{{ mcpEditName ? "Edit" : "New" }} MCP server</span>
              <button class="ml-auto flex rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground" @click="mcpFormOpen = false"><PhX :size="12" /></button>
            </div>
            <input
              class="max-w-[280px] rounded-md border border-border bg-base/40 px-2.5 py-1.5 text-[13px] text-foreground outline-none placeholder:text-muted-foreground/50 focus:border-accent"
              v-model="mcpName"
              :disabled="!!mcpEditName"
              placeholder="server-name"
              spellcheck="false"
            />
            <textarea
              class="box-border w-full resize-y rounded-md border border-border bg-panel px-2.5 py-2 font-mono text-xs leading-relaxed text-foreground outline-none placeholder:text-muted-foreground/50 focus:border-accent"
              v-model="mcpConfig"
              rows="7"
              spellcheck="false"
              placeholder='{ "command": "npx", "args": ["-y", "@some/mcp-server"] }'
            />
            <div class="flex items-center gap-2">
              <span v-if="mcpError" class="text-[11px] text-destructive">{{ mcpError }}</span>
              <span class="flex-1" />
              <Button variant="outline" size="sm" @click="mcpFormOpen = false">Cancel</Button>
              <Button size="sm" :disabled="mcpSaving" @click="saveMcp">
                {{ mcpSaving ? "Saving…" : "Save" }}
              </Button>
            </div>
          </div>

          <div class="flex flex-col gap-2">
            <div v-for="m in mcpServers" :key="m.name" class="flex items-start gap-3 rounded-lg border border-border bg-panel px-3.5 py-3">
              <div class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[7px] bg-hover text-secondary-foreground"><PhPlugsConnected :size="15" /></div>
              <div class="flex-1 min-w-0">
                <div class="text-[13px] font-semibold text-foreground">{{ m.name }}</div>
                <pre class="mt-1.5 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-base px-2.5 py-2 font-mono text-[11px] leading-snug text-secondary-foreground">{{ m.config }}</pre>
              </div>
              <div class="flex shrink-0 items-center gap-1">
                <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-secondary-foreground hover:bg-hover hover:text-foreground" title="Edit" @click="editMcp(m)">
                  <PhPencilSimple :size="14" />
                </button>
                <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-secondary-foreground hover:bg-hover hover:text-destructive" title="Remove server" @click="removeMcp(m)">
                  <PhTrash :size="14" />
                </button>
              </div>
            </div>
            <div v-if="mcpServers.length === 0 && !mcpFormOpen" class="py-5 text-center text-xs text-muted-foreground/50">
              No MCP servers configured. Add one to give every Claude session new tools.
            </div>
          </div>
        </section>

        <!-- Extensions (browser — planned) -->
        <section v-else-if="active === 'extensions'" class="flex flex-col gap-3.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-baseline gap-2.5">
              <h2 class="text-[15px] font-semibold text-foreground">Extensions</h2>
              <span class="text-xs text-muted-foreground">Browser &amp; editor integrations</span>
            </div>
          </div>
          <div class="h-px bg-border" />

          <div class="flex flex-col gap-2">
            <div class="flex items-start gap-3 rounded-lg border border-dashed border-border bg-panel px-3.5 py-3">
              <div class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[7px] bg-hover text-secondary-foreground"><PhBrowser :size="15" /></div>
              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2 text-[13px] font-semibold text-foreground">
                  Browser extension
                  <Badge variant="warning" class="rounded-full text-[9.5px] font-bold uppercase tracking-[0.04em]">Planned</Badge>
                </div>
                <div class="mt-0.75 text-[11.5px] leading-snug text-secondary-foreground">
                  Drive a real browser tab from inside Burrow — let agents open pages,
                  fill forms, and read the DOM without leaving the IDE.
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- Other panels (placeholder) -->
        <section v-else class="flex h-full flex-col items-center justify-center gap-3 text-[13px] text-muted-foreground/40">
          <component :is="activeIcon" :size="22" />
          <span>{{ activeLabel }} settings coming soon</span>
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, type Component } from "vue";
import {
  PhGearSix, PhX, PhPlus, PhTrash, PhArrowCounterClockwise,
  PhSlidersHorizontal, PhFolderOpen, PhRobot, PhPalette, PhKeyboard,
  PhPuzzlePiece, PhInfo, PhSparkle,
  PhFolder, PhPencilSimple, PhCheck, PhBell, PhPlay,
  PhArrowClockwise, PhDownloadSimple, PhTerminalWindow,
  PhPlugsConnected, PhBrowser, PhToggleLeft, PhToggleRight, PhArrowSquareOut, PhImage,
  PhPaperPlaneTilt, PhPawPrint, PhPlayCircle, PhArrowUp, PhArrowDown,
  PhDesktop, PhSun, PhMoon,
} from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { pickDir, pickFile, AUDIO_EXTS, IMAGE_EXTS } from "@/lib/pickPath";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Select } from "@/components/ui/select";
import { useScriptsStore, type Script } from "@/stores/scripts";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore, UI_FONTS, TERMINAL_FONTS, NTFY_EVENTS, TOAST_POSITIONS, type NtfyEvent } from "@/stores/ui";
import { loadSystemFonts, isMonospace, toPreset } from "@/lib/systemFonts";
import { useProvidersStore, transportLabel } from "@/stores/providers";
import ProvidersPanel from "@/components/ProvidersPanel.vue";
import { testNtfy } from "@/lib/ntfy";
import { useUpdateStore } from "@/stores/update";
import { THEMES } from "@/themes";
import { soundsForKind, playSound, type SoundKind } from "@/lib/sounds";
import { eventToShortcut } from "@/lib/shortcuts";
import { useKeybindingsStore } from "@/stores/keybindings";
import { FIXED_SHORTCUTS } from "@/lib/keymap";
import { MODELS_BY_AGENT } from "@/lib/chatModels";

defineEmits<{ close: [] }>();

// Deep-link into one provider instance (chat header → "configure this agent").
const focusId = computed(() => ui.settingsFocusId);

const scriptsStore = useScriptsStore();
const wsStore = useWorkspaceStore();
// Installed fonts, listed alongside the built-in presets. The terminal picker
// only offers the monospace ones — a proportional font there is unusable.
const systemFonts = ref<string[]>([]);
loadSystemFonts().then((f) => (systemFonts.value = f));
const presetLabels = new Set([...UI_FONTS, ...TERMINAL_FONTS].map((f) => f.label));
const installedUiFonts = computed(() =>
  systemFonts.value.filter((f) => !presetLabels.has(f)).map((f) => toPreset(f, "sans-serif"))
);
const installedMonoFonts = computed(() =>
  systemFonts.value.filter((f) => !presetLabels.has(f) && isMonospace(f)).map((f) => toPreset(f, "monospace"))
);
const uiFontOptions = computed(() => [...UI_FONTS, ...installedUiFonts.value.map((font) => ({ ...font, label: `Installed · ${font.label}` }))]);
const terminalFontOptions = computed(() => [...TERMINAL_FONTS, ...installedMonoFonts.value.map((font) => ({ ...font, label: `Installed · ${font.label}` }))]);

const THEME_MODES: { id: "system" | "light" | "dark"; label: string; icon: Component }[] = [
  { id: "system", label: "System", icon: PhDesktop as Component },
  { id: "light", label: "Light", icon: PhSun as Component },
  { id: "dark", label: "Dark", icon: PhMoon as Component },
];

const ui = useUIStore();
const providers = useProvidersStore();
const update = useUpdateStore();

// ── Select option lists ──
const chatAgentOptions = computed(() =>
  providers.chatAgents.map((a) => ({ value: a.id, label: `${a.name} (${transportLabel(a.transport as Exclude<typeof a.transport, "none">)})` })),
);
const SPAWN_MODE_OPTIONS = [
  { value: "terminal", label: "Terminal tab" },
  { value: "chat", label: "Chat" },
];
const commitMessageModelOptions = MODELS_BY_AGENT.claude.map((m) => ({ value: m.id, label: m.label }));
const FLOAT_CORNER_OPTIONS = [
  { value: "top-right", label: "Top right" },
  { value: "top-left", label: "Top left" },
  { value: "bottom-right", label: "Bottom right" },
  { value: "bottom-left", label: "Bottom left" },
];
const UI_SCALE_OPTIONS = [
  { value: "0.8", label: "80%" },
  { value: "0.9", label: "90%" },
  { value: "1", label: "100%" },
  { value: "1.1", label: "110%" },
  { value: "1.25", label: "125%" },
  { value: "1.5", label: "150%" },
];
const uiScaleModel = computed({
  get: () => String(ui.uiScale),
  set: (v: string) => { ui.uiScale = Number(v); },
});
const toastPositionOptions = computed(() => TOAST_POSITIONS.map((p) => ({ value: p.id, label: p.label })));
const soundDoneOptions = computed(() => [
  ...soundsForKind("done").map((s) => ({ value: s.id, label: s.label })),
  { value: "custom", label: "Custom file…" },
]);
const soundWaitingOptions = computed(() => [
  ...soundsForKind("waiting").map((s) => ({ value: s.id, label: s.label })),
  { value: "custom", label: "Custom file…" },
]);

// ── Scripts ──
// The active repo's scripts are scoped to the active workspace.
const activeWsId = computed(() => wsStore.active?.id ?? null);
const activeWsPath = computed(() => wsStore.active?.path ?? null);
const activeWsName = computed(() => wsStore.active?.name ?? "");

// Step helpers mutate the reactive Script in place — the store's deep watcher
// persists the change.
function addStep(s: Script) { s.steps.push(""); }
function removeStep(s: Script, i: number) {
  s.steps.splice(i, 1);
  if (s.steps.length === 0) s.steps.push("");
}
function moveStep(s: Script, from: number, to: number) {
  if (to < 0 || to >= s.steps.length) return;
  const [item] = s.steps.splice(from, 1);
  s.steps.splice(to, 0, item);
}
function setStep(s: Script, i: number, value: string) { s.steps[i] = value; }
function scriptPreview(s: Script): string { return scriptsStore.commandLine(s) || "—"; }

// ── Integrations: ntfy.sh ──
const ntfyTesting = ref(false);
const ntfyTestMsg = ref("");
const ntfyTestErr = ref(false);

function toggleNtfyEvent(ev: NtfyEvent, on: boolean) {
  const set = new Set(ui.ntfyEvents);
  if (on) set.add(ev);
  else set.delete(ev);
  // Reassign the array so the store watcher persists the change.
  ui.ntfyEvents = NTFY_EVENTS.map((e) => e.id).filter((id) => set.has(id));
}

async function sendNtfyTest() {
  if (!ui.ntfyTopic) return;
  ntfyTesting.value = true;
  ntfyTestMsg.value = "";
  ntfyTestErr.value = false;
  try {
    await testNtfy({ server: ui.ntfyServer, topic: ui.ntfyTopic, token: ui.ntfyToken || undefined });
    ntfyTestMsg.value = "Sent — check your ntfy app";
  } catch (e) {
    ntfyTestErr.value = true;
    ntfyTestMsg.value = `Failed: ${e instanceof Error ? e.message : String(e)}`;
  } finally {
    ntfyTesting.value = false;
  }
}

// Resolved at mount from the Tauri runtime so the displayed version always
// matches the actual bundle, not a hard-coded string.
const appVersion = ref("");
import("@tauri-apps/api/app")
  .then((m) => m.getVersion())
  .then((v) => { appVersion.value = v; })
  .catch(() => { appVersion.value = "dev"; });

// Agent-status repair: force-reclaim hook.port + reinstall hooks. Rescues
// revived/reattached PTYs whose baked port went stale (e.g. after running a dev
// build that clobbered the shared port file).
const repairing = ref(false);
const reinstalling = ref(false);
const repairMsg = ref("");
async function reinstallStatusHooks() {
  reinstalling.value = true;
  repairMsg.value = "";
  try {
    await invoke("reinstall_status_hooks");
    repairMsg.value = "Status hooks reinstalled.";
  } catch (e) {
    repairMsg.value = `Reinstall failed: ${e}`;
  } finally {
    reinstalling.value = false;
  }
}
async function repairAgentStatus() {
  repairing.value = true;
  repairMsg.value = "";
  try {
    const port = await invoke<number>("repair_agent_status");
    repairMsg.value = port ? `Status hooks repaired (port ${port}).` : "Status hooks repaired.";
  } catch (e) {
    repairMsg.value = `Repair failed: ${e}`;
  } finally {
    repairing.value = false;
  }
}

const lastCheckedLabel = computed(() => {
  if (!update.lastChecked) return "never";
  const mins = Math.round((Date.now() - update.lastChecked) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  return `${Math.round(mins / 60)} h ago`;
});

function mimeForPath(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "svg") return "image/svg+xml";
  if (ext === "ico") return "image/x-icon";
  if (ext === "jpg" || ext === "jpeg") return "image/jpeg";
  return "image/png";
}

async function pickWsIcon(id: number) {
  const selected = await pickFile({ title: "Set icon", extensions: ["png", "jpg", "jpeg", "svg", "ico"] });
  if (!selected) return;
  const b64 = await invoke<string>("read_file_base64", { path: selected });
  wsStore.setIcon(id, `data:${mimeForPath(selected)};base64,${b64}`);
}

async function pickWorktreesDir() {
  const selected = await pickDir({ title: "Choose folder", start: ui.worktreesDir || "~/" });
  if (selected) ui.worktreesDir = selected;
}
// Notification sounds: choose a custom audio file for done/waiting and store its
// path; sounds.ts reads it lazily via read_file_base64.
async function pickSound(kind: SoundKind) {
  const current = kind === "done" ? ui.soundDoneCustomPath : ui.soundWaitingCustomPath;
  const selected = await pickFile({ title: "Use sound", start: current || "~/", extensions: AUDIO_EXTS });
  if (!selected) return;
  if (kind === "done") {
    ui.soundDoneCustomPath = selected;
    ui.soundDoneId = "custom";
  } else {
    ui.soundWaitingCustomPath = selected;
    ui.soundWaitingId = "custom";
  }
}

function soundFileName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

async function pickBgImage() {
  const selected = await pickFile({ title: "Use wallpaper", start: ui.bgImagePath || "~/", extensions: IMAGE_EXTS });
  if (selected) ui.bgImagePath = selected;
}

function bgFileName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

// Per-element backdrop-blur sliders (keys map to ui store refs).
const blurControls = [
  { key: "blurPanels", label: "Panels (sidebar, bars)" },
  { key: "blurContent", label: "Dashboard" },
  { key: "blurTerminal", label: "Terminal" },
  { key: "blurOverlay", label: "Overlays (spotlight, settings)" },
  { key: "blurDropdown", label: "Dropdowns (menus, notifications)" },
] as const;

// Deep-link target set by the caller (⌘P → "Keyboard Shortcuts" etc.).
const active = ref(ui.settingsSection || "general");

// ── Skills manager ────────────────────────────────────────────────────────────
type SkillInfo = { name: string; description: string; dir: string; enabled: boolean };
const skills = ref<SkillInfo[]>([]);
const skillsLoading = ref(false);

async function loadSkills() {
  skillsLoading.value = true;
  try {
    skills.value = await invoke<SkillInfo[]>("list_skills");
  } catch (e) {
    console.error("list_skills failed", e);
  } finally {
    skillsLoading.value = false;
  }
}

async function toggleSkill(s: SkillInfo) {
  try {
    await invoke("set_skill_enabled", { dir: s.dir, enabled: !s.enabled });
    s.enabled = !s.enabled;
  } catch (e) {
    console.error("set_skill_enabled failed", e);
  }
}

function revealSkill(s: SkillInfo) {
  invoke("open_path_in", { path: s.dir, target: "finder" }).catch(() => {});
}

async function deleteSkill(s: SkillInfo) {
  if (!confirm(`Delete skill "${s.name}"? This removes its folder permanently.`)) return;
  try {
    await invoke("delete_skill", { dir: s.dir });
    skills.value = skills.value.filter((x) => x.dir !== s.dir);
  } catch (e) {
    console.error("delete_skill failed", e);
  }
}

// ── MCP server manager ────────────────────────────────────────────────────────
type McpServer = { name: string; config: string };
const mcpServers = ref<McpServer[]>([]);
const mcpFormOpen = ref(false);
const mcpEditName = ref<string | null>(null);
const mcpName = ref("");
const mcpConfig = ref("");
const mcpError = ref("");
const mcpSaving = ref(false);

async function loadMcp() {
  try {
    mcpServers.value = await invoke<McpServer[]>("list_mcp_servers");
  } catch (e) {
    console.error("list_mcp_servers failed", e);
  }
}

function startAddMcp() {
  mcpEditName.value = null;
  mcpName.value = "";
  mcpConfig.value = '{\n  "command": "npx",\n  "args": ["-y", "@some/mcp-server"]\n}';
  mcpError.value = "";
  mcpFormOpen.value = true;
}

function editMcp(m: McpServer) {
  mcpEditName.value = m.name;
  mcpName.value = m.name;
  mcpConfig.value = m.config;
  mcpError.value = "";
  mcpFormOpen.value = true;
}

async function saveMcp() {
  mcpError.value = "";
  if (!mcpName.value.trim()) { mcpError.value = "Name is required."; return; }
  try {
    JSON.parse(mcpConfig.value);
  } catch (e) {
    mcpError.value = `Invalid JSON: ${e instanceof Error ? e.message : e}`;
    return;
  }
  mcpSaving.value = true;
  try {
    await invoke("add_mcp_server", { name: mcpName.value.trim(), config: mcpConfig.value });
    mcpFormOpen.value = false;
    await loadMcp();
  } catch (e) {
    mcpError.value = String(e);
  } finally {
    mcpSaving.value = false;
  }
}

async function removeMcp(m: McpServer) {
  if (!confirm(`Remove MCP server "${m.name}"?`)) return;
  try {
    await invoke("remove_mcp_server", { name: m.name });
    mcpServers.value = mcpServers.value.filter((x) => x.name !== m.name);
  } catch (e) {
    console.error("remove_mcp_server failed", e);
  }
}

// Lazy-load each panel's data the first time it's opened.
watch(active, (id) => {
  if (id === "skills" && skills.value.length === 0) loadSkills();
  if (id === "mcp" && mcpServers.value.length === 0) loadMcp();
});

// --- App keybindings (Keybindings section) ---
const keys = useKeybindingsStore();
const kbRecording = ref<string | null>(null);

function startKeyRecord(id: string, e: MouseEvent) {
  kbRecording.value = kbRecording.value === id ? null : id;
  // WebKit doesn't focus a <button> on click, so its @keydown never fires.
  if (kbRecording.value === id) (e.currentTarget as HTMLElement)?.focus();
}

function onKeyRecord(id: string, e: KeyboardEvent) {
  if (kbRecording.value !== id) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") return void (kbRecording.value = null);
  if (e.key === "Backspace" || e.key === "Delete") {
    keys.set(id, "");
    kbRecording.value = null;
    return;
  }
  const sc = eventToShortcut(e);
  if (!sc) return; // wait for a non-modifier key
  keys.set(id, sc);
  kbRecording.value = null;
}

async function openConfigFile() {
  const path = await invoke<string>("config_file_path").catch(() => "");
  if (path) invoke("open_path_in", { path, target: "editor" }).catch(() => {});
}

interface NavItem {
  id?: string;
  label?: string;
  icon?: Component;
  divider?: boolean;
}

const navItems: NavItem[] = [
  { id: "general", label: "General", icon: PhSlidersHorizontal },
  { id: "workspaces", label: "Workspaces", icon: PhFolderOpen },
  { id: "providers", label: "Providers", icon: PhRobot },
  { id: "scripts", label: "Scripts", icon: PhPlayCircle },
  { id: "skills", label: "Skills", icon: PhSparkle },
  { id: "mcp", label: "MCP Servers", icon: PhPlugsConnected },
  { divider: true },
  { id: "remote", label: "Remote access", icon: PhTerminalWindow },
  { id: "appearance", label: "Appearance", icon: PhPalette },
  { id: "notifications", label: "Notifications", icon: PhBell },
  { id: "integrations", label: "Integrations", icon: PhPlugsConnected },
  { id: "keybindings", label: "Keybindings", icon: PhKeyboard },
  { id: "plugins", label: "Plugins", icon: PhPawPrint },
  { id: "extensions", label: "Extensions", icon: PhPuzzlePiece },
  { id: "about", label: "About", icon: PhInfo },
];

const activeLabel = computed(
  () => navItems.find((i) => i.id === active.value)?.label ?? "",
);
const activeIcon = computed(
  () => navItems.find((i) => i.id === active.value)?.icon ?? PhInfo,
);

function val(e: Event): string {
  return (e.target as HTMLInputElement).value;
}

function clampRange(v: string, min: number, max: number, fallback: number): number {
  const n = Number(v);
  if (Number.isNaN(n)) return fallback;
  return Math.min(max, Math.max(min, Math.round(n)));
}

// §5 HTTP/WS transport toggle. Read-only status (port/token path) refreshed
// on mount + after every toggle; actual bind only happens on next restart
// (server::maybe_start runs once at Tauri setup), so this just writes the
// pref file and reflects the pending state back.
const httpEnabled = ref(false);
const httpStatus = ref<{ enabled: boolean; port: number; tokenPath: string; token: string; pairCode: string; pairLocked: boolean } | null>(null);

async function onRegeneratePairCode() {
  await invoke("regenerate_pair_code");
  await refreshHttpStatus();
}
async function refreshHttpStatus() {
  try {
    const s = await invoke<{ enabled: boolean; port: number; tokenPath: string; token: string; pairCode: string; pairLocked: boolean }>("get_http_server_status");
    httpStatus.value = s;
    httpEnabled.value = s.enabled;
  } catch { /* browser-only dev — no Tauri backend */ }
}
async function onToggleHttp(checked: boolean) {
  httpEnabled.value = checked;
  try {
    await invoke("set_http_enabled", { enabled: checked });
  } catch { /* browser-only dev */ }
  await refreshHttpStatus();
}
refreshHttpStatus();

interface TailscaleStatus {
  installed: boolean;
  logged_in: boolean;
  dns_name: string | null;
  serving: boolean;
  serve_url: string | null;
}
const tailscaleStatus = ref<TailscaleStatus | null>(null);
async function refreshTailscaleStatus() {
  try {
    tailscaleStatus.value = await invoke<TailscaleStatus>("get_tailscale_status");
  } catch { /* browser-only dev */ }
}
async function onToggleTailscale(checked: boolean) {
  try {
    tailscaleStatus.value = await invoke<TailscaleStatus>("set_tailscale_serve", {
      enabled: checked,
      port: httpStatus.value?.port ?? 8420,
    });
  } catch { /* leaves tailscaleStatus as-is; toggle snaps back via v-model binding to server state */ }
  await refreshTailscaleStatus();
}
refreshTailscaleStatus();

const copiedLabel = ref<string | null>(null);
async function copyToClipboard(text: string, label: string) {
  try {
    await navigator.clipboard.writeText(text);
    copiedLabel.value = label;
    setTimeout(() => { if (copiedLabel.value === label) copiedLabel.value = null; }, 1500);
  } catch { /* clipboard unavailable */ }
}

</script>
