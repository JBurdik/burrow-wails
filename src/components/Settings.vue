<template>
  <div class="settings-page">
    <!-- Header -->
    <div class="s-header">
      <div class="s-head-title">
        <PhGearSix :size="15" class="s-head-icon" />
        <span class="s-title">Settings</span>
      </div>
      <button class="s-close" title="Close (Esc)" @click="$emit('close')">
        <PhX :size="15" />
      </button>
    </div>

    <div class="s-body">
      <!-- Nav -->
      <nav class="s-nav">
        <template v-for="item in navItems" :key="item.id">
          <div v-if="item.divider" class="nav-divider" />
          <button
            v-else
            class="nav-item"
            :class="{ active: active === item.id }"
            @click="active = item.id!"
          >
            <component :is="item.icon" :size="14" class="nav-icon" />
            <span class="nav-label">{{ item.label }}</span>
          </button>
        </template>
      </nav>

      <!-- Content -->
      <div class="s-content">
        <!-- Agents -->
        <section v-if="active === 'agents'" class="section">
          <div v-if="flagEditId || iconPickerId || showTemplatePicker" class="flag-backdrop" @click="flagEditId = null; iconPickerId = null; showTemplatePicker = false" />
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Agents</h2>
              <span class="sec-sub">Quick-launch terminal commands</span>
            </div>
            <div class="add-area">
              <button class="add-btn" @click="store.add()">
                <PhPlus :size="11" /> Add Agent
              </button>
              <div class="template-wrap">
                <button class="add-btn template-btn" title="Add from template" @click.stop="showTemplatePicker = !showTemplatePicker">
                  <PhCaretDown :size="11" />
                </button>
                <div v-if="showTemplatePicker" class="template-pop" @click.stop>
                  <div class="tp-head">Quick add from template</div>
                  <button v-for="t in TEMPLATES" :key="t.id" class="tp-row" @click="addFromTemplate(t); showTemplatePicker = false">
                    <span class="tp-icon" :style="{ background: t.color + '22', borderColor: t.color + '44' }">
                      <component :is="iconFor(t.icon)" :size="12" :style="{ color: t.color }" />
                    </span>
                    <span class="tp-name">{{ t.name }}</span>
                    <code class="tp-cmd">{{ t.command }}</code>
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="tbl">
            <div class="tbl-head">
              <span class="col col-grip" />
              <span class="col col-agent">Agent</span>
              <span class="col col-cmd">Command</span>
              <span class="col col-args">Args / Flags</span>
              <span class="col col-kbd">Shortcut</span>
              <span class="col col-act" />
            </div>

            <div
              v-for="(a, i) in store.agents"
              :key="a.id"
              class="row"
              :data-reorder-idx="i"
              :class="{ dragging: dragIndex === i, 'drag-over': dragOverIndex === i && dragIndex !== i }"
            >
              <!-- Drag handle -->
              <div
                class="col-grip grip"
                title="Drag to reorder"
                @pointerdown="(e: PointerEvent) => onGripDown(i, e)"
              >
                <PhDotsSixVertical :size="14" />
              </div>

              <!-- Agent -->
              <div class="col-agent cell-agent">
                <!-- Color picker on the dot -->
                <label class="dot-label" title="Pick color">
                  <span class="dot" :style="{ background: a.color }" />
                  <input
                    type="color"
                    class="color-input"
                    :value="a.color"
                    @input="store.update(a.id, { color: val($event) })"
                  />
                </label>
                <!-- Icon picker popover -->
                <div class="icon-wrap">
                  <button
                    class="icon-box"
                    :style="{ background: a.color + '22', borderColor: a.color + '55' }"
                    title="Pick icon"
                    @click.stop="toggleIconPicker(a.id)"
                  >
                    <component :is="iconFor(a.icon)" :size="13" :style="{ color: a.color }" />
                  </button>
                  <div v-if="iconPickerId === a.id" class="icon-pop" @click.stop>
                    <button
                      v-for="ic in ICON_OPTIONS"
                      :key="ic.key"
                      class="ip-btn"
                      :class="{ active: a.icon === ic.key }"
                      :title="ic.label"
                      @click="store.update(a.id, { icon: ic.key }); iconPickerId = null"
                    >
                      <component :is="ic.component" :size="14" />
                    </button>
                  </div>
                </div>
                <input
                  class="inp name-inp"
                  :value="a.name"
                  placeholder="Agent name"
                  @input="store.update(a.id, { name: val($event) })"
                />
              </div>

              <!-- Command -->
              <div class="col-cmd">
                <div class="pill">
                  <input
                    class="inp mono cmd-inp"
                    :style="{ color: a.color }"
                    :value="a.command"
                    placeholder="command"
                    @input="store.update(a.id, { command: val($event) })"
                  />
                </div>
              </div>

              <!-- Args -->
              <div class="col-args">
                <input
                  class="inp mono args-inp"
                  :value="a.args"
                  placeholder="--flags"
                  @input="store.update(a.id, { args: val($event) })"
                />
                <button
                  class="flag-edit"
                  :class="{ on: flagEditId === a.id }"
                  title="Edit flags"
                  @click="toggleFlagEditor(a.id)"
                >
                  <PhListBullets :size="13" />
                </button>

                <!-- Flag editor popover -->
                <div v-if="flagEditId === a.id" class="flag-pop" @click.stop>
                  <div class="fp-head">
                    <span class="fp-title">Flags</span>
                    <span class="fp-sub">one per line</span>
                    <button class="fp-close" @click="flagEditId = null">
                      <PhX :size="12" />
                    </button>
                  </div>
                  <textarea
                    class="fp-area mono"
                    rows="6"
                    spellcheck="false"
                    placeholder="--flag&#10;--key value"
                    :value="flagDraft"
                    @input="onFlagInput(a.id, $event)"
                  />
                  <div class="fp-foot">
                    <code class="fp-preview">{{ store.commandLine(a) || "—" }}</code>
                  </div>
                </div>
              </div>

              <!-- Shortcut: click to record a key combo -->
              <div class="col-kbd">
                <button
                  class="kbd-rec"
                  :class="{ recording: recordingId === a.id, set: !!a.shortcut }"
                  :title="recordingId === a.id ? 'Press keys… (Esc to cancel)' : 'Click to set shortcut'"
                  @click="startRecording(a.id, $event)"
                  @keydown="onRecordKey(a.id, $event)"
                  @blur="recordingId === a.id && (recordingId = null)"
                >
                  {{ recordingId === a.id ? "Press…" : (a.shortcut || "—") }}
                </button>
                <button
                  v-if="a.shortcut && recordingId !== a.id"
                  class="kbd-clear"
                  title="Clear shortcut"
                  @click.stop="store.update(a.id, { shortcut: '' })"
                >
                  <PhX :size="11" />
                </button>
              </div>

              <!-- Actions -->
              <div class="col-act">
                <button class="row-del" title="Remove agent" @click="store.remove(a.id)">
                  <PhTrash :size="13" />
                </button>
              </div>
            </div>

            <div v-if="store.agents.length === 0" class="tbl-empty">
              No agents. Click "Add Agent".
            </div>
          </div>

          <!-- Config directories: where Burrow installs its status hooks + agent docs -->
          <div class="settings-group cfg-dirs">
            <span class="group-label">Config directories</span>
            <p class="cfg-hint">
              Burrow installs its status hooks (<code>settings.json</code> / <code>hooks.json</code>)
              and the <code>burrow</code> agent docs into these dirs. Add the dir an agent uses if
              you point it elsewhere — e.g. a per-project <code>CLAUDE_CONFIG_DIR</code> or
              <code>CODEX_HOME</code>. Defaults (<code>~/.claude</code>, <code>~/.codex</code>,
              <code>~/.copilot</code>) plus any value set in Burrow's own environment at launch
              are seeded automatically.
            </p>

            <div class="cfg-col">
              <span class="cfg-col-label">Claude (<code>CLAUDE_CONFIG_DIR</code>)</span>
              <div v-for="(_, i) in claudeDirs" :key="'c' + i" class="cfg-row">
                <input v-model="claudeDirs[i]" class="select cfg-inp" placeholder="/path/to/.claude" spellcheck="false" />
                <button class="row-del" title="Remove" @click="claudeDirs.splice(i, 1)">
                  <PhTrash :size="13" />
                </button>
              </div>
              <button class="add-btn cfg-add" @click="claudeDirs.push('')">
                <PhPlus :size="11" /> Add Claude dir
              </button>
            </div>

            <div class="cfg-col">
              <span class="cfg-col-label">Codex (<code>CODEX_HOME</code>)</span>
              <div v-for="(_, i) in codexDirs" :key="'x' + i" class="cfg-row">
                <input v-model="codexDirs[i]" class="select cfg-inp" placeholder="/path/to/.codex" spellcheck="false" />
                <button class="row-del" title="Remove" @click="codexDirs.splice(i, 1)">
                  <PhTrash :size="13" />
                </button>
              </div>
              <button class="add-btn cfg-add" @click="codexDirs.push('')">
                <PhPlus :size="11" /> Add Codex dir
              </button>
            </div>

            <div class="cfg-col">
              <span class="cfg-col-label">Copilot (<code>COPILOT_HOME</code>)</span>
              <div v-for="(_, i) in copilotDirs" :key="'p' + i" class="cfg-row">
                <input v-model="copilotDirs[i]" class="select cfg-inp" placeholder="/path/to/.copilot" spellcheck="false" />
                <button class="row-del" title="Remove" @click="copilotDirs.splice(i, 1)">
                  <PhTrash :size="13" />
                </button>
              </div>
              <button class="add-btn cfg-add" @click="copilotDirs.push('')">
                <PhPlus :size="11" /> Add Copilot dir
              </button>
            </div>

            <div class="cfg-actions">
              <button class="add-btn cfg-save" :disabled="cfgSaving" @click="saveConfigDirs">
                <PhArrowCounterClockwise v-if="cfgSaving" :size="11" />
                {{ cfgSaving ? "Installing…" : "Save & install hooks" }}
              </button>
              <span v-if="cfgStatus" class="cfg-status">{{ cfgStatus }}</span>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Sub-agent delegation</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Max concurrent sub-agents</span>
                <span class="field-desc">Soft per-workspace cap the <code>/burrow</code> skill respects when it spawns agents (1–20)</span>
              </div>
              <div class="size-ctl">
                <input
                  class="select size-inp"
                  type="number"
                  min="1"
                  max="20"
                  :value="ui.maxAgents"
                  @input="ui.maxAgents = clampRange(val($event), 1, 20, 3)"
                />
                <span class="size-unit">agents</span>
              </div>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">MCP recursion depth</span>
                <span class="field-desc">How deep MCP-spawned sub-agents may spawn further sub-agents before the depth cap refuses (1–10)</span>
              </div>
              <div class="size-ctl">
                <input
                  class="select size-inp"
                  type="number"
                  min="1"
                  max="10"
                  :value="ui.mcpMaxDepth"
                  @input="ui.mcpMaxDepth = clampRange(val($event), 1, 10, 3)"
                />
                <span class="size-unit">levels</span>
              </div>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Remote access (HTTP/WS)</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Enable HTTP/WebSocket server</span>
                <span class="field-desc">Loopback-only, token-authed transport for the dispatch router. Use <code>tailscale serve</code> to reach it remotely — never exposes 0.0.0.0. Takes effect on next app restart.</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="httpEnabled" @change="onToggleHttp(($event.target as HTMLInputElement).checked)" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div v-if="httpStatus" class="field">
              <div class="field-info">
                <span class="field-name">Status</span>
                <span class="field-desc">
                  <template v-if="httpStatus.enabled">
                    Port <code>{{ httpStatus.port }}</code> — restart to bind.<br />
                    Token: <code>{{ httpStatus.token }}</code>
                    <button class="copy-btn" type="button" @click="copyToClipboard(httpStatus.token, 'token')">
                      {{ copiedLabel === 'token' ? 'Copied' : 'Copy' }}
                    </button>
                  </template>
                  <template v-else>Disabled</template>
                </span>
              </div>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Tailscale tunnel</span>
                <span class="field-desc">
                  <template v-if="!tailscaleStatus?.installed">Install the Tailscale app to enable remote tunneling.</template>
                  <template v-else-if="!tailscaleStatus.logged_in">Log in to Tailscale to enable remote tunneling.</template>
                  <template v-else-if="!httpEnabled">Enable the HTTP/WS server above first.</template>
                  <template v-else>Proxies the loopback server onto your tailnet via <code>tailscale serve</code> (never public internet).</template>
                  <template v-if="tailscaleStatus?.serving">
                    <br />URL: <code>{{ tailscaleStatus.serve_url }}</code>
                    <button class="copy-btn" type="button" @click="copyToClipboard(tailscaleStatus.serve_url ?? '', 'url')">
                      {{ copiedLabel === 'url' ? 'Copied' : 'Copy' }}
                    </button>
                    <br />Open this on your phone, paste the token above.
                  </template>
                </span>
              </div>
              <label
                class="toggle"
                :title="!httpEnabled ? 'Enable the HTTP/WS server first' : (!tailscaleStatus?.installed ? 'Tailscale not installed' : (!tailscaleStatus?.logged_in ? 'Not logged in to Tailscale' : ''))"
              >
                <input
                  type="checkbox"
                  :checked="tailscaleStatus?.serving ?? false"
                  :disabled="!httpEnabled || !tailscaleStatus?.installed || !tailscaleStatus?.logged_in"
                  @change="onToggleTailscale(($event.target as HTMLInputElement).checked)"
                />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Chat</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Default agent</span>
                <span class="field-desc">Agent used when opening a new chat.</span>
              </div>
              <select
                class="select"
                :value="ui.defaultChatAgent"
                @change="ui.defaultChatAgent = ($event.target as HTMLSelectElement).value"
              >
                <option v-for="a in chatAgents.agents" :key="a.id" :value="a.id">
                  {{ a.name }} ({{ a.transport === 'acp' ? 'ACP' : 'native' }})
                </option>
              </select>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Spawn sub-agents as</span>
                <span class="field-desc">How <code>burrow spawn</code> sub-agents open. Chat prefills the task in a new chat's input; Terminal keeps <code>burrow wait</code> result capture.</span>
              </div>
              <select
                class="select"
                :value="ui.spawnMode"
                @change="ui.spawnMode = ($event.target as HTMLSelectElement).value as 'terminal' | 'chat'"
              >
                <option value="terminal">Terminal tab</option>
                <option value="chat">Chat</option>
              </select>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Chat agents</span>
                <span class="field-desc">Add / edit ACP agents — command, args, environment variables, icon &amp; color.</span>
              </div>
              <button class="btn" @click="agentConfigOpen = true">Configure agents…</button>
            </div>
          </div>
          <ChatAgentConfig v-if="agentConfigOpen" @close="agentConfigOpen = false" />

          <div class="settings-group">
            <span class="group-label">Floating windows</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Snap corner</span>
                <span class="field-desc">Which screen corner popped-out terminal bubbles snap to and stack at</span>
              </div>
              <select
                class="select"
                :value="ui.floatCorner"
                @change="ui.floatCorner = ($event.target as HTMLSelectElement).value"
              >
                <option value="top-right">Top right</option>
                <option value="top-left">Top left</option>
                <option value="bottom-right">Bottom right</option>
                <option value="bottom-left">Bottom left</option>
              </select>
            </div>
          </div>

          <div class="sec-foot">
            <button class="reset-btn" @click="store.reset()">
              <PhArrowCounterClockwise :size="12" /> Reset to defaults
            </button>
          </div>
        </section>

        <!-- Scripts -->
        <section v-else-if="active === 'scripts'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Scripts</h2>
              <span class="sec-sub">Named, multi-step commands run sequentially in a new terminal tab</span>
            </div>
          </div>
          <div class="sec-divider" />

          <p class="profile-help">
            A script is an ordered list of steps. They run one after another —
            with <strong>&amp;&amp;</strong> so each step starts only if the previous
            <em>succeeded</em>, or with <strong>;</strong> (Continue on error) so every
            step runs regardless. Launch from the toolbar's <strong>Scripts</strong> menu
            or by name in <strong>⌘P</strong>.
          </p>

          <!-- Active repo scripts -->
          <div class="settings-group">
            <div class="scripts-group-head">
              <span class="group-label">
                {{ activeWsId != null ? `This repo — ${activeWsName}` : "This repo" }}
              </span>
              <button class="add-btn" :disabled="activeWsPath == null" @click="activeWsPath != null && scriptsStore.addScript(activeWsPath)">
                <PhPlus :size="11" /> Add Script
              </button>
            </div>
            <p v-if="activeWsPath == null" class="cfg-hint">Open a workspace to add repo-specific scripts.</p>

            <div
              v-for="s in (activeWsPath != null ? scriptsStore.scriptsFor(activeWsPath) : [])"
              :key="s.id"
              class="script-card"
            >
              <div class="sc-head">
                <label class="dot-label" title="Pick color">
                  <span class="dot" :style="{ background: s.color || '#34d399' }" />
                  <input type="color" class="color-input" :value="s.color || '#34d399'" @input="s.color = val($event)" />
                </label>
                <input class="inp sc-name" :value="s.name" placeholder="Script name" @input="s.name = val($event)" />
                <label class="sc-toggle" title="Continue on error — chain steps with ; instead of &&">
                  <input type="checkbox" :checked="s.continueOnError" @change="s.continueOnError = ($event.target as HTMLInputElement).checked" />
                  <span class="toggle-track"><span class="toggle-thumb" /></span>
                  <span class="sc-toggle-label">Continue on error</span>
                </label>
                <span class="spacer" />
                <button class="row-del" title="Remove script" @click="activeWsPath != null && scriptsStore.removeScript(activeWsPath, s.id)">
                  <PhTrash :size="13" />
                </button>
              </div>
              <div class="sc-steps">
                <div v-for="(_, i) in s.steps" :key="i" class="sc-step">
                  <span class="sc-step-idx">{{ i + 1 }}</span>
                  <input class="inp mono sc-step-inp" :value="s.steps[i]" placeholder="npm install" @input="setStep(s, i, val($event))" />
                  <button class="sc-step-btn" title="Move up" :disabled="i === 0" @click="moveStep(s, i, i - 1)"><PhArrowUp :size="12" /></button>
                  <button class="sc-step-btn" title="Move down" :disabled="i === s.steps.length - 1" @click="moveStep(s, i, i + 1)"><PhArrowDown :size="12" /></button>
                  <button class="sc-step-btn del" title="Remove step" @click="removeStep(s, i)"><PhX :size="12" /></button>
                </div>
                <button class="add-btn sc-add-step" @click="addStep(s)"><PhPlus :size="11" /> Add step</button>
              </div>
              <div class="sc-preview"><span class="sc-preview-label">Runs:</span> <code>{{ scriptPreview(s) }}</code></div>
            </div>
          </div>

        </section>

        <!-- Claude Profiles -->
        <section v-else-if="active === 'profiles'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Claude Profiles</h2>
              <span class="sec-sub">Launch Mission Control tasks with a different config dir, binary, or flags</span>
            </div>
            <div class="add-area">
              <button class="add-btn" @click="profilesStore.add()">
                <PhPlus :size="11" /> Add Profile
              </button>
            </div>
          </div>
          <div class="sec-divider" />

          <p class="profile-help">
            A profile sets <code>CLAUDE_CONFIG_DIR</code> (a separate Claude account / settings / session
            store), the binary to run, and extra flags. Pick one per task in Mission Control's composer.
          </p>

          <div class="profiles-list">
            <div v-for="p in profilesStore.profiles" :key="p.id" class="profile-card">
              <div class="pc-head">
                <PhUserGear :size="14" class="pc-ico" />
                <input
                  class="inp pc-name"
                  :value="p.name"
                  placeholder="Profile name"
                  :disabled="p.id === DEFAULT_PROFILE_ID"
                  @input="profilesStore.update(p.id, { name: val($event) })"
                />
                <span v-if="p.id === DEFAULT_PROFILE_ID" class="pc-badge">built-in</span>
                <span class="spacer" />
                <button
                  v-if="p.id !== DEFAULT_PROFILE_ID"
                  class="pc-del"
                  title="Delete profile"
                  @click="profilesStore.remove(p.id)"
                ><PhTrash :size="13" /></button>
              </div>
              <div class="pc-grid">
                <label class="pc-field">
                  <span>Command</span>
                  <input
                    class="inp mono"
                    :value="p.command"
                    placeholder="claude"
                    @input="profilesStore.update(p.id, { command: val($event) })"
                  />
                </label>
                <label class="pc-field">
                  <span>Extra flags</span>
                  <input
                    class="inp mono"
                    :value="p.args"
                    placeholder="--model haiku"
                    @input="profilesStore.update(p.id, { args: val($event) })"
                  />
                </label>
                <label class="pc-field pc-field-wide">
                  <span>Config dir <em>(CLAUDE_CONFIG_DIR — blank = default)</em></span>
                  <div class="pc-dir">
                    <input
                      class="inp mono"
                      :value="p.configDir"
                      placeholder="~/.claude-work"
                      @input="profilesStore.update(p.id, { configDir: val($event) })"
                    />
                    <button class="pc-browse" title="Browse…" @click="pickProfileConfigDir(p.id)">
                      <PhFolderOpen :size="13" />
                    </button>
                  </div>
                </label>
                <label class="pc-field pc-field-wide pc-org-row">
                  <input
                    type="checkbox"
                    :checked="p.orgAccount"
                    @change="profilesStore.update(p.id, { orgAccount: ($event.target as HTMLInputElement).checked })"
                  />
                  <span>Org / team account <em>(skips OAuth usage API, reads local transcripts instead)</em></span>
                </label>
              </div>
            </div>
          </div>
        </section>

        <!-- General -->
        <section v-else-if="active === 'general'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">General</h2>
              <span class="sec-sub">Fonts &amp; appearance</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">Interface</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">UI font</span>
                <span class="field-desc">Font used across the app interface</span>
              </div>
              <select
                class="select"
                :value="ui.uiFont"
                :style="{ fontFamily: ui.uiFont }"
                @change="ui.uiFont = val($event)"
              >
                <option v-for="f in UI_FONTS" :key="f.value" :value="f.value" :style="{ fontFamily: f.value }">
                  {{ f.label }}
                </option>
              </select>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">UI font size</span>
                <span class="field-desc">Base interface text size (10–20)</span>
              </div>
              <div class="size-ctl">
                <input
                  class="select size-inp"
                  type="number"
                  min="10"
                  max="20"
                  :value="ui.uiFontSize"
                  @input="ui.uiFontSize = clampRange(val($event), 10, 20, 13)"
                />
                <span class="size-unit">px</span>
              </div>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">UI scale</span>
                <span class="field-desc">Zoom the entire interface</span>
              </div>
              <select
                class="select"
                :value="String(ui.uiScale)"
                @change="ui.uiScale = Number(val($event))"
              >
                <option value="0.8">80%</option>
                <option value="0.9">90%</option>
                <option value="1">100%</option>
                <option value="1.1">110%</option>
                <option value="1.25">125%</option>
                <option value="1.5">150%</option>
              </select>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Terminal</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Terminal font</span>
                <span class="field-desc">Monospace font for terminal panes</span>
              </div>
              <select
                class="select"
                :value="ui.terminalFont"
                :style="{ fontFamily: ui.terminalFont }"
                @change="ui.terminalFont = val($event)"
              >
                <option v-for="f in TERMINAL_FONTS" :key="f.value" :value="f.value" :style="{ fontFamily: f.value }">
                  {{ f.label }}
                </option>
              </select>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Terminal font size</span>
                <span class="field-desc">Size in pixels (8–24)</span>
              </div>
              <div class="size-ctl">
                <input
                  class="select size-inp"
                  type="number"
                  min="8"
                  max="24"
                  :value="ui.terminalFontSize"
                  @input="ui.terminalFontSize = clampRange(val($event), 8, 24, 13)"
                />
                <span class="size-unit">px</span>
              </div>
            </div>
            <div class="term-preview" :style="{ fontFamily: ui.terminalFont, fontSize: ui.terminalFontSize + 'px' }">
              <span class="tp-prompt">~/agentic-ide $</span> claude --resume
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Layout</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Swap panel sides</span>
                <span class="field-desc">Move primary panel to the right</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.swapPanels" @change="ui.swapPanels = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Developer</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Terminal debug overlay</span>
                <span class="field-desc">Show per-terminal diagnostics (size, bytes, buffer)</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.debugOverlay" @change="ui.debugOverlay = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
          </div>

          <div class="sec-foot">
            <button class="reset-btn" @click="ui.resetFonts()">
              <PhArrowCounterClockwise :size="12" /> Reset fonts
            </button>
          </div>
        </section>

        <!-- Notifications -->
        <section v-else-if="active === 'notifications'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Notifications</h2>
              <span class="sec-sub">Sounds for agent activity</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">Toasts</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Position</span>
                <span class="field-desc">Where on-screen toast notifications appear</span>
              </div>
              <select
                class="select"
                :value="ui.toastPosition"
                @change="ui.toastPosition = ($event.target as HTMLSelectElement).value as ToastPosition"
              >
                <option v-for="p in TOAST_POSITIONS" :key="p.id" :value="p.id">{{ p.label }}</option>
              </select>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">General</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Enable sounds</span>
                <span class="field-desc">Master switch for all notification sounds</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.soundEnabled" @change="ui.soundEnabled = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Volume</span>
                <span class="field-desc">Playback volume ({{ ui.soundVolume }}%)</span>
              </div>
              <input
                class="vol-range"
                type="range"
                min="0"
                max="100"
                :value="ui.soundVolume"
                @input="ui.soundVolume = clampRange(val($event), 0, 100, 70)"
              />
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Agent finished</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Play when an agent finishes while you're away</span>
                <span class="field-desc">Fires on the "review" state (another tab/window)</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.soundDoneEnabled" @change="ui.soundDoneEnabled = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Sound</span>
                <span class="field-desc">Choose a built-in sound or a custom file</span>
              </div>
              <div class="sound-ctl">
                <select class="select" :value="ui.soundDoneId" @change="ui.soundDoneId = val($event)">
                  <option v-for="s in soundsForKind('done')" :key="s.id" :value="s.id">{{ s.label }}</option>
                  <option value="custom">Custom file…</option>
                </select>
                <button class="icon-btn" title="Test" @click="playSound('done', true)"><PhPlay :size="13" /></button>
              </div>
            </div>
            <div v-if="ui.soundDoneId === 'custom'" class="field">
              <div class="field-info">
                <span class="field-name">Custom file</span>
                <span class="field-desc">{{ ui.soundDoneCustomPath ? soundFileName(ui.soundDoneCustomPath) : "No file selected" }}</span>
              </div>
              <button class="reset-btn" @click="pickSound('done')"><PhFolderOpen :size="12" /> Choose…</button>
            </div>
          </div>

          <div class="settings-group">
            <span class="group-label">Needs input</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Play when an agent is waiting for your input</span>
                <span class="field-desc">Fires on the "waiting" state</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.soundWaitingEnabled" @change="ui.soundWaitingEnabled = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Sound</span>
                <span class="field-desc">Choose a built-in sound or a custom file</span>
              </div>
              <div class="sound-ctl">
                <select class="select" :value="ui.soundWaitingId" @change="ui.soundWaitingId = val($event)">
                  <option v-for="s in soundsForKind('waiting')" :key="s.id" :value="s.id">{{ s.label }}</option>
                  <option value="custom">Custom file…</option>
                </select>
                <button class="icon-btn" title="Test" @click="playSound('waiting', true)"><PhPlay :size="13" /></button>
              </div>
            </div>
            <div v-if="ui.soundWaitingId === 'custom'" class="field">
              <div class="field-info">
                <span class="field-name">Custom file</span>
                <span class="field-desc">{{ ui.soundWaitingCustomPath ? soundFileName(ui.soundWaitingCustomPath) : "No file selected" }}</span>
              </div>
              <button class="reset-btn" @click="pickSound('waiting')"><PhFolderOpen :size="12" /> Choose…</button>
            </div>
          </div>
        </section>

        <!-- Integrations -->
        <section v-else-if="active === 'integrations'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Integrations</h2>
              <span class="sec-sub">Send agent status to external services</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">ntfy.sh — push notifications</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Enable ntfy</span>
                <span class="field-desc">Push agent events to your phone/desktop via <a href="https://ntfy.sh" target="_blank" rel="noopener">ntfy.sh</a></span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.ntfyEnabled" @change="ui.ntfyEnabled = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>

            <template v-if="ui.ntfyEnabled">
              <div class="field">
                <div class="field-info">
                  <span class="field-name">Server</span>
                  <span class="field-desc">Base URL of your ntfy server</span>
                </div>
                <input v-model="ui.ntfyServer" class="select cfg-inp" placeholder="https://ntfy.sh" spellcheck="false" />
              </div>
              <div class="field">
                <div class="field-info">
                  <span class="field-name">Topic</span>
                  <span class="field-desc">Subscribe to this topic in the ntfy app to receive pushes</span>
                </div>
                <input v-model="ui.ntfyTopic" class="select cfg-inp" placeholder="my-burrow-agents" spellcheck="false" />
              </div>
              <div class="field">
                <div class="field-info">
                  <span class="field-name">Access token</span>
                  <span class="field-desc">Optional — only for protected topics (Bearer token)</span>
                </div>
                <input v-model="ui.ntfyToken" type="password" class="select cfg-inp" placeholder="tk_…" spellcheck="false" autocomplete="off" />
              </div>
            </template>
          </div>

          <div v-if="ui.ntfyEnabled" class="settings-group">
            <span class="group-label">Notify on</span>
            <div v-for="ev in NTFY_EVENTS" :key="ev.id" class="field">
              <div class="field-info">
                <span class="field-name">{{ ev.label }}</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.ntfyEvents.includes(ev.id)" @change="toggleNtfyEvent(ev.id, ($event.target as HTMLInputElement).checked)" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Only when away</span>
                <span class="field-desc">Skip pushes while the Burrow window is focused</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.ntfyOnlyWhenAway" @change="ui.ntfyOnlyWhenAway = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>
            <div class="sec-foot">
              <span v-if="ntfyTestMsg" class="ntfy-test-msg" :class="{ err: ntfyTestErr }">{{ ntfyTestMsg }}</span>
              <button class="reset-btn" :disabled="!ui.ntfyTopic || ntfyTesting" @click="sendNtfyTest">
                <PhPaperPlaneTilt :size="12" /> {{ ntfyTesting ? "Sending…" : "Send test" }}
              </button>
            </div>
          </div>
        </section>

        <!-- Plugins -->
        <section v-else-if="active === 'plugins'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Plugins</h2>
              <span class="sec-sub">Optional fun + experimental add-ons</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">🐾 Terminal Pets</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Enable pets</span>
                <span class="field-desc">A mixed pixel zoo — cat, mole, slime, ghost, duck — roams the bottom of the window. One critter per active agent; it struts while the agent works, bounces when it needs input, hops when a turn finishes, and shakes red on error.</span>
              </div>
              <label class="toggle">
                <input type="checkbox" :checked="ui.petsEnabled" @change="ui.petsEnabled = ($event.target as HTMLInputElement).checked" />
                <span class="toggle-track"><span class="toggle-thumb" /></span>
              </label>
            </div>

            <template v-if="ui.petsEnabled">
              <div class="field">
                <div class="field-info">
                  <span class="field-name">Speech bubbles</span>
                  <span class="field-desc">Pets squeak tiny status quips — “working…”, “need input!”, “done!”</span>
                </div>
                <label class="toggle">
                  <input type="checkbox" :checked="ui.petsSpeech" @change="ui.petsSpeech = ($event.target as HTMLInputElement).checked" />
                  <span class="toggle-track"><span class="toggle-thumb" /></span>
                </label>
              </div>
              <div class="field">
                <div class="field-info">
                  <span class="field-name">Leveling &amp; crowns</span>
                  <span class="field-desc">Pets level up as their agent finishes turns and earn a ♛ crown once they hit veteran status.</span>
                </div>
                <label class="toggle">
                  <input type="checkbox" :checked="ui.petsLeveling" @change="ui.petsLeveling = ($event.target as HTMLInputElement).checked" />
                  <span class="toggle-track"><span class="toggle-thumb" /></span>
                </label>
              </div>
              <div class="sec-foot">
                <span class="sec-sub">Tip: click a pet to give it a poke.</span>
              </div>
            </template>
          </div>
        </section>

        <!-- Keybindings -->
        <section v-else-if="active === 'keybindings'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Keyboard Shortcuts</h2>
              <span class="sec-sub">Read-only reference — all app shortcuts</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div v-for="group in SHORTCUT_GROUPS" :key="group.label" class="settings-group">
            <span class="group-label">{{ group.label }}</span>
            <div v-for="s in group.shortcuts" :key="s.keys" class="kb-row">
              <span class="kb-desc">{{ s.desc }}</span>
              <span class="kb-keys">
                <kbd v-for="k in s.keys.split(' ')" :key="k" class="kb-key">{{ k }}</kbd>
              </span>
            </div>
          </div>
        </section>

        <!-- Workspaces -->
        <section v-else-if="active === 'workspaces'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Workspaces</h2>
              <span class="sec-sub">Customize project icons</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">Worktrees directory</span>
            <div class="field">
              <div class="field-info">
                <span class="field-name">Where new git worktrees are created</span>
                <span class="field-desc">Worktrees land at &lt;dir&gt;/&lt;repo&gt;/&lt;branch&gt;</span>
              </div>
              <div class="wt-dir-ctl">
                <input
                  class="select wt-dir-input"
                  :value="ui.worktreesDir"
                  @input="ui.worktreesDir = ($event.target as HTMLInputElement).value"
                  spellcheck="false"
                />
                <button class="reset-btn" @click="pickWorktreesDir"><PhFolderOpen :size="12" /> Browse…</button>
              </div>
            </div>
          </div>

          <div class="ws-list">
            <div v-for="w in wsStore.workspaces" :key="w.id" class="ws-row">
              <button class="ws-icon-btn" title="Change icon" @click="pickWsIcon(w.id)">
                <img v-if="wsStore.icons[w.id]" :src="wsStore.icons[w.id]" class="ws-icon-img" />
                <PhFolder v-else :size="18" weight="fill" class="ws-icon-fb" />
                <span class="ws-icon-edit"><PhPencilSimple :size="10" /></span>
              </button>
              <div class="ws-meta">
                <span class="ws-name">{{ w.name }}</span>
                <span class="ws-path">{{ w.path }}</span>
              </div>
              <button
                v-if="wsStore.icons[w.id]"
                class="ws-clear"
                title="Reset to default icon"
                @click="wsStore.clearIcon(w.id)"
              >
                <PhArrowCounterClockwise :size="13" />
              </button>
            </div>

            <div v-if="wsStore.workspaces.length === 0" class="tbl-empty">
              No workspaces yet. Open a folder first.
            </div>
          </div>
        </section>

        <!-- Appearance -->
        <section v-else-if="active === 'appearance'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Appearance</h2>
              <span class="sec-sub">Color theme</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <span class="group-label">Theme</span>
            <div class="theme-grid">
              <button
                v-for="t in THEMES"
                :key="t.key"
                class="theme-card"
                :class="{ selected: ui.theme === t.key }"
                @click="ui.setTheme(t.key)"
              >
                <div
                  class="theme-swatch"
                  :style="{ background: t.vars['bg-base'], borderColor: t.vars.border }"
                >
                  <div class="sw-panel" :style="{ background: t.vars['bg-panel'] }">
                    <span class="sw-line" :style="{ background: t.vars['text-primary'] }" />
                    <span class="sw-line short" :style="{ background: t.vars['text-secondary'] }" />
                  </div>
                  <div class="sw-dots">
                    <span :style="{ background: t.vars.accent }" />
                    <span :style="{ background: t.vars.green }" />
                    <span :style="{ background: t.vars.yellow }" />
                    <span :style="{ background: t.vars.red }" />
                  </div>
                </div>
                <div class="theme-card-foot">
                  <span class="theme-name">{{ t.label }}</span>
                  <PhCheck v-if="ui.theme === t.key" :size="13" class="theme-check" />
                </div>
              </button>
            </div>
          </div>

          <div class="sec-divider" />

          <!-- Background image -->
          <div class="settings-group bg-group">
            <span class="group-label">Background</span>

            <!-- Image picker card -->
            <div class="bg-card">
              <div
                class="bg-thumb"
                :class="{ 'is-empty': !ui.bgImageUrl }"
                :style="ui.bgImageUrl ? { backgroundImage: `url('${ui.bgImageUrl}')` } : {}"
                @click="pickBgImage"
              >
                <template v-if="!ui.bgImageUrl">
                  <PhImage :size="22" weight="thin" />
                  <span class="bg-thumb-hint">Click to choose</span>
                </template>
                <div v-else class="bg-thumb-overlay"><PhPencilSimple :size="16" weight="bold" /></div>
              </div>
              <div class="bg-card-body">
                <span class="bg-card-name">{{ ui.bgImagePath ? bgFileName(ui.bgImagePath) : "No background image" }}</span>
                <span class="bg-card-sub">{{ ui.bgImagePath ? "Shown behind the workspace" : "PNG, JPG or WebP" }}</span>
                <div class="bg-card-btns">
                  <button class="bg-btn bg-btn-primary" @click="pickBgImage">
                    {{ ui.bgImagePath ? "Replace…" : "Choose image…" }}
                  </button>
                  <button v-if="ui.bgImagePath" class="bg-btn bg-btn-clear" @click="ui.clearBgImage()">Remove</button>
                </div>
              </div>
            </div>

            <template v-if="ui.bgImagePath">
              <!-- Opacity -->
              <div class="bg-control">
                <div class="bg-control-head">
                  <span class="bg-control-name">Opacity</span>
                  <span class="bg-control-val">{{ Math.round(ui.bgOpacity * 100) }}%</span>
                </div>
                <input
                  type="range"
                  min="0.2"
                  max="1"
                  step="0.01"
                  :value="ui.bgOpacity"
                  class="bg-slider"
                  @input="ui.bgOpacity = parseFloat(($event.target as HTMLInputElement).value)"
                />
              </div>

              <!-- Backdrop blur -->
              <div class="bg-control bg-blur-block">
                <div class="bg-control-head">
                  <span class="bg-control-name">Backdrop blur</span>
                  <span class="bg-control-sub">Frosted-glass over the image</span>
                </div>
                <div class="blur-grid">
                  <div v-for="b in blurControls" :key="b.key" class="blur-row">
                    <span class="blur-name">{{ b.label }}</span>
                    <input
                      type="range"
                      min="0"
                      max="40"
                      step="1"
                      :value="(ui as any)[b.key]"
                      class="bg-slider"
                      @input="(ui as any)[b.key] = parseInt(($event.target as HTMLInputElement).value)"
                    />
                    <span class="blur-val">{{ (ui as any)[b.key] }}px</span>
                  </div>
                </div>
              </div>
            </template>
          </div>
        </section>

        <!-- About / Updates -->
        <section v-else-if="active === 'about'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">About</h2>
              <span class="sec-sub">Version &amp; updates</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="settings-group">
            <div class="about-id">
              <div class="about-logo"><PhTerminalWindow :size="26" weight="duotone" /></div>
              <div>
                <div class="about-name">Burrow</div>
                <div class="about-ver">Version {{ appVersion || "…" }}</div>
              </div>
            </div>

            <div class="update-box">
              <div class="update-box-row">
                <div class="update-box-text">
                  <template v-if="update.installed">
                    <span class="upd-strong">Update installed</span>
                    <span class="upd-dim">Restart to finish updating to v{{ update.newVersion }}.</span>
                  </template>
                  <template v-else-if="update.downloading">
                    <span class="upd-strong">Downloading v{{ update.newVersion }}…</span>
                    <span class="upd-dim">{{ update.progress >= 0 ? Math.round(update.progress * 100) + "%" : "…" }}</span>
                  </template>
                  <template v-else-if="update.available">
                    <span class="upd-strong">Update available — v{{ update.newVersion }}</span>
                    <span class="upd-dim">You have v{{ update.currentVersion }}.</span>
                  </template>
                  <template v-else>
                    <span class="upd-strong">You're up to date</span>
                    <span class="upd-dim">Last checked {{ lastCheckedLabel }}.</span>
                  </template>
                </div>

                <div class="update-box-actions">
                  <button v-if="update.installed" class="reset-btn primary" @click="update.relaunch()">
                    <PhArrowClockwise :size="12" /> Restart now
                  </button>
                  <button
                    v-else-if="update.available && !update.downloading"
                    class="reset-btn primary"
                    @click="update.downloadAndInstall()"
                  >
                    <PhDownloadSimple :size="12" /> Install v{{ update.newVersion }}
                  </button>
                  <button
                    v-else-if="!update.downloading"
                    class="reset-btn"
                    :disabled="update.checking"
                    @click="update.check()"
                  >
                    <PhArrowClockwise :size="12" :class="{ spin: update.checking }" />
                    {{ update.checking ? "Checking…" : "Check for updates" }}
                  </button>
                </div>
              </div>

              <div v-if="update.notes && update.available && !update.installed" class="update-box-notes">
                {{ update.notes }}
              </div>
              <div v-if="update.error && !update.checking" class="update-box-err">
                Update check failed: {{ update.error }}
              </div>
            </div>

            <div class="update-box">
              <div class="update-box-row">
                <div class="update-box-text">
                  <span class="upd-strong">Agent status hooks</span>
                  <span class="upd-dim">
                    Fix agent status dots if they get stuck or stop updating —
                    re-points revived sessions at the live server and reinstalls
                    the global hooks.
                  </span>
                </div>
                <div class="update-box-actions">
                  <button class="reset-btn" :disabled="repairing" @click="repairAgentStatus">
                    <PhArrowClockwise :size="12" :class="{ spin: repairing }" />
                    {{ repairing ? "Repairing…" : "Fix agent status" }}
                  </button>
                </div>
              </div>
              <div v-if="repairMsg" class="update-box-notes">{{ repairMsg }}</div>
            </div>
          </div>
        </section>

        <!-- Skills -->
        <section v-else-if="active === 'skills'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Skills</h2>
              <span class="sec-sub">Agent skills installed in <code>~/.claude/skills</code></span>
            </div>
            <button class="add-btn" :disabled="skillsLoading" @click="loadSkills">
              <PhArrowClockwise :size="11" :class="{ spin: skillsLoading }" /> Refresh
            </button>
          </div>
          <div class="sec-divider" />

          <div class="ext-list">
            <div v-for="s in skills" :key="s.dir" class="ext-row" :class="{ off: !s.enabled }">
              <div class="ext-icon"><PhSparkle :size="15" /></div>
              <div class="ext-main">
                <div class="ext-name">{{ s.name }}</div>
                <div class="ext-desc">{{ s.description || "No description" }}</div>
              </div>
              <div class="ext-actions">
                <button
                  class="icon-act"
                  :title="s.enabled ? 'Disable skill' : 'Enable skill'"
                  @click="toggleSkill(s)"
                >
                  <component :is="s.enabled ? PhToggleRight : PhToggleLeft" :size="20"
                    :style="{ color: s.enabled ? 'var(--green)' : 'var(--text-secondary)' }" />
                </button>
                <button class="icon-act" title="Reveal in Finder" @click="revealSkill(s)">
                  <PhArrowSquareOut :size="14" />
                </button>
                <button class="icon-act danger" title="Delete skill" @click="deleteSkill(s)">
                  <PhTrash :size="14" />
                </button>
              </div>
            </div>
            <div v-if="!skillsLoading && skills.length === 0" class="tbl-empty">
              No skills found. Install one via Claude Code or the skill marketplace.
            </div>
          </div>
        </section>

        <!-- MCP Servers -->
        <section v-else-if="active === 'mcp'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">MCP Servers</h2>
              <span class="sec-sub">Model Context Protocol servers in <code>~/.claude.json</code></span>
            </div>
            <button class="add-btn" @click="startAddMcp">
              <PhPlus :size="11" /> Add Server
            </button>
          </div>
          <div class="sec-divider" />

          <!-- Add / edit form -->
          <div v-if="mcpFormOpen" class="mcp-form">
            <div class="mcp-form-head">
              <span class="group-label">{{ mcpEditName ? "Edit" : "New" }} MCP server</span>
              <button class="fp-close" @click="mcpFormOpen = false"><PhX :size="12" /></button>
            </div>
            <input
              class="inp mcp-name"
              v-model="mcpName"
              :disabled="!!mcpEditName"
              placeholder="server-name"
              spellcheck="false"
            />
            <textarea
              class="fp-area mono mcp-config"
              v-model="mcpConfig"
              rows="7"
              spellcheck="false"
              placeholder='{ "command": "npx", "args": ["-y", "@some/mcp-server"] }'
            />
            <div class="mcp-form-foot">
              <span v-if="mcpError" class="mcp-err">{{ mcpError }}</span>
              <span class="s-spacer" />
              <button class="reset-btn" @click="mcpFormOpen = false">Cancel</button>
              <button class="reset-btn primary" :disabled="mcpSaving" @click="saveMcp">
                {{ mcpSaving ? "Saving…" : "Save" }}
              </button>
            </div>
          </div>

          <div class="ext-list">
            <div v-for="m in mcpServers" :key="m.name" class="ext-row">
              <div class="ext-icon"><PhPlugsConnected :size="15" /></div>
              <div class="ext-main">
                <div class="ext-name">{{ m.name }}</div>
                <pre class="ext-config mono">{{ m.config }}</pre>
              </div>
              <div class="ext-actions">
                <button class="icon-act" title="Edit" @click="editMcp(m)">
                  <PhPencilSimple :size="14" />
                </button>
                <button class="icon-act danger" title="Remove server" @click="removeMcp(m)">
                  <PhTrash :size="14" />
                </button>
              </div>
            </div>
            <div v-if="mcpServers.length === 0 && !mcpFormOpen" class="tbl-empty">
              No MCP servers configured. Add one to give every Claude session new tools.
            </div>
          </div>
        </section>

        <!-- Extensions (browser — planned) -->
        <section v-else-if="active === 'extensions'" class="section">
          <div class="sec-head">
            <div class="sec-titles">
              <h2 class="sec-title">Extensions</h2>
              <span class="sec-sub">Browser &amp; editor integrations</span>
            </div>
          </div>
          <div class="sec-divider" />

          <div class="ext-list">
            <div class="ext-row planned">
              <div class="ext-icon"><PhBrowser :size="15" /></div>
              <div class="ext-main">
                <div class="ext-name">
                  Browser extension
                  <span class="planned-badge">Planned</span>
                </div>
                <div class="ext-desc">
                  Drive a real browser tab from inside Burrow — let agents open pages,
                  fill forms, and read the DOM without leaving the IDE.
                </div>
              </div>
            </div>
          </div>
        </section>

        <!-- Other panels (placeholder) -->
        <section v-else class="section placeholder">
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
  PhPuzzlePiece, PhInfo, PhSparkle, PhCode, PhGitBranch, PhTerminal,
  PhListBullets, PhCaretDown, PhFolder, PhPencilSimple, PhCheck, PhBell, PhPlay,
  PhDotsSixVertical, PhArrowClockwise, PhDownloadSimple, PhTerminalWindow,
  PhPlugsConnected, PhBrowser, PhToggleLeft, PhToggleRight, PhArrowSquareOut, PhImage,
  PhUserGear, PhPaperPlaneTilt, PhPawPrint, PhPlayCircle, PhArrowUp, PhArrowDown,
} from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import OpenAIIcon from "@/components/icons/OpenAIIcon.vue";
import GitHubCopilotIcon from "@/components/icons/GitHubCopilotIcon.vue";
import { useAgentsStore, type AgentIcon } from "@/stores/agents";
import { useScriptsStore, type Script } from "@/stores/scripts";
import { useProfilesStore, DEFAULT_PROFILE_ID } from "@/stores/profiles";
import { useWorkspaceStore } from "@/stores/workspace";
import { useUIStore, UI_FONTS, TERMINAL_FONTS, NTFY_EVENTS, TOAST_POSITIONS, type NtfyEvent, type ToastPosition } from "@/stores/ui";
import { useChatAgentsStore } from "@/stores/chatAgents";
import ChatAgentConfig from "@/components/ChatAgentConfig.vue";
import { testNtfy } from "@/lib/ntfy";
import { useUpdateStore } from "@/stores/update";
import { THEMES } from "@/themes";
import { soundsForKind, playSound, type SoundKind } from "@/lib/sounds";
import { usePointerReorder } from "@/composables/usePointerReorder";
import { eventToShortcut } from "@/lib/shortcuts";

defineEmits<{ close: [] }>();

const store = useAgentsStore();
const scriptsStore = useScriptsStore();
const profilesStore = useProfilesStore();
async function pickProfileConfigDir(id: string) {
  try {
    const dir = await openDialog({ directory: true, multiple: false });
    if (typeof dir === "string") profilesStore.update(id, { configDir: dir });
  } catch { /* dialog cancelled */ }
}
const wsStore = useWorkspaceStore();
const ui = useUIStore();
const chatAgents = useChatAgentsStore();
const agentConfigOpen = ref(false);
const update = useUpdateStore();

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
const repairMsg = ref("");
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
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: "Image", extensions: ["png", "jpg", "jpeg", "svg", "ico"] }],
  });
  if (!selected || typeof selected !== "string") return;
  const b64 = await invoke<string>("read_file_base64", { path: selected });
  wsStore.setIcon(id, `data:${mimeForPath(selected)};base64,${b64}`);
}

async function pickWorktreesDir() {
  const selected = await openDialog({ directory: true, multiple: false });
  if (typeof selected === "string") ui.worktreesDir = selected;
}
// Notification sounds: choose a custom audio file for done/waiting and store its
// path; sounds.ts reads it lazily via read_file_base64.
async function pickSound(kind: SoundKind) {
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: "Audio", extensions: ["wav", "mp3", "ogg", "m4a", "aac", "flac"] }],
  });
  if (!selected || typeof selected !== "string") return;
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
  const selected = await openDialog({
    multiple: false,
    filters: [{ name: "Images", extensions: ["png", "jpg", "jpeg", "gif", "webp", "avif"] }],
  });
  if (selected && typeof selected === "string") {
    ui.bgImagePath = selected;
  }
}

function bgFileName(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

// Per-element backdrop-blur sliders (keys map to ui store refs).
const blurControls = [
  { key: "blurPanels", label: "Panels (sidebar, bars)" },
  { key: "blurContent", label: "Mission Control & Dashboard" },
  { key: "blurTerminal", label: "Terminal" },
  { key: "blurOverlay", label: "Overlays (spotlight, settings)" },
  { key: "blurDropdown", label: "Dropdowns (menus, notifications)" },
] as const;

const active = ref("general");
const flagEditId = ref<string | null>(null);

// Agent config dirs (where Burrow installs hooks + docs). Loaded from the backend,
// which seeds defaults (~/.claude, ~/.codex) + any CLAUDE_CONFIG_DIR/CODEX_HOME env.
const claudeDirs = ref<string[]>([]);
const codexDirs = ref<string[]>([]);
const copilotDirs = ref<string[]>([]);
const cfgSaving = ref(false);
const cfgStatus = ref("");

type CfgDirs = { claude: string[]; codex: string[]; copilot: string[] };

async function loadConfigDirs() {
  try {
    const cd = await invoke<CfgDirs>("get_config_dirs");
    claudeDirs.value = cd.claude;
    codexDirs.value = cd.codex;
    copilotDirs.value = cd.copilot ?? [];
  } catch (e) {
    console.error("get_config_dirs failed", e);
  }
}

async function saveConfigDirs() {
  cfgSaving.value = true;
  cfgStatus.value = "";
  try {
    const cd = await invoke<CfgDirs>("set_config_dirs", {
      claude: claudeDirs.value.map((s) => s.trim()).filter(Boolean),
      codex: codexDirs.value.map((s) => s.trim()).filter(Boolean),
      copilot: copilotDirs.value.map((s) => s.trim()).filter(Boolean),
    });
    claudeDirs.value = cd.claude;
    codexDirs.value = cd.codex;
    copilotDirs.value = cd.copilot;
    cfgStatus.value = `Installed into ${cd.claude.length + cd.codex.length + cd.copilot.length} dir(s).`;
  } catch (e) {
    cfgStatus.value = `Failed: ${e}`;
  } finally {
    cfgSaving.value = false;
  }
}

loadConfigDirs();

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

const flagDraft = ref("");
const iconPickerId = ref<string | null>(null);
const showTemplatePicker = ref(false);

// --- Reorder (pointer-based) ---
// HTML5 drag-and-drop is unreliable here: Tauri's WKWebView keeps its native
// drag-drop handler on (for terminal file drops), which swallows the webview's
// own `drop` events. Pointer events sidestep it. Drag the grip handle.
const {
  dragIdx: dragIndex,
  overIdx: dragOverIndex,
  down: onGripDown,
} = usePointerReorder((from, to) => store.move(from, to));

// --- Shortcut recorder ---
const recordingId = ref<string | null>(null);

function startRecording(id: string, e: MouseEvent) {
  recordingId.value = recordingId.value === id ? null : id;
  // WebKit doesn't focus a <button> on click, so its @keydown never fires.
  // Focus it explicitly so the recorder can capture the next key combo.
  if (recordingId.value === id) (e.currentTarget as HTMLElement)?.focus();
}

function onRecordKey(id: string, e: KeyboardEvent) {
  if (recordingId.value !== id) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") {
    recordingId.value = null;
    return;
  }
  const sc = eventToShortcut(e);
  if (!sc) return; // wait for a non-modifier key
  store.update(id, { shortcut: sc });
  recordingId.value = null;
}

function toggleIconPicker(id: string) {
  iconPickerId.value = iconPickerId.value === id ? null : id;
  flagEditId.value = null;
}

function addFromTemplate(t: typeof TEMPLATES[0]) {
  store.addFromTemplate(t);
}

function toggleFlagEditor(id: string) {
  if (flagEditId.value === id) {
    flagEditId.value = null;
    return;
  }
  const a = store.agents.find((x) => x.id === id);
  flagDraft.value = argsToLines(a?.args ?? "");
  flagEditId.value = id;
}

// Seed editor: split args on whitespace, one token per line.
function argsToLines(args: string): string {
  return args.trim().split(/\s+/).filter(Boolean).join("\n");
}
// Collapse lines back to a single args string (newlines + extra spaces -> single space).
function linesToArgs(text: string): string {
  return text.split("\n").map((l) => l.trim()).filter(Boolean).join(" ");
}

function onFlagInput(id: string, e: Event) {
  flagDraft.value = (e.target as HTMLTextAreaElement).value;
  store.update(id, { args: linesToArgs(flagDraft.value) });
}

const ICON_OPTIONS: { key: AgentIcon; label: string; component: unknown }[] = [
  { key: "claude",        label: "Claude",          component: ClaudeIcon },
  { key: "openai",        label: "OpenAI / Codex",  component: OpenAIIcon },
  { key: "github-copilot",label: "GitHub Copilot",  component: GitHubCopilotIcon },
  { key: "robot",         label: "Robot",            component: PhRobot },
  { key: "sparkle",       label: "Sparkle",          component: PhSparkle },
  { key: "code",          label: "Code",             component: PhCode },
  { key: "git-branch",    label: "Git branch",       component: PhGitBranch },
  { key: "terminal",      label: "Terminal",         component: PhTerminal },
];

interface TemplateConfig {
  id: string;
  name: string;
  command: string;
  args: string;
  color: string;
  icon: AgentIcon;
}

const TEMPLATES: TemplateConfig[] = [
  { id: "tpl-claude",      name: "Claude Code",     command: "claude",        args: "--dangerously-skip-permissions", color: "#d97757", icon: "claude" },
  { id: "tpl-claude-opus", name: "Claude (Opus)",   command: "claude",        args: "--model claude-opus-4-8 --dangerously-skip-permissions", color: "#f59e0b", icon: "claude" },
  { id: "tpl-codex",       name: "Codex",           command: "codex",         args: "",                              color: "#34d399", icon: "openai" },
  { id: "tpl-copilot",     name: "GitHub Copilot",  command: "copilot",       args: "",                              color: "#8957e5", icon: "github-copilot" },
  { id: "tpl-aider",       name: "Aider",           command: "aider",         args: "",                              color: "#fbbf24", icon: "robot" },
  { id: "tpl-gemini",      name: "Gemini CLI",      command: "gemini",        args: "",                              color: "#4285f4", icon: "sparkle" },
];

interface NavItem {
  id?: string;
  label?: string;
  icon?: Component;
  divider?: boolean;
}

const navItems: NavItem[] = [
  { id: "general", label: "General", icon: PhSlidersHorizontal },
  { id: "workspaces", label: "Workspaces", icon: PhFolderOpen },
  { id: "agents", label: "Agents", icon: PhRobot },
  { id: "scripts", label: "Scripts", icon: PhPlayCircle },
  { id: "profiles", label: "Claude Profiles", icon: PhUserGear },
  { id: "skills", label: "Skills", icon: PhSparkle },
  { id: "mcp", label: "MCP Servers", icon: PhPlugsConnected },
  { divider: true },
  { id: "appearance", label: "Appearance", icon: PhPalette },
  { id: "notifications", label: "Notifications", icon: PhBell },
  { id: "integrations", label: "Integrations", icon: PhPlugsConnected },
  { id: "keybindings", label: "Keybindings", icon: PhKeyboard },
  { id: "plugins", label: "Plugins", icon: PhPawPrint },
  { id: "extensions", label: "Extensions", icon: PhPuzzlePiece },
  { id: "about", label: "About", icon: PhInfo },
];

const iconMap = {
  sparkle: PhSparkle,
  code: PhCode,
  "git-branch": PhGitBranch,
  robot: PhRobot,
  terminal: PhTerminal,
  claude: ClaudeIcon,
  openai: OpenAIIcon,
  "github-copilot": GitHubCopilotIcon,
};
function iconFor(icon: AgentIcon) {
  return iconMap[icon] ?? PhRobot;
}

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
const httpStatus = ref<{ enabled: boolean; port: number; tokenPath: string; token: string } | null>(null);
async function refreshHttpStatus() {
  try {
    const s = await invoke<{ enabled: boolean; port: number; tokenPath: string; token: string }>("get_http_server_status");
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

const SHORTCUT_GROUPS = [
  {
    label: "Global",
    shortcuts: [
      { keys: "⌘ ,",   desc: "Open Settings" },
      { keys: "⌘ P",   desc: "Command Palette (Spotlight)" },
      { keys: "⌘ /",   desc: "Toggle Keyboard Shortcut Cheatsheet" },
      { keys: "Esc",   desc: "Close Settings / Cheatsheet" },
    ],
  },
  {
    label: "Tabs & Panes",
    shortcuts: [
      { keys: "⌘ T",   desc: "New tab" },
      { keys: "⌘ W",   desc: "Close pane" },
      { keys: "⌘ D",   desc: "Split pane horizontally" },
      { keys: "⌘ ⇧ D", desc: "Split pane vertically" },
    ],
  },
  {
    label: "Terminal Input",
    shortcuts: [
      { keys: "⇧ ↵",  desc: "Insert newline (Claude multiline input)" },
    ],
  },
  {
    label: "Agents",
    shortcuts: [
      { keys: "⌘ ⇧ 1",  desc: "Launch Claude Code" },
      { keys: "⌘ ⇧ 2",  desc: "Launch Codex" },
      { keys: "⌘ ⇧ 3",  desc: "Launch GitHub Copilot" },
      { keys: "⌘ ⇧ 4",  desc: "Launch Aider" },
      { keys: "⌘ ⇧ 5",  desc: "Launch Cursor AI" },
    ],
  },
  {
    label: "Spotlight",
    shortcuts: [
      { keys: "↑ ↓",  desc: "Navigate results" },
      { keys: "↵",    desc: "Activate selected item" },
      { keys: "⌘ ↵",  desc: "Open in new tab" },
      { keys: "Esc",  desc: "Close Spotlight" },
    ],
  },
];
</script>

<style scoped>
.settings-page {
  position: fixed;
  inset: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-base);
  backdrop-filter: var(--blur-overlay, none);
  -webkit-backdrop-filter: var(--blur-overlay, none);
  z-index: 1000;
}

/* Header */
.s-header {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  height: 52px;
  padding: 0 24px;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.s-head-title {
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 10px;
}
.s-head-icon { color: var(--text-muted); }
.s-title { font-size: 14px; font-weight: 600; color: var(--text-primary); }
.s-spacer { flex: 1; }
.s-close {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  display: flex;
  padding: 4px;
  border-radius: 4px;
}
.s-close:hover { color: var(--text-primary); background: var(--bg-hover); }

.s-body { display: flex; flex: 1; overflow: hidden; }

/* Nav */
.s-nav {
  width: 220px;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
  padding: 10px 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex-shrink: 0;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 34px;
  padding: 0 16px;
  background: none;
  border: none;
  border-left: 2px solid transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  text-align: left;
}
.nav-icon { color: var(--text-muted); flex-shrink: 0; }
.nav-item:hover { background: var(--bg-hover); }
.nav-item.active {
  background: var(--bg-hover);
  border-left-color: var(--accent);
  color: var(--text-primary);
}
.nav-item.active .nav-icon { color: var(--accent); }
.nav-divider { height: 1px; background: var(--border); margin: 8px 0; }

/* Content */
.s-content { flex: 1; overflow-y: auto; padding: 32px 40px; background: var(--bg-base); }

.section { display: flex; flex-direction: column; gap: 14px; }

.sec-head { display: flex; align-items: center; gap: 10px; }
.sec-titles { display: flex; align-items: baseline; gap: 10px; }
.sec-title { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.sec-sub { font-size: 12px; color: var(--text-muted); }
.sec-head .add-btn { margin-left: auto; }

.add-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  background: #161616;
  border: 1px solid #2a2a2a;
  border-radius: 5px;
  color: #999;
  cursor: pointer;
  font-size: 12px;
  padding: 5px 12px;
}
.add-btn:hover { color: #e2e2e2; border-color: #444; }

.sec-divider { height: 1px; background: var(--border); }

/* Table */
.tbl { display: flex; flex-direction: column; gap: 8px; }
.tbl-head {
  display: flex;
  align-items: center;
  height: 26px;
  padding: 0 16px;
}
.col { font-size: 11px; font-weight: 500; color: #3a3a3a; }
.col-grip { width: 22px; flex-shrink: 0; display: flex; justify-content: center; }
.col-agent { width: 200px; flex-shrink: 0; }
.col-cmd { width: 165px; flex-shrink: 0; }
.col-args { flex: 1; min-width: 0; }
.col-kbd { width: 84px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; gap: 4px; }
.col-act { width: 52px; flex-shrink: 0; display: flex; justify-content: flex-end; }

.row {
  display: flex;
  align-items: center;
  height: 50px;
  padding: 0 16px;
  background: #0f0f0f;
  border: 1px solid #1e1e1e;
  border-radius: 6px;
}

.row.dragging { opacity: 0.4; }
.row.drag-over { border-color: var(--accent); box-shadow: inset 0 0 0 1px var(--accent); }

.grip {
  color: #3a3a3a;
  cursor: grab;
  align-items: center;
  touch-action: none;
}
.grip:hover { color: #888; }
.grip:active { cursor: grabbing; }

.cell-agent { display: flex; align-items: center; gap: 8px; }
.dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }

.icon-box {
  position: relative;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  border: 1px solid;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  cursor: pointer;
  overflow: hidden;
}
.color-input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
  border: none;
  padding: 0;
}

.inp {
  background: none;
  border: none;
  outline: none;
  color: #e2e2e2;
  font-size: 13px;
  font-family: var(--font-ui);
  width: 100%;
  min-width: 0;
}
.inp::placeholder { color: #3a3a3a; }
.inp.mono { font-family: var(--font-mono); font-size: 12px; }

/* Claude Profiles */
.profile-help { font-size: 12px; color: var(--text-muted); line-height: 1.5; margin: 0; }
.profile-help code { font-family: var(--font-mono); font-size: 11px; color: var(--text-secondary); background: var(--bg-hover); padding: 1px 5px; border-radius: 4px; }
.profiles-list { display: flex; flex-direction: column; gap: 12px; }
.profile-card { border: 1px solid var(--border); border-radius: 10px; padding: 12px 14px; background: var(--bg-panel); }
.pc-head { display: flex; align-items: center; gap: 9px; }
.pc-ico { color: var(--accent); flex: none; }
.pc-name { font-weight: 600; flex: 0 1 auto; width: auto; max-width: 220px; }
.pc-name:disabled { opacity: 0.7; }
.pc-badge { font-size: 10px; color: var(--text-muted); border: 1px solid var(--border); border-radius: 5px; padding: 1px 6px; }
.pc-del { background: none; border: none; color: var(--text-muted); cursor: pointer; padding: 4px; border-radius: 5px; }
.pc-del:hover { color: #f87171; background: var(--bg-hover); }
.pc-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 14px; margin-top: 12px; }
.pc-field { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
.pc-field > span { font-size: 11px; color: var(--text-secondary); }
.pc-field > span em { color: var(--text-muted); font-style: normal; }
.pc-field-wide { grid-column: 1 / -1; }
.pc-field .inp { border: 1px solid var(--border); border-radius: 7px; padding: 7px 9px; background: var(--bg-input, #00000022); }
.pc-field .inp:focus { border-color: var(--accent); }
.pc-dir { display: flex; gap: 6px; align-items: stretch; }
.pc-dir .inp { flex: 1; }
.pc-browse { flex: none; display: flex; align-items: center; justify-content: center; width: 34px; border: 1px solid var(--border); border-radius: 7px; background: var(--bg-hover); color: var(--text-secondary); cursor: pointer; }
.pc-browse:hover { border-color: var(--accent); color: var(--text-primary); }
.pc-org-row { flex-direction: row !important; align-items: center; gap: 8px; }
.pc-org-row input[type="checkbox"] { width: 14px; height: 14px; flex: none; accent-color: var(--accent); cursor: pointer; }
.pc-org-row > span { font-size: 11px; color: var(--text-secondary); }
.pc-org-row > span em { color: var(--text-muted); font-style: normal; }

.name-inp { font-weight: 500; }

.pill {
  display: inline-flex;
  align-items: center;
  background: #161616;
  border: 1px solid #252525;
  border-radius: 4px;
  padding: 3px 8px;
  max-width: 140px;
}
.cmd-inp { font-size: 12px; }

.col-args { position: relative; display: flex; align-items: center; gap: 6px; }
.args-inp { color: #555; font-size: 11px; }

.flag-edit {
  background: none;
  border: 1px solid #252525;
  border-radius: 4px;
  color: #555;
  cursor: pointer;
  display: flex;
  padding: 4px;
  flex-shrink: 0;
}
.flag-edit:hover { color: #999; border-color: #3a3a3a; }
.flag-edit.on { color: #a78bfa; border-color: #7c3aed55; background: #7c3aed14; }

.flag-backdrop { position: fixed; inset: 0; z-index: 20; }

.flag-pop {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 21;
  width: 280px;
  background: #131313;
  border: 1px solid #2a2a2a;
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.fp-head { display: flex; align-items: baseline; gap: 8px; }
.fp-title { font-size: 12px; font-weight: 600; color: #e2e2e2; }
.fp-sub { font-size: 10px; color: #555; }
.fp-close {
  margin-left: auto;
  background: none;
  border: none;
  color: #555;
  cursor: pointer;
  display: flex;
  padding: 2px;
  border-radius: 3px;
}
.fp-close:hover { color: #e2e2e2; background: var(--bg-hover); }
.fp-area {
  background: #0c0c0c;
  border: 1px solid #252525;
  border-radius: 5px;
  color: #e2e2e2;
  font-size: 12px;
  line-height: 1.6;
  outline: none;
  padding: 8px 10px;
  resize: vertical;
  width: 100%;
}
.fp-area:focus { border-color: var(--accent); }
.fp-area::placeholder { color: #3a3a3a; }
.fp-foot { display: flex; }
.fp-preview {
  font-family: var(--font-mono);
  font-size: 10px;
  color: #666;
  background: #0c0c0c;
  border: 1px solid #1e1e1e;
  border-radius: 4px;
  padding: 5px 8px;
  width: 100%;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kbd-rec {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: #141414;
  border: 1px solid #252525;
  border-radius: 4px;
  padding: 4px 7px;
  min-width: 44px;
  color: #777;
  font-family: var(--font-ui);
  font-size: 11px;
  cursor: pointer;
}
.kbd-rec:hover { color: #aaa; border-color: #3a3a3a; }
.kbd-rec.set { color: #cbd5e1; }
.kbd-rec.recording {
  color: #a78bfa;
  border-color: #7c3aed66;
  background: #7c3aed14;
}
.kbd-clear {
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  color: #444;
  cursor: pointer;
  padding: 2px;
  border-radius: 3px;
  flex-shrink: 0;
}
.kbd-clear:hover { color: var(--red); background: rgba(239,68,68,0.12); }

.row-del {
  background: none;
  border: none;
  color: #3a2020;
  cursor: pointer;
  display: flex;
  padding: 5px;
  border-radius: 4px;
}
.row-del:hover { color: var(--red); background: rgba(239, 68, 68, 0.12); }

.tbl-empty { font-size: 12px; color: #444; padding: 20px; text-align: center; }

.sec-foot { margin-top: 8px; display: flex; align-items: center; gap: 10px; }
.ntfy-test-msg { font-size: 12px; color: var(--text-muted, #888); }
.ntfy-test-msg.err { color: var(--danger, #e5534b); }
.reset-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: 1px solid #1e1e1e;
  border-radius: 5px;
  color: #555;
  cursor: pointer;
  font-size: 11px;
  padding: 5px 10px;
}
.reset-btn:hover { color: #888; border-color: #333; }

/* Config directories */
.cfg-dirs { margin-top: 22px; gap: 12px; }
.cfg-hint {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: #777;
}
.cfg-hint code {
  font-size: 10.5px;
  background: #161616;
  border: 1px solid #262626;
  border-radius: 3px;
  padding: 0 4px;
  color: #aaa;
}
.cfg-col { display: flex; flex-direction: column; gap: 6px; }
.cfg-col-label { font-size: 11px; color: #888; }
.cfg-col-label code { font-size: 10px; color: #999; }
.cfg-row { display: flex; align-items: center; gap: 6px; }
.cfg-inp { flex: 1; font-family: var(--font-mono, monospace); font-size: 12px; }
.cfg-add { align-self: flex-start; }
.cfg-actions { display: flex; align-items: center; gap: 12px; margin-top: 4px; }
.cfg-save:disabled { opacity: 0.6; cursor: default; }
.cfg-status { font-size: 11px; color: #6ee7b7; }

/* General panel */
.settings-group { display: flex; flex-direction: column; gap: 10px; }
.group-label {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
}
.field {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.field-info { display: flex; flex-direction: column; gap: 3px; flex: 1; min-width: 0; }
.field-name { font-size: 13px; font-weight: 500; color: var(--text-primary); }
.field-desc { font-size: 11px; color: var(--text-muted); }
.copy-btn {
  font-size: 10px;
  margin-left: 6px;
  padding: 1px 6px;
  border-radius: 4px;
  border: 1px solid var(--border-color, #444);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
}
.copy-btn:hover { color: var(--text-primary); }

.select {
  background: var(--bg-hover);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-primary);
  font-size: 12px;
  padding: 6px 10px;
  outline: none;
  cursor: pointer;
  min-width: 200px;
}
.select:hover { border-color: var(--text-muted); }
.select:focus { border-color: var(--accent); }

.size-ctl { display: flex; align-items: center; gap: 6px; }
.sound-ctl { display: flex; align-items: center; gap: 6px; }
.wt-dir-ctl { display: flex; align-items: center; gap: 6px; }
.wt-dir-input { min-width: 240px; font-family: var(--font-mono, monospace); }
.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: 1px solid #1e1e1e;
  border-radius: 5px;
  color: #777;
  cursor: pointer;
  padding: 5px 7px;
}
.icon-btn:hover { color: #aaa; border-color: #333; }
.vol-range { width: 160px; accent-color: var(--accent, #d97757); cursor: pointer; }
.size-inp { min-width: 0; width: 64px; cursor: text; text-align: center; }
.size-unit { font-size: 12px; color: #555; }

.term-preview {
  background: #0a0a0a;
  border: 1px solid #1e1e1e;
  border-radius: 6px;
  padding: 12px 14px;
  color: #e2e8f0;
  line-height: 1.4;
}
.tp-prompt { color: #22c55e; }

/* Appearance — theme picker */
.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
}
.theme-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px;
  background: var(--bg-base);
  border: 1px solid var(--border);
  border-radius: 8px;
  cursor: pointer;
  text-align: left;
  transition: border-color 0.12s, box-shadow 0.12s;
}
.theme-card:hover { border-color: var(--text-muted); }
.theme-card.selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px var(--accent);
}
.theme-swatch {
  position: relative;
  height: 64px;
  border: 1px solid;
  border-radius: 6px;
  overflow: hidden;
}
.sw-panel {
  position: absolute;
  top: 8px;
  left: 8px;
  width: 46%;
  height: calc(100% - 16px);
  border-radius: 4px;
  padding: 7px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.sw-line {
  height: 4px;
  width: 80%;
  border-radius: 2px;
  opacity: 0.9;
}
.sw-line.short { width: 50%; opacity: 0.6; }
.sw-dots {
  position: absolute;
  bottom: 9px;
  right: 9px;
  display: flex;
  gap: 5px;
}
.sw-dots span {
  width: 9px;
  height: 9px;
  border-radius: 50%;
}
.theme-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2px;
}
.theme-name {
  font-size: 12px;
  color: var(--text-primary);
}
.theme-check { color: var(--accent); }

/* Background image picker */
/* Background settings */
.bg-group { max-width: 560px; }

.bg-card {
  display: flex;
  gap: 14px;
  align-items: center;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated, #141414);
}
.bg-thumb {
  position: relative;
  width: 116px;
  height: 74px;
  border-radius: 7px;
  border: 1px solid var(--border);
  background-color: #0a0a0a;
  background-size: cover;
  background-position: center;
  flex-shrink: 0;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  overflow: hidden;
  transition: border-color 0.12s ease;
}
.bg-thumb:hover { border-color: var(--accent); }
.bg-thumb.is-empty { color: var(--text-muted); border-style: dashed; }
.bg-thumb-hint { font-size: 10px; color: var(--text-muted); }
.bg-thumb-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: rgba(0, 0, 0, 0.45);
  opacity: 0;
  transition: opacity 0.12s ease;
}
.bg-thumb:hover .bg-thumb-overlay { opacity: 1; }

.bg-card-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.bg-card-name {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bg-card-sub { font-size: 11px; color: var(--text-muted); }
.bg-card-btns { display: flex; gap: 6px; margin-top: 8px; }
.bg-btn {
  padding: 5px 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: #161616;
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
  transition: border-color 0.12s ease, color 0.12s ease, background 0.12s ease;
}
.bg-btn:hover { border-color: var(--accent); color: var(--accent); }
.bg-btn-primary {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  color: var(--accent);
}
.bg-btn-primary:hover { background: color-mix(in srgb, var(--accent) 26%, transparent); }
.bg-btn-clear:hover { border-color: var(--red); color: var(--red); }

.bg-control {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated, #141414);
}
.bg-control-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}
.bg-control-name { font-size: 12px; font-weight: 600; color: var(--text-primary); }
.bg-control-sub { font-size: 11px; color: var(--text-muted); }
.bg-control-val {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent);
  font-variant-numeric: tabular-nums;
}

.bg-slider {
  width: 100%;
  height: 4px;
  accent-color: var(--accent);
  cursor: pointer;
}

.blur-grid {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 2px;
}
.blur-row {
  display: grid;
  grid-template-columns: 170px 1fr 42px;
  align-items: center;
  gap: 12px;
}
.blur-name { font-size: 12px; color: var(--text-secondary); }
.blur-val {
  font-size: 11px;
  color: var(--accent);
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.placeholder {
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #333;
  font-size: 13px;
  gap: 12px;
}

.kb-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 0;
  border-bottom: 1px solid #141414;
  gap: 12px;
}
.kb-row:last-child { border-bottom: none; }
.kb-desc { font-size: 12px; color: #94a3b8; }
.kb-keys { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
.kb-key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 6px;
  border-radius: 4px;
  background: #1a1a1a;
  border: 1px solid #2a2a2a;
  color: #cbd5e1;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  line-height: 1.4;
}

.toggle { display: flex; align-items: center; cursor: pointer; flex-shrink: 0; }
.toggle input { position: absolute; opacity: 0; width: 0; height: 0; }
.toggle-track {
  width: 36px;
  height: 20px;
  background: #252525;
  border: 1px solid #333;
  border-radius: 10px;
  display: flex;
  align-items: center;
  padding: 2px;
  transition: background 0.15s, border-color 0.15s;
}
.toggle input:checked ~ .toggle-track {
  background: #7c3aed;
  border-color: #7c3aed;
}
.toggle-thumb {
  width: 14px;
  height: 14px;
  background: #555;
  border-radius: 50%;
  transition: transform 0.15s, background 0.15s;
}
.toggle input:checked ~ .toggle-track .toggle-thumb {
  transform: translateX(16px);
  background: #fff;
}

/* Add area with template picker */
.add-area {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 1px;
}
.add-area .add-btn { border-radius: 5px 0 0 5px; border-right: none; }
.template-btn { border-radius: 0 5px 5px 0 !important; padding: 5px 8px !important; }
.template-wrap { position: relative; }
.template-pop {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 30;
  width: 280px;
  background: #131313;
  border: 1px solid #2a2a2a;
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0,0,0,.5);
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.tp-head {
  font-size: 10px;
  font-weight: 600;
  color: #555;
  letter-spacing: .04em;
  text-transform: uppercase;
  padding: 4px 8px 6px;
}
.tp-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-radius: 5px;
  background: none;
  border: none;
  color: #ccc;
  cursor: pointer;
  font-size: 12px;
  font-family: var(--font-ui);
  text-align: left;
  width: 100%;
}
.tp-row:hover { background: #1e1e1e; color: #e2e2e2; }
.tp-icon {
  width: 24px;
  height: 24px;
  border-radius: 5px;
  border: 1px solid;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.tp-name { flex: 1; font-weight: 500; }
.tp-cmd {
  font-family: var(--font-mono);
  font-size: 10px;
  color: #555;
  background: #0c0c0c;
  border: 1px solid #1e1e1e;
  border-radius: 3px;
  padding: 2px 5px;
}

/* Dot as color picker */
.dot-label {
  position: relative;
  display: flex;
  align-items: center;
  cursor: pointer;
  flex-shrink: 0;
}
.dot-label .color-input {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
  border: none;
  padding: 0;
  width: 100%;
  height: 100%;
}
.dot-label:hover .dot { transform: scale(1.3); }
.dot { transition: transform 0.1s; }

/* Icon picker */
.icon-wrap { position: relative; flex-shrink: 0; }
.icon-box {
  position: relative;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  border: 1px solid;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  background: none;
  padding: 0;
}
.icon-box:hover { filter: brightness(1.2); }
.icon-pop {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  z-index: 25;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 8px;
  background: #131313;
  border: 1px solid #2a2a2a;
  border-radius: 8px;
  box-shadow: 0 12px 32px rgba(0,0,0,.5);
  width: 168px;
}
.ip-btn {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  background: #1a1a1a;
  border: 1px solid #252525;
  color: #888;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}
.ip-btn:hover { background: #252525; color: #e2e2e2; }
.ip-btn.active { background: #7c3aed22; border-color: #7c3aed66; color: #a78bfa; }

/* Workspaces list */
.ws-list { display: flex; flex-direction: column; gap: 6px; }
.ws-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 8px;
  background: var(--bg-elev, #1a1a1a);
}
.ws-icon-btn {
  position: relative;
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border-radius: 8px;
  border: 1px solid var(--border, #2a2a2a);
  background: #202020;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  padding: 0;
  overflow: hidden;
}
.ws-icon-btn:hover { border-color: #7c3aed66; }
.ws-icon-img { width: 100%; height: 100%; object-fit: cover; }
.ws-icon-fb { color: #60a5fa; }
.ws-icon-edit {
  position: absolute;
  bottom: -1px;
  right: -1px;
  width: 14px;
  height: 14px;
  border-radius: 4px 0 6px 0;
  background: #7c3aed;
  color: #fff;
  display: none;
  align-items: center;
  justify-content: center;
}
.ws-icon-btn:hover .ws-icon-edit { display: flex; }
.ws-meta { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.ws-name { font-size: 13px; color: #e2e2e2; font-weight: 500; }
.ws-path {
  font-size: 11px;
  color: #777;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ws-clear {
  flex-shrink: 0;
  width: 26px;
  height: 26px;
  border-radius: 6px;
  border: 1px solid var(--border, #2a2a2a);
  background: none;
  color: #888;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.ws-clear:hover { color: #e2e2e2; background: #252525; }

/* About / Updates */
.about-id { display: flex; align-items: center; gap: 14px; }
.about-logo {
  width: 48px;
  height: 48px;
  border-radius: 11px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-hover);
  border: 1px solid var(--border);
  color: var(--accent);
}
.about-name { font-size: 15px; font-weight: 600; color: var(--text-primary); }
.about-ver { font-size: 12px; color: var(--text-secondary); margin-top: 2px; }

.update-box {
  margin-top: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-panel);
  padding: 14px 16px;
}
.update-box-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.update-box-text { display: flex; flex-direction: column; gap: 3px; min-width: 0; }
.upd-strong { font-size: 12.5px; font-weight: 600; color: var(--text-primary); }
.upd-dim { font-size: 11.5px; color: var(--text-secondary); }
.update-box-actions { flex-shrink: 0; }
.reset-btn.primary {
  background: var(--accent);
  border-color: transparent;
  color: #fff;
}
.reset-btn.primary:hover { filter: brightness(1.08); color: #fff; border-color: transparent; }
.reset-btn:disabled { opacity: 0.6; cursor: default; }
.update-box-notes {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
  font-size: 11.5px;
  line-height: 1.45;
  color: var(--text-secondary);
  white-space: pre-wrap;
  max-height: 160px;
  overflow-y: auto;
}
.update-box-err {
  margin-top: 10px;
  font-size: 11px;
  color: var(--red);
  word-break: break-word;
}
.spin { animation: upd-spin 0.9s linear infinite; }
@keyframes upd-spin { to { transform: rotate(360deg); } }

/* ── Skills / MCP / Extensions lists ───────────────────────────────────────── */
.ext-list { display: flex; flex-direction: column; gap: 8px; }
.ext-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.ext-row.off { opacity: 0.55; }
.ext-row.planned { border-style: dashed; }
.ext-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex: none;
  border-radius: 7px;
  background: var(--bg-hover);
  color: var(--text-secondary);
}
.ext-main { flex: 1; min-width: 0; }
.ext-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.ext-desc { margin-top: 3px; font-size: 11.5px; line-height: 1.45; color: var(--text-secondary); }
.ext-config {
  margin: 6px 0 0;
  padding: 8px 10px;
  font-size: 11px;
  line-height: 1.4;
  color: var(--text-secondary);
  background: var(--bg-base);
  border: 1px solid var(--border);
  border-radius: 6px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 160px;
  overflow: auto;
}
.ext-actions { display: flex; align-items: center; gap: 4px; flex: none; }
.icon-act {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.icon-act:hover { background: var(--bg-hover); color: var(--text-primary); }
.icon-act.danger:hover { color: var(--red); }
.planned-badge {
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  padding: 2px 6px;
  border-radius: 999px;
  color: var(--yellow);
  background: color-mix(in srgb, var(--yellow) 16%, transparent);
}

/* MCP add/edit form */
.mcp-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.mcp-form-head { display: flex; align-items: center; }
.mcp-form-head .group-label { margin: 0; }
.mcp-form-head .fp-close { margin-left: auto; }
.mcp-name { max-width: 280px; }
.mcp-config { width: 100%; box-sizing: border-box; }
.mcp-form-foot { display: flex; align-items: center; gap: 8px; }
.mcp-err { font-size: 11px; color: var(--red); }

/* ── Scripts ── */
.scripts-group-head { display: flex; align-items: center; gap: 10px; margin-bottom: 6px; }
.scripts-group-head .group-label { margin: 0; }
.scripts-group-head .add-btn { margin-left: auto; }

.script-card {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--bg-base) 50%, transparent);
  padding: 10px 12px;
  margin-bottom: 10px;
}
.sc-head { display: flex; align-items: center; gap: 8px; }
.sc-name { flex: 0 1 200px; font-weight: 500; }
.sc-toggle { display: flex; align-items: center; gap: 6px; cursor: pointer; }
.sc-toggle input { display: none; }
.sc-toggle-label { font-size: 11px; color: var(--text-secondary); white-space: nowrap; }

.sc-steps { margin-top: 10px; display: flex; flex-direction: column; gap: 5px; }
.sc-step { display: flex; align-items: center; gap: 6px; }
.sc-step-idx {
  width: 18px; height: 18px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; color: var(--text-muted);
  background: color-mix(in srgb, var(--accent) 12%, transparent);
  border-radius: 4px;
}
.sc-step-inp { flex: 1; }
.sc-step-btn {
  display: flex; align-items: center; justify-content: center;
  width: 24px; height: 24px; flex-shrink: 0;
  border: 1px solid var(--border); border-radius: 5px;
  background: transparent; color: var(--text-muted); cursor: pointer;
  transition: background .12s, color .12s;
}
.sc-step-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--accent) 12%, transparent); color: var(--text-primary); }
.sc-step-btn:disabled { opacity: 0.3; cursor: default; }
.sc-step-btn.del:hover:not(:disabled) { background: color-mix(in srgb, var(--red) 16%, transparent); color: var(--red); }
.sc-add-step { margin-top: 4px; align-self: flex-start; }

.sc-preview {
  margin-top: 10px;
  font-size: 11px;
  color: var(--text-muted);
  display: flex; align-items: baseline; gap: 6px;
  overflow: hidden;
}
.sc-preview-label { flex-shrink: 0; }
.sc-preview code {
  font-family: var(--font-mono);
  color: var(--text-secondary);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
</style>
