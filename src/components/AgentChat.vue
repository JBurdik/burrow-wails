<template>
  <div class="claude-chat flex h-full flex-row overflow-hidden bg-base" :style="{ '--agent-accent': agentAccentColor }">
    <div class="chat-main flex min-w-0 flex-1 flex-col overflow-hidden bg-base">

    <!-- Permission prompt (Bash / generic tool) -->
    <div v-if="pendingPermission" class="status-banner status-banner--warn perm-slide-in flex flex-shrink-0 items-center gap-2 py-2.5 pl-3.5 pr-3 mx-3 mt-2 mb-0.5">
      <PhShieldWarning :size="14" class="perm-icon flex-shrink-0" />
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="perm-title text-[11px] font-semibold text-foreground">{{ pendingPermission.toolName }} wants to run</span>
        <code class="perm-detail max-w-full overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground">{{ permissionDetail }}</code>
      </div>
      <div class="flex flex-shrink-0 items-center gap-1.5">
        <div class="perm-allow-group relative flex">
          <button class="perm-btn perm-allow rounded-l-md rounded-r-none" :disabled="nativeControlResponsePending" @click="respondPermission(true)" title="Allow once (Y)">
            Allow <kbd class="perm-kbd">Y</kbd>
          </button>
          <button class="perm-btn perm-allow !rounded-l-none !rounded-r-md border-l border-black/[0.15] !px-[5px]" :disabled="nativeControlResponsePending" @click="permDropdownOpen = !permDropdownOpen" title="More options">
            <PhCaretDown :size="9" weight="bold" />
          </button>
          <div v-if="permDropdownOpen" class="absolute bottom-[calc(100%+4px)] right-0 z-[100] min-w-[200px] rounded-lg border border-[var(--chat-border)] bg-[var(--chat-dropdown)] p-1 shadow-[0_4px_16px_rgba(0,0,0,0.4)]">
            <button class="perm-dropdown-item" @click="permDropdownOpen = false; respondPermission(true)">
              Allow once
            </button>
            <button
              v-if="pendingPermission.toolName === 'Bash' && permissionDetail"
              class="perm-dropdown-item"
              @click="permDropdownOpen = false; respondPermission(true, { always: true })"
              :title="`Always allow: ${permissionDetail.split(' ')[0]}`"
            >
              Always allow <code class="perm-pattern">{{ permissionDetail.split(' ')[0] }}…</code>
            </button>
            <button class="perm-dropdown-item" @click="permDropdownOpen = false; respondPermission(true, { always: true })">
              Always allow {{ pendingPermission.toolName }}
            </button>
            <button class="perm-dropdown-item !text-red-500/80 hover:!bg-red-500/10" @click="permDropdownOpen = false; respondPermission(false)">
              Deny <kbd class="perm-kbd">N</kbd>
            </button>
          </div>
        </div>
        <button class="perm-btn perm-deny" :disabled="nativeControlResponsePending" @click="respondPermission(false)" title="Deny (N)">
          Deny <kbd class="perm-kbd">N</kbd>
        </button>
      </div>
    </div>

    <!-- File edit: diff preview with Accept / Reject -->
    <div v-if="pendingDiff && diffPreview" class="status-banner status-banner--info diff-banner perm-slide-in mx-3 mt-2 mb-0.5 flex-shrink-0 overflow-hidden">
      <div class="flex items-center gap-2 px-3 py-2">
        <PhGitDiff :size="13" class="perm-icon flex-shrink-0" />
        <span class="perm-title text-[11px] font-semibold text-foreground">{{ pendingDiff.toolName }}</span>
        <code class="perm-detail max-w-full overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground" :title="diffPreview.path">{{ diffPreview.path }}</code>
        <span class="flex-1" />
        <button class="perm-btn perm-allow" :disabled="nativeControlResponsePending" @click="respondPermission(true)" title="Accept (Y)">Accept <kbd class="perm-kbd">Y</kbd></button>
        <button class="perm-btn perm-always" :disabled="nativeControlResponsePending" @click="respondPermission(true, { always: true })" title="Always allow this tool">Always</button>
        <button class="perm-btn perm-deny" :disabled="nativeControlResponsePending" @click="respondPermission(false)" title="Reject (N)">Reject <kbd class="perm-kbd">N</kbd></button>
      </div>
      <pre v-if="diffPreview.isWrite" class="diff-banner-body m-0 max-h-[220px] overflow-auto px-3 pb-2.5 pt-1.5 font-mono text-[11px] leading-[1.5]"><span
        v-for="(line, i) in diffPreview.content.split('\n')" :key="i" class="diff-line diff-add block whitespace-pre-wrap break-all">{{ line }}</span></pre>
      <pre v-else class="diff-banner-body m-0 max-h-[220px] overflow-auto px-3 pb-2.5 pt-1.5 font-mono text-[11px] leading-[1.5]"><span
        v-for="(line, i) in diffPreview.oldStr.split('\n')" :key="'o'+i" class="diff-line diff-del block whitespace-pre-wrap break-all">{{ line }}</span><span
        v-for="(line, i) in diffPreview.newStr.split('\n')" :key="'n'+i" class="diff-line diff-add block whitespace-pre-wrap break-all">{{ line }}</span></pre>
    </div>

    <!-- ExitPlanMode: plan approval -->
    <div v-if="pendingPlan" class="status-banner status-banner--success plan-banner perm-slide-in mx-3 mt-2 mb-0.5 flex-shrink-0 px-[13px] py-[11px]">
      <div class="mb-1.5 flex items-center gap-[7px]">
        <PhListChecks :size="14" class="perm-icon" />
        <span class="perm-title text-[11px] font-semibold text-foreground">Claude proposed a plan</span>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div class="plan-body md-body max-h-[260px] overflow-auto text-xs text-foreground" v-html="planMd" />
      <textarea
        v-model="planFeedback"
        class="my-2 box-border w-full resize-y rounded-[5px] border border-border bg-base px-2 py-1.5 font-sans text-[11px] text-foreground"
        rows="1"
        placeholder="Optional feedback if you keep planning…"
      />
      <div class="flex justify-end gap-2">
        <button class="perm-btn perm-allow" :disabled="nativeControlResponsePending" @click="respondPlan(true)">Approve plan</button>
        <button class="perm-btn perm-deny" :disabled="nativeControlResponsePending" @click="respondPlan(false)" title="Keep planning (Esc)">Keep planning</button>
      </div>
    </div>

    <!-- ACP permission request — renders the adapter's real option set -->
    <div
      v-if="acpPermReq"
      class="status-banner permission-banner acp-perm-banner perm-slide-in mx-3 mt-2 mb-0.5 flex flex-shrink-0 flex-col items-stretch gap-2 py-2.5 pl-3.5 pr-3"
      :class="{
        'acp-perm-plan': acpPermPlan,
        'acp-perm-diff': acpPermDiff && !acpPermPlan,
        'status-banner--success': acpPermPlan,
        'status-banner--info': acpPermDiff && !acpPermPlan,
        'status-banner--warn': !acpPermPlan && !acpPermDiff,
      }"
    >
      <div class="flex min-w-0 items-center gap-[7px]">
        <PhListChecks v-if="acpPermPlan" :size="14" class="perm-icon" />
        <PhGitDiff v-else-if="acpPermDiff" :size="14" class="perm-icon" />
        <PhShieldWarning v-else :size="14" class="perm-icon" />
        <span class="perm-title text-[11px] font-semibold text-foreground">{{ acpPermPlan ? 'Review plan' : acpPermReq.title }}</span>
        <code v-if="acpPermDiff" class="perm-detail overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground" :title="acpPermDiff.path">{{ acpPermDiff.path }}</code>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div v-if="acpPermPlan" class="plan-body md-body max-h-[320px] overflow-auto text-xs text-foreground" v-html="acpPermPlan" />
      <pre v-else-if="acpPermDiff && acpPermDiff.isWrite" class="diff-banner-body m-0 max-h-[220px] overflow-auto px-3 pb-2.5 pt-1.5 font-mono text-[11px] leading-[1.5]"><span
        v-for="(line, i) in acpPermDiff.content.split('\n')" :key="i" class="diff-line diff-add block whitespace-pre-wrap break-all">{{ line }}</span></pre>
      <pre v-else-if="acpPermDiff" class="diff-banner-body m-0 max-h-[220px] overflow-auto px-3 pb-2.5 pt-1.5 font-mono text-[11px] leading-[1.5]"><span
        v-for="(line, i) in acpPermDiff.oldStr.split('\n')" :key="'o'+i" class="diff-line diff-del block whitespace-pre-wrap break-all">{{ line }}</span><span
        v-for="(line, i) in acpPermDiff.newStr.split('\n')" :key="'n'+i" class="diff-line diff-add block whitespace-pre-wrap break-all">{{ line }}</span></pre>
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="o in acpPermReq.options"
          :key="o.optionId"
          class="perm-btn flex-none"
          :class="acpOptClass(o.kind)"
          :disabled="permissionResponsePending"
          @click="acpRespond(o.optionId, o.name, o.kind)"
        >{{ o.name }}</button>
      </div>
    </div>

    <CodexUserInputPanel
      v-if="codexUserInput"
      :questions="codexUserInput.questions"
      :submitting="codexUserInputPending"
      @submit="respondCodexUserInput"
      @cancel="cancelCodexUserInput"
    />

    <div ref="scrollEl" @scroll.passive="onScroll" class="chat-messages relative flex flex-1 flex-col overflow-y-auto py-6 pb-2 [scroll-behavior:smooth] [-webkit-user-select:text] [user-select:text]">
      <div class="mx-auto flex w-full max-w-[760px] flex-1 flex-col gap-0.5">
      <div v-if="messages.length === 0" class="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-10 text-center">
        <div class="chat-empty-avatar mb-2 flex h-11 w-11 items-center justify-center rounded-[11px] text-white shadow-[0_0_0_1px_color-mix(in_srgb,var(--agent-accent,#ec4899)_36%,transparent)]" style="background: color-mix(in srgb, var(--agent-accent, #ec4899) 72%, #16161a);" aria-hidden="true">
          <component :is="currentAgentIcon" :size="28" :style="{ color: '#fff' }" />
        </div>
        <span class="text-[9px] font-semibold uppercase tracking-[.08em] text-muted-foreground">New conversation</span>
        <span class="text-base font-semibold text-foreground">Start a focused conversation</span>
        <span class="mt-0.5 font-mono text-[11px] text-muted-foreground">Working in {{ cwdDisplay }}</span>
      </div>

      <div
        v-for="(msg, msgIdx) in displayItems"
        :key="msg.id"
        class="chat-message"
        :class="[`role-${msg.role}`, { partial: msg.partial }]"
      >
        <!-- Collapsed tool-call group pill ("Ran N commands" / "Used N tools") -->
        <template v-if="msg.role === 'tool-group-header'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="w-[26px] flex-shrink-0" />
            <div class="tool-row" :class="{ 'tool-row-failed': groupHasFailure(msg.items) }" @click="toggleGroup(msg.groupId)">
              <PhCaretRight :size="10" class="tool-caret" :class="{ 'tool-caret-open': isGroupExpanded(msg.groupId) }" />
              <component :is="groupIcon(msg.items)" :size="12" class="tool-icon" />
              <span class="font-medium">{{ groupLabel(msg.items) }}</span>
              <span v-if="groupIsRunning(msg.items)" class="tool-status-icon tool-pulse-dot flex-shrink-0" aria-hidden="true" />
              <PhWarningCircle v-else-if="groupHasFailure(msg.items)" :size="10" class="tool-status-icon flex-shrink-0 text-destructive" />
            </div>
          </div>
        </template>

        <!-- Settled turn: its thinking/tool rows fold behind this one row -->
        <template v-else-if="msg.role === 'turn-fold'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="w-[26px] flex-shrink-0" />
            <div class="tool-row" @click="foldedTurns[msg.turnKey] = !msg.folded">
              <PhCaretRight :size="10" class="tool-caret" :class="{ 'tool-caret-open': !msg.folded }" />
              <PhClock :size="11" class="tool-icon" />
              <span>{{ msg.label }}</span>
            </div>
          </div>
        </template>

        <!-- Changed files this turn touched, with a +/- diffstat -->
        <template v-else-if="msg.role === 'changed-files'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="w-[26px] flex-shrink-0" />
            <div class="changed-files max-w-[min(560px,90%)]">
              <div class="changed-files-head">
                <PhGitDiff :size="11" class="flex-shrink-0" />
                <span>Changed files ({{ msg.files.length }})</span>
                <span class="ml-auto flex-shrink-0 font-mono text-[10px]"><span class="diff-add">+{{ msg.added }}</span> <span class="diff-del">−{{ msg.removed }}</span></span>
              </div>
              <div v-for="f in msg.files" :key="f.path" class="changed-files-row" :title="f.path">
                <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ basename(f.path) }}</span>
                <span class="ml-auto flex-shrink-0 font-mono text-[10px]"><span class="diff-add">+{{ f.added }}</span> <span class="diff-del">−{{ f.removed }}</span></span>
              </div>
            </div>
          </div>
        </template>

        <!-- User message -->
        <template v-else-if="msg.role === 'user'">
          <div class="group flex items-end justify-end gap-1.5 px-4" :class="isFirstOfRun(msgIdx) ? 'pt-2.5 pb-[3px]' : 'py-[1px]'">
            <div class="bubble-user max-w-[72%] rounded-[16px_16px_5px_16px] border px-3.5 py-2.5 text-[13px] leading-[1.55] shadow-[0_2px_10px_rgba(0,0,0,0.22)]" style="background: var(--chat-user-bg, #1e1b2e); border-color: var(--chat-user-border, rgba(124,58,237,0.35)); color: var(--chat-text, rgba(255,255,255,0.88));">
              <div v-if="msg.images && msg.images.length > 0" class="mb-1.5 flex flex-wrap gap-1.5">
                <img
                  v-for="(img, i) in msg.images"
                  :key="i"
                  :src="img"
                  class="block max-h-40 max-w-[200px] rounded object-cover"
                  :alt="`Image ${i + 1}`"
                />
              </div>
              <div class="md-body" v-html="renderUserMd(msg.text)" />
            </div>
            <div v-if="isLastOfRun(msgIdx)" class="flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full border border-border bg-hover text-[11px] font-bold text-secondary-foreground">U</div>
            <div v-else class="w-[26px] flex-shrink-0" />
          </div>
        </template>

        <!-- Tool call — compact row, expandable; hidden while its parent group pill is collapsed -->
        <template v-else-if="msg.role === 'tool'">
          <template v-if="!groupIdOf(msg.id) || isGroupExpanded(groupIdOf(msg.id))">
            <div class="group flex items-start gap-2.5 px-4 py-[3px]">
              <div class="w-[26px] flex-shrink-0" />
              <div class="tool-row" :class="`tool-row-${toolStatus(msg)}`" @click="msg.toolExpanded = !msg.toolExpanded">
                <PhCaretRight :size="10" class="tool-caret" :class="{ 'tool-caret-open': msg.toolExpanded }" />
                <component :is="toolIconFor(msg)" :size="12" class="tool-icon" />
                <span class="overflow-hidden text-ellipsis whitespace-nowrap" :class="{ 'font-mono': toolMonospace(msg) }">{{ toolSummaryFor(msg) }}</span>
                <span v-if="toolStatus(msg) === 'running'" class="tool-status-icon tool-pulse-dot flex-shrink-0" aria-hidden="true" />
                <PhWarningCircle v-else-if="toolStatus(msg) === 'failed'" :size="10" class="tool-status-icon flex-shrink-0 text-destructive" />
                <span v-if="msg.toolOutput && !msg.toolExpanded" class="max-w-[220px] flex-shrink overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-muted-foreground">{{ msg.toolOutput.split('\n')[0].slice(0, 60) }}</span>
              </div>
            </div>
            <div v-if="msg.toolExpanded" class="flex items-start gap-2.5 px-4 py-[3px]">
              <div class="w-[26px] flex-shrink-0" />
              <div class="flex flex-col gap-1.5">
                <div v-if="msg.toolInput && Object.keys(msg.toolInput).length" class="flex flex-col gap-[3px]">
                  <div v-for="k in ['file_path','command','pattern','url','description']" :key="k" v-show="msg.toolInput[k] !== undefined" class="flex max-w-[min(560px,90vw)] gap-1.5 font-mono text-[10px]">
                    <span class="flex-shrink-0 text-muted-foreground">{{ k }}</span><span class="overflow-hidden text-ellipsis whitespace-nowrap text-secondary-foreground">{{ msg.toolInput[k] }}</span>
                  </div>
                  <pre class="tool-args">{{ JSON.stringify(msg.toolInput, null, 2) }}</pre>
                </div>
                <pre v-if="msg.toolOutput" class="tool-output" :class="{ 'tool-output-failed': msg.toolFailed }">{{ msg.toolOutput }}</pre>
              </div>
            </div>
          </template>
        </template>

        <!-- System info marker (permission requested, plan ready, etc.) -->
        <template v-else-if="msg.role === 'system-info'">
          <div class="flex justify-center px-4 py-1">
            <span class="rounded-[20px] border border-border bg-hover px-2.5 py-0.5 text-[11px] text-muted-foreground">{{ msg.text }}</span>
          </div>
        </template>

        <!-- Queued message placeholder -->
        <template v-else-if="msg.role === 'queued'">
          <div class="flex items-end justify-end gap-2 px-4 py-[3px]">
            <div class="inline-flex max-w-[min(460px,85%)] items-center gap-1.5 rounded-[14px] border border-dashed border-border bg-hover px-3 py-2 text-right text-[13px] text-muted-foreground opacity-70">
              <PhClock :size="11" class="flex-shrink-0" />
              {{ msg.text }}
            </div>
            <div class="flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full border border-border bg-hover text-[11px] font-bold text-secondary-foreground opacity-35">U</div>
          </div>
        </template>

        <!-- Permission log -->
        <template v-else-if="msg.role === 'permission'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="w-[26px] flex-shrink-0" />
            <div class="bubble-permission" :class="msg.text.startsWith('✓') ? 'perm-granted' : 'perm-rejected'">
              <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ msg.text }}</span>
            </div>
          </div>
        </template>

        <!-- Thinking -->
        <template v-else-if="msg.role === 'thinking'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="w-[26px] flex-shrink-0" />
            <div class="max-w-[90%]">
              <div class="tool-row" @click="thinkingExpanded[msg.id] = !thinkingExpanded[msg.id]">
                <PhCaretRight :size="10" class="tool-caret" :class="{ 'tool-caret-open': thinkingExpanded[msg.id] }" />
                <span class="italic">Thinking…</span>
              </div>
              <pre v-if="thinkingExpanded[msg.id]" class="thinking-body">{{ msg.text }}</pre>
            </div>
          </div>
        </template>

        <!-- Assistant message -->
        <template v-else>
          <div class="flex items-start gap-2.5 px-4" :class="isFirstOfRun(msgIdx) ? 'pt-2.5 pb-[3px]' : 'py-[1px]'">
            <div v-if="isFirstOfRun(msgIdx)" class="agent-avatar mt-0.5 flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full text-white shadow-[0_2px_6px_color-mix(in_srgb,var(--agent-accent,#ec4899)_28%,transparent)]">
              <component :is="currentAgentIcon" :size="14" :style="{ color: '#fff' }" />
            </div>
            <div v-else class="mt-0.5 w-[26px] flex-shrink-0" />
            <div class="min-w-0 flex-1 pt-1 text-[13px] leading-[1.65] text-foreground">
              <!-- eslint-disable-next-line vue/no-v-html -->
              <div class="md-body" v-html="renderMd(msg.text)" />
            </div>
            <button class="message-copy-btn mt-0.5" :aria-label="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" :title="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" @click="copyMessage(msg)">
              <PhCheck v-if="copiedMessageId === msg.id" :size="13" weight="bold" />
              <PhCopy v-else :size="13" />
            </button>
          </div>
        </template>
      </div>

      <div v-if="busy" class="flex items-center gap-1.5 px-4 py-1.5">
        <div class="agent-avatar flex h-[22px] w-[22px] items-center justify-center rounded-full text-white shadow-[0_2px_6px_color-mix(in_srgb,var(--agent-accent,#ec4899)_28%,transparent)]">
          <component :is="currentAgentIcon" :size="12" :style="{ color: '#fff' }" />
        </div>
        <span class="thinking-dot" /><span class="thinking-dot" /><span class="thinking-dot" />
        <span class="ml-1 text-[11px] italic text-muted-foreground tabular-nums">Working for {{ workingElapsed }}</span>
      </div>
      </div>
    </div>

    <!-- Jump back to live: only while scrolled up (autoscroll paused) -->
    <div class="relative h-0">
      <button
        v-if="!atBottom"
        class="absolute bottom-2 left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 rounded-full border border-border bg-panel px-2.5 py-1 text-[11px] text-secondary-foreground shadow-md transition-colors hover:bg-hover"
        title="Jump to latest"
        @click="scrollToBottom(true)"
      >
        <PhArrowDown :size="12" weight="bold" /> Latest
      </button>
    </div>

    <!-- Command suggestions dropdown -->
    <div v-if="suggestions.length > 0" ref="suggestionsEl" class="max-h-[200px] flex-shrink-0 overflow-y-auto border-t border-border bg-panel">
      <div
        v-for="(s, i) in suggestions"
        :key="s.name"
        class="flex cursor-pointer items-baseline gap-2.5 px-3 py-1.5 transition-colors hover:bg-hover"
        :class="{ '!bg-hover': i === suggestionIdx }"
        @mousedown.prevent="applySuggestion(s.name)"
      >
        <span class="min-w-[100px] flex-shrink-0 font-mono text-xs font-semibold text-[var(--chat-accent)]">/{{ s.name }}</span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-muted-foreground">{{ s.description }}</span>
      </div>
    </div>

    <!-- @-mention file suggestions dropdown -->
    <div v-if="atSuggestions.length > 0" class="max-h-[200px] flex-shrink-0 overflow-y-auto border-t border-border bg-panel">
      <div
        v-for="(p, i) in atSuggestions"
        :key="p"
        class="flex cursor-pointer items-baseline gap-2.5 px-3 py-1.5 transition-colors hover:bg-hover"
        :class="{ '!bg-hover': i === atIdx }"
        @mousedown.prevent="applyAtSuggestion(p)"
      >
        <span class="min-w-[100px] flex-shrink-0 font-mono text-xs font-semibold text-[var(--chat-accent)]">@{{ p.slice(p.lastIndexOf('/') + 1) }}</span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-muted-foreground">{{ p }}</span>
      </div>
    </div>


    <!-- New-style input bar -->
    <div v-if="!hideComposer" class="flex-shrink-0 bg-base px-[18px] pb-2 pt-2.5">
      <div class="mx-auto w-full max-w-[760px]">
      <div class="chat-input-box overflow-hidden rounded-xl border border-border transition-[border-color,box-shadow]" :class="{ 'input-queued': busy && inputText.trim() }" style="background: color-mix(in srgb, var(--agent-accent, #ec4899) 4%, var(--chat-surface));">
        <!-- Queued messages panel (Zed-style) -->
        <div v-if="messageQueue.length > 0" class="border-b border-border bg-[color-mix(in_srgb,var(--chat-accent)_5%,transparent)]">
          <div class="flex cursor-pointer select-none items-center gap-1.5 px-2.5 py-1.5 hover:bg-hover" @click="queueExpanded = !queueExpanded">
            <PhCaretDown :size="10" class="text-muted-foreground transition-transform" :class="{ '-rotate-90': !queueExpanded }" />
            <span class="flex-1 text-[11px] text-muted-foreground">{{ messageQueue.length }} Queued {{ messageQueue.length === 1 ? 'Message' : 'Messages' }}</span>
            <button class="border-none bg-transparent px-1 py-px text-[10px] text-muted-foreground hover:text-foreground" @click.stop="clearQueue" title="Clear All">Clear All</button>
          </div>
          <div v-if="queueExpanded" class="flex flex-col gap-[3px] px-2.5 pb-1.5">
            <div v-for="msg in messageQueue" :key="msg.id" class="flex items-center gap-1.5 py-[3px]">
              <span class="flex-shrink-0 text-xs text-[var(--chat-accent)]">•</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-secondary-foreground">{{ msg.text }}</span>
              <button class="queue-item-btn" @click="removeQueued(msg.id)" title="Remove"><PhX :size="10" /></button>
              <button class="queue-item-btn !text-[var(--chat-accent)] !border-[color-mix(in_srgb,var(--chat-accent)_35%,transparent)] hover:!border-[color-mix(in_srgb,var(--chat-accent)_65%,transparent)]" @click="sendQueuedNext(msg.id)" title="Run after the active turn">Send Next</button>
            </div>
          </div>
        </div>
        <!-- Working indicator — sits above the textarea, only for tool activity.
             Thinking has its own bubble in the chat body, so it's not duplicated here. -->
        <div v-if="currentActivity" class="flex items-center gap-1.5 border-b border-border px-3 pb-1 pt-1.5">
          <span class="working-dot" /><span class="working-dot" /><span class="working-dot" />
          <span class="text-[11px] italic text-muted-foreground">{{ currentActivity }}</span>
        </div>
        <!-- AskUserQuestion: one question at a time, stepped, above the textarea -->
        <div v-if="pendingQuestion && activeQuestion" class="question-panel perm-slide-in border-b border-border bg-[color-mix(in_srgb,var(--chat-info)_4%,transparent)] px-3.5 py-3">
          <div class="mb-2 flex items-center gap-2">
            <span v-if="activeQuestion.header" class="rounded bg-[color-mix(in_srgb,var(--chat-info)_22%,transparent)] px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-[0.04em] text-[var(--chat-info)]">{{ activeQuestion.header }}</span>
            <span v-if="questionSpecs.length > 1" class="ml-auto flex h-5 flex-shrink-0 items-center rounded-md bg-hover px-1.5 text-[10px] font-medium tabular-nums text-secondary-foreground">{{ activeQuestionIndex + 1 }}/{{ questionSpecs.length }}</span>
          </div>
          <p class="mb-1 text-[13px] font-semibold text-foreground">{{ activeQuestion.question }}</p>
          <p v-if="activeQuestion.multiSelect" class="mb-1.5 text-[10.5px] text-secondary-foreground/70">Select one or more options.</p>
          <div class="mt-1.5 flex flex-col gap-1.5">
            <button
              v-for="(opt, oi) in activeQuestion.options"
              :key="oi"
              type="button"
              class="question-opt flex w-full cursor-pointer items-center gap-2 rounded-md border px-2.5 py-[7px] text-left transition-colors"
              :class="isPicked(activeQuestion.question, opt.label)
                ? 'border-[var(--chat-info)] bg-[color-mix(in_srgb,var(--chat-info)_16%,var(--bg-base))]'
                : 'border-border bg-base hover:border-[color-mix(in_srgb,var(--chat-info)_55%,transparent)]'"
              :disabled="nativeControlResponsePending"
              @click="selectQuestionOption(opt.label)"
            >
              <span class="flex min-w-0 flex-1 flex-col gap-px">
                <span class="text-xs font-semibold text-foreground">{{ opt.label }}</span>
                <span v-if="opt.description" class="text-[10px] text-secondary-foreground">{{ opt.description }}</span>
              </span>
              <PhCheck v-if="isPicked(activeQuestion.question, opt.label)" :size="13" weight="bold" class="flex-shrink-0 text-[var(--chat-info)]" />
              <kbd v-else-if="oi < 9" class="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded border border-border text-[10px] text-secondary-foreground/60">{{ oi + 1 }}</kbd>
            </button>
            <input
              type="text"
              class="question-opt-other w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-xs text-foreground outline-none placeholder:text-muted-foreground focus:border-[color-mix(in_srgb,var(--chat-info)_55%,transparent)]"
              placeholder="Or write your own answer…"
              :value="questionCustomAnswers[activeQuestion.question] ?? ''"
              :disabled="nativeControlResponsePending"
              @input="setCustomAnswer(activeQuestion.question, ($event.target as HTMLInputElement).value)"
              @keydown.enter="canAdvanceQuestion && advanceQuestion()"
            />
          </div>
          <div class="mt-2.5 flex items-center justify-between gap-2">
            <button class="perm-btn perm-deny !px-2 !py-1 text-[11px]" :disabled="nativeControlResponsePending" @click="cancelQuestion" title="Dismiss (Esc)">Skip</button>
            <div class="flex items-center gap-1.5">
              <button v-if="activeQuestionIndex > 0" class="perm-btn !px-2.5 !py-1 text-[11px]" :disabled="nativeControlResponsePending" @click="previousQuestion">Back</button>
              <button
                class="perm-btn perm-allow !px-3 !py-1 text-[11px]"
                :disabled="!canAdvanceQuestion || nativeControlResponsePending"
                @click="advanceQuestion"
              >{{ isLastQuestion ? 'Submit' : 'Next' }}</button>
            </div>
          </div>
        </div>
        <div class="relative">
          <!-- Highlight backdrop: same metrics as the textarea, renders /skill tokens as pills. -->
          <div
            v-if="hasSkillPill"
            ref="hlEl"
            aria-hidden="true"
            class="pointer-events-none absolute inset-0 box-border overflow-hidden whitespace-pre-wrap [overflow-wrap:break-word] px-3 pb-1 pt-2.5 font-sans text-[13px] leading-[1.5] text-foreground"
          ><template v-for="(p, i) in skillParts" :key="i"><span v-if="p.pill" class="skill-pill">{{ p.v }}</span><template v-else>{{ p.v }}</template></template></div>
          <textarea
            ref="inputEl"
            v-model="inputText"
            class="chat-input composer-input box-border block max-h-40 min-h-10 w-full resize-none border-none bg-transparent px-3 pb-1 pt-2.5 font-sans text-[13px] leading-[1.5] text-foreground outline-none placeholder:text-muted-foreground"
            :class="hasSkillPill && 'composer-ghost'"
            :placeholder="busy ? 'Type next message — will send when Claude finishes…' : 'Ask your agent anything...'"
            rows="1"
            @keydown="onKeydown"
            @input="onInput"
            @paste="onPaste"
            @scroll="syncHighlightScroll"
          /></div>
          <!-- Image previews, matching welcome-image-preview sizing/placement -->
          <div v-if="pendingImages.length > 0" class="flex flex-wrap gap-1.5 px-3 pb-1.5">
            <div v-for="(img, i) in pendingImages" :key="i" class="relative h-16 w-16 flex-shrink-0">
              <img :src="img" class="block h-full w-full rounded-[7px] border border-border object-cover" :alt="`Image ${i + 1}`" />
              <button class="pending-img-remove" @click="pendingImages.splice(i, 1)" :aria-label="'Remove attached image ' + (i + 1)" title="Remove">
                <PhX :size="9" weight="bold" />
              </button>
            </div>
          </div>
          <div class="flex items-center justify-between gap-1.5 px-2 pb-2 pt-1.5">
          <!-- Left: share selection, model dropdown, perm mode -->
          <div class="flex items-center gap-1">
            <img v-if="avatarSrc" :src="avatarSrc" class="toolbar-avatar mr-0.5 h-[22px] w-[22px] flex-shrink-0 rounded-full border border-border object-cover [object-position:center_18%]" alt="Manager" />
            <button
              v-if="editorCtx.selection"
              class="toolbar-btn"
              :title="`Add selection: ${relPath(editorCtx.selection.path)}#L${editorCtx.selection.startLine}-L${editorCtx.selection.endLine}`"
              @click="shareSelection"
            >
              <PhTextAa :size="13" />
            </button>
            <!-- Agent + model in one popover (the old header bar is gone) -->
            <ModelPicker
              :agent-id="agentKind"
              :model-id="effectiveTransport === 'claude-cli' ? selectedModel : acpActiveModelId"
              :models="liveModels.length ? liveModels : undefined"
              :cwd="cwd"
              @select="onPickModel"
            />
            <!-- Claude Agent SDK effort is forwarded to the local Claude Code CLI. -->
            <div v-if="effectiveTransport === 'claude-cli'" class="model-dropdown">
              <button ref="effortBtnEl" class="toolbar-btn toolbar-btn-label" title="Claude reasoning effort" @click="toggleEffortMenu">
                {{ selectedEffortLabel }}
                <PhCaretDown :size="9" weight="bold" class="btn-caret" />
              </button>
              <Teleport to="body">
                <div v-if="effortMenuOpen" ref="effortMenuEl" class="floating-menu" :style="{ top: effortMenuPos.top + 'px', left: effortMenuPos.left + 'px' }">
                  <button
                    v-for="effort in CLAUDE_EFFORTS"
                    :key="effort.id"
                    class="floating-menu-item"
                    :class="{ 'floating-menu-item-active': selectedEffort === effort.id }"
                    @click="selectEffort(effort.id)"
                  >
                    {{ effort.label }}
                  </button>
                </div>
              </Teleport>
            </div>
            <!-- Profile switcher (only shown when more than one profile exists) -->
            <div v-if="effectiveTransport === 'claude-cli' && claudeProfiles.length > 1" class="model-dropdown">
              <button
                ref="profileBtnEl"
                class="toolbar-btn toolbar-btn-label"
                :class="{ 'btn-active': selectedProfileId !== defaultProfileId }"
                :title="selectedProfile?.configDir ? `CLAUDE_CONFIG_DIR: ${selectedProfile.configDir}` : 'Claude profile'"
                @click="toggleProfileMenu"
              >
                <PhUserGear :size="12" />
                {{ selectedProfile?.name ?? 'Default' }}
                <PhCaretDown :size="9" weight="bold" class="btn-caret" />
              </button>
              <Teleport to="body">
                <div
                  v-if="profileMenuOpen"
                  ref="profileMenuEl"
                  class="floating-menu"
                  :style="{ bottom: profileMenuPos.bottom + 'px', left: profileMenuPos.left + 'px' }"
                >
                  <button
                    v-for="p in claudeProfiles"
                    :key="p.id"
                    class="floating-menu-item"
                    :class="{ 'floating-menu-item-active': selectedProfileId === p.id }"
                    @click="selectProfile(p.id)"
                  >
                    {{ p.name }}
                    <span v-if="p.configDir" class="model-id-hint">{{ p.configDir }}</span>
                  </button>
                </div>
              </Teleport>
            </div>
            <!-- Permission mode switcher (native Claude only) -->
            <div v-if="effectiveTransport === 'claude-cli'" class="perm-mode-dropdown">
              <button
                ref="permBtnEl"
                class="toolbar-btn"
                :class="{ 'btn-danger-active': permMeta.danger, 'btn-active': permMode === 'acceptEdits' }"
                :title="permMeta.title"
                @click="togglePermMenu"
              >
                <component :is="PERM_ICON[permMode]" :size="15" weight="bold" />
                <span class="perm-mode-label">{{ permMeta.label }}</span>
                <PhCaretDown :size="9" weight="bold" class="perm-mode-caret" />
              </button>
              <!-- Teleported to body so the float-card's `overflow:hidden` can't clip it. -->
              <Teleport to="body">
                <div
                  v-if="permMenuOpen"
                  ref="permMenuEl"
                  class="perm-mode-menu"
                  :style="{ top: permMenuPos.top + 'px', left: permMenuPos.left + 'px' }"
                >
                  <button
                    v-for="m in PERM_MODES"
                    :key="m"
                    class="perm-mode-item"
                    :class="{ 'perm-mode-item-active': permMode === m, 'perm-mode-item-danger': PERM_META[m].danger }"
                    :title="PERM_META[m].title"
                    @click="selectPermMode(m)"
                  >
                    <component :is="PERM_ICON[m]" :size="16" weight="bold" />
                    <span class="perm-mode-copy">
                      <span>{{ PERM_META[m].label }}</span>
                      <span>{{ PERM_META[m].description }}</span>
                    </span>
                  </button>
                </div>
              </Teleport>
            </div>

            <!-- ACP model switcher (driven by the adapter's configOptions) -->
            <div v-if="isAcpRuntime && acpEffortOption" class="model-dropdown">
              <button ref="acpEffortBtnEl" class="toolbar-btn toolbar-btn-label" @click="openAcpMenu('effort')">
                {{ acpEffortLabel }}
                <PhCaretDown :size="9" weight="bold" class="btn-caret" />
              </button>
              <Teleport to="body">
                <div v-if="acpEffortMenuOpen" ref="acpEffortMenuEl" class="floating-menu" :style="{ top: acpEffortMenuPos.top + 'px', left: acpEffortMenuPos.left + 'px' }">
                  <button
                    v-for="c in acpEffortOption.options"
                    :key="c.value"
                    class="floating-menu-item"
                    :class="{ 'floating-menu-item-active': acpEffortOption.currentValue === c.value }"
                    :title="c.description"
                    @click="acpSelectEffort(c.value)"
                  >
                    {{ c.name }}
                  </button>
                </div>
              </Teleport>
            </div>

            <!-- ACP permission-mode switcher (driven by the adapter's session modes) -->
            <div v-if="isAcpRuntime && acpModes" class="perm-mode-dropdown">
              <button ref="acpModeBtnEl" class="toolbar-btn" :title="`Permission mode: ${acpModeLabel}`" @click="openAcpMenu('mode')">
                <PhShieldCheck :size="15" weight="bold" />
                <span class="perm-mode-label">{{ acpModeLabel }}</span>
                <PhCaretDown :size="9" weight="bold" class="perm-mode-caret" />
              </button>
              <Teleport to="body">
                <div v-if="acpModeMenuOpen" ref="acpModeMenuEl" class="perm-mode-menu" :style="{ top: acpModeMenuPos.top + 'px', left: acpModeMenuPos.left + 'px' }">
                  <button
                    v-for="m in acpModes.availableModes"
                    :key="m.id"
                    class="perm-mode-item"
                    :class="{ 'floating-menu-item-active': acpModes.currentModeId === m.id }"
                    :title="m.description"
                    @click="acpSelectMode(m.id)"
                  >
                    <component :is="acpModeIcon(m.id)" :size="16" weight="bold" />
                    <span class="perm-mode-copy">
                      <span>{{ m.name }}</span>
                      <span>{{ m.description }}</span>
                    </span>
                  </button>
                </div>
              </Teleport>
            </div>
          </div>

          <!-- Right: cost badge + abort/send -->
          <div class="flex items-center gap-1.5">
            <span v-if="sessionCost > 0 && !busy" class="px-1 font-mono text-[10px] text-muted-foreground">${{ sessionCost.toFixed(4) }}</span>
            <button
              v-if="busy"
              class="send-btn send-btn-abort"
              :class="{ 'send-btn-stalled': stalled }"
              :title="stalled ? 'No response for a while — looks stuck. Click to restart (Esc)' : 'Abort (Esc)'"
              @click="abortTurn"
            >
              <PhStop :size="14" weight="bold" />
            </button>
            <button
              v-else-if="messageQueue.length > 0"
              class="send-btn"
              disabled
              :title="`${messageQueue.length} message${messageQueue.length > 1 ? 's' : ''} queued`"
            >
              {{ messageQueue.length }}
            </button>
            <button v-else class="send-btn" :disabled="!inputText.trim()" @click="sendMessage()">
              <PhArrowUp :size="14" weight="bold" />
            </button>
          </div>
        </div>
      </div>
      <WorkspaceTargetPicker
        :mode="chatWorkspace?.parent_id ? 'new' : 'current'"
        :current-branch="chatBranch"
        :detail="chatWorkspace?.worktree_branch || undefined"
        appearance="attached"
        wide
        readonly
      />
      </div>

      <!-- Context usage bar -->
      <div v-if="contextUsageRatio > 0" class="h-0.5 overflow-hidden bg-hover" :title="`${turnStats?.inputTokens.toLocaleString()} / ${CONTEXT_MAX.toLocaleString()} tokens`">
        <div class="ctx-usage-bar h-full rounded-[1px] transition-[width]" :class="contextUsageClass" :style="{ width: (contextUsageRatio * 100) + '%' }" />
      </div>

      <!-- Status line below input — hidden when nothing to show -->
      <div v-show="fiveHourWindow" class="relative z-[1] flex flex-shrink-0 items-center gap-2 border-t border-border px-2.5 py-[3px] min-h-[22px]">
        <span v-if="fiveHourWindow" class="whitespace-nowrap font-mono text-[10px] text-muted-foreground" :title="'5h usage window'">5h: {{ fiveHourWindow }}</span>
        <span class="flex-1" />
        <span v-if="turnStats" class="whitespace-nowrap font-mono text-[10px] text-muted-foreground opacity-70">
          {{ turnStats.inputTokens.toLocaleString() }}↑ {{ turnStats.outputTokens.toLocaleString() }}↓
        </span>
      </div>
    </div>
    </div><!-- end .chat-main -->

  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick, onMounted, onBeforeUnmount, watch } from "vue";
import { PhArrowDown, PhArrowUp, PhWrench, PhStop, PhShieldWarning, PhShieldCheck, PhPencilSimple, PhGitDiff, PhListChecks, PhTextAa, PhCaretDown, PhCaretRight, PhX, PhUserGear, PhClock, PhSparkle, PhFastForward, PhFileText, PhTerminalWindow, PhMagnifyingGlass, PhGlobe, PhRobot, PhWarningCircle, PhCopy, PhCheck, PhImage } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { parseAcpPermRequest } from "@/lib/acpParser";
import {
  applyChatEvent, isProjectedEvent, settleTranscript,
  type ChatEventBatch, type ChatProjectionState,
} from "@/lib/chatProjection";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useSubagentsStore } from "@/stores/subagents";
import { useNotificationsStore } from "@/stores/notifications";
import { useEditorContextStore } from "@/stores/editorContext";
import { useScriptsStore } from "@/stores/scripts";
import { useGitStore } from "@/stores/git";
import { useWorkspaceStore } from "@/stores/workspace";
import { useProvidersStore, chatTransportFor, binaryFor, type ChatTransport } from "@/stores/providers";
import { agentIconComp } from "@/lib/agentIcons";
import ModelPicker from "@/components/ModelPicker.vue";
import WorkspaceTargetPicker from "@/components/WorkspaceTargetPicker.vue";
import CodexUserInputPanel, { type CodexUserInputQuestion } from "@/components/CodexUserInputPanel.vue";
import { chatSession, replayChatStream } from "@/lib/chatSession";
import type { AcpConfigOption, AcpModes, CanUseToolReq, ChatMessage } from "@/lib/chatTypes";
import { modelsFor, learnModels, type ModelEntry } from "@/lib/chatModels";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import { playSound } from "@/lib/sounds";
import { splitSkillTokens } from "@/lib/skillTokens";
import { chatSettingKey } from "@/lib/chatSettings";
import { splitMentions } from "@/lib/mentionTokens";
import { editOf, fmtDuration, mergeEdits, type FileEdit } from "@/lib/chatTurns";
import { notifyNtfy } from "@/lib/ntfy";
import { useUIStore, type NtfyEvent } from "@/stores/ui";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

function renderMd(text: string): string {
  return DOMPurify.sanitize(marked.parse(text) as string);
}

// A sent message is rendered through markdown, exactly like an assistant one —
// it used to be interpolated as flat text, so every list, fence and even plain
// line break the user typed collapsed into one blob. `breaks` is on because in
// a chat composer Enter means "new line", not "same paragraph" (t3code renders
// every message, whatever the role, through the same markdown formatter).
function renderUserMd(text: string): string {
  const html = DOMPurify.sanitize(marked.parse(text, { breaks: true, gfm: true }) as string);
  return pillifyMentions(html);
}

// `@path` → pill. Done as a DOM walk over the ALREADY-sanitized output instead
// of a regex over the HTML string: a regex would also rewrite text inside
// attributes and code fences. Skipping pre/code/a leaves a literal `@foo` in a
// code block alone, which is what someone quoting code means.
const MENTION_SKIP = new Set(["PRE", "CODE", "A"]);
function pillifyMentions(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
  const targets: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const node = n as Text;
    if (!node.data.includes("@")) continue;
    let el: HTMLElement | null = node.parentElement;
    let skip = false;
    for (; el && el !== doc.body; el = el.parentElement) {
      if (MENTION_SKIP.has(el.tagName)) { skip = true; break; }
    }
    if (!skip) targets.push(node);
  }
  for (const node of targets) {
    const parts = splitMentions(node.data);
    if (!parts.some((p) => p.mention)) continue;
    const frag = doc.createDocumentFragment();
    for (const part of parts) {
      if (!part.mention) { frag.append(doc.createTextNode(part.v)); continue; }
      const pill = doc.createElement("span");
      pill.className = "mention-pill";
      pill.textContent = part.v.slice(1); // drop the leading @ — the pill icon says "file"
      frag.append(pill);
    }
    node.replaceWith(frag);
  }
  return doc.body.innerHTML;
}

const props = defineProps<{
  chatId: number;
  workspaceId: number;
  cwd: string;
  // Compact mode (float chat): hide the heavy chrome (changes panel + diff
  // sidebar), keep the message stream + input + inline permission gates.
  compact?: boolean;
  // Mission-control primer passed to claude_start as --append-system-prompt.
  appendSystemPrompt?: string;
  // Hide the built-in text composer — the host (e.g. the Manager bar) drives
  // sends from its own external input via the exposed sendMessage(). Permission
  // / plan / question gates stay visible.
  hideComposer?: boolean;
  // Optional avatar shown at the start of the composer's bottom toolbar row
  // (used by the Manager bar to give the agent a face).
  avatarSrc?: string;
  // Use a dedicated localStorage key for the model selection instead of the
  // shared global one, so this chat's model is independent of every other chat
  // (the Manager keeps its own model). Falls back to the global key.
  modelKey?: string;
  // Initial model when nothing is stored under modelKey yet.
  defaultModel?: string;
  // Optional runtime override, otherwise the selected agent defines it.
  transport?: ChatTransport;
  // Which agent to run — a chatAgents store id (default 'claude').
  agentKind?: string;
  // Whether this chat's tab is actually the one on screen (its workspace active,
  // terminal mode, this tab selected) — passed by Terminal.vue via its isWatching()
  // helper. Callers that don't track tab-level visibility (float chat, Manager bar)
  // omit it and fall back to plain window focus.
  isWatching?: boolean;
  // First message to send automatically once this chat's runtime is up (used by
  // the welcome-screen composer, which creates the chat and its prompt at once).
  initialPrompt?: string;
  // Images paired with the first prompt from the welcome-screen composer.
  initialImages?: string[];
}>();

const emit = defineEmits<{ (e: "prompt-sent"): void }>();

const chats = useClaudeChatsStore();
const subagents = useSubagentsStore();
const workspaces = useWorkspaceStore();
const git = useGitStore();
const notifStore = useNotificationsStore();
const uiStore = useUIStore();
const scriptsStore = useScriptsStore();
const chatAgents = useProvidersStore();
const editorCtx = useEditorContextStore();
const chatWorkspace = computed(() => workspaces.workspaces.find((workspace) => workspace.id === props.workspaceId));
const chatBranch = computed(() => chatWorkspace.value?.worktree_branch || git.branchByWs[props.workspaceId] || "HEAD");

// Local mirror of the session's agentKind (a chatAgents id), drives the switcher.
const agentKind = ref<string>(
  chats.sessions.find((s) => s.id === props.chatId)?.agentKind ?? props.agentKind ?? 'claude'
);
// The resolved agent definition from the registry.
const currentAgent = computed(() => chatAgents.resolve(agentKind.value));
const currentAgentIcon = computed(() => agentIconComp(currentAgent.value?.icon));
const effectiveTransport = computed<ChatTransport>(() =>
  props.transport ?? (currentAgent.value ? chatTransportFor(currentAgent.value) : 'claude-cli')
);
const usesRpcRuntime = computed(() => effectiveTransport.value !== 'claude-cli');
// Codex's app-server bridge reports the same modes/configOptions shape, so
// everything ACP-flavoured (effort, mode, history) applies to it too.
const isAcpRuntime = computed(() => effectiveTransport.value !== 'claude-cli');
const runtimeLabel = computed(() =>
  effectiveTransport.value === 'codex-app-server' ? 'Codex app-server' : effectiveTransport.value === 'acp' ? 'ACP adapter' : 'Claude CLI'
);
// Per-agent accent color.
const agentAccentColor = computed(() =>
  agentKind.value === 'claude' ? 'var(--chat-accent)' : (currentAgent.value?.color ?? 'var(--chat-accent)'),
);
// Rich ACP permission request — carries the adapter's own option list (allow/
// reject variants, or ExitPlanMode's auto/acceptEdits/manual/keep-planning) so
// we render the REAL choices instead of collapsing everything to Allow/Deny.
// The chat's stream and everything the stream mutates live in the session, not
// in this component — so a running turn keeps arriving while nobody is looking
// at it. See lib/chatSession.ts. Parents key <AgentChat> by chatId, so this
// handle stays correct for the component's whole life.
const S = chatSession(props.chatId);
const {
  messages, busy, lastActivityAt, turnStartedAt, messageQueue, suppressNextDone,
  sessionId, turnStats, sessionCost, runtimeStarted,
  pendingPermission, pendingQuestion, pendingPlan, pendingDiff,
  pendingPermissionMsgId, pendingQuestionMsgId, pendingPlanMsgId, pendingDiffMsgId,
  settledControlRequestIds,
  acpPermReq, acpPermRpcId, acpPermMsgId, acpPromptRpcId, acpControlIds, acpModes, acpConfigOptions,
  enqueueMessage, removeQueuedMessage, clearQueuedMessages, moveQueuedMessageNext, takeNextQueuedMessage, restoreQueuedMessages,
} = S;
const permissionResponsePending = ref(false);
const codexUserInput = ref<{ rpcId: number; questions: CodexUserInputQuestion[] } | null>(null);
const codexUserInputPending = ref(false);
// ExitPlanMode arrives as a permission request with the plan in rawInput.plan.
const acpPermPlan = computed(() => {
  const p = acpPermReq.value?.rawInput?.plan;
  return typeof p === "string" && p.trim() ? renderMd(p) : "";
});
// Edit/Write arrive with file_path + old/new (or content) — show the diff inline.
const acpPermDiff = computed(() => {
  const r = acpPermReq.value;
  if (!r) return null;
  const i = r.rawInput;
  const path = (i.file_path ?? i.path) as string | undefined;
  const hasEdit = i.old_string !== undefined || i.new_string !== undefined;
  const hasWrite = i.content !== undefined && !hasEdit;
  if (!path || (r.kind !== "edit" && !hasEdit && !hasWrite)) return null;
  return {
    path,
    isWrite: hasWrite,
    content: String(i.content ?? ""),
    oldStr: String(i.old_string ?? ""),
    newStr: String(i.new_string ?? ""),
  };
});
// Icon class by option kind (allow → green, reject → red, else neutral).
function acpOptClass(kind: string) {
  if (kind.startsWith("allow")) return "perm-allow";
  if (kind.startsWith("reject")) return "perm-deny";
  return "perm-neutral";
}

// ── ACP session state (model + permission mode + resume) ──────────────────────
// JSON-RPC id of the in-flight session/prompt — correlates the turn-done response.
// rpc ids of in-flight control calls (set_mode/set_config/list) → refresh UI on reply.
// Legacy per-chat localStorage key prefixes (kept only for the one-time migration below).
type AcpChatSettings = { mode?: string; model?: string; effort?: string };
function getAcpSetting(cid: number, field: keyof AcpChatSettings): string | undefined {
  const rec = getConfig<Record<string, AcpChatSettings>>("chatAcpSettings", {});
  return rec[String(cid)]?.[field];
}
function setAcpSetting(cid: number, field: keyof AcpChatSettings, value: string) {
  const rec = { ...getConfig<Record<string, AcpChatSettings>>("chatAcpSettings", {}) };
  rec[String(cid)] = { ...rec[String(cid)], [field]: value };
  setConfig("chatAcpSettings", rec);
  // Also remember it per agent kind, so a brand-new chat starts where the last
  // one left off instead of at the adapter's default.
  const last = { ...getConfig<Record<string, AcpChatSettings>>("chatAcpLast", {}) };
  last[agentKind.value] = { ...last[agentKind.value], [field]: value };
  setConfig("chatAcpLast", last);
}
function lastAcpSetting(field: keyof AcpChatSettings): string | undefined {
  return getConfig<Record<string, AcpChatSettings>>("chatAcpLast", {})[agentKind.value]?.[field];
}
const acpModelOption = computed(() => acpConfigOptions.value.find((o) => o.id === "model"));
const acpEffortOption = computed(() => acpConfigOptions.value.find((o) => o.id === "effort"));
const acpModeLabel = computed(() => acpModes.value?.availableModes.find((m) => m.id === acpModes.value?.currentModeId)?.name ?? "Mode");
const acpActiveModelId = computed(() => acpModelOption.value?.currentValue ?? "");
const acpEffortLabel = computed(() => { const o = acpEffortOption.value; return o?.options.find((c) => c.value === o.currentValue)?.name ?? "Effort"; });

const acpModeMenuOpen = ref(false);
const acpModeBtnEl = ref<HTMLElement | null>(null);
const acpModeMenuEl = ref<HTMLElement | null>(null);
const acpModeMenuPos = ref({ top: 0, left: 0 });
const acpModelMenuOpen = ref(false);
const acpModelBtnEl = ref<HTMLElement | null>(null);
const acpModelMenuEl = ref<HTMLElement | null>(null);
const acpModelMenuPos = ref({ top: 0, left: 0 });
const acpEffortMenuOpen = ref(false);
const acpEffortBtnEl = ref<HTMLElement | null>(null);
const acpEffortMenuEl = ref<HTMLElement | null>(null);
const acpEffortMenuPos = ref({ top: 0, left: 0 });

function openAcpMenu(which: "mode" | "model" | "effort") {
  const btn = which === "mode" ? acpModeBtnEl.value : which === "effort" ? acpEffortBtnEl.value : acpModelBtnEl.value;
  const openRef = which === "mode" ? acpModeMenuOpen : which === "effort" ? acpEffortMenuOpen : acpModelMenuOpen;
  const posRef = which === "mode" ? acpModeMenuPos : which === "effort" ? acpEffortMenuPos : acpModelMenuPos;
  const count = which === "mode" ? (acpModes.value?.availableModes.length ?? 0) : which === "effort" ? (acpEffortOption.value?.options.length ?? 0) : (acpModelOption.value?.options.length ?? 0);
  const rowHeight = which === "mode" ? 64 : 36;
  if (!openRef.value && btn) {
    const r = btn.getBoundingClientRect();
    posRef.value = { top: Math.max(8, Math.round(r.top - (count * rowHeight + 12) - 6)), left: Math.round(r.left) };
  }
  openRef.value = !openRef.value;
}
function onAcpMenuOutside(e: MouseEvent) {
  const t = e.target as Node;
  if (acpModeMenuOpen.value && !acpModeBtnEl.value?.contains(t) && !acpModeMenuEl.value?.contains(t)) acpModeMenuOpen.value = false;
  if (acpModelMenuOpen.value && !acpModelBtnEl.value?.contains(t) && !acpModelMenuEl.value?.contains(t)) acpModelMenuOpen.value = false;
  if (acpEffortMenuOpen.value && !acpEffortBtnEl.value?.contains(t) && !acpEffortMenuEl.value?.contains(t)) acpEffortMenuOpen.value = false;
}
// Request ids of OUR OWN restore pushes. The reply to a restore carries the
// adapter's selector set again, so re-restoring from it is what would ping-pong
// forever — skipping just those replies breaks the loop, while every OTHER
// reset (session start, or the reply to a user's mode/effort/model pick) still
// gets repaired. The old "remember the value we last pushed" guard skipped the
// SECOND reset too, which silently flipped the model back to the adapter's
// default after a permission-mode or effort switch.
const acpRestorePushIds = new Set<number>();

async function acpSelectMode(modeId: string, userPick = true) {
  acpModeMenuOpen.value = false;
  if (acpModes.value) acpModes.value.currentModeId = modeId;
  setAcpSetting(props.chatId, "mode", modeId);
  try {
    const rid = await invoke<number>("acp_set_mode", { id: props.chatId, modeId });
    acpControlIds.add(rid);
    if (!userPick) acpRestorePushIds.add(rid);
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Failed to set mode: ${e}` });
  }
}
async function acpSelectModel(value: string, userPick = true) {
  acpModelMenuOpen.value = false;
  if (acpModelOption.value) acpModelOption.value.currentValue = value;
  setAcpSetting(props.chatId, "model", value);
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "model", value });
    acpControlIds.add(rid);
    if (!userPick) acpRestorePushIds.add(rid);
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Failed to set model: ${e}` });
  }
}
async function acpSelectEffort(value: string, userPick = true) {
  acpEffortMenuOpen.value = false;
  if (acpEffortOption.value) acpEffortOption.value.currentValue = value;
  setAcpSetting(props.chatId, "effort", value);
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "effort", value });
    acpControlIds.add(rid);
    if (!userPick) acpRestorePushIds.add(rid);
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Failed to set effort: ${e}` });
  }
}

// Re-apply this chat's model / permission mode / effort. The adapter resets its
// selectors to defaults on (re)start AND in the reply to a model switch, which
// is what used to silently drop the effort and permission mode the user picked.
function restoreAcpSelections() {
  // Fallback is the per-agent-kind memory setAcpSetting writes ("chatAcpLast");
  // the old code read a "chatAcpLastModel" key that nothing ever wrote.
  const savedModel = getAcpSetting(props.chatId, "model") ?? lastAcpSetting("model");
  const modelOffered = acpModelOption.value?.options.some((o) => o.value === savedModel);
  if (savedModel && modelOffered && acpModelOption.value && acpModelOption.value.currentValue !== savedModel) {
    acpSelectModel(savedModel, false);
  }
  const savedMode = getAcpSetting(props.chatId, "mode") ?? lastAcpSetting("mode");
  const modeOffered = acpModes.value?.availableModes.some((m) => m.id === savedMode);
  if (savedMode && modeOffered && acpModes.value && acpModes.value.currentModeId !== savedMode) {
    acpSelectMode(savedMode, false);
  }
  // Only push a value the adapter actually offers — a saved effort can be stale
  // after a model switch (Codex publishes its efforts per model).
  const savedEffort = getAcpSetting(props.chatId, "effort") ?? lastAcpSetting("effort");
  const effortOffered = acpEffortOption.value?.options.some((o) => o.value === savedEffort);
  if (savedEffort && effortOffered && acpEffortOption.value && acpEffortOption.value.currentValue !== savedEffort) {
    acpSelectEffort(savedEffort, false);
  }
}

// Agent switcher dropdown.
async function selectAgent(id: string) {
  if (id === agentKind.value) return;
  // Stop OLD process before agentKind changes (effectiveTransport depends on it).
  await (usesRpcRuntime.value ? stopRpcRuntime() : invoke('claude_stop', { id: props.chatId })).catch(() => {});
  agentKind.value = id;
  chats.sync(props.chatId, { agentKind: id, transport: currentAgent.value ? chatTransportFor(currentAgent.value) : 'claude-cli' });
  await clearChat();
}

// Build the acp_start invoke payload from the current agent + per-project settings.
function acpStartPayload(emitHistory = false) {
  const a = currentAgent.value;
  const proj = scriptsStore.settingsFor(props.cwd);
  return {
    emitHistory,
    id: props.chatId,
    cwd: props.cwd,
    command: a ? binaryFor(a) || "npx" : "npx",
    args: a?.transportArgs ?? [],
    env: a?.env ?? {},
    kind: a?.kind ?? "custom",
    configDir: proj.claude_config_dir || a?.env?.CLAUDE_CONFIG_DIR || null,
    envFile: proj.env_file || null,
    // Resume the chat's prior ACP session (server-side history) when we have its id.
    resumeSessionId: sessionId.value || null,
  };
}

async function startRpcRuntime(emitHistory = false) {
  await ensureAcpListeners();
  if (effectiveTransport.value === "codex-app-server") {
    const agent = currentAgent.value;
    return invoke("codex_start", {
      id: props.chatId,
      cwd: props.cwd,
      env: agent?.env ?? {},
      resumeSessionId: sessionId.value || null,
    });
  }
  return invoke("acp_start", acpStartPayload(emitHistory));
}

function stopRpcRuntime() {
  return invoke(effectiveTransport.value === "codex-app-server" ? "codex_stop" : "acp_stop", { id: props.chatId });
}

function sendRpcRuntime(text: string, images?: string[]) {
  return invoke<number>(effectiveTransport.value === "codex-app-server" ? "codex_send" : "acp_send", { id: props.chatId, text, images });
}

// Relative-to-cwd path for a shared selection's @-reference.
function relPath(abs: string): string {
  if (props.cwd && abs.startsWith(props.cwd + "/")) return abs.slice(props.cwd.length + 1);
  return abs.split("/").pop() ?? abs;
}

// Insert the current editor selection as a fenced context block + @file#range header.
function shareSelection() {
  const sel = editorCtx.selection;
  if (!sel) return;
  const ref = `@${relPath(sel.path)}#L${sel.startLine}-L${sel.endLine}`;
  const block = `${ref}\n\`\`\`\n${sel.text}\n\`\`\`\n`;
  inputText.value = inputText.value ? `${inputText.value}\n${block}` : block;
  nextTick(() => { inputEl.value?.focus(); autoResize(); });
}

// Profile switcher — the Claude provider instances (each one is a config dir).
const claudeProfiles = computed(() => chatAgents.active.filter((a) => a.providerId === "claude"));
const defaultProfileId = computed(() => claudeProfiles.value[0]?.id ?? "claude");
// Legacy per-chat localStorage key prefix (kept only for the one-time migration below).
function loadProfileId(id: number): string {
  const rec = getConfig<Record<string, string>>("chatProfileSelection", {});
  return rec[String(id)] ?? defaultProfileId.value;
}
function saveProfileId(id: number, profileId: string) {
  const rec = { ...getConfig<Record<string, string>>("chatProfileSelection", {}) };
  rec[String(id)] = profileId;
  setConfig("chatProfileSelection", rec);
}
const selectedProfileId = ref<string>(loadProfileId(props.chatId));
const selectedProfile = computed(() => chatAgents.byId(selectedProfileId.value) ?? claudeProfiles.value[0]);
const profileMenuOpen = ref(false);
const profileBtnEl = ref<HTMLElement | null>(null);
const profileMenuEl = ref<HTMLElement | null>(null);
const profileMenuPos = ref({ bottom: 0, left: 0 });
function toggleProfileMenu() {
  if (!profileMenuOpen.value && profileBtnEl.value) {
    const r = profileBtnEl.value.getBoundingClientRect();
    profileMenuPos.value = { bottom: Math.round(window.innerHeight - r.top + 4), left: Math.round(r.left) };
  }
  profileMenuOpen.value = !profileMenuOpen.value;
}
function onProfileMenuOutside(e: MouseEvent) {
  if (!profileMenuOpen.value) return;
  const t = e.target as Node;
  if (profileBtnEl.value?.contains(t) || profileMenuEl.value?.contains(t)) return;
  profileMenuOpen.value = false;
}
async function selectProfile(id: string) {
  profileMenuOpen.value = false;
  if (id === selectedProfileId.value) return;
  selectedProfileId.value = id;
  saveProfileId(props.chatId, id);
  await restartClaude();
}

// One popover drives both provider and model. Switching provider restarts the
// chat under the new agent (selectAgent); a same-provider pick just swaps model.
// Models this agent reported at runtime; empty for the native Claude CLI, which
// has a static catalog.
const liveModels = computed<ModelEntry[]>(() =>
  (acpModelOption.value?.options ?? []).map((o) => ({ id: o.value, label: o.name || o.value })),
);

function onPickModel(agentId: string, modelId: string) {
  if (agentId !== agentKind.value) {
    if (modelId && agentId === "claude") selectModel(modelId);
    selectAgent(agentId);
    return;
  }
  if (effectiveTransport.value === "claude-cli") selectModel(modelId);
  else if (modelId) acpSelectModel(modelId);
}

// Model switcher — ids come from the shared catalog (src/lib/chatModels.ts).
const CLAUDE_MODELS = modelsFor("claude");
type ClaudeModelId = string;
// Legacy localStorage key (kept only for the one-time migration below).
const MODEL_KEY = props.modelKey ?? "burrow.claude.model";
// Config key: dedicated per-modelKey key (mirrors the old dedicated-localStorage-key
// behavior) so a future caller passing modelKey still gets an isolated selection;
// with no modelKey it collapses to the single global "chatLastUsedModel" key.
const MODEL_CONFIG_KEY = props.modelKey ? `chatLastUsedModel:${props.modelKey}` : "chatLastUsedModel";
// Per-chat model, keyed by chatId (same shape as chatPermissionMode.byChat).
// A chat's model is resolved ONCE — on first mount, from the last-used /
// default — and then belongs to that chat: another chat picking a different
// model, or a default appearing later, must never move it. Without this the
// only store was the shared last-used key, so remounting a chat (workspace
// reopen, app restart) silently re-seeded it from whatever was picked last.
const MODEL_BY_CHAT_KEY = "chatModelByChat";
const chatSettingId = computed(() => chatSettingKey(props.chatId, props.modelKey));
function storedChatModel(): ClaudeModelId | null {
  const v = getConfig<Record<string, string>>(MODEL_BY_CHAT_KEY, {})[chatSettingId.value];
  return CLAUDE_MODELS.some((m) => m.id === v) ? (v as ClaudeModelId) : null;
}
/** The model this chat starts with: its own pick, else last-used, else the
 *  caller's default, else the catalog head. */
function loadModel(): ClaudeModelId {
  const own = storedChatModel();
  if (own) return own;
  const v = getConfig<string | null>(MODEL_CONFIG_KEY, null);
  if (CLAUDE_MODELS.some((m) => m.id === v)) return v as ClaudeModelId;
  if (props.defaultModel && CLAUDE_MODELS.some((m) => m.id === props.defaultModel)) {
    return props.defaultModel as ClaudeModelId;
  }
  return CLAUDE_MODELS[0].id;
}
function saveChatModel(id: ClaudeModelId) {
  const rec = { ...getConfig<Record<string, string>>(MODEL_BY_CHAT_KEY, {}) };
  rec[chatSettingId.value] = id;
  setConfig(MODEL_BY_CHAT_KEY, rec);
}
const selectedModel = ref<ClaudeModelId>(loadModel());
async function selectModel(id: ClaudeModelId) {
  if (id === selectedModel.value) return;
  selectedModel.value = id;
  saveChatModel(id);
  setConfig(MODEL_CONFIG_KEY, id); // seed for the NEXT new chat only
  await restartClaude();
}

// Mirror the live model into the session so the Sidebar can badge the thread
// with what's actually running it.
watch(
  () => (effectiveTransport.value === "claude-cli" ? selectedModel.value : acpActiveModelId.value),
  (m) => { if (m) chats.setModel(props.chatId, m); },
  { immediate: true },
);

const CLAUDE_EFFORTS = [
  { id: "low", label: "Low effort" },
  { id: "medium", label: "Medium effort" },
  { id: "high", label: "High effort" },
  { id: "xhigh", label: "Extra high" },
  { id: "max", label: "Max effort" },
] as const;
type ClaudeEffort = typeof CLAUDE_EFFORTS[number]["id"];
const EFFORT_CONFIG_KEY = props.modelKey ? `chatClaudeEffort:${props.modelKey}` : "chatClaudeEffort";
// Per-chat effort, keyed like the model (see MODEL_BY_CHAT_KEY): resolved once
// from the last-used value, then owned by this chat.
const EFFORT_BY_CHAT_KEY = "chatEffortByChat";
function isEffort(v: unknown): v is ClaudeEffort {
  return CLAUDE_EFFORTS.some((option) => option.id === v);
}
function storedChatEffort(): ClaudeEffort | null {
  const v = getConfig<Record<string, string>>(EFFORT_BY_CHAT_KEY, {})[chatSettingId.value];
  return isEffort(v) ? v : null;
}
function loadEffort(): ClaudeEffort {
  const own = storedChatEffort();
  if (own) return own;
  const saved = getConfig<string | null>(EFFORT_CONFIG_KEY, null);
  return isEffort(saved) ? saved : "high";
}
function saveChatEffort(effort: ClaudeEffort) {
  const rec = { ...getConfig<Record<string, string>>(EFFORT_BY_CHAT_KEY, {}) };
  rec[chatSettingId.value] = effort;
  setConfig(EFFORT_BY_CHAT_KEY, rec);
}
const selectedEffort = ref<ClaudeEffort>(loadEffort());
const selectedEffortLabel = computed(() => CLAUDE_EFFORTS.find((option) => option.id === selectedEffort.value)?.label ?? "High effort");
const effortMenuOpen = ref(false);
const effortBtnEl = ref<HTMLElement | null>(null);
const effortMenuEl = ref<HTMLElement | null>(null);
const effortMenuPos = ref({ top: 0, left: 0 });
function toggleEffortMenu() {
  if (!effortMenuOpen.value && effortBtnEl.value) {
    const r = effortBtnEl.value.getBoundingClientRect();
    effortMenuPos.value = { top: Math.round(r.top - CLAUDE_EFFORTS.length * 36 - 18), left: Math.round(r.left) };
  }
  effortMenuOpen.value = !effortMenuOpen.value;
}
function onEffortMenuOutside(e: MouseEvent) {
  if (!effortMenuOpen.value) return;
  const t = e.target as Node;
  if (effortBtnEl.value?.contains(t) || effortMenuEl.value?.contains(t)) return;
  effortMenuOpen.value = false;
}
async function selectEffort(effort: ClaudeEffort) {
  effortMenuOpen.value = false;
  if (effort === selectedEffort.value) return;
  selectedEffort.value = effort;
  saveChatEffort(effort);
  setConfig(EFFORT_CONFIG_KEY, effort); // seed for the NEXT new chat only
  await restartClaude();
}


// Per-tool icon + one-line human summary for the compact tool-call row.
const TOOL_ICONS: Record<string, unknown> = {
  Read: PhFileText, Edit: PhPencilSimple, Write: PhPencilSimple, MultiEdit: PhPencilSimple,
  Bash: PhTerminalWindow, Grep: PhMagnifyingGlass, Glob: PhMagnifyingGlass,
  TodoWrite: PhListChecks, WebFetch: PhGlobe, WebSearch: PhGlobe, Task: PhRobot,
};
function toolIcon(name: string): unknown {
  return TOOL_ICONS[name] ?? PhWrench;
}
const IMAGE_PATH_RE = /\.(png|jpe?g|gif|webp|svg|bmp|avif)$/i;
function toolIconFor(msg: ChatMessage): unknown {
  // "Agent looked at an image" reads as its own thing, whatever tool did it.
  const fp = msg.toolInput?.file_path ?? msg.toolInput?.path;
  if (typeof fp === "string" && IMAGE_PATH_RE.test(fp)) return PhImage;
  if (msg.toolRawName) return toolIcon(msg.text);
  const t = msg.text.toLowerCase();
  if (t.startsWith("read")) return PhFileText;
  if (t.startsWith("edit") || t.startsWith("write") || t.startsWith("creat")) return PhPencilSimple;
  if (t.startsWith("run") || t.startsWith("execut") || t.startsWith("bash") || t.startsWith("$")) return PhTerminalWindow;
  if (t.startsWith("search") || t.startsWith("find") || t.startsWith("grep") || t.startsWith("glob")) return PhMagnifyingGlass;
  if (t.startsWith("fetch") || t.startsWith("http")) return PhGlobe;
  return PhWrench;
}
function basename(p: unknown): string {
  if (typeof p !== "string" || !p) return "";
  return p.split("/").filter(Boolean).pop() ?? p;
}
function toolSummary(name: string, input: Record<string, unknown> | undefined): string {
  const inp = input ?? {};
  switch (name) {
    case "Read":
      return basename(inp.file_path) ? `Read ${basename(inp.file_path)}` : "Read file";
    case "Edit":
    case "MultiEdit":
    case "Write":
      return basename(inp.file_path) ? `Edit ${basename(inp.file_path)}` : "Edit file";
    case "Bash":
      return typeof inp.command === "string" ? inp.command.slice(0, 60) : "Run command";
    case "Grep":
    case "Glob":
      return typeof inp.pattern === "string" ? `Search "${inp.pattern}"` : "Search";
    case "TodoWrite":
      return "Updated plan";
    case "WebFetch":
      case "WebSearch": {
        const url = typeof inp.url === "string" ? inp.url : (typeof inp.query === "string" ? inp.query : "");
        try { return url ? new URL(url).host : "Web search"; } catch { return url || "Web search"; }
      }
    case "Task":
      return typeof inp.description === "string" ? inp.description : "Sub-agent task";
    default:
      return name.replace(/[_-]+/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
}
function toolSummaryFor(msg: ChatMessage): string {
  return msg.toolRawName ? toolSummary(msg.text, msg.toolInput) : msg.text;
}
function toolMonospace(msg: ChatMessage): boolean {
  return msg.toolRawName ? msg.text === "Bash" : /^[$#]|[/.]\w|\(\)/.test(msg.text);
}
type ToolStatus = "running" | "done" | "failed";
function toolStatus(msg: ChatMessage): ToolStatus {
  if (msg.toolFailed) return "failed";
  if (msg.toolOutput !== undefined) return "done";
  return "running";
}
// Safety net for a turn ending (normally or via a dead adapter process) while a
// tool row never got its matching update — a dropped/unparseable status line
// otherwise leaves the spinner running forever. `failed` marks stuck rows as
// failed instead of done when the turn ended abnormally.
function finalizeStuckTools(failed = false) {
  for (const m of messages.value) {
    if (m.role === "tool" && m.toolOutput === undefined) {
      m.toolOutput = "";
      if (failed) m.toolFailed = true;
    }
  }
}

// Collapsed "Ran N commands" / "Used N tools" pill grouping — folds consecutive
// finished tool calls (2+) between two non-tool messages into one header.
// The trailing in-flight run is left ungrouped so the live tool call stays visible.
interface ToolGroupHeader {
  role: "tool-group-header";
  id: string;
  groupId: string;
  items: ChatMessage[];
  partial?: false;
}
const toolGroupExpanded = reactive<Record<string, boolean>>({});
const thinkingExpanded = reactive<Record<number, boolean>>({});
function isBashTool(msg: ChatMessage): boolean {
  return toolIconFor(msg) === PhTerminalWindow;
}
function groupLabel(items: ChatMessage[]): string {
  const n = items.length;
  const bash = items.filter(isBashTool).length;
  const s = (c: number) => (c === 1 ? "" : "s");
  if (bash === n) return `Ran ${n} command${s(n)}`;
  if (bash === 0) return `Used ${n} tool${s(n)}`;
  return `Used ${n} tools and ran ${bash} command${s(bash)}`;
}
function groupIcon(items: ChatMessage[]): unknown {
  return items.every(isBashTool) ? PhTerminalWindow : PhWrench;
}
function groupHasFailure(items: ChatMessage[]): boolean {
  return items.some((m) => m.toolFailed);
}
function groupIsRunning(items: ChatMessage[]): boolean {
  return items.some((m) => toolStatus(m) === "running");
}
const grouping = computed(() => {
  const display: (ChatMessage | ToolGroupHeader)[] = [];
  const groupIdByMsgId = new Map<number, string>();
  const groupsById = new Map<string, ToolGroupHeader>();
  const msgs = messages.value;
  let i = 0;
  while (i < msgs.length) {
    const m = msgs[i];
    if (m.role !== "tool") { display.push(m); i++; continue; }
    const start = i;
    while (i < msgs.length && msgs[i].role === "tool") i++;
    const run = msgs.slice(start, i);
    const trailingLive = i === msgs.length && toolStatus(run[run.length - 1]) === "running";
    if (run.length < 2 || trailingLive) { display.push(...run); continue; }
    const groupId = String(run[0].toolUseId ?? run[0].id);
    const header: ToolGroupHeader = { role: "tool-group-header", id: `hdr-${groupId}`, groupId, items: run };
    display.push(header);
    groupsById.set(groupId, header);
    for (const item of run) { display.push(item); groupIdByMsgId.set(item.id, groupId); }
  }
  return { display, groupIdByMsgId, groupsById };
});
// Turn fold + per-turn changed-files summary. A turn starts at a user message
// and ends at the next one (or at the end of an idle chat). Once it's settled,
// its thinking/tool rows collapse behind one "Worked for Xs" row and the files
// it touched are summed into a diffstat card — the transcript stays readable
// when scrolling back over a long session.
interface TurnFoldRow { role: "turn-fold"; id: string; turnKey: number; label: string; folded: boolean; partial?: false }
interface ChangedFilesRow { role: "changed-files"; id: string; files: FileEdit[]; added: number; removed: number; partial?: false }
type DisplayRow = ChatMessage | ToolGroupHeader | TurnFoldRow | ChangedFilesRow;

const WORK_ROLES = new Set(["thinking", "tool", "tool-group-header"]);
const foldedTurns = reactive<Record<number, boolean>>({});

const displayItems = computed<DisplayRow[]>(() => {
  const src = grouping.value.display;
  const out: DisplayRow[] = [];
  let i = 0;
  while (i < src.length) {
    const row = src[i];
    if (row.role !== "user") { out.push(row); i++; continue; }
    const user = row as ChatMessage;
    out.push(user);
    i++;
    const turnRows: (ChatMessage | ToolGroupHeader)[] = [];
    while (i < src.length && src[i].role !== "user") { turnRows.push(src[i]); i++; }
    const hasLaterTurn = i < src.length;
    // The last turn is settled only once the chat is idle; earlier ones always are.
    const settled = hasLaterTurn || !busy.value;
    const folded = settled && (foldedTurns[user.id] ?? hasLaterTurn);
    const label = user.turnMs ? `Worked for ${fmtDuration(user.turnMs)}` : "Worked";

    const edits: FileEdit[] = [];
    let foldEmitted = false;
    for (const r of turnRows) {
      if (r.role === "tool") {
        const e = editOf((r as ChatMessage).toolInput);
        if (e) edits.push(e);
      }
      if (settled && WORK_ROLES.has(r.role)) {
        if (!foldEmitted) {
          out.push({ role: "turn-fold", id: `fold-${user.id}`, turnKey: user.id, label, folded });
          foldEmitted = true;
        }
        if (folded) continue;
      }
      out.push(r);
    }
    if (settled && edits.length > 0) {
      const files = mergeEdits(edits);
      out.push({
        role: "changed-files",
        id: `cf-${user.id}`,
        files,
        added: files.reduce((n, f) => n + f.added, 0),
        removed: files.reduce((n, f) => n + f.removed, 0),
      });
    }
  }
  return out;
});
// Consecutive same-role bubbles (user asking two things in a row, or an
// assistant reply split across messages) read as one grouped turn: only the
// edge message of the run carries an avatar + extra spacing, same-role
// neighbors sit tight against each other. Keeps the message list from
// reading as one flat avatar-per-line log.
function isFirstOfRun(idx: number): boolean {
  const items = displayItems.value;
  return items[idx - 1]?.role !== items[idx].role;
}
function isLastOfRun(idx: number): boolean {
  const items = displayItems.value;
  return items[idx + 1]?.role !== items[idx].role;
}
function groupIdOf(msgId: number): string | undefined {
  return grouping.value.groupIdByMsgId.get(msgId);
}
function isGroupExpanded(groupId: string | undefined): boolean {
  if (!groupId) return true;
  const explicit = toolGroupExpanded[groupId];
  if (explicit !== undefined) return explicit;
  return groupHasFailure(grouping.value.groupsById.get(groupId)?.items ?? []);
}
function toggleGroup(groupId: string) {
  toolGroupExpanded[groupId] = !isGroupExpanded(groupId);
}

// Built-in claude slash commands
interface Command { name: string; description: string }

// Only commands that work in stream-json mode (no TTY display, no editor).
const BUILTIN_COMMANDS: Command[] = [
  { name: "pr",      description: "Write a PR description from recent git diff" },
  { name: "clear",   description: "Clear conversation history" },
  { name: "compact", description: "Compact conversation with summary" },
  { name: "help",    description: "Show available commands" },
  { name: "review",  description: "Review changes in current directory" },
  { name: "init",    description: "Initialize project with CLAUDE.md" },
  { name: "cost",    description: "Show token and cost usage for this session" },
];

const allCommands = ref<Command[]>([...BUILTIN_COMMANDS]);
const skillCommands = ref<Command[]>([]); // installed skills, completed via `$`

// Skill pills in the composer: backdrop re-render of the input with /skill
// tokens highlighted (see composer.css .skill-pill / .composer-ghost).
const hlEl = ref<HTMLElement | null>(null);
const skillNames = computed(() => skillCommands.value.map((c) => c.name));
const skillParts = computed(() => splitSkillTokens(inputText.value, skillNames.value));
const hasSkillPill = computed(() => skillParts.value.some((p) => p.pill));
function syncHighlightScroll() {
  if (hlEl.value && inputEl.value) hlEl.value.scrollTop = inputEl.value.scrollTop;
}
const suggestions = ref<Command[]>([]);
const suggestionIdx = ref(0);

// @-mention file completion — lazy repo file list (git ls-files), filtered on `@query`.
const fileList = ref<string[]>([]);
let fileListLoaded = false;
const atSuggestions = ref<string[]>([]);
const atIdx = ref(0);

async function ensureFileList() {
  if (fileListLoaded) return;
  fileListLoaded = true;
  try {
    const out = await invoke<{ stdout: string }>("run_git", {
      cwd: props.cwd,
      args: ["ls-files", "--cached", "--others", "--exclude-standard"],
    });
    fileList.value = out.stdout.split("\n").map((s) => s.trim()).filter(Boolean).slice(0, 20000);
  } catch { fileList.value = []; }
}


interface AccountInfo {
  email: string;
  display_name: string;
  organization_type: string;  // "claude_max" | "pro" | ...
  rate_limit_tier: string;    // "default_claude_max_5x" | ...
  status_text: string;        // raw `claude status` stdout
}

// Legacy per-chat localStorage key prefix (kept only for the one-time migration below).

// Transcripts live in SQLite (chat_messages), not config.json: one row per
// message means saving a turn writes that turn, where the old config-backed
// version re-serialised every chat's history — 5.6 MB per saved message once a
// few dozen chats had accumulated. Migration of existing histories happens once
// in Go at startup.
async function loadMessages(chatId: number): Promise<ChatMessage[]> {
  try {
    const raw = await invoke<string>("load_chat_messages", { chatId });
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as ChatMessage[]) : [];
  } catch {
    return [];
  }
}

// Fire-and-forget so the 12 call sites stay synchronous — a lost transcript
// write is not worth blocking the stream over, and the next save rewrites it.
function saveMessages(chatId: number, msgs: ChatMessage[]) {
  // Partial messages are mid-stream and get re-sent; the cap only guards against
  // a pathological chat, not storage (a row per message is cheap now).
  const toSave = msgs.filter((m) => !m.partial).slice(-2000);
  // foldedOrd tells the backend this transcript already accounts for every
  // stream line up to S.lastOrd, which is what makes the chat_stream trim safe
  // (chatstream.go). -1 while nothing has streamed yet: "don't move the mark".
  const foldedOrd = S.lastOrd >= 0 ? S.lastOrd + 1 : -1;
  void invoke("save_chat_messages", { chatId, messages: JSON.stringify(toSave), foldedOrd }).catch(() => {});
}

function clearMessageHistory(chatId: number) {
  void invoke("delete_chat_messages", { chatId }).catch(() => {});
}

const DRAFT_KEY = computed(() => `burrow.draft.chat.${props.chatId}`);
const inputText = ref(localStorage.getItem(DRAFT_KEY.value) ?? "");
watch(inputText, (val) => {
  if (val) {
    localStorage.setItem(DRAFT_KEY.value, val);
  } else {
    localStorage.removeItem(DRAFT_KEY.value);
  }
});
// Stall watchdog: while busy, any incoming acp-data/claude-data line bumps this.
// If nothing arrives for a while, the adapter is probably wedged (hung tool call,
// dropped final response) rather than genuinely still working — surface a hint
// on the abort button instead of leaving the user staring at a spinner forever.
// ponytail: blind idle timer, not a real liveness check — false positive on a
// legitimately silent long-running tool call. Upgrade path: have the Go side
// report process liveness so this can confirm instead of guess.
const nowTick = ref(Date.now());
const STALL_MS = 90_000;
const stalled = computed(() => busy.value && nowTick.value - lastActivityAt.value > STALL_MS);
let stallTimer: ReturnType<typeof setInterval> | null = null;
// 1 Hz so the "Working for Xs" label ticks, but only while busy — an idle chat
// shouldn't re-render once a second.
stallTimer = setInterval(() => { if (busy.value) nowTick.value = Date.now(); }, 1_000);

// Turn clock: live elapsed while busy, stamped onto the user message that
// opened the turn when it settles (so it persists with the history and the
// fold row can say "Worked for 26s" after a reload).
const workingElapsed = computed(() =>
  turnStartedAt.value ? fmtDuration(nowTick.value - turnStartedAt.value) : "0s"
);
watch(busy, (val) => {
  if (val) {
    lastActivityAt.value = Date.now();
    nowTick.value = Date.now();
    turnStartedAt.value = Date.now();
    return;
  }
  if (turnStartedAt.value) {
    const lastUser = [...messages.value].reverse().find((m) => m.role === "user");
    if (lastUser) lastUser.turnMs = Date.now() - turnStartedAt.value;
    turnStartedAt.value = 0;
  }
}, { flush: "sync" });
const copiedMessageId = ref<number | null>(null);
let copyFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
// Set before an INTENTIONAL claude restart (mode switch / abort) so the `exit`
// event that teardown emits doesn't fire a spurious "Claude finished" toast.
const pendingImages = ref<string[]>([]); // data URIs
const scrollEl = ref<HTMLElement | null>(null);
const inputEl = ref<HTMLTextAreaElement | null>(null);
const suggestionsEl = ref<HTMLElement | null>(null);
// Attach the acp-data/acp-req listeners if not already. onMounted only attaches
// them when the chat STARTS as an ACP agent; switching to an ACP agent at runtime
// (selectAgent → clearChat → acp_start) must attach them too, or every adapter
// event (model/config + the whole turn) is dropped → no model, stuck "thinking".
const ensureAcpListeners = () => S.listenAcp();

// Permission mode (per-chat, persisted). Mirrors `claude --permission-mode`:
// default | auto | acceptEdits | plan | dontAsk | bypassPermissions.
type PermMode = "default" | "auto" | "acceptEdits" | "plan" | "dontAsk" | "bypassPermissions";
// Legacy per-chat localStorage key prefixes (kept only for the one-time migration below).
const PERM_LAST_KEY = "burrow.claude.permMode.last";
const PERM_VALUES: PermMode[] = ["default", "auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions"];
function isPermMode(v: unknown): v is PermMode {
  return typeof v === "string" && (PERM_VALUES as string[]).includes(v);
}
interface ChatPermissionModeConfig {
  byChat: Record<string, string>;
  last?: string;
  dangerousByChat: Record<string, boolean>;
}
function loadPermMode(id: number): PermMode {
  const cfg = getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} });
  const v = cfg.byChat[String(id)];
  if (isPermMode(v)) return v;
  // Migrate the old boolean "dangerous mode" flag → bypassPermissions.
  if (cfg.dangerousByChat[String(id)]) return "bypassPermissions";
  // New chat: inherit the last-used mode so the user doesn't have to re-pick every time.
  if (isPermMode(cfg.last)) return cfg.last;
  return "default";
}
function savePermMode(id: number, mode: PermMode) {
  const cfg = { ...getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }) };
  cfg.byChat = { ...cfg.byChat, [String(id)]: mode };
  cfg.last = mode;
  setConfig("chatPermissionMode", cfg);
}
const permMode = ref<PermMode>(loadPermMode(props.chatId));
const PERM_META: Record<PermMode, { label: string; description: string; title: string; danger?: boolean }> = {
  default: { label: "Supervised", description: "Ask before commands and file changes.", title: "Ask before edits & commands (click to change)" },
  auto: { label: "Auto", description: "Claude decides which routine actions can proceed.", title: "Claude decides when to ask (click to change)" },
  acceptEdits: { label: "Auto-accept edits", description: "Auto-approve edits, ask before other actions.", title: "Auto-accept file edits; still ask for other tools (click to change)" },
  plan: { label: "Plan mode", description: "Plan only until you approve implementation.", title: "Plan only — no edits or commands until you approve (click to change)" },
  dontAsk: { label: "Don't ask", description: "Run edits and commands without routine prompts.", title: "Run edits & commands without asking; still blocks dangerous ops (click to change)" },
  bypassPermissions: { label: "Full access", description: "Skip all permission checks.", title: "Skip ALL permission checks (click to change)", danger: true },
};
const permMeta = computed(() => PERM_META[permMode.value]);
const PERM_MODES: PermMode[] = PERM_VALUES;
const PERM_ICON: Record<PermMode, unknown> = {
  default: PhShieldCheck,
  auto: PhSparkle,
  acceptEdits: PhPencilSimple,
  plan: PhListChecks,
  dontAsk: PhFastForward,
  bypassPermissions: PhShieldWarning,
};
const ACP_MODE_ICON: Record<string, unknown> = {
  "read-only": PhShieldCheck,
  "auto-accept-edits": PhPencilSimple,
  auto: PhSparkle,
  dontAsk: PhFastForward,
  "full-access": PhShieldWarning,
};
function acpModeIcon(id: string): unknown { return ACP_MODE_ICON[id] ?? PhShieldCheck; }
const permMenuOpen = ref(false);
const permBtnEl = ref<HTMLElement | null>(null);
const permMenuEl = ref<HTMLElement | null>(null);
// The menu is teleported + position:fixed, so anchor it to the button's rect.
const permMenuPos = ref({ top: 0, left: 0 });
function togglePermMenu() {
  if (!permMenuOpen.value && permBtnEl.value) {
    const r = permBtnEl.value.getBoundingClientRect();
    const menuH = PERM_MODES.length * 64 + 12;
    permMenuPos.value = { top: Math.max(8, Math.round(r.top - menuH - 6)), left: Math.round(r.left) };
  }
  permMenuOpen.value = !permMenuOpen.value;
}
function onPermMenuOutside(e: MouseEvent) {
  if (!permMenuOpen.value) return;
  const t = e.target as Node;
  if (permBtnEl.value?.contains(t) || permMenuEl.value?.contains(t)) return;
  permMenuOpen.value = false;
}

// Same ntfy gating as Terminal.vue (enabled, topic set, event subscribed, away-only).
function maybeNtfy(event: NtfyEvent, message: string) {
  if (!uiStore.ntfyEnabled || !uiStore.ntfyTopic) return;
  if (!uiStore.ntfyEvents.includes(event)) return;
  if (uiStore.ntfyOnlyWhenAway && document.hasFocus()) return;
  notifyNtfy(
    { server: uiStore.ntfyServer, topic: uiStore.ntfyTopic, token: uiStore.ntfyToken || undefined },
    event,
    message || "Chat",
  ).catch(() => {}); // best-effort: a failed push must never disrupt the UI
}

// Is the user actually looking at this chat right now?
//
// Asks the SESSION, not our own props: a chat leaf unmounts when it leaves the
// screen (Terminal.isChatVisible) and an unmounted component's props are frozen
// at their last value — so `props.isWatching` would still claim `true` for a
// turn that finished after the user walked away.
function watchingNow(): boolean {
  return S.isWatched() && document.hasFocus();
}

async function notifyDone() {
  const session = chats.sessions.find((s) => s.id === props.chatId);
  const body = session?.title || "Claude finished";
  notifStore.push({ type: "done", title: "Claude", body, workspaceId: props.workspaceId });
  // Mirror Terminal.vue: no chime while the user is watching the turn finish.
  if (!watchingNow()) playSound("done");
  maybeNtfy("done", body);
  if (!document.hasFocus()) {
    let granted = await isPermissionGranted();
    if (!granted) { const p = await requestPermission(); granted = p === "granted"; }
    if (granted) sendNotification({ title: "Burrow", body: `✓ ${body}` });
  }
}

// Alert the user that Claude is blocked on a permission/question/plan decision:
// in-app toast always, plus a native OS notification (with sound) when Burrow is
// not focused — mirrors notifyDone's unfocused path.
async function notifyPermission(cr: CanUseToolReq) {
  const target = (cr.input?.command ?? cr.input?.file_path ?? cr.input?.path ?? cr.description ?? "") as string;
  const body = target ? `${cr.toolName}: ${String(target).slice(0, 80)}` : cr.toolName;
  notifStore.push({ type: "info", title: "Povolení", body, workspaceId: props.workspaceId });
  playSound("waiting");
  maybeNtfy("permission", body);
  if (!document.hasFocus()) {
    let granted = await isPermissionGranted();
    if (!granted) { const p = await requestPermission(); granted = p === "granted"; }
    if (granted) sendNotification({ title: "Burrow — povolení", body });
  }
}

// A `can_use_tool` control_request from claude. Every blocking surface (permission,
// ExitPlanMode, AskUserQuestion, file edits) arrives on this one channel; we route by toolName.
// Feed marker message IDs — removed when permission is resolved
// Keep native Claude prompts mounted until the control JSON was accepted by
// stdin. This prevents a failed write from looking like an automatic denial.
const nativeControlResponsePending = ref(false);
// Claude may replay an in-flight control request after a reconnect. Keep a
// small settled-id ledger so an already answered question cannot re-open.

function settleControlRequest(requestId: string) {
  if (!requestId) return;
  settledControlRequestIds.add(requestId);
  // IDs are unique per process; retain enough to cover a reconnect without
  // growing a long-lived chat indefinitely.
  if (settledControlRequestIds.size > 200) {
    const oldest = settledControlRequestIds.values().next().value;
    if (oldest) settledControlRequestIds.delete(oldest);
  }
}

function hasActiveControlRequest(requestId: string) {
  return [pendingPermission.value, pendingDiff.value, pendingQuestion.value, pendingPlan.value]
    .some((request) => request?.requestId === requestId);
}

function dismissCancelledControlRequest(requestId: string) {
  let dismissed = false;
  if (pendingPermission.value?.requestId === requestId) {
    removeFeedMarker(pendingPermissionMsgId.value); pendingPermissionMsgId.value = null;
    pendingPermission.value = null; dismissed = true;
  }
  if (pendingDiff.value?.requestId === requestId) {
    removeFeedMarker(pendingDiffMsgId.value); pendingDiffMsgId.value = null;
    pendingDiff.value = null; dismissed = true;
  }
  if (pendingQuestion.value?.requestId === requestId) {
    removeFeedMarker(pendingQuestionMsgId.value); pendingQuestionMsgId.value = null;
    pendingQuestion.value = null; dismissed = true;
  }
  if (pendingPlan.value?.requestId === requestId) {
    removeFeedMarker(pendingPlanMsgId.value); pendingPlanMsgId.value = null;
    pendingPlan.value = null; dismissed = true;
  }
  if (dismissed) {
    settleControlRequest(requestId);
    nativeControlResponsePending.value = false;
    chats.sendStatusEvent(props.chatId, { type: "RESUME" });
    syncStore();
  }
}

function removeFeedMarker(id: number | null) {
  if (id === null) return;
  const idx = messages.value.findIndex((m) => m.id === id);
  if (idx !== -1) messages.value.splice(idx, 1);
}

// Queue panel
const queueExpanded = ref(true);
function clearQueue() {
  clearQueuedMessages();
  saveMessages(props.chatId, messages.value);
}
function removeQueued(id: number) {
  removeQueuedMessage(id);
  saveMessages(props.chatId, messages.value);
}
function sendQueuedNext(id: number) {
  // This deliberately reorders instead of steering. A follow-up has to remain
  // its own turn for every provider, including Codex and generic ACP adapters.
  moveQueuedMessageNext(id);
  saveMessages(props.chatId, messages.value);
}

// Context usage bar — 200k for all current models
const CONTEXT_MAX = 200_000;
const contextUsageRatio = computed(() => {
  if (!turnStats.value) return 0;
  return Math.min(turnStats.value.inputTokens / CONTEXT_MAX, 1);
});
const contextUsageClass = computed(() => {
  const r = contextUsageRatio.value;
  if (r >= 0.9) return "ctx-exceeded";
  if (r >= 0.75) return "ctx-warning";
  return "ctx-ok";
});

// Permission dropdown
const permDropdownOpen = ref(false);

const currentActivity = computed(() => {
  if (!busy.value) return "";
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i];
    if (m.role === "tool") return `Running ${m.text}…`;
    // Thinking/assistant text streams into the chat body itself — no need
    // to mirror it in the composer bar above the input.
    if (m.role === "assistant" || m.role === "thinking") return "";
  }
  return "";
});

// AskUserQuestion working selection: question text → chosen option label(s).
const questionAnswers = ref<Record<string, string[]>>({});
// AskUserQuestion free-text override ("Other") — non-empty wins over picked options,
// mirroring t3code's customAnswer (they can write one, so should we).
const questionCustomAnswers = ref<Record<string, string>>({});
// ExitPlanMode "keep planning" feedback.
const planFeedback = ref("");

const permissionDetail = computed(() => {
  const cr = pendingPermission.value;
  if (!cr) return "";
  const r = cr.input;
  return (r.command ?? r.file_path ?? r.path ?? cr.description ?? JSON.stringify(r).slice(0, 120)) as string;
});

// Match keys for "Allow always" rules. Bash gets a command-prefix key so allowing
// `git` once doesn't blanket-allow every Bash call.
function ruleKeys(toolName: string, input: Record<string, unknown>): string[] {
  const keys = [toolName];
  if (toolName === "Bash" && typeof input.command === "string") {
    const first = (input.command as string).trim().split(/\s+/)[0];
    if (first) keys.push(`Bash:${first}`);
  }
  return keys;
}

const planMd = computed(() => {
  const p = pendingPlan.value?.input?.plan;
  return typeof p === "string" ? renderMd(p) : "";
});
interface QuestionSpec { question: string; header?: string; multiSelect?: boolean; options: Array<{ label: string; description?: string }> }
const questionSpecs = computed<QuestionSpec[]>(() =>
  ((pendingQuestion.value?.input?.questions ?? []) as QuestionSpec[]));
function hasAnswer(question: string) {
  return (questionCustomAnswers.value[question] ?? "").trim().length > 0
    || (questionAnswers.value[question] ?? []).length > 0;
}
const canSubmitQuestion = computed(() =>
  questionSpecs.value.every((q) => hasAnswer(q.question)));

// Stepped AskUserQuestion — one question shown at a time, mirroring t3code's
// ComposerPendingUserInputPanel (index reset whenever a new request arrives).
const activeQuestionIndex = ref(0);
const activeQuestion = computed(() => questionSpecs.value[activeQuestionIndex.value] ?? null);
const isLastQuestion = computed(() => activeQuestionIndex.value >= questionSpecs.value.length - 1);
const canAdvanceQuestion = computed(() => {
  const q = activeQuestion.value;
  return !!q && hasAnswer(q.question);
});
let questionAutoAdvanceTimer: number | null = null;
function clearQuestionAutoAdvance() {
  if (questionAutoAdvanceTimer !== null) { window.clearTimeout(questionAutoAdvanceTimer); questionAutoAdvanceTimer = null; }
}
function selectQuestionOption(label: string) {
  const q = activeQuestion.value;
  if (!q) return;
  clearQuestionAutoAdvance();
  toggleOption(q.question, label, !!q.multiSelect);
  // Single-select auto-advances shortly after picking, same as t3code; multi-select
  // needs an explicit Next since the user may still want to toggle more options.
  if (!q.multiSelect) {
    questionAutoAdvanceTimer = window.setTimeout(() => { questionAutoAdvanceTimer = null; advanceQuestion(); }, 200);
  }
}
function advanceQuestion() {
  if (!canAdvanceQuestion.value) return;
  if (isLastQuestion.value) { void submitQuestion(); return; }
  activeQuestionIndex.value++;
}
function previousQuestion() {
  clearQuestionAutoAdvance();
  activeQuestionIndex.value = Math.max(0, activeQuestionIndex.value - 1);
}
// Number-key shortcuts (1-9) pick the corresponding option, matching t3code —
// only while a question is pending and focus isn't in an editable field.
function onQuestionKeydown(event: KeyboardEvent) {
  if (!pendingQuestion.value || nativeControlResponsePending.value) return;
  if (event.metaKey || event.ctrlKey || event.altKey) return;
  const target = event.target;
  if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement) return;
  const digit = Number.parseInt(event.key, 10);
  if (Number.isNaN(digit) || digit < 1 || digit > 9) return;
  const q = activeQuestion.value;
  const opt = q?.options[digit - 1];
  if (!opt) return;
  event.preventDefault();
  selectQuestionOption(opt.label);
}
watch(pendingQuestion, (cr) => {
  clearQuestionAutoAdvance();
  activeQuestionIndex.value = 0;
  questionCustomAnswers.value = {};
  if (cr) document.addEventListener("keydown", onQuestionKeydown);
  else document.removeEventListener("keydown", onQuestionKeydown);
});
onBeforeUnmount(() => {
  clearQuestionAutoAdvance();
  document.removeEventListener("keydown", onQuestionKeydown);
});

// Diff preview for a pending Edit/Write. For Write/NotebookEdit it's full content;
// for Edit it's old→new strings.
const diffPreview = computed(() => {
  const cr = pendingDiff.value;
  if (!cr) return null;
  const i = cr.input;
  return {
    path: (i.file_path ?? i.path ?? cr.description ?? "") as string,
    isWrite: cr.toolName === "Write" || cr.toolName === "NotebookEdit",
    content: (i.content ?? "") as string,
    oldStr: (i.old_string ?? "") as string,
    newStr: (i.new_string ?? "") as string,
  };
});
const accountInfo = ref<AccountInfo | null>(null);

// Parse 5h window from `claude status` plain text.
// Expected line: "5h window: 23% (2h 14m remaining)" or similar.
const fiveHourWindow = computed(() => {
  const text = accountInfo.value?.status_text ?? "";
  const m = text.match(/5[- ]h(?:our)?[^:]*:\s*([^\n]+)/i);
  return m ? m[1].trim() : "";
});

const cwdDisplay = computed(() => {
  const parts = props.cwd.replace(/^\/Users\/[^/]+/, "~").split("/");
  return parts.slice(-2).join("/") || props.cwd;
});

// ponytail: 64px slop so a smooth-scroll in flight still counts as "at bottom"
const atBottom = ref(true);
function onScroll() {
  const el = scrollEl.value;
  if (el) atBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < 64;
}
function scrollToBottom(force = false) {
  if (!force && !atBottom.value) return;
  nextTick(() => {
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight;
    atBottom.value = true;
  });
}

// Auto-title helpers
const FILLER_PREFIX = /^(can you |please |i want (you )?to |how (do i|to) |what (is|are) (the |a )?|could you |would you |help me |i need (you )?to )/i;
function smartTitle(text: string): string {
  const clean = text.replace(FILLER_PREFIX, "").replace(/\s+/g, " ").trim();
  const words = clean.split(" ");
  const slug = words.slice(0, 6).join(" ");
  const title = slug.charAt(0).toUpperCase() + slug.slice(1);
  return title.length < clean.length ? title + "…" : title;
}
function isDefaultTitle(title: string): boolean {
  return /^Chat(\s+\d+)?$/.test(title.trim());
}
// Upgrade the heuristic title with a model-written one (headless `claude -p`,
// same cheap model as the commit-message button). Fire-and-forget: the
// heuristic title is already on screen, so a failure or a slow answer costs
// nothing. Bails if anything better claimed the title meanwhile.
async function refineTitle(text: string) {
  const heuristic = smartTitle(text);
  let title = "";
  try {
    title = await invoke<string>("generate_chat_title", {
      cwd: props.cwd,
      model: uiStore.textGenerationModel,
      policy: uiStore.textGenerationPolicy,
      text,
    });
  } catch {
    return;
  }
  if (!title || claudeGeneratedTitle.value) return;
  const session = chats.sessions.find((s) => s.id === props.chatId);
  if (!session || session.pinnedTitle || session.title !== heuristic) return;
  chats.sync(props.chatId, { title });
}

// Once Claude sends us a generated title, prefer it and stop overwriting.
const claudeGeneratedTitle = ref(false);
function applyClaudeTitle(raw: unknown) {
  if (typeof raw !== "string" || !raw.trim()) return;
  claudeGeneratedTitle.value = true;
  chats.sync(props.chatId, { title: raw.trim().slice(0, 60) });
}

// A turn does not always start with a user send: Claude resumes the same
// session on its own after a background task finishes or an interim Stop, and
// nothing had marked the thread running — so it worked with no dot and no
// "Working" in the Sidebar. Assistant output while we think it's idle IS a turn.
// ponytail: claude-cli only. ACP replays history through the same feed on
// session/load with no turn-done, so marking active there sticks running.
function markAgentActive() {
  if (busy.value) return;
  busy.value = true;
  chats.sendStatusEvent(props.chatId, { type: "START" });
  syncStore();
}

function syncStore() {
  chats.sync(props.chatId, {
    busy: busy.value,
    messageCount: messages.value.filter((m) => m.role !== "tool").length,
  });
  publishRemoteChat();
}

// Remote deliberately consumes the identical normalized conversation feed as
// this component. The Rust mirror is discovery + reconnect history only;
// incremental updates still travel directly over claude-data / acp-data.
function publishRemoteChat() {
  const session = chats.sessions.find((item) => item.id === props.chatId);
  if (!session) return;
  invoke("remote_sync_chat", {
    chat: {
      id: props.chatId,
      workspaceId: props.workspaceId,
      title: session.title,
      busy: busy.value,
      status: session.status ?? null,
      agentKind: session.agentKind ?? null,
      transport: session.transport ?? "claude-cli",
      claudeSessionId: sessionId.value,
      messages: messages.value.filter((message) => !message.partial).slice(-200),
    },
  }).catch(() => {});
}

// This is the shared renderer boundary: Claude's stream-json protocol and every
// ACP adapter (including the locally logged-in Codex CLI) update the same feed.
// Provider-neutral events from `chat-event-{chatId}`. The wire formats are read
// in Go (src-wails/providerruntime.go) and the transcript rules live in
// lib/chatProjection.ts, so what is left here is what only a mounted view can
// do: scroll, notify, account for the turn.
// The projection mutates a plain {messages, nextMsgId}; the session keeps the
// first as a ref and the second as a field. This adapter is the whole bridge,
// and it keeps lib/chatProjection.ts free of Vue.
const projection: ChatProjectionState = {
  get messages() { return messages.value; },
  get nextMsgId() { return S.nextMsgId; },
  set nextMsgId(v: number) { S.nextMsgId = v; },
};

function onEvents(batch: ChatEventBatch) {
  for (const event of batch.events) {
    if (isProjectedEvent(event.type)) {
      // Native transport only, per markAgentActive's own caveat: an ACP
      // session/load replays its whole history through this same feed with no
      // turn-done at the end, so marking active there would stick on "running".
      if (!usesRpcRuntime.value) markAgentActive();
      if (applyChatEvent(projection, event)) scrollToBottom();
      // Sub-agent bookkeeping is the view's, not the transcript's.
      if (event.type === "tool.started" && event.name === "Task" && event.toolCallId) {
        subagents.started(props.chatId, event.toolCallId, event.input);
      }
      if (event.type === "tool.completed" && event.toolCallId) {
        subagents.completed(event.toolCallId, event.failed === true);
      }
      continue;
    }

    switch (event.type) {
      case "turn.completed":
      case "turn.failed":
        if (event.type === "turn.completed" && (event.inputTokens || event.outputTokens)) {
          const inp = event.inputTokens ?? 0;
          const out = event.outputTokens ?? 0;
          turnStats.value = { inputTokens: inp, outputTokens: out, costUsd: event.costUsd ?? 0 };
          sessionCost.value += event.costUsd ?? 0;
          chats.recordTurn(inp, out);
        }
        finishTurn();
        break;
      case "session.title":
        // Once Claude has named the thread, a later result repeating the title
        // must not re-sync it — the user may have renamed the tab since.
        if (!claudeGeneratedTitle.value) applyClaudeTitle(event.title);
        break;
      case "session.exited":
        // The process is gone, so the next send must spawn a replacement rather
        // than write to a dead pipe.
        runtimeStarted.value = false;
        if (busy.value) {
          // Died mid-turn with no boundary of its own — settle it here or the
          // spinner runs forever.
          busy.value = false;
          settleTranscript(projection);
          finalizeStuckTools(true);
          syncStore();
        }
        S.maybeEvict();
        break;
    }
  }
}

// Everything a finished turn does beyond the transcript. Shared by the native
// boundary (a turn.completed event) and the ACP one (the response to our own
// session/prompt, which only the sender can correlate).
function finishTurn() {
  busy.value = false;
  settleTranscript(projection);
  finalizeStuckTools();
  saveMessages(props.chatId, messages.value);
  syncStore();
  scrollToBottom();
  // An `exit` from an intentional restart (mode switch / abort) is not a real
  // turn boundary — skip the "finished" toast/notification once.
  if (suppressNextDone.value) {
    suppressNextDone.value = false;
  } else {
    chats.sendStatusEvent(props.chatId, { type: "STOP", watching: watchingNow() });
    notifyDone();
  }
  drainQueuedMessage();
  // The session outlives this component while a turn is running; now that the
  // turn is over it is only worth keeping if someone is still watching.
  S.maybeEvict();
}

function drainQueuedMessage() {
  if (busy.value) return;
  const next = takeNextQueuedMessage();
  if (!next) return;
  saveMessages(props.chatId, messages.value);
  // Finish handlers first: the next prompt starts only after the prior turn
  // has fully settled its transcript, status and provider correlation.
  nextTick(() => sendMessage(next.text, next.images));
}

function onLine(line: string) {
  let event: Record<string, unknown>;
  try { event = JSON.parse(line) as Record<string, unknown>; }
  catch { return; }
  lastActivityAt.value = Date.now();

  const type = event.type as string;

  // Claude withdraws a pending question/permission when the turn is aborted,
  // answered from another client, or otherwise no longer needs input.
  if (type === "control_cancel_request") {
    dismissCancelledControlRequest(event.request_id as string);
    return;
  }

  if (type === "control_request") {
    const req = (event.request ?? {}) as Record<string, unknown>;
    if (req.subtype !== "can_use_tool") return; // other control subtypes: ignore (fail-open)
    const cr: CanUseToolReq = {
      requestId: event.request_id as string,
      toolName: (req.tool_name as string) ?? "",
      input: (req.input ?? {}) as Record<string, unknown>,
      description: req.description as string | undefined,
      suggestions: (req.permission_suggestions ?? []) as Array<Record<string, unknown>>,
      toolUseId: req.tool_use_id as string | undefined,
    };
    // A request can be replayed during reconnect. Rendering it again after we
    // replied is what made AskUserQuestion look permanently stuck.
    if (settledControlRequestIds.has(cr.requestId) || hasActiveControlRequest(cr.requestId)) return;
    // Auto-allow when an "always" rule matches — no UI.
    if (chats.hasPermissionRule(ruleKeys(cr.toolName, cr.input))) {
      void respondControl(cr.requestId, { behavior: "allow", updatedInput: cr.input }).catch((e) => {
        messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
        saveMessages(props.chatId, messages.value);
      });
      return;
    }
    if (cr.toolName === "AskUserQuestion") {
      questionAnswers.value = {};
      pendingQuestion.value = cr;
      const qText = ((cr.input.questions as Array<{question: string}>)?.[0]?.question ?? "Question").slice(0, 80);
      const qMid = S.nextMsgId++;
      pendingQuestionMsgId.value = qMid;
      messages.value.push({ id: qMid, role: "system-info", text: `❓ ${qText}` });
      chats.sendStatusEvent(props.chatId, { type: "WAIT" });
    } else if (cr.toolName === "ExitPlanMode") {
      planFeedback.value = "";
      pendingPlan.value = cr;
      const pMid = S.nextMsgId++;
      pendingPlanMsgId.value = pMid;
      messages.value.push({ id: pMid, role: "system-info", text: `📋 Plan ready for review` });
      chats.sendStatusEvent(props.chatId, { type: "WAIT" });
    } else if (["Edit", "Write", "MultiEdit", "NotebookEdit"].includes(cr.toolName)) {
      pendingDiff.value = cr;
      const filePath = ((cr.input.file_path ?? cr.input.path ?? "") as string);
      const dMid = S.nextMsgId++;
      pendingDiffMsgId.value = dMid;
      messages.value.push({ id: dMid, role: "system-info", text: `✏️ ${cr.toolName}: ${filePath.split("/").slice(-2).join("/")}` });
      chats.sendStatusEvent(props.chatId, { type: "PERMISSION_REQUEST" });
    } else {
      pendingPermission.value = cr;
      const pmMid = S.nextMsgId++;
      pendingPermissionMsgId.value = pmMid;
      messages.value.push({ id: pmMid, role: "system-info", text: `⚡ ${cr.toolName} wants permission` });
      chats.sendStatusEvent(props.chatId, { type: "PERMISSION_REQUEST" });
    }
    notifyPermission(cr);
    syncStore(); // surface busy/messageCount in the Sidebar
    scrollToBottom();
    return;
  }

  if (type === "system") {
    const sub = event.subtype as string;
    if (sub === "init") {
      const sid = (event.session_id as string) ?? "";
      sessionId.value = sid;
      chats.sync(props.chatId, { claudeSessionId: sid });
    }
    // session_title arrives as a `session.title` event; not read twice here.
    if (sub === "hook_started" || sub === "hook_response") return;
  }

  // NOTE: `assistant`, `user` (tool results) and `result`/`exit` are NOT read
  // here any more. They arrive as domain events on `chat-event-{chatId}`,
  // parsed once in Go (src-wails/providerruntime.go) and applied by onEvents.
  // What stays on this raw channel is only what has no domain event: the
  // control (permission) protocol and the CLI's own bookkeeping, both of which
  // are decisions for a UI rather than transcript.
}

// ── ACP transport ──────────────────────────────────────────────────────────
// Lines from acp-data-{chatId}: session/update notifications + session/prompt
// responses (turn done) + the {_burrow:"exit"} EOF marker.
function onAcpData(raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { console.warn(`[chat-diag] unparseable acp-data line, dropped (len=${raw.length})`); return; }
  lastActivityAt.value = Date.now();

  // The app-server resolves requests asynchronously. Keep the approval visible
  // until this acknowledgement arrives, so a failed response can be retried
  // instead of appearing as an automatic deny or a lost prompt.
  if (msg.method === "serverRequest/resolved") {
    const requestId = (msg.params as { requestId?: number })?.requestId;
    if (requestId != null && requestId === acpPermRpcId.value) {
      removeFeedMarker(acpPermMsgId.value); acpPermMsgId.value = null;
      acpPermReq.value = null;
      acpPermRpcId.value = null;
      permissionResponsePending.value = false;
      chats.sendStatusEvent(props.chatId, { type: "RESUME" });
      syncStore();
    }
    return;
  }

  // Session info emitted by acp_start after the handshake: sessionId (for resume)
  // + modes/configOptions (populate the permission-mode / model selectors).
  if (msg._burrow === "session") {
    const sid = msg.sessionId as string;
    if (sid) { sessionId.value = sid; chats.sync(props.chatId, { claudeSessionId: sid }); }
    acpModes.value = (msg.modes as AcpModes) ?? null;
    acpConfigOptions.value = (msg.configOptions as AcpConfigOption[]) ?? [];
    learnModels(agentKind.value, liveModels.value);
    // Finalize any messages rendered from a session/load replay (no turn-done fires
    // for a load) and persist the restored history.
    if (messages.value.some((m) => m.partial)) {
      settleTranscript(projection);
      finalizeStuckTools();
      saveMessages(props.chatId, messages.value);
      scrollToBottom();
    }
    restoreAcpSelections();
    return;
  }

  // Turn done — response to OUR session/prompt (id matches the in-flight prompt).
  // Other id'd responses share this channel: control replies refresh selectors;
  // everything else is ignored.
  if ('id' in msg && !('method' in msg)) {
    const rid = msg.id as number;
    if (acpControlIds.has(rid)) {
      acpControlIds.delete(rid);
      // Reply to a restore push we sent ourselves: apply it, but do NOT restore
      // from it — that is the ping-pong the guard exists for.
      const wasRestorePush = acpRestorePushIds.delete(rid);
      const result = msg.result as { configOptions?: AcpConfigOption[]; modes?: AcpModes } | undefined;
      if (result?.configOptions) acpConfigOptions.value = result.configOptions;
      if (result?.modes) acpModes.value = result.modes;
      // A model / mode / effort switch comes back with the adapter's whole
      // selector set reset to its defaults — put the user's picks back.
      if (!wasRestorePush && (result?.configOptions || result?.modes)) restoreAcpSelections();
      return;
    }
    // The turn is settled by the response to OUR session/prompt, and only the
    // sender can correlate that — which is why this one boundary stays on the
    // raw channel instead of becoming an event.
    if (acpPromptRpcId.value === null || rid !== acpPromptRpcId.value) return;
    acpPromptRpcId.value = null;
    finishTurn();
    return;
  }

  // The {_burrow:"exit"} EOF arrives as a `session.exited` event; onEvents owns
  // it, so both transports settle a dead runtime the same way.

  if (msg.method !== "session/update") return;

  // session/update notifications — message chunks, thoughts, tool calls and
  // the user turns a session/load replays — are read in Go and applied by
  // onEvents. Nothing on this channel needs them.
}

// Lines from acp-req-{chatId}: blocking session/request_permission requests.
function onAcpReq(raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { return; }
  if (msg.method === "item/tool/requestUserInput") {
    const params = (msg.params ?? {}) as Record<string, unknown>;
    const questions = ((params.questions ?? []) as Array<Record<string, unknown>>)
      .filter((question) => typeof question.id === "string" && typeof question.question === "string")
      .map((question) => ({
        id: question.id as string,
        header: typeof question.header === "string" ? question.header : "Question",
        question: question.question as string,
        isOther: question.isOther === true,
        isSecret: question.isSecret === true,
        options: ((question.options ?? []) as Array<Record<string, unknown>>)
          .filter((option) => typeof option.label === "string")
          .map((option) => ({ label: option.label as string, ...(typeof option.description === "string" ? { description: option.description } : {}) })),
      }));
    if (typeof msg.id !== "number" || questions.length === 0) return;
    codexUserInput.value = { rpcId: msg.id, questions };
    codexUserInputPending.value = false;
    chats.sendStatusEvent(props.chatId, { type: "WAIT" });
    syncStore();
    return;
  }
  const perm = parseAcpPermRequest(msg);
  if (!perm) return;

  acpPermRpcId.value = perm.rpcId;
  // Render the adapter's OWN option list (allow_once/allow_always/reject, or
  // ExitPlanMode's auto/acceptEdits/manual/keep-planning) — don't flatten to Y/N.
  acpPermReq.value = {
    rpcId: perm.rpcId,
    toolCallId: perm.toolCallId,
    title: perm.title,
    kind: perm.kind,
    options: perm.options,
    rawInput: perm.rawInput,
  };

  const isPlan = typeof perm.rawInput?.plan === "string";
  const pmMid = S.nextMsgId++;
  acpPermMsgId.value = pmMid;
  messages.value.push({ id: pmMid, role: "system-info", text: isPlan ? "📋 Plan ready for review" : `⚡ Permission: ${perm.title}` });
  chats.sendStatusEvent(props.chatId, { type: "PERMISSION_REQUEST" });
  notifyPermission({ requestId: String(perm.rpcId), toolName: perm.title, input: perm.rawInput, suggestions: [] } as CanUseToolReq);
  syncStore();
  scrollToBottom();
}

async function respondCodexUserInput(answers: Record<string, string[]>) {
  const request = codexUserInput.value;
  if (!request || codexUserInputPending.value) return;
  codexUserInputPending.value = true;
  try {
    await invoke("acp_respond_user_input", { id: props.chatId, rpcId: request.rpcId, answers });
    codexUserInput.value = null;
    chats.sendStatusEvent(props.chatId, { type: "RESUME" });
  } catch (error) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Unable to submit Codex input: ${error}` });
    codexUserInputPending.value = false;
  } finally {
    syncStore();
  }
}

function cancelCodexUserInput() {
  const request = codexUserInput.value;
  if (!request) return;
  const answers = Object.fromEntries(request.questions.map((question) => [question.id, [] as string[]]));
  void respondCodexUserInput(answers);
}

// Reply to a rich ACP permission request with the chosen adapter optionId.
async function acpRespond(optionId: string, optName: string, kind: string) {
  const r = acpPermReq.value;
  if (!r || permissionResponsePending.value) return;
  permissionResponsePending.value = true;
  const reject = kind.startsWith("reject");
  try {
    await invoke("acp_respond_permission", { id: props.chatId, rpcId: r.rpcId, optionId });
    messages.value.push({ id: S.nextMsgId++, role: "permission", text: `${reject ? "✗" : "✓"} ${optName}: ${r.title}` });
    saveMessages(props.chatId, messages.value);
    // serverRequest/resolved closes the prompt and updates the Sidebar state.
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Permission response failed: ${e}` });
    saveMessages(props.chatId, messages.value);
    permissionResponsePending.value = false;
  }
}

async function copyMessage(msg: ChatMessage) {
  if (!msg.text) return;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(msg.text);
    } else {
      const temporary = document.createElement("textarea");
      temporary.value = msg.text;
      temporary.setAttribute("readonly", "");
      temporary.style.position = "fixed";
      temporary.style.opacity = "0";
      document.body.appendChild(temporary);
      temporary.select();
      const copied = document.execCommand("copy");
      temporary.remove();
      if (!copied) throw new Error("Clipboard API unavailable");
    }
    copiedMessageId.value = msg.id;
    if (copyFeedbackTimer) clearTimeout(copyFeedbackTimer);
    copyFeedbackTimer = setTimeout(() => {
      copiedMessageId.value = null;
      copyFeedbackTimer = null;
    }, 1_200);
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Could not copy message: ${e}` });
    saveMessages(props.chatId, messages.value);
  }
}

// The welcome-screen composer creates a chat and its first prompt together. The
// send has to wait for the runtime: claude_start is awaited, but an ACP adapter
// only has a session after its handshake lands on acp-data.
// ponytail: 20 s cap, no retry — if the adapter is that slow the prompt stays in
// the feed as a plain user message and the user can resend.
async function sendInitialPrompt(prompt: string, images?: string[]) {
  emit("prompt-sent");
  if (usesRpcRuntime.value && !sessionId.value) {
    await new Promise<void>((resolve) => {
      const stop = watch(sessionId, (v) => { if (v) { stop(); resolve(); } });
      setTimeout(() => { stop(); resolve(); }, 20_000);
    });
  }
  await sendMessage(prompt, images);
}

async function sendMessage(forcedText?: string, extraImages?: string[]) {
  let text = (forcedText ?? inputText.value).trim();
  if (!text) return;
  // A cold chat (never opened this launch) has no process yet — start it now.
  if (await ensureRuntime()) return;
  const images = [...pendingImages.value, ...(extraImages ?? [])];
  // A follow-up is always a separate turn.  In particular, do not rely on an
  // ACP adapter's prompt queueing: Codex can reinterpret a second turn/start
  // as steering, and generic adapters have no negotiated queue capability.
  if (busy.value) {
    enqueueMessage(text, images);
    pendingImages.value = [];
    inputText.value = "";
    saveMessages(props.chatId, messages.value);
    await nextTick();
    autoResize();
    scrollToBottom(true);
    return;
  }
  if (!forcedText) {
    inputText.value = "";
    await nextTick();
    autoResize();
  }

  // /pr: build a PR description prompt from git diff
  if (text.match(/^\/pr\b/)) {
    try {
      const stat = await invoke<{ stdout: string }>("run_git", { cwd: props.cwd, args: ["diff", "HEAD~1", "--stat", "--no-color"] });
      const diff = await invoke<{ stdout: string }>("run_git", { cwd: props.cwd, args: ["diff", "HEAD~1", "--no-color"] });
      text = `Write a PR description for these changes:\n\n${stat.stdout}\n\`\`\`diff\n${diff.stdout.slice(0, 8000)}\n\`\`\``;
    } catch (e) {
      messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Error reading git diff: ${e}` });
      return;
    }
  }

  const msgImages = images.length > 0 ? images : undefined;
  messages.value.push({ id: S.nextMsgId++, role: "user", text, images: msgImages });
  // Snapshot the worktree before the turn so it is revertable from the History
  // panel. Best-effort, and a no-op outside a git repo.
  invoke("create_checkpoint", {
    cwd: props.cwd,
    ptyId: `chat-${props.chatId}`,
    label: text.slice(0, 60),
  }).catch(() => {});
  busy.value = true;
  chats.sendStatusEvent(props.chatId, { type: "START" });

  // Auto-title from first user message (only if still at default and Claude hasn't set one yet)
  if (!claudeGeneratedTitle.value) {
    const session = chats.sessions.find((s) => s.id === props.chatId);
    if (session && isDefaultTitle(session.title)) {
      chats.sync(props.chatId, { title: smartTitle(text) });
      void refineTitle(text);
    }
  }

  saveMessages(props.chatId, messages.value);
  syncStore();
  scrollToBottom(true);
  if (usesRpcRuntime.value) {
    try {
      pendingImages.value = [];
      acpPromptRpcId.value = await sendRpcRuntime(text, msgImages);
    } catch (e) {
      messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Error: ${e}` });
      busy.value = false;
      chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
      syncStore();
    }
    return;
  }
  try {
    pendingImages.value = [];
    await invoke("claude_send", { id: props.chatId, text, sessionId: sessionId.value || null, images: msgImages });
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Error: ${e}` });
    busy.value = false;
    chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
    syncStore();
  }
}

// Reply to a can_use_tool control_request. `response` is the inner decision object
// ({behavior:"allow",updatedInput} | {behavior:"deny",message}); the Rust side wraps it.
async function respondControl(requestId: string, response: Record<string, unknown>) {
  await invoke("claude_respond_control", { id: props.chatId, requestId, response });
  settleControlRequest(requestId);
  chats.sendStatusEvent(props.chatId, { type: "RESUME" });
  syncStore();
}

async function resolveClaudePrompt(
  cr: CanUseToolReq,
  response: Record<string, unknown>,
  clearPrompt: () => void,
) {
  nativeControlResponsePending.value = true;
  try {
    await respondControl(cr.requestId, response);
    clearPrompt();
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
    saveMessages(props.chatId, messages.value);
    // respondControl throws before RESUME fires — clear anyway so status doesn't stay stuck on waiting/permission.
    clearPrompt();
    chats.sendStatusEvent(props.chatId, { type: "RESUME" });
  } finally {
    nativeControlResponsePending.value = false;
    syncStore();
  }
}

// Generic tool permission + diff Accept/Reject (both pull from pendingPermission|pendingDiff).
async function respondPermission(allow: boolean, opts?: { always?: boolean; updatedInput?: Record<string, unknown>; message?: string }) {
  const cr = pendingPermission.value ?? pendingDiff.value;
  if (!cr) return;
  // ACP transport: reply to the agent's blocking request_permission.
  if (usesRpcRuntime.value && acpPermRpcId.value !== null) {
    // ACP optionIds are agent-defined — pick the matching one by kind from the
    // request's options (NOT a hardcoded string), else fall back to the first.
    const optsList = ((cr as unknown as { suggestions?: Array<{ optionId: string; kind: string }> }).suggestions ?? []);
    const pick = (...kinds: string[]) => {
      for (const k of kinds) { const o = optsList.find((x) => x.kind === k); if (o) return o.optionId; }
      return optsList[0]?.optionId ?? "";
    };
    const optionId = allow
      ? (opts?.always ? pick("allow_always", "allow_once") : pick("allow_once", "allow_always"))
      : pick("reject_once", "reject_always");
    messages.value.push({ id: S.nextMsgId++, role: "permission", text: `${allow ? "✓ Allowed" : "✗ Denied"}: ${cr.toolName}` });
    saveMessages(props.chatId, messages.value);
    invoke("acp_respond_permission", { id: props.chatId, rpcId: acpPermRpcId.value, optionId }).catch((e) => {
      messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Permission response failed: ${e}` });
    });
    acpPermRpcId.value = null;
    chats.sendStatusEvent(props.chatId, { type: "RESUME" });
    syncStore();
    return;
  }
  if (nativeControlResponsePending.value) return;
  nativeControlResponsePending.value = true;
  const detail = (cr.input.command ?? cr.input.file_path ?? cr.input.path ?? cr.description ?? "") as string;
  const detailStr = detail ? ` — ${detail.length > 80 ? detail.slice(0, 80) + "…" : detail}` : "";
  try {
    await respondControl(cr.requestId, allow
      ? { behavior: "allow", updatedInput: opts?.updatedInput ?? cr.input }
      : { behavior: "deny", message: opts?.message || "User denied this action." });
    removeFeedMarker(pendingPermissionMsgId.value); pendingPermissionMsgId.value = null;
    removeFeedMarker(pendingDiffMsgId.value); pendingDiffMsgId.value = null;
    pendingPermission.value = null;
    pendingDiff.value = null;
    if (allow && opts?.always) {
      const keys = ruleKeys(cr.toolName, cr.input);
      chats.addPermissionRule(keys[keys.length - 1]);
    }
    const label = allow ? (opts?.always ? "✓ Always allowed" : "✓ Allowed") : "✗ Denied";
    messages.value.push({ id: S.nextMsgId++, role: "permission", text: `${label}: ${cr.toolName}${detailStr}` });
    saveMessages(props.chatId, messages.value);
  } catch (e) {
    messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
    saveMessages(props.chatId, messages.value);
  } finally {
    nativeControlResponsePending.value = false;
    syncStore();
  }
}

function toggleOption(question: string, label: string, multi: boolean) {
  questionCustomAnswers.value[question] = "";
  const cur = questionAnswers.value[question] ?? [];
  if (multi) {
    questionAnswers.value[question] = cur.includes(label) ? cur.filter((l) => l !== label) : [...cur, label];
  } else {
    questionAnswers.value[question] = cur.includes(label) ? [] : [label];
  }
}
function isPicked(question: string, label: string) {
  return (questionAnswers.value[question] ?? []).includes(label);
}
// Typing an "Other" answer clears picked options — the two are mutually exclusive,
// same as t3code's setPendingUserInputCustomAnswer.
function setCustomAnswer(question: string, text: string) {
  questionCustomAnswers.value[question] = text;
  if (text.trim().length > 0) questionAnswers.value[question] = [];
}

async function submitQuestion() {
  const cr = pendingQuestion.value;
  if (!cr || !canSubmitQuestion.value || nativeControlResponsePending.value) return;
  // The tool reads input.answers keyed by question text. A multi-select answer
  // must stay an array (the CLI expects the same shape it gave options in) —
  // joining it into a comma string here is what made multi-select questions
  // look permanently stuck after Submit.
  const answers: Record<string, string | string[]> = {};
  for (const q of questionSpecs.value) {
    const custom = (questionCustomAnswers.value[q.question] ?? "").trim();
    if (custom) { answers[q.question] = custom; continue; }
    const labels = questionAnswers.value[q.question] ?? [];
    if (!labels.length) continue;
    answers[q.question] = q.multiSelect ? labels : labels[0];
  }
  await resolveClaudePrompt(cr, { behavior: "allow", updatedInput: { ...cr.input, answers } }, () => {
    if (pendingQuestion.value?.requestId !== cr.requestId) return; // superseded by a newer request
    removeFeedMarker(pendingQuestionMsgId.value); pendingQuestionMsgId.value = null;
    pendingQuestion.value = null;
  });
}
async function cancelQuestion() {
  const cr = pendingQuestion.value;
  if (!cr || nativeControlResponsePending.value) return;
  // allow with empty answers → tool reports "did not answer" (clean dismiss, no error).
  await resolveClaudePrompt(cr, { behavior: "allow", updatedInput: { ...cr.input, answers: {} } }, () => {
    if (pendingQuestion.value?.requestId !== cr.requestId) return; // superseded by a newer request
    removeFeedMarker(pendingQuestionMsgId.value); pendingQuestionMsgId.value = null;
    pendingQuestion.value = null;
  });
}

async function respondPlan(approve: boolean) {
  const cr = pendingPlan.value;
  if (!cr || nativeControlResponsePending.value) return;
  await resolveClaudePrompt(cr, approve
    ? { behavior: "allow", updatedInput: cr.input }
    : { behavior: "deny", message: planFeedback.value.trim() || "Keep planning — do not exit plan mode yet." }, () => {
      removeFeedMarker(pendingPlanMsgId.value); pendingPlanMsgId.value = null;
      pendingPlan.value = null;
    });
}

// Pick a permission mode from the header dropdown (default / acceptEdits / bypassPermissions).
// RPC runtimes update the active thread in place; restarting them loses the
// pending approval context and was the source of "unknown agent session" errors.
async function selectPermMode(mode: PermMode) {
  permMenuOpen.value = false;
  if (mode === permMode.value) return;

  const previousMode = permMode.value;
  permMode.value = mode;
  savePermMode(props.chatId, permMode.value);

  if (usesRpcRuntime.value) {
    try {
      const requestId = await invoke<number>("acp_set_mode", {
        id: props.chatId,
        modeId: mode,
      });
      acpControlIds.add(requestId);
    } catch (err) {
      // Do not leave the picker claiming a policy that the server rejected.
      permMode.value = previousMode;
      savePermMode(props.chatId, previousMode);
      messages.value.push({
        id: S.nextMsgId++,
        role: "assistant",
        text: `Couldn't update the permission mode: ${String(err)}`,
      });
      syncStore();
    }
    return;
  }

  await restartClaude();
}

// Stop + restart the claude proc (with --resume so the session continues) and
// settle all turn state. Used by abort AND every setting switch (mode/model/
// profile) — the teardown `exit` is suppressed so it emits no STOP, so the
// status machine MUST be settled here via INTERRUPT or the dot sticks at
// running/permission forever.
async function restartClaude() {
  suppressNextDone.value = true; // restart — don't toast on the teardown `exit`
  if (usesRpcRuntime.value) {
    await stopRpcRuntime().catch(() => {});
    await startRpcRuntime().catch(() => {});
    // Drop any in-flight permission gate — the rpc id is dead after restart.
    removeFeedMarker(acpPermMsgId.value); acpPermMsgId.value = null;
    acpPermReq.value = null; acpPermRpcId.value = null;
    busy.value = false;
    const lastAcp = messages.value[messages.value.length - 1];
    if (lastAcp?.partial) lastAcp.partial = false;
    chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
    syncStore();
    drainQueuedMessage();
    return;
  }
  // claude_stop removes the proc from the map so the subsequent claude_start actually spawns.
  // claude_abort (SIGINT) leaves a dead entry in the map → claude_start is a no-op.
  await invoke("claude_stop", { id: props.chatId }).catch(() => {});
  await invoke("claude_start", {
    id: props.chatId,
    cwd: props.cwd,
    resumeSessionId: sessionId.value || null,
    permissionMode: permMode.value,
    appendSystemPrompt: props.appendSystemPrompt || null,
    model: selectedModel.value,
    effort: selectedEffort.value,
    configDir: selectedProfile.value?.configDir || null,
    profileCommand: selectedProfile.value?.binary || null,
    profileArgs: selectedProfile.value?.args.join(" ") || null,
  }).catch(() => {});
  runtimeStarted.value = true;
  busy.value = false;
  // Drop any in-flight permission/question/plan prompts — the proc backing them is gone.
  pendingPermission.value = null;
  pendingDiff.value = null;
  pendingQuestion.value = null;
  pendingPlan.value = null;
  const last = messages.value[messages.value.length - 1];
  if (last?.partial) last.partial = false;
  chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
  syncStore();
  drainQueuedMessage();
}

async function abortTurn() {
  await restartClaude();
}

async function clearChat() {
  const rpc = usesRpcRuntime.value;
  await (rpc ? stopRpcRuntime() : invoke("claude_stop", { id: props.chatId })).catch(() => {});
  messages.value = [];
  sessionId.value = "";
  busy.value = false;
  clearQueuedMessages();
  pendingImages.value = [];
  turnStats.value = null;
  sessionCost.value = 0;
  claudeGeneratedTitle.value = false;
  acpPermRpcId.value = null;
  clearMessageHistory(props.chatId);
  chats.sync(props.chatId, { claudeSessionId: "", busy: false, messageCount: 0, title: `Chat` });
  const projSettings = scriptsStore.settingsFor(props.cwd);
  if (rpc) {
    const startErr = await startRpcRuntime().catch((e: unknown) => e);
    if (startErr) messages.value.push({ id: S.nextMsgId++, role: 'assistant', text: `Failed to start ${runtimeLabel.value}: ${startErr}` });
    return;
  }
  await invoke("claude_start", {
    id: props.chatId,
    cwd: props.cwd,
    permissionMode: permMode.value,
    appendSystemPrompt: props.appendSystemPrompt || null,
    model: selectedModel.value,
    effort: selectedEffort.value,
    configDir: selectedProfile.value?.configDir || projSettings.claude_config_dir || null,
    profileCommand: selectedProfile.value?.binary || null,
    profileArgs: selectedProfile.value?.args.join(" ") || null,
  }).catch(() => {});
  runtimeStarted.value = true;
  // Switched to a stream-json agent at runtime → ensure the claude-data listener
  // exists (onMounted only attaches it when the chat starts as stream-json).
  await S.listenClaude();
}

// `/cmd` or `$skill` token immediately before the cursor — at line start OR after
// whitespace, so command help works mid-message, not only when the input starts
// with the trigger. `/` completes built-in commands, `$` completes skills; both
// insert `/name` (the invocation Claude understands — `$` is only the menu trigger).
function slashQueryBeforeCursor(): { lead: string; q: string; full: string; trigger: string } | null {
  const el = inputEl.value;
  const pos = el?.selectionStart ?? inputText.value.length;
  const upto = inputText.value.slice(0, pos);
  const m = upto.match(/(^|\s)([/$])([^\s/$]*)$/);
  return m ? { lead: m[1], trigger: m[2], q: m[3], full: m[0] } : null;
}

function updateSuggestions() {
  const m = slashQueryBeforeCursor();
  if (!m) { suggestions.value = []; return; }
  const q = m.q.toLowerCase();
  const source = m.trigger === "$" ? skillCommands.value : allCommands.value;
  suggestions.value = source.filter(
    (c) => c.name.toLowerCase().startsWith(q)
  );
  suggestionIdx.value = 0;
}

function applySuggestion(name: string) {
  const el = inputEl.value;
  const pos = el?.selectionStart ?? inputText.value.length;
  const m = slashQueryBeforeCursor();
  if (!m) { inputText.value = `/${name} `; }
  else {
    const upto = inputText.value.slice(0, pos);
    const after = inputText.value.slice(pos);
    const base = upto.slice(0, upto.length - m.full.length);
    inputText.value = `${base}${m.lead}/${name} ${after}`;
  }
  suggestions.value = [];
  nextTick(() => { inputEl.value?.focus(); autoResize(); });
}

function scrollSuggestionIntoView(idx: number) {
  nextTick(() => {
    if (!suggestionsEl.value) return;
    const items = suggestionsEl.value.querySelectorAll(".cmd-suggestion");
    items[idx]?.scrollIntoView({ block: "nearest" });
  });
}

// ── @-mention: complete a file path from the repo file list ─────────────────
function atQueryBeforeCursor(): string | null {
  const el = inputEl.value;
  const pos = el?.selectionStart ?? inputText.value.length;
  const upto = inputText.value.slice(0, pos);
  const m = upto.match(/(?:^|\s)@([^\s@]*)$/);
  return m ? m[1] : null;
}

async function updateAtSuggestions() {
  const q = atQueryBeforeCursor();
  if (q === null) { atSuggestions.value = []; return; }
  await ensureFileList();
  if (atQueryBeforeCursor() !== q) return; // cursor moved while loading
  const ql = q.toLowerCase();
  atSuggestions.value = fileList.value
    .filter((p) => p.toLowerCase().includes(ql))
    .sort((a, b) => {
      const ab = a.slice(a.lastIndexOf("/") + 1).toLowerCase();
      const bb = b.slice(b.lastIndexOf("/") + 1).toLowerCase();
      return (Number(!ab.startsWith(ql)) - Number(!bb.startsWith(ql))) || a.length - b.length;
    })
    .slice(0, 8);
  atIdx.value = 0;
}

function applyAtSuggestion(path: string) {
  const el = inputEl.value;
  const pos = el?.selectionStart ?? inputText.value.length;
  const upto = inputText.value.slice(0, pos);
  const after = inputText.value.slice(pos);
  const m = upto.match(/@([^\s@]*)$/);
  if (!m) return;
  // Insert the @path inline where it was typed (rendered as a pill in the bubble).
  const base = upto.slice(0, upto.length - m[0].length);
  const sep = after.startsWith(" ") ? "" : " ";
  inputText.value = `${base}@${path}${sep}${after}`;
  atSuggestions.value = [];
  nextTick(() => {
    inputEl.value?.focus();
    autoResize();
    const el2 = inputEl.value;
    if (el2) { const c = base.length + path.length + 1 + sep.length; el2.selectionStart = el2.selectionEnd = c; }
  });
}

function onKeydown(e: KeyboardEvent) {
  if (pendingPermission.value || pendingDiff.value) {
    if (e.key === "y" || e.key === "Y") { e.preventDefault(); respondPermission(true); return; }
    if (e.key === "n" || e.key === "N") { e.preventDefault(); respondPermission(false); return; }
  }
  if (pendingQuestion.value && e.key === "Escape") { e.preventDefault(); cancelQuestion(); return; }
  if (pendingPlan.value && e.key === "Escape") { e.preventDefault(); respondPlan(false); return; }
  if (busy.value && e.key === "Escape" && !pendingPermission.value && !pendingDiff.value) { e.preventDefault(); abortTurn(); nextTick(() => inputEl.value?.focus()); return; }
  if ((e.metaKey || e.ctrlKey) && e.key === "k") { e.preventDefault(); clearChat(); return; }
  if (e.key === "ArrowUp" && inputText.value === "" && !busy.value) {
    const lastUser = [...messages.value].reverse().find((m) => m.role === "user");
    if (lastUser) {
      e.preventDefault();
      inputText.value = lastUser.text;
      messages.value = messages.value.filter((m) => m !== lastUser);
      nextTick(() => { inputEl.value?.focus(); autoResize(); const el = inputEl.value; if (el) el.selectionStart = el.selectionEnd = el.value.length; });
      return;
    }
  }
  if (atSuggestions.value.length > 0) {
    if (e.key === "ArrowDown") { e.preventDefault(); atIdx.value = Math.min(atIdx.value + 1, atSuggestions.value.length - 1); return; }
    if (e.key === "ArrowUp") { e.preventDefault(); atIdx.value = Math.max(atIdx.value - 1, 0); return; }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) { e.preventDefault(); applyAtSuggestion(atSuggestions.value[atIdx.value]); return; }
    if (e.key === "Escape") { atSuggestions.value = []; return; }
  }
  if (suggestions.value.length > 0) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      suggestionIdx.value = Math.min(suggestionIdx.value + 1, suggestions.value.length - 1);
      scrollSuggestionIntoView(suggestionIdx.value);
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      suggestionIdx.value = Math.max(suggestionIdx.value - 1, 0);
      scrollSuggestionIntoView(suggestionIdx.value);
      return;
    }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
      e.preventDefault();
      applySuggestion(suggestions.value[suggestionIdx.value].name);
      return;
    }
    if (e.key === "Escape") { suggestions.value = []; return; }
  }
  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); sendMessage(); }
}

function onInput() {
  autoResize();
  updateSuggestions();
  updateAtSuggestions();
  nextTick(syncHighlightScroll);
}

function onPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const item of Array.from(items)) {
    if (item.type.startsWith("image/")) {
      e.preventDefault();
      const file = item.getAsFile();
      if (!file) continue;
      const reader = new FileReader();
      reader.onload = () => {
        if (typeof reader.result === "string") pendingImages.value.push(reader.result);
      };
      reader.readAsDataURL(file);
    }
  }
}

function autoResize() {
  const el = inputEl.value;
  if (!el) return;
  el.style.height = "auto";
  el.style.height = Math.min(el.scrollHeight, 160) + "px";
}

function onWindowKeydown(e: KeyboardEvent) {
  if (!pendingPermission.value && !pendingDiff.value) return;
  if (document.activeElement === inputEl.value) return; // handled by onKeydown
  if (e.key === "y" || e.key === "Y") { e.preventDefault(); respondPermission(true); }
  if (e.key === "n" || e.key === "N") { e.preventDefault(); respondPermission(false); }
}

// One-time migration of the legacy per-chat-id localStorage keys into config.json.
// Enumerates ALL chat ids found in localStorage (not just props.chatId), so every
// chat's history/settings survive the move. Guarded per config-key so it only runs
// once app-wide (subsequent calls see the config key already populated and no-op).
function migrateLegacyChatConfig() {
  const MISSING = Symbol("missing");

  // Legacy per-chat transcripts: burrow.claude.msgs.<id>. These now go straight
  // to SQLite — routing them through config.json's chatMessageHistory would drop
  // them silently, since Go's config→SQLite migration has already run by the
  // time this executes and nothing reads that key any more.
  {
    const re = /^burrow\.claude\.msgs\.(\d+)$/;
    const keys = Object.keys(localStorage).filter((k) => re.test(k));
    for (const k of keys) {
      const id = Number(k.match(re)![1]);
      const raw = localStorage.getItem(k);
      if (raw === null) continue;
      try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) continue;
        void invoke("save_chat_messages", { chatId: id, messages: JSON.stringify(parsed) })
          .then(() => localStorage.removeItem(k))
          .catch(() => { /* keep the key so the next launch retries */ });
      } catch { /* skip malformed entry */ }
    }
  }

  // chatAcpSettings: burrow.acpMode.<id> / burrow.acpModel.<id> / burrow.acpEffort.<id>
  if (getConfig<unknown>("chatAcpSettings", MISSING) === MISSING) {
    const reMode = /^burrow\.acpMode\.(\d+)$/;
    const reModel = /^burrow\.acpModel\.(\d+)$/;
    const reEffort = /^burrow\.acpEffort\.(\d+)$/;
    const rec: Record<string, AcpChatSettings> = {};
    const keys = Object.keys(localStorage).filter((k) => reMode.test(k) || reModel.test(k) || reEffort.test(k));
    for (const k of keys) {
      const raw = localStorage.getItem(k);
      if (raw === null) continue;
      let m = k.match(reMode);
      if (m) { (rec[m[1]] ??= {}).mode = raw; continue; }
      m = k.match(reModel);
      if (m) { (rec[m[1]] ??= {}).model = raw; continue; }
      m = k.match(reEffort);
      if (m) { (rec[m[1]] ??= {}).effort = raw; continue; }
    }
    setConfig("chatAcpSettings", rec);
    for (const k of keys) localStorage.removeItem(k);
  }

  // chatProfileSelection: burrow.claude.profileId.<id>
  if (getConfig<unknown>("chatProfileSelection", MISSING) === MISSING) {
    const re = /^burrow\.claude\.profileId\.(\d+)$/;
    const rec: Record<string, string> = {};
    const keys = Object.keys(localStorage).filter((k) => re.test(k));
    for (const k of keys) {
      const id = k.match(re)![1];
      const raw = localStorage.getItem(k);
      if (raw !== null) rec[id] = raw;
    }
    setConfig("chatProfileSelection", rec);
    for (const k of keys) localStorage.removeItem(k);
  }

  // chatLastUsedModel: global "burrow.claude.model" (or the per-instance MODEL_KEY).
  migrateFromLocalStorage(MODEL_KEY, MODEL_CONFIG_KEY);

  // chatPermissionMode: burrow.claude.permMode.<id> / burrow.claude.permMode.last /
  // burrow.claude.dangerous.<id> — collapse into one record.
  if (getConfig<unknown>("chatPermissionMode", MISSING) === MISSING) {
    const rePerm = /^burrow\.claude\.permMode\.(\d+)$/;
    const reDanger = /^burrow\.claude\.dangerous\.(\d+)$/;
    const byChat: Record<string, string> = {};
    const dangerousByChat: Record<string, boolean> = {};
    const keys = Object.keys(localStorage).filter(
      (k) => k !== PERM_LAST_KEY && (rePerm.test(k) || reDanger.test(k)),
    );
    for (const k of keys) {
      let m = k.match(rePerm);
      if (m) { const v = localStorage.getItem(k); if (v !== null) byChat[m[1]] = v; continue; }
      m = k.match(reDanger);
      if (m) { dangerousByChat[m[1]] = localStorage.getItem(k) === "1"; continue; }
    }
    const last = localStorage.getItem(PERM_LAST_KEY);
    const cfg: ChatPermissionModeConfig = { byChat, dangerousByChat };
    if (last !== null) cfg.last = last;
    setConfig("chatPermissionMode", cfg);
    for (const k of keys) localStorage.removeItem(k);
    if (last !== null) localStorage.removeItem(PERM_LAST_KEY);
  }
}

// --- Lazy runtime start -----------------------------------------------------
// A chat's CLI is expensive (each idle `claude` holds ~150 MB), and Terminal.vue
// keeps EVERY chat of every opened workspace mounted. Starting a process per
// mount meant 40 persisted chats = 40 live CLIs = multi-GB at launch for chats
// the user never opened. So the process is started on demand instead: on send,
// or up front only for a chat spawned with a prompt to deliver. agentproc's
// sweeper takes it back once the chat goes quiet.
let runtimeStarting: Promise<unknown> | null = null;

// Only a chat spawned with a prompt still to deliver warms itself up. Merely
// looking at a chat does not need a process — the transcript is on disk.
function shouldAutoStart(): boolean {
  return props.initialPrompt !== undefined;
}

// Starts the backing process once. Returns a truthy error if the start failed
// (the message is already surfaced in the feed), falsy on success.
async function ensureRuntime(): Promise<unknown> {
  if (runtimeStarted.value) return null;
  if (runtimeStarting) return runtimeStarting;
  runtimeStarting = (async (): Promise<unknown> => {
    const stored = chats.sessions.find((s) => s.id === props.chatId)?.claudeSessionId ?? sessionId.value ?? "";
    if (usesRpcRuntime.value) {
      await scriptsStore.loadForPath(props.cwd);
      const startErr = await startRpcRuntime().catch((e: unknown) => e);
      if (startErr) {
        messages.value.push({ id: S.nextMsgId++, role: 'assistant', text: `Failed to start ${runtimeLabel.value}: ${startErr}` });
        return startErr;
      }
      runtimeStarted.value = true;
      return null;
    }
    const startErr = await invoke("claude_start", {
      id: props.chatId,
      cwd: props.cwd,
      resumeSessionId: stored || null,
      permissionMode: permMode.value,
      appendSystemPrompt: props.appendSystemPrompt || null,
      model: selectedModel.value,
      effort: selectedEffort.value,
      configDir: selectedProfile.value?.configDir || null,
      profileCommand: selectedProfile.value?.binary || null,
      profileArgs: selectedProfile.value?.args.join(" ") || null,
    }).catch((e: unknown) => {
      // A swallowed failure here (missing `claude` binary, bad profile) used to
      // look like a chat that simply never answers.
      messages.value.push({ id: S.nextMsgId++, role: "assistant", text: `Failed to start Claude CLI: ${e}` });
      return e;
    });
    await S.listenClaude();
    if (!startErr) runtimeStarted.value = true;
    return startErr ?? null;
  })();
  try {
    return await runtimeStarting;
  } finally {
    runtimeStarting = null;
  }
}

onMounted(async () => {
  // Install this mount's reducers into the session and take a reference. The
  // session already holds the listeners; setHandlers just points them at the
  // live view, so no stream is ever torn down and re-attached on a remount.
  S.setHandlers({ onEvents, onLine, onAcpData, onAcpReq });
  // The transcript arrives on this channel for BOTH transports, so it is
  // attached unconditionally — unlike the raw ones, which are per-runtime.
  await S.listenEvents();
  S.retain();
  // Config must be loaded (and legacy localStorage migrated) before any of the
  // config-backed refs below are trusted — reload them here once configReady settles.
  await configReady;
  migrateLegacyChatConfig();
  // Only read the transcript back from SQLite when the session has none. A
  // non-empty session is the LIVE copy — it kept receiving while this view was
  // unmounted, so it is ahead of the DB, and assigning over it would throw the
  // newer part away (exactly the hole this plan is about).
  if (messages.value.length === 0) {
    messages.value = await loadMessages(props.chatId);
    // Queue markers are persisted with the transcript. Rebuild the in-memory
    // scheduler after a relaunch before any runtime can dispatch a follow-up.
    restoreQueuedMessages();
    // Ids must continue past the loaded history, or new messages collide with
    // old ones on the same `:key`.
    S.nextMsgId = messages.value.reduce((max, m) => Math.max(max, m.id + 1), 0);
    // Catch up on anything the agent said after that transcript was written —
    // i.e. a turn that was in flight when the app was last closed or crashed.
    await replayChatStream(props.chatId);
  }
  // A remount is how you come back to a chat now (fáze 3 unmounts hidden chat
  // leaves), so land on the newest message. The activeByWs watcher below cannot
  // do it: the active id is already this chat before the fresh instance mounts,
  // so it never fires and the view opened scrolled to the top of the history.
  scrollToBottom(true);
  selectedProfileId.value = loadProfileId(props.chatId);
  selectedModel.value = loadModel();
  // Pin the resolved model to this chat on first mount, so it survives a
  // remount even if the shared last-used key moves on in the meantime.
  if (!storedChatModel()) saveChatModel(selectedModel.value);
  selectedEffort.value = loadEffort();
  if (!storedChatEffort()) saveChatEffort(selectedEffort.value);
  permMode.value = loadPermMode(props.chatId);

  // Mounted normally means visible (Terminal.isChatVisible), but a chat spawned
  // with a prompt can mount unwatched — marking that one seen would clear a dot
  // nobody looked at.
  if (props.isWatching ?? true) chats.markSeen(props.chatId);
  window.addEventListener("keydown", onWindowKeydown);
  window.addEventListener("mousedown", onPermMenuOutside);
  window.addEventListener("mousedown", onEffortMenuOutside);
  window.addEventListener("mousedown", onProfileMenuOutside);
  window.addEventListener("mousedown", onAcpMenuOutside);
  // Float (compact) control chat: pre-allow `burrow` Bash commands so routine
  // control calls (focus/list/new-tab/spawn) don't prompt every time. User can
  // still tighten via the perm-mode switch / Deny.
  if (props.compact) chats.addPermissionRule("Bash:burrow");
  const stored = chats.sessions.find((s) => s.id === props.chatId)?.claudeSessionId ?? "";
  if (stored) sessionId.value = stored;
  publishRemoteChat();
  // The stream-json listener is JS-only and free — attach it even when the
  // runtime is still cold, so a later ensureRuntime() streams immediately.
  if (!usesRpcRuntime.value) {
    await S.listenClaude();
  }
  if (shouldAutoStart()) {
    const startErr = await ensureRuntime();
    if (!startErr && props.initialPrompt) sendInitialPrompt(props.initialPrompt, props.initialImages);
    else if (!startErr) drainQueuedMessage();
    if (usesRpcRuntime.value) return;
  }

  // Load account info (plan, 5h window) — non-blocking.
  invoke<AccountInfo>("claude_get_account", { cwd: props.cwd })
    .then((info) => { accountInfo.value = info; })
    .catch(() => {});

  // Load installed skills into the `$` completion list (`/` stays built-ins only).
  // Map-based dedup ensures no duplicates regardless of list_skills returning overlaps.
  try {
    const skills = await invoke<{ name: string; description: string; enabled: boolean }[]>("list_skills");
    const merged = new Map<string, Command>();
    for (const s of skills) {
      if (s.enabled) merged.set(s.name, { name: s.name, description: s.description || `/${s.name} skill` });
    }
    skillCommands.value = [...merged.values()].sort((a, b) => a.name.localeCompare(b.name));
  } catch { /* browser-only dev without Tauri */ }
});

onBeforeUnmount(() => {
  if (copyFeedbackTimer) clearTimeout(copyFeedbackTimer);
  if (stallTimer) clearInterval(stallTimer);
  window.removeEventListener("keydown", onWindowKeydown);
  window.removeEventListener("mousedown", onPermMenuOutside);
  window.removeEventListener("mousedown", onEffortMenuOutside);
  window.removeEventListener("mousedown", onProfileMenuOutside);
  window.removeEventListener("mousedown", onAcpMenuOutside);
  // Hand the session back instead of unsubscribing: it drops its listeners only
  // when nothing is in flight (lib/chatSession.ts). A turn running in a tab the
  // user navigated away from keeps streaming into the session and is simply
  // there on the next mount.
  S.release();
  // NOTE: deliberately do NOT stop the adapter/CLI here. The backend process
  // lifetime is tied to the SESSION, not this component's mount — a background
  // chat gets unmounted whenever its host tears down (FloatChat when ws.active
  // flips, ManagerBar when the repo set changes, a workspace leaving `opened`),
  // and killing the proc there halted live agents mid-turn ("blik"). Teardown
  // now happens on explicit close (Terminal.closeTab / closePane → stopChatSession)
  // and on `remove()` in the claudeChats store.
});

watch(() => props.chatId, () => nextTick(() => inputEl.value?.focus()));

// Scroll to bottom when this chat becomes the active one (user clicked it in sidebar).
watch(() => chats.activeByWs[props.workspaceId], (activeId) => {
  if (activeId === props.chatId) nextTick(() => scrollToBottom(true));
});

// Exposed for host shells (e.g. the Manager bar) that drive this chat from an
// external compact input: send a message and focus the textarea.
function focusInput() {
  nextTick(() => { inputEl.value?.focus(); autoResize(); });
}
function getPermMode(): PermMode {
  return permMode.value;
}
defineExpose({ sendMessage, focusInput, selectModel, selectedModel, allCommands, getPermMode, selectPermMode, permMode });
</script>

<style scoped>
.claude-chat {
  /* Inherit the app theme (set as :root vars by the ui store); fall back to the
     original dark palette when a var is absent. Every color used below reads
     through one of these, so the chat re-skins with the active theme (and the
     per-agent accent) instead of carrying its own fixed dark palette. */
  --chat-bg: var(--bg-base, #0f0f11);
  --chat-surface: var(--bg-panel, #18181c);
  --chat-dropdown: var(--bg-dropdown, #1a1726);
  --chat-border: var(--border, rgba(255,255,255,0.08));
  --chat-accent: var(--accent, #ec4899);
  --chat-accent-dim: var(--accent-dim, #6d28d9);
  --chat-text: var(--text-primary, rgba(255,255,255,0.88));
  --chat-text-secondary: var(--text-secondary, rgba(255,255,255,0.6));
  --chat-muted: var(--text-muted, rgba(255,255,255,0.42));
  --chat-user-bg: color-mix(in srgb, var(--chat-accent) 14%, var(--chat-bg));
  --chat-user-border: color-mix(in srgb, var(--chat-accent) 35%, transparent);
  /* Semantic status hues for gate banners — warn/success ride the theme's own
     yellow/green so they shift with it; info has no theme slot (diff preview
     and question prompts aren't a themed surface color) so it stays a fixed,
     deliberately neutral blue across every theme. */
  --chat-warn: var(--yellow, #f59e0b);
  --chat-success: var(--green, #10b981);
  --chat-info: #3b82f6;
}

.diff-line { line-height: 1.5; }
.diff-add { color: var(--success); }
.diff-del { color: var(--destructive); }

.chat-header-btn { position: relative; }

.chat-runtime-dot {
  box-shadow: 0 0 7px color-mix(in srgb, var(--success, #22c55e) 75%, transparent);
}

.btn-danger-active { color: var(--red, #ef4444) !important; background: color-mix(in srgb, var(--red, #ef4444) 15%, transparent) !important; }
.btn-active { color: var(--chat-accent) !important; background: color-mix(in srgb, var(--chat-accent) 15%, transparent) !important; }

/* Permission-mode dropdown */
.perm-mode-dropdown { position: relative; display: flex; }
.perm-mode-menu {
  position: fixed;
  z-index: 1000;
  min-width: 330px;
  padding: 5px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  /* Match the provider/model picker: all composer popovers share one surface. */
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.6);
}
.perm-mode-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: 100%;
  min-height: 58px;
  padding: 9px 10px;
  background: none;
  border: none;
  border-radius: 8px;
  color: var(--chat-text-secondary);
  font-size: 13.5px;
  font-weight: 550;
  text-align: left;
  cursor: pointer;
  transition: color .12s ease-out, background-color .12s ease-out;
}
.perm-mode-item > svg { flex: 0 0 auto; margin-top: 1px; color: var(--chat-muted); }
.perm-mode-item:hover,
.perm-mode-item:focus-visible {
  color: var(--chat-text);
  background: var(--bg-hover) !important;
  outline: none;
}
.perm-mode-item:hover > svg { color: var(--chat-accent); }
.perm-mode-item-active { color: var(--chat-accent); background: color-mix(in srgb, var(--chat-accent) 12%, transparent); }
.perm-mode-item-active > svg { color: var(--chat-accent); }
.perm-mode-item-danger { color: var(--red, #ef4444); }
.perm-mode-item-danger:hover,
.perm-mode-item-danger:focus-visible {
  background: var(--bg-hover) !important;
}
.perm-mode-item-danger.perm-mode-item-active { color: var(--red, #ef4444); background: color-mix(in srgb, var(--red, #ef4444) 14%, transparent); }
.perm-mode-copy { display: grid; gap: 3px; min-width: 0; text-align: left; }
.perm-mode-copy > span:last-child { color: var(--chat-muted); font-size: 11.5px; font-weight: 400; line-height: 1.35; }
.perm-mode-item:hover .perm-mode-copy > span:last-child,
.perm-mode-item-active .perm-mode-copy > span:last-child { color: color-mix(in srgb, currentColor 68%, var(--chat-muted)); }
.perm-mode-label { font-size: 12px; font-weight: 650; }
.perm-mode-caret { opacity: .6; margin-left: -1px; }

/* Permission-gate banners: shared animation */
.perm-slide-in { animation: perm-slide-in 0.18s cubic-bezier(0.16, 1, 0.3, 1); }
@keyframes perm-slide-in {
  from { opacity: 0; transform: translateY(-4px); }
  to   { opacity: 1; transform: translateY(0); }
}

/* Status banners (permission / diff / plan / ACP) — one shared color system
   instead of each banner carrying its own inline hex + a decorative
   border-left stripe. A full tinted border replaces the stripe. */
.status-banner {
  border-radius: 10px;
  border: 1px solid var(--_sb-border);
  background: var(--_sb-bg);
  box-shadow: 0 8px 24px -6px rgba(0,0,0,0.3);
}
.status-banner--warn {
  --_sb-border: color-mix(in srgb, var(--chat-warn) 34%, transparent);
  --_sb-bg: color-mix(in srgb, var(--chat-warn) 10%, var(--chat-surface));
}
.status-banner--success {
  --_sb-border: color-mix(in srgb, var(--chat-success) 30%, transparent);
  --_sb-bg: color-mix(in srgb, var(--chat-success) 9%, var(--chat-surface));
}
.status-banner--info {
  --_sb-border: color-mix(in srgb, var(--chat-info) 30%, transparent);
  --_sb-bg: var(--chat-surface);
}
.status-banner--warn .perm-icon { color: var(--chat-warn); }
.status-banner--success .perm-icon { color: var(--chat-success); }
.status-banner--info .perm-icon { color: var(--chat-info); }

/* Shared perm-btn design system, reused across every permission/diff/plan/
   question/ACP banner — kept as real classes rather than repeating these
   color/state combinations as Tailwind utilities at every call site. */
.perm-btn {
  display: flex;
  align-items: center;
  gap: 5px;
  border: none;
  border-radius: 5px;
  font-size: 11px;
  font-weight: 600;
  font-family: var(--font-ui);
  padding: 5px 11px;
  cursor: pointer;
  flex-shrink: 0;
  transition: filter .1s;
}
.perm-btn:hover { filter: brightness(1.1); }
.perm-btn:active { filter: brightness(0.9); }
.perm-allow { background: #16a34a; color: #fff; }
.perm-always { background: color-mix(in srgb, #16a34a 22%, var(--bg-panel)); color: var(--text-primary); }
.perm-deny  { background: #b91c1c; color: #fff; }
.perm-neutral { background: color-mix(in srgb, var(--agent-accent, #a855f7) 20%, var(--bg-panel)); color: var(--text-primary); border: 1px solid color-mix(in srgb, var(--agent-accent, #a855f7) 35%, transparent); }
.perm-btn:disabled { opacity: 0.4; cursor: default; filter: none; }
.perm-kbd {
  font-size: 9px;
  font-family: var(--font-mono);
  font-weight: 700;
  background: rgba(255,255,255,0.2);
  border-radius: 3px;
  padding: 1px 4px;
  line-height: 1.4;
}
.perm-dropdown-item {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 10px;
  background: none;
  border: none;
  border-radius: 5px;
  font-size: 12px;
  color: var(--chat-text-secondary);
  cursor: pointer;
  text-align: left;
  transition: background .1s ease-out, color .1s ease-out;
}
.perm-dropdown-item:hover { background: color-mix(in srgb, var(--chat-text) 7%, transparent); color: var(--chat-text); }
.perm-pattern {
  font-size: 10px;
  color: var(--chat-muted);
  background: color-mix(in srgb, var(--chat-text) 7%, transparent);
  border-radius: 3px;
  padding: 1px 4px;
}

/* ACP permission banner: plan body just needs the taller scroll area; color
   now comes from the shared .status-banner--success/--info modifiers. */
.acp-perm-plan .plan-body { max-height: 320px; overflow: auto; }

/* Avatars */
.agent-avatar {
  background: radial-gradient(circle at 30% 25%, color-mix(in srgb, var(--agent-accent, #ec4899) 80%, #fff) 0%, var(--agent-accent, #ec4899) 60%, color-mix(in srgb, var(--agent-accent, #ec4899) 55%, #000) 100%);
  box-shadow: 0 1px 2px rgba(0,0,0,0.35), inset 0 1px 0 rgba(255,255,255,0.18);
}

.mention-pill {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 6px;
  margin: 0 1px;
  background: color-mix(in srgb, var(--chat-info) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--chat-info) 32%, transparent);
  border-radius: 10px;
  font-size: 0.92em;
  vertical-align: baseline;
}
/* Icon as a mask instead of an <svg> child: the pill is now built by
   pillifyMentions() into an HTML string, where a Vue icon component can't go.
   Path is phosphor "file", regular weight. */
.mention-pill::before {
  content: "";
  width: 0.85em;
  height: 0.85em;
  flex-shrink: 0;
  background: var(--chat-info);
  -webkit-mask: var(--mention-pill-icon) center / contain no-repeat;
  mask: var(--mention-pill-icon) center / contain no-repeat;
}
.mention-pill {
  --mention-pill-icon: url('data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><path d="M213.66,82.34l-56-56A8,8,0,0,0,152,24H56A16,16,0,0,0,40,40V216a16,16,0,0,0,16,16H200a16,16,0,0,0,16-16V88A8,8,0,0,0,213.66,82.34ZM160,51.31,188.69,80H160ZM200,216H56V40h88V88a8,8,0,0,0,8,8h48V216Z"/></svg>');
}

/* A user bubble already sits on the accent surface, so the assistant's dark
   code-block fill would fight it — quiet the fences down inside the bubble. */
.bubble-user .md-body :deep(pre) {
  background: rgba(0, 0, 0, 0.28);
  border-color: rgba(255, 255, 255, 0.12);
}
.bubble-user .md-body :deep(code) { background: rgba(0, 0, 0, 0.25); color: inherit; }
.bubble-user .md-body :deep(a) { color: inherit; }

/* Tool row — quiet activity-log line, expandable */
.tool-row {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px 3px 6px;
  background: color-mix(in srgb, var(--agent-accent, #ec4899) 7%, transparent);
  border: 1px solid color-mix(in srgb, var(--agent-accent, #ec4899) 18%, transparent);
  border-radius: 8px;
  font-size: 11px;
  color: var(--text-secondary, rgba(255,255,255,0.55));
  cursor: pointer;
  user-select: none;
  max-width: 100%;
  overflow: hidden;
  transition: background .1s, color .1s, border-color .1s;
}
.tool-row:hover {
  background: color-mix(in srgb, var(--agent-accent, #ec4899) 13%, transparent);
  color: var(--text-primary);
}
.tool-row-running { border-color: color-mix(in srgb, var(--agent-accent, #ec4899) 20%, transparent); }
.tool-row-failed {
  background: color-mix(in srgb, var(--destructive, #ef4444) 8%, transparent);
  border-color: color-mix(in srgb, var(--destructive, #ef4444) 30%, transparent);
}
.changed-files {
  width: 100%;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  font-size: 11px;
  color: var(--text-secondary, rgba(255,255,255,0.55));
}
.changed-files-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 9px;
  background: color-mix(in srgb, var(--agent-accent, #ec4899) 6%, transparent);
  border-bottom: 1px solid var(--border);
}
.changed-files-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  font-family: var(--font-mono, monospace);
}
.changed-files-row + .changed-files-row { border-top: 1px solid color-mix(in srgb, var(--border) 45%, transparent); }
.tool-caret {
  flex-shrink: 0;
  color: var(--agent-accent, #ec4899);
  transition: transform .15s;
}
.tool-caret-open { transform: rotate(90deg); }
.tool-icon { color: var(--agent-accent, #ec4899); flex-shrink: 0; }
.tool-row-failed .tool-icon { color: var(--destructive, #ef4444); }
.tool-status-icon { flex-shrink: 0; }
.tool-pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--agent-accent, #ec4899);
  animation: tool-pulse 1.4s ease-in-out infinite;
}
@keyframes tool-pulse {
  0%, 100% { opacity: 0.32; transform: scale(0.7); }
  50% { opacity: 0.9; transform: scale(1); }
}
.tool-args {
  margin: 0;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  border: 1px solid color-mix(in srgb, var(--text-primary) 8%, transparent);
  border-radius: 8px;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--text-secondary, var(--text-primary));
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  max-width: min(560px, 90vw);
}
.tool-output {
  margin: 0;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--success, #16a34a) 5%, transparent);
  border: 1px solid color-mix(in srgb, var(--success, #16a34a) 16%, transparent);
  border-radius: 8px;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--text-secondary, var(--text-primary));
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  max-width: min(560px, 90vw);
}
.tool-output-failed {
  background: color-mix(in srgb, var(--destructive, #ef4444) 6%, transparent);
  border-color: color-mix(in srgb, var(--destructive, #ef4444) 20%, transparent);
}

/* Working / thinking dots */
.working-dot, .thinking-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--chat-accent) 75%, transparent);
  animation: thinking 1.2s ease-in-out infinite;
}
.thinking-dot { width: 5px; height: 5px; background: color-mix(in srgb, var(--chat-accent) 65%, transparent); }
.working-dot:nth-child(2), .thinking-dot:nth-child(2) { animation-delay: 0.2s; }
.working-dot:nth-child(3), .thinking-dot:nth-child(3) { animation-delay: 0.4s; }
@keyframes thinking { 0%, 80%, 100% { opacity: 0.3; transform: scale(0.8); } 40% { opacity: 1; transform: scale(1); } }

.thinking-body {
  margin: 6px 0 2px;
  white-space: pre-wrap;
  color: var(--chat-muted, rgba(255,255,255,0.42));
  font-size: 10px;
  line-height: 1.4;
  max-height: 200px;
  overflow-y: auto;
  scrollbar-width: thin;
}

/* Queue item action buttons */
.queue-item-btn {
  font-size: 10px;
  color: var(--chat-muted);
  background: none;
  border: 1px solid var(--chat-border);
  border-radius: 4px;
  padding: 1px 5px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
  transition: color .12s ease-out, border-color .12s ease-out;
}
.queue-item-btn:hover { color: var(--chat-text); border-color: color-mix(in srgb, var(--chat-text) 25%, transparent); }

/* Context usage bar fill colors */
.ctx-usage-bar.ctx-ok { background: color-mix(in srgb, var(--chat-accent) 55%, transparent); }
.ctx-usage-bar.ctx-warning { background: color-mix(in srgb, var(--chat-warn) 75%, transparent); }
.ctx-usage-bar.ctx-exceeded { background: color-mix(in srgb, var(--red, #ef4444) 80%, transparent); }

/* Permission log bubble */
.bubble-permission {
  display: inline-flex;
  align-items: center;
  padding: 3px 9px;
  border-radius: 20px;
  font-size: 11px;
  font-family: var(--font-mono);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bubble-permission.perm-granted {
  background: color-mix(in srgb, var(--chat-success) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--chat-success) 32%, transparent);
  color: var(--chat-success);
}
.bubble-permission.perm-rejected {
  background: color-mix(in srgb, var(--red, #ef4444) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--red, #ef4444) 32%, transparent);
  color: var(--red, #ef4444);
}

.message-copy-btn {
  display: inline-flex;
  width: 26px;
  height: 26px;
  flex: 0 0 26px;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--muted-foreground);
  cursor: pointer;
  opacity: 0;
  transition: color .12s ease, background-color .12s ease, opacity .12s ease;
}
.group:hover .message-copy-btn, .message-copy-btn:focus-visible { opacity: 1; }
.message-copy-btn:hover { background: color-mix(in srgb, var(--foreground) 8%, transparent); color: var(--foreground); }
.message-copy-btn:focus-visible { outline: 1px solid var(--accent); outline-offset: 1px; }

/* Command suggestions */
.cmd-suggestion.selected { background: color-mix(in srgb, var(--chat-text) 5%, transparent); }

/* Input toolbar buttons */
.toolbar-btn {
  background: none;
  border: none;
  color: var(--chat-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 7px;
  border-radius: 7px;
  font-size: 11px;
  font-family: var(--font-ui);
  transition: color .12s ease-out, background .12s ease-out;
}
.toolbar-btn:hover { color: var(--chat-text); background: color-mix(in srgb, var(--chat-text) 6%, transparent); }
.toolbar-btn-label { font-weight: 500; }
.btn-caret { opacity: 0.6; }

/* Model / floating menus */
.model-dropdown { position: relative; }

.floating-menu {
  position: fixed;
  z-index: 1000;
  min-width: 200px;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.6);
}
.floating-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 7px 10px;
  background: none;
  border: none;
  border-radius: 8px;
  color: var(--chat-text-secondary);
  font-size: 12px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: color .12s ease-out, background-color .12s ease-out;
  gap: 6px;
}
.floating-menu-item:hover,
.floating-menu-item:focus-visible {
  color: var(--chat-text);
  background: var(--bg-hover) !important;
  outline: none;
}
.floating-menu-item-active { color: var(--chat-accent); background: color-mix(in srgb, var(--chat-accent) 12%, transparent); }
.model-id-hint {
  font-size: 9px;
  font-family: var(--font-mono);
  color: var(--chat-muted);
  margin-left: 6px;
}
.floating-menu-item > .model-id-hint { margin-left: auto; }

.agent-dropdown { position: relative; display: inline-flex; }

/* Send button */
.send-btn {
  background: var(--agent-accent, #ec4899);
  border: none;
  border-radius: 50%;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  transition: background .12s ease-out, opacity .12s ease-out, box-shadow .12s ease-out, transform .15s cubic-bezier(0.16, 1, 0.3, 1);
  box-shadow: 0 2px 10px color-mix(in srgb, var(--agent-accent, #ec4899) 40%, transparent);
}
.send-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--agent-accent, #ec4899) 80%, #000); transform: translateY(-1px); }
.send-btn:active:not(:disabled) { transform: translateY(0); }
.send-btn:disabled { opacity: 0.35; cursor: default; }
.send-btn-abort { background: var(--red, #dc2626); }
.send-btn-abort:hover:not(:disabled) { background: color-mix(in srgb, var(--red, #dc2626) 80%, #000); }
.send-btn-stalled { animation: send-btn-stalled-pulse 1.6s ease-in-out infinite; }
@keyframes send-btn-stalled-pulse {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--red, #dc2626) 55%, transparent); }
  50% { box-shadow: 0 0 0 5px color-mix(in srgb, var(--red, #dc2626) 0%, transparent); }
}

.pending-img-remove {
  position: absolute;
  top: -5px;
  right: -5px;
  width: 18px;
  height: 18px;
  background: var(--chat-dropdown);
  border: 1px solid var(--chat-border);
  border-radius: 50%;
  color: var(--chat-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  transition: color .1s ease-out, background .1s ease-out;
}
.pending-img-remove:hover { color: var(--red, #f87171); background: color-mix(in srgb, var(--red, #f87171) 18%, transparent); }

/* Markdown body inside assistant messages — v-html content, needs real
   selectors (:deep) since these elements aren't authored in this SFC. */
.md-body :deep(p) { margin: 0 0 10px; }
.md-body :deep(p:last-child) { margin-bottom: 0; }
.md-body :deep(ul), .md-body :deep(ol) { margin: 4px 0 10px; padding-left: 20px; }
.md-body :deep(li) { margin: 3px 0; }
.md-body :deep(code) { font-family: var(--font-mono); font-size: 11px; background: color-mix(in srgb, var(--chat-accent) 14%, transparent); color: color-mix(in srgb, var(--chat-accent) 55%, var(--chat-text)); padding: 1px 5px; border-radius: 4px; }
.md-body :deep(pre) { background: color-mix(in srgb, var(--chat-bg) 55%, black); border: 1px solid var(--chat-border); border-radius: 8px; padding: 12px 14px; overflow-x: auto; margin: 8px 0; }
.md-body :deep(pre code) { background: none; padding: 0; font-size: 11px; color: var(--chat-text-secondary); }
/* Quote-indent rule is the standard blockquote convention, not a decorative
   card stripe — kept, recolored to the theme accent. */
.md-body :deep(blockquote) { border-left: 3px solid color-mix(in srgb, var(--chat-accent) 55%, transparent); margin: 6px 0; padding-left: 12px; color: var(--chat-text-secondary); }
.md-body :deep(h1), .md-body :deep(h2), .md-body :deep(h3) { font-weight: 700; margin: 14px 0 6px; color: var(--chat-text); }
.md-body :deep(h1) { font-size: 16px; }
.md-body :deep(h2) { font-size: 14px; }
.md-body :deep(h3) { font-size: 13px; }
.md-body :deep(a) { color: var(--chat-accent); text-decoration: underline; text-underline-offset: 2px; }
.md-body :deep(hr) { border: none; border-top: 1px solid var(--chat-border); margin: 10px 0; }
.md-body :deep(table) { border-collapse: collapse; font-size: 12px; margin: 8px 0; }
.md-body :deep(th), .md-body :deep(td) { border: 1px solid var(--chat-border); padding: 5px 10px; }
.md-body :deep(th) { background: color-mix(in srgb, var(--chat-text) 5%, transparent); font-weight: 600; }

/* Chat input box: quiet by default, agent-accent ring on focus so the active
   composer is unambiguous without a heavy persistent border. */
.chat-input-box:focus-within {
  border-color: color-mix(in srgb, var(--agent-accent, #ec4899) 55%, transparent);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--agent-accent, #ec4899) 14%, transparent);
}

.chat-input-box { position: relative; z-index: 1; }

/* Empty-state avatar: soft halo instead of a flat icon tile. */
.chat-empty-avatar {
  position: relative;
}
.chat-empty-avatar::before {
  content: "";
  position: absolute;
  inset: -10px;
  border-radius: 16px;
  background: radial-gradient(circle, color-mix(in srgb, var(--agent-accent, #ec4899) 22%, transparent) 0%, transparent 70%);
  z-index: -1;
}
</style>
