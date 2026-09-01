<template>
  <div class="claude-chat flex h-full flex-row overflow-hidden bg-base" :style="{ '--agent-accent': agentAccentColor }">
    <div class="chat-main flex min-w-0 flex-1 flex-col overflow-hidden bg-base">

    <!-- Permission prompt (Bash / generic tool) -->
    <div v-if="pendingPermission" class="permission-banner perm-slide-in flex flex-shrink-0 items-center gap-2 rounded-[10px] border py-2.5 pl-3.5 pr-3 mx-3 mt-2 mb-0.5 shadow-[0_6px_20px_rgba(0,0,0,0.28)]" style="background: color-mix(in srgb, #f59e0b 12%, var(--bg-panel)); border-color: color-mix(in srgb, #f59e0b 30%, transparent); border-left-width: 3px; border-left-color: #f59e0b;">
      <PhShieldWarning :size="14" class="perm-icon flex-shrink-0 text-[#f59e0b]" />
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <span class="perm-title text-[11px] font-semibold text-foreground">{{ pendingPermission.toolName }} wants to run</span>
        <code class="perm-detail max-w-full overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground">{{ permissionDetail }}</code>
      </div>
      <div class="flex flex-shrink-0 items-center gap-1.5">
        <div class="perm-allow-group relative flex">
          <button class="perm-btn perm-allow rounded-l-md rounded-r-none" :disabled="nativeControlResponsePending" @click="respondPermission(true)" title="Allow once (Y)">
            Allow <kbd class="perm-kbd">Y</kbd>
          </button>
          <button class="perm-btn perm-allow !rounded-l-none !rounded-r-md border-l border-white/[0.12] !px-[5px]" :disabled="nativeControlResponsePending" @click="permDropdownOpen = !permDropdownOpen" title="More options">
            <PhCaretDown :size="9" weight="bold" />
          </button>
          <div v-if="permDropdownOpen" class="absolute bottom-[calc(100%+4px)] right-0 z-[100] min-w-[200px] rounded-lg border border-white/[0.12] bg-[#1e1e2e] p-1 shadow-[0_4px_16px_rgba(0,0,0,0.4)]">
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
    <div v-if="pendingDiff && diffPreview" class="diff-banner perm-slide-in mx-3 mt-2 mb-0.5 flex-shrink-0 overflow-hidden rounded-[10px] border shadow-[0_6px_20px_rgba(0,0,0,0.28)]" style="background: var(--bg-panel); border-color: color-mix(in srgb, #6366f1 28%, transparent); border-left-width: 3px; border-left-color: #6366f1;">
      <div class="flex items-center gap-2 px-3 py-2">
        <PhGitDiff :size="13" class="perm-icon flex-shrink-0 text-[#818cf8]" />
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
    <div v-if="pendingPlan" class="plan-banner perm-slide-in mx-3 mt-2 mb-0.5 flex-shrink-0 rounded-[10px] border px-[13px] py-[11px] shadow-[0_6px_20px_rgba(0,0,0,0.28)]" style="background: color-mix(in srgb, #10b981 10%, var(--bg-panel)); border-color: color-mix(in srgb, #10b981 28%, transparent); border-left-width: 3px; border-left-color: #10b981;">
      <div class="mb-1.5 flex items-center gap-[7px]">
        <PhListChecks :size="14" class="perm-icon text-[#10b981]" />
        <span class="perm-title text-[11px] font-semibold text-foreground">Claude proposed a plan</span>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div class="plan-body md-body max-h-[260px] overflow-auto text-xs text-foreground" v-html="planMd" />
      <textarea
        v-model="planFeedback"
        class="my-2 box-border w-full resize-y rounded-[5px] border border-white/10 bg-base px-2 py-1.5 font-sans text-[11px] text-foreground"
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
      class="permission-banner acp-perm-banner perm-slide-in mx-3 mt-2 mb-0.5 flex flex-shrink-0 flex-col items-stretch gap-2 rounded-[10px] border py-2.5 pl-3.5 pr-3 shadow-[0_6px_20px_rgba(0,0,0,0.28)]"
      :class="{ 'acp-perm-plan': acpPermPlan, 'acp-perm-diff': acpPermDiff && !acpPermPlan }"
      :style="acpPermPlan
        ? 'background: color-mix(in srgb, #10b981 10%, var(--bg-panel)); border-color: color-mix(in srgb, #10b981 28%, transparent); border-left-width: 3px; border-left-color: #10b981;'
        : (acpPermDiff ? 'background: var(--bg-panel); border-color: color-mix(in srgb, #6366f1 28%, transparent); border-left-width: 3px; border-left-color: #6366f1;' : 'background: color-mix(in srgb, #f59e0b 12%, var(--bg-panel)); border-color: color-mix(in srgb, #f59e0b 30%, transparent); border-left-width: 3px; border-left-color: #f59e0b;')"
    >
      <div class="flex min-w-0 items-center gap-[7px]">
        <PhListChecks v-if="acpPermPlan" :size="14" class="perm-icon text-[#10b981]" />
        <PhGitDiff v-else-if="acpPermDiff" :size="14" class="perm-icon text-[#818cf8]" />
        <PhShieldWarning v-else :size="14" class="perm-icon text-[#f59e0b]" />
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

    <div ref="scrollEl" class="chat-messages flex flex-1 flex-col gap-0.5 overflow-y-auto py-6 pb-2 [scroll-behavior:smooth] [-webkit-user-select:text] [user-select:text]">
      <div v-if="messages.length === 0" class="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-10 text-center">
        <div class="chat-empty-avatar mb-2 flex h-11 w-11 items-center justify-center rounded-[11px] text-white shadow-[0_0_0_1px_color-mix(in_srgb,var(--agent-accent,#ec4899)_36%,transparent)]" style="background: color-mix(in srgb, var(--agent-accent, #ec4899) 72%, #16161a);" aria-hidden="true">
          <component :is="currentAgentIcon" :size="28" :style="{ color: '#fff' }" />
        </div>
        <span class="text-[9px] font-semibold uppercase tracking-[.08em] text-muted-foreground">New conversation</span>
        <span class="text-base font-semibold text-foreground">Start a focused conversation</span>
        <span class="mt-0.5 font-mono text-[11px] text-muted-foreground">Working in {{ cwdDisplay }}</span>
      </div>

      <div
        v-for="msg in displayItems"
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
              <PhCircleNotch v-if="groupIsRunning(msg.items)" :size="10" class="tool-status-icon tool-spin flex-shrink-0" />
              <PhWarningCircle v-else-if="groupHasFailure(msg.items)" :size="10" class="tool-status-icon flex-shrink-0 text-destructive" />
            </div>
          </div>
        </template>

        <!-- User message -->
        <template v-else-if="msg.role === 'user'">
          <div class="group flex items-end justify-end gap-2 px-4 py-[3px]">
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
              <template v-for="(p, i) in msgParts(msg.text)" :key="i"><span v-if="p.mention" class="mention-pill"><PhFile :size="10" class="mention-pill-icon" />{{ p.v.slice(1) }}</span><template v-else>{{ p.v }}</template></template>
            </div>
            <button class="message-copy-btn" :aria-label="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" :title="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" @click="copyMessage(msg)">
              <PhCheck v-if="copiedMessageId === msg.id" :size="13" weight="bold" />
              <PhCopy v-else :size="13" />
            </button>
            <div class="flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full border border-white/15 bg-white/10 text-[11px] font-bold text-white/70">U</div>
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
                <PhCircleNotch v-if="toolStatus(msg) === 'running'" :size="10" class="tool-status-icon tool-spin flex-shrink-0" />
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
            <span class="rounded-[20px] border border-white/[0.08] bg-white/[0.04] px-2.5 py-0.5 text-[11px] text-white/35">{{ msg.text }}</span>
          </div>
        </template>

        <!-- Queued message placeholder -->
        <template v-else-if="msg.role === 'queued'">
          <div class="flex items-end justify-end gap-2 px-4 py-[3px]">
            <div class="inline-flex max-w-[min(460px,85%)] items-center gap-1.5 rounded-[14px] border border-dashed border-white/[0.12] bg-white/[0.04] px-3 py-2 text-right text-[13px] text-white/30">
              <PhClock :size="11" class="flex-shrink-0 text-white/25" />
              {{ msg.text }}
            </div>
            <div class="flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full border border-white/15 bg-white/10 text-[11px] font-bold text-white/70 opacity-35">U</div>
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
            <details class="max-w-[90%] rounded-lg border border-dashed border-white/[0.12] px-2.5 py-1 font-mono text-[11px] text-muted-foreground opacity-75">
              <summary class="cursor-pointer select-none italic text-muted-foreground hover:text-white/70">Thinking…</summary>
              <pre class="thinking-body">{{ msg.text }}</pre>
            </details>
          </div>
        </template>

        <!-- Assistant message -->
        <template v-else>
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="agent-avatar mt-0.5 flex h-[26px] w-[26px] flex-shrink-0 items-center justify-center rounded-full text-white shadow-[0_2px_6px_color-mix(in_srgb,var(--agent-accent,#ec4899)_28%,transparent)]">
              <component :is="currentAgentIcon" :size="14" :style="{ color: '#fff' }" />
            </div>
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

      <div v-if="busy && !hasPartialAssistant" class="flex items-center gap-1.5 px-4 py-1.5">
        <div class="agent-avatar flex h-[22px] w-[22px] items-center justify-center rounded-full text-white shadow-[0_2px_6px_color-mix(in_srgb,var(--agent-accent,#ec4899)_28%,transparent)]">
          <component :is="currentAgentIcon" :size="12" :style="{ color: '#fff' }" />
        </div>
        <span class="thinking-dot" /><span class="thinking-dot" /><span class="thinking-dot" />
      </div>
    </div>

    <!-- Command suggestions dropdown -->
    <div v-if="suggestions.length > 0" ref="suggestionsEl" class="max-h-[200px] flex-shrink-0 overflow-y-auto border-t border-white/[0.07] bg-panel">
      <div
        v-for="(s, i) in suggestions"
        :key="s.name"
        class="flex cursor-pointer items-baseline gap-2.5 px-3 py-1.5 transition-colors hover:bg-white/5"
        :class="{ '!bg-white/5': i === suggestionIdx }"
        @mousedown.prevent="applySuggestion(s.name)"
      >
        <span class="min-w-[100px] flex-shrink-0 font-mono text-xs font-semibold text-[#f472b6]">/{{ s.name }}</span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-white/38">{{ s.description }}</span>
      </div>
    </div>

    <!-- @-mention file suggestions dropdown -->
    <div v-if="atSuggestions.length > 0" class="max-h-[200px] flex-shrink-0 overflow-y-auto border-t border-white/[0.07] bg-panel">
      <div
        v-for="(p, i) in atSuggestions"
        :key="p"
        class="flex cursor-pointer items-baseline gap-2.5 px-3 py-1.5 transition-colors hover:bg-white/5"
        :class="{ '!bg-white/5': i === atIdx }"
        @mousedown.prevent="applyAtSuggestion(p)"
      >
        <span class="min-w-[100px] flex-shrink-0 font-mono text-xs font-semibold text-[#f472b6]">@{{ p.slice(p.lastIndexOf('/') + 1) }}</span>
        <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-white/38">{{ p }}</span>
      </div>
    </div>

    <!-- Image previews above input -->
    <div v-if="pendingImages.length > 0" class="flex flex-shrink-0 flex-wrap gap-1.5 px-3.5 pt-1.5">
      <div v-for="(img, i) in pendingImages" :key="i" class="relative flex-shrink-0">
        <img :src="img" class="block h-[72px] w-[72px] rounded-md border border-white/10 object-cover" :alt="`Image ${i + 1}`" />
        <button class="pending-img-remove" @click="pendingImages.splice(i, 1)" title="Remove">
          <PhX :size="9" weight="bold" />
        </button>
      </div>
    </div>

    <!-- New-style input bar -->
    <div v-if="!hideComposer" class="flex-shrink-0 bg-base px-[18px] pb-2 pt-2.5">
      <!-- Queued messages panel (Zed-style) -->
      <div v-if="messageQueue.length > 0" class="border-b border-white/[0.07] bg-[rgba(124,58,237,0.04)]">
        <div class="flex cursor-pointer select-none items-center gap-1.5 px-2.5 py-1.5 hover:bg-white/[0.03]" @click="queueExpanded = !queueExpanded">
          <PhCaretDown :size="10" class="text-white/40 transition-transform" :class="{ '-rotate-90': !queueExpanded }" />
          <span class="flex-1 text-[11px] text-white/45">{{ messageQueue.length }} Queued {{ messageQueue.length === 1 ? 'Message' : 'Messages' }}</span>
          <button class="border-none bg-transparent px-1 py-px text-[10px] text-white/30 hover:text-white/60" @click.stop="clearQueue" title="Clear All">Clear All</button>
        </div>
        <div v-if="queueExpanded" class="flex flex-col gap-[3px] px-2.5 pb-1.5">
          <div v-for="(msg, i) in messageQueue" :key="i" class="flex items-center gap-1.5 py-[3px]">
            <span class="flex-shrink-0 text-xs text-[rgba(124,58,237,0.7)]">•</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-white/50">{{ msg }}</span>
            <button class="queue-item-btn" @click="removeQueued(i)" title="Remove"><PhX :size="10" /></button>
            <button class="queue-item-btn !text-[rgba(124,58,237,0.8)] !border-[rgba(124,58,237,0.3)] hover:!text-[rgba(124,58,237,1)] hover:!border-[rgba(124,58,237,0.6)]" @click="sendQueuedNow(i)" title="Send Now">Send Now <kbd>↵</kbd></button>
          </div>
        </div>
      </div>
      <!-- Working indicator — sits above the textarea, only when busy -->
      <div v-if="busy" class="flex items-center gap-1.5 border-b border-white/5 px-3 pb-1 pt-1.5">
        <span class="working-dot" /><span class="working-dot" /><span class="working-dot" />
        <span class="text-[11px] italic text-white/35">{{ currentActivity }}</span>
      </div>
      <div class="chat-input-box overflow-hidden rounded-[10px] border border-white/10 transition-[border-color,box-shadow]" :class="{ 'input-queued': busy && inputText.trim() }" style="background: color-mix(in srgb, var(--agent-accent, #ec4899) 4%, var(--chat-surface));">
        <!-- AskUserQuestion: one question at a time, stepped, above the textarea -->
        <div v-if="pendingQuestion && activeQuestion" class="question-panel perm-slide-in border-b border-white/10 bg-white/[0.03] px-3.5 py-3">
          <div class="mb-2 flex items-center gap-2">
            <span v-if="activeQuestion.header" class="rounded bg-[color-mix(in_srgb,#3b82f6_22%,transparent)] px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-[0.04em] text-[#93c5fd]">{{ activeQuestion.header }}</span>
            <span v-if="questionSpecs.length > 1" class="ml-auto flex h-5 flex-shrink-0 items-center rounded-md bg-white/10 px-1.5 text-[10px] font-medium tabular-nums text-secondary-foreground">{{ activeQuestionIndex + 1 }}/{{ questionSpecs.length }}</span>
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
                ? 'border-[#3b82f6] bg-[color-mix(in_srgb,#3b82f6_16%,var(--bg-base))]'
                : 'border-white/[0.12] bg-base hover:border-[color-mix(in_srgb,#3b82f6_55%,transparent)]'"
              :disabled="nativeControlResponsePending"
              @click="selectQuestionOption(opt.label)"
            >
              <span class="flex min-w-0 flex-1 flex-col gap-px">
                <span class="text-xs font-semibold text-foreground">{{ opt.label }}</span>
                <span v-if="opt.description" class="text-[10px] text-secondary-foreground">{{ opt.description }}</span>
              </span>
              <PhCheck v-if="isPicked(activeQuestion.question, opt.label)" :size="13" weight="bold" class="flex-shrink-0 text-[#3b82f6]" />
              <kbd v-else-if="oi < 9" class="flex h-5 w-5 flex-shrink-0 items-center justify-center rounded border border-white/15 text-[10px] text-secondary-foreground/60">{{ oi + 1 }}</kbd>
            </button>
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
        <textarea
          ref="inputEl"
          v-model="inputText"
          class="chat-input composer-input box-border block max-h-40 min-h-10 w-full resize-none border-none bg-transparent px-3 pb-1 pt-2.5 font-sans text-[13px] leading-[1.5] text-white/88 outline-none placeholder:text-white/30"
          :placeholder="busy ? 'Type next message — will send when Claude finishes…' : 'Ask your agent anything...'"
          rows="1"
          @keydown="onKeydown"
          @input="onInput"
          @paste="onPaste"
        />
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
            <button class="toolbar-btn" title="New conversation" @click="clearChat">
              <PhArrowCounterClockwise :size="13" />
            </button>
            <button class="toolbar-btn" title="Configure providers…" @click="openProviderSettings">
              <PhGear :size="13" />
            </button>
            <div v-if="isAcpRuntime && sessionId" class="agent-dropdown relative inline-flex">
              <button ref="acpHistoryBtnEl" class="toolbar-btn" title="Resume a past session" @click="openAcpHistory">
          <PhClockCounterClockwise :size="13" />
        </button>
        <Teleport to="body">
          <div v-if="acpHistoryOpen" ref="acpHistoryMenuEl" class="floating-menu acp-history-menu" :style="{ top: acpHistoryPos.top + 'px', left: acpHistoryPos.left + 'px' }">
            <div class="px-2.5 pb-1.5 pt-1 text-[10px] uppercase tracking-[0.04em] text-white/35">{{ currentAgent?.name }} sessions</div>
            <div v-if="!acpSessions.length" class="p-2.5 text-center text-[11px] text-white/40">No past sessions</div>
            <button
              v-for="s in acpSessions"
              :key="s.sessionId"
              class="floating-menu-item flex-col items-start gap-0.5"
              :class="{ 'floating-menu-item-active': s.sessionId === sessionId }"
              :title="s.sessionId"
              @click="resumeAcpSession(s.sessionId)"
            >
              <div class="flex max-w-full items-center gap-1.5">
                <component :is="currentAgentIcon" :size="12" :style="{ color: currentAgent?.color }" />
                <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ s.title || s.sessionId.slice(0, 8) }}</span>
              </div>
              <span v-if="s.updatedAt" class="model-id-hint ml-[18px]">{{ new Date(s.updatedAt).toLocaleString() }}</span>
            </button>
          </div>
        </Teleport>
      </div>
            <button
              v-if="isAcpRuntime"
              class="toolbar-btn"
              title="Browse past sessions"
              @click="openSessionBrowser"
            ><PhClock :size="13" /></button>
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
                <component :is="PERM_ICON[permMode]" :size="13" weight="bold" />
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
                    <component :is="PERM_ICON[m]" :size="13" weight="bold" />
                    <span>{{ PERM_META[m].label }}</span>
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
                <PhShieldCheck :size="13" weight="bold" />
                <span class="perm-mode-label">{{ acpModeLabel }}</span>
                <PhCaretDown :size="9" weight="bold" class="perm-mode-caret" />
              </button>
              <Teleport to="body">
                <div v-if="acpModeMenuOpen" ref="acpModeMenuEl" class="floating-menu" :style="{ top: acpModeMenuPos.top + 'px', left: acpModeMenuPos.left + 'px' }">
                  <button
                    v-for="m in acpModes.availableModes"
                    :key="m.id"
                    class="floating-menu-item"
                    :class="{ 'floating-menu-item-active': acpModes.currentModeId === m.id }"
                    :title="m.description"
                    @click="acpSelectMode(m.id)"
                  >
                    {{ m.name }}
                  </button>
                </div>
              </Teleport>
            </div>
          </div>

          <!-- Right: cost badge + abort/send -->
          <div class="flex items-center gap-1.5">
            <span v-if="sessionCost > 0 && !busy" class="px-1 font-mono text-[10px] text-white/30">${{ sessionCost.toFixed(4) }}</span>
            <button v-if="busy" class="send-btn send-btn-abort" title="Abort (Esc)" @click="abortTurn">
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

      <!-- Context usage bar -->
      <div v-if="contextUsageRatio > 0" class="h-0.5 overflow-hidden bg-white/[0.06]" :title="`${turnStats?.inputTokens.toLocaleString()} / ${CONTEXT_MAX.toLocaleString()} tokens`">
        <div class="ctx-usage-bar h-full rounded-[1px] transition-[width]" :class="contextUsageClass" :style="{ width: (contextUsageRatio * 100) + '%' }" />
      </div>

      <!-- Status line below input — hidden when nothing to show -->
      <div v-show="fiveHourWindow" class="relative z-[1] flex flex-shrink-0 items-center gap-2 border-t border-white/[0.06] px-2.5 py-[3px] min-h-[22px]">
        <span v-if="fiveHourWindow" class="whitespace-nowrap font-mono text-[10px] text-white/38" :title="'5h usage window'">5h: {{ fiveHourWindow }}</span>
        <span class="flex-1" />
        <span v-if="turnStats" class="whitespace-nowrap font-mono text-[10px] text-white/28">
          {{ turnStats.inputTokens.toLocaleString() }}↑ {{ turnStats.outputTokens.toLocaleString() }}↓
        </span>
      </div>
    </div>
    </div><!-- end .chat-main -->

    <!-- Session browser modal -->
    <div v-if="sessionBrowserOpen" class="absolute inset-0 z-[200] flex items-center justify-center bg-black/50" @click.self="sessionBrowserOpen = false">
      <div class="flex max-h-[60vh] w-[420px] flex-col overflow-y-auto rounded-lg border border-border" style="background: var(--bg2, #1e1e1e);">
        <div class="flex items-center justify-between border-b border-border px-3.5 py-2.5 font-semibold">
          <span>Recent sessions</span>
          <button class="cursor-pointer border-none bg-transparent text-foreground" @click="sessionBrowserOpen = false">✕</button>
        </div>
        <div v-if="sessionBrowserLoading" class="px-3.5 py-5 text-muted-foreground">Loading…</div>
        <div v-else-if="!sessionBrowserItems.length" class="px-3.5 py-5 text-muted-foreground">No sessions found for this project.</div>
        <div
          v-for="s in sessionBrowserItems"
          :key="s.session_id"
          class="flex cursor-pointer flex-col gap-0.5 border-b border-[var(--border-faint,#2a2a2a)] px-3.5 py-2.5 hover:bg-hover"
          @click="pickSession(s.session_id)"
        >
          <span class="text-[13px] text-foreground">{{ s.first_message }}</span>
          <span class="text-[11px] text-muted-foreground">{{ s.updated_at }} · {{ s.session_id.slice(0, 8) }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick, onMounted, onBeforeUnmount, watch } from "vue";
import { PhArrowUp, PhArrowCounterClockwise, PhWrench, PhStop, PhShieldWarning, PhShieldCheck, PhPencilSimple, PhGitDiff, PhListChecks, PhTextAa, PhCaretDown, PhCaretRight, PhX, PhUserGear, PhClock, PhFile, PhSparkle, PhFastForward, PhGear, PhClockCounterClockwise, PhFileText, PhTerminalWindow, PhMagnifyingGlass, PhGlobe, PhRobot, PhWarningCircle, PhCircleNotch, PhCopy, PhCheck } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { parseAcpUpdate, parseAcpPermRequest } from "@/lib/acpParser";
import { normalizeAcpRuntimeEvent, normalizeClaudeStreamEvent, type ProviderRuntimeEvent } from "@/lib/providerRuntime";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useNotificationsStore } from "@/stores/notifications";
import { useEditorContextStore } from "@/stores/editorContext";
import { useScriptsStore } from "@/stores/scripts";
import { useProvidersStore, chatTransportFor, binaryFor, type ChatTransport } from "@/stores/providers";
import { agentIconComp } from "@/lib/agentIcons";
import ModelPicker from "@/components/ModelPicker.vue";
import { modelsFor, learnModels, type ModelEntry } from "@/lib/chatModels";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import { playSound } from "@/lib/sounds";
import { notifyNtfy } from "@/lib/ntfy";
import { useUIStore, type NtfyEvent } from "@/stores/ui";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";

function renderMd(text: string): string {
  return DOMPurify.sanitize(marked.parse(text) as string);
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
const notifStore = useNotificationsStore();
const uiStore = useUIStore();
const scriptsStore = useScriptsStore();
const chatAgents = useProvidersStore();
const editorCtx = useEditorContextStore();

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
// ACP permission: JSON-RPC id of the agent's blocking request_permission.
const acpPermRpcId = ref<number | null>(null);
// Rich ACP permission request — carries the adapter's own option list (allow/
// reject variants, or ExitPlanMode's auto/acceptEdits/manual/keep-planning) so
// we render the REAL choices instead of collapsing everything to Allow/Deny.
interface AcpPermReq {
  rpcId: number;
  toolCallId: string;
  title: string;
  kind: string;
  options: Array<{ optionId: string; name: string; kind: string }>;
  rawInput: Record<string, unknown>;
}
const acpPermReq = ref<AcpPermReq | null>(null);
const acpPermMsgId = ref<number | null>(null);
const permissionResponsePending = ref(false);
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
interface AcpMode { id: string; name: string; description?: string }
interface AcpModes { currentModeId: string; availableModes: AcpMode[] }
interface AcpConfigChoice { value: string; name: string; description?: string }
interface AcpConfigOption { id: string; name: string; type: string; currentValue: string; options: AcpConfigChoice[] }
interface AcpSessionInfo { sessionId: string; title?: string; updatedAt?: string }
interface ClaudeSessionInfo {
  session_id: string;
  first_message: string;
  updated_at: string;
}
// JSON-RPC id of the in-flight session/prompt — correlates the turn-done response.
const acpPromptRpcId = ref<number | null>(null);
// rpc ids of in-flight control calls (set_mode/set_config/list) → refresh UI on reply.
const acpControlIds = new Set<number>();
const acpModes = ref<AcpModes | null>(null);
const acpConfigOptions = ref<AcpConfigOption[]>([]);
const acpSessions = ref<AcpSessionInfo[]>([]);
const acpHistoryOpen = ref(false);
const sessionBrowserOpen = ref(false);
const sessionBrowserItems = ref<ClaudeSessionInfo[]>([]);
const sessionBrowserLoading = ref(false);

async function openSessionBrowser() {
  sessionBrowserOpen.value = true;
  sessionBrowserLoading.value = true;
  try {
    const { invoke } = await import("@tauri-apps/api/core");
    sessionBrowserItems.value = await invoke<ClaudeSessionInfo[]>("list_claude_sessions", {
      cwd: props.cwd,
      configDir: null,
    });
  } catch (e) {
    sessionBrowserItems.value = [];
  } finally {
    sessionBrowserLoading.value = false;
  }
}

async function pickSession(sid: string) {
  sessionBrowserOpen.value = false;
  await resumeAcpSession(sid);
}
const acpHistoryBtnEl = ref<HTMLElement | null>(null);
const acpHistoryMenuEl = ref<HTMLElement | null>(null);
const acpHistoryPos = ref({ top: 0, left: 0 });
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
  if (!openRef.value && btn) {
    const r = btn.getBoundingClientRect();
    posRef.value = { top: Math.round(r.top - (count * 36 + 12) - 6), left: Math.round(r.left) };
  }
  openRef.value = !openRef.value;
}
function onAcpMenuOutside(e: MouseEvent) {
  const t = e.target as Node;
  if (acpModeMenuOpen.value && !acpModeBtnEl.value?.contains(t) && !acpModeMenuEl.value?.contains(t)) acpModeMenuOpen.value = false;
  if (acpModelMenuOpen.value && !acpModelBtnEl.value?.contains(t) && !acpModelMenuEl.value?.contains(t)) acpModelMenuOpen.value = false;
  if (acpEffortMenuOpen.value && !acpEffortBtnEl.value?.contains(t) && !acpEffortMenuEl.value?.contains(t)) acpEffortMenuOpen.value = false;
  if (acpHistoryOpen.value && !acpHistoryBtnEl.value?.contains(t) && !acpHistoryMenuEl.value?.contains(t)) acpHistoryOpen.value = false;
}
// What we last pushed back after an adapter reset its own selectors. An adapter
// that refuses a value would otherwise ping-pong with us forever; a user pick
// clears this, so the next legitimate reset is pushed again.
let acpRestored: AcpChatSettings = {};

async function acpSelectMode(modeId: string, userPick = true) {
  if (userPick) acpRestored = {};
  acpModeMenuOpen.value = false;
  if (acpModes.value) acpModes.value.currentModeId = modeId;
  setAcpSetting(props.chatId, "mode", modeId);
  try {
    const rid = await invoke<number>("acp_set_mode", { id: props.chatId, modeId });
    acpControlIds.add(rid);
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Failed to set mode: ${e}` });
  }
}
async function acpSelectModel(value: string, userPick = true) {
  if (userPick) acpRestored = {};
  acpModelMenuOpen.value = false;
  if (acpModelOption.value) acpModelOption.value.currentValue = value;
  setAcpSetting(props.chatId, "model", value);
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "model", value });
    acpControlIds.add(rid);
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Failed to set model: ${e}` });
  }
}
async function acpSelectEffort(value: string, userPick = true) {
  if (userPick) acpRestored = {};
  acpEffortMenuOpen.value = false;
  if (acpEffortOption.value) acpEffortOption.value.currentValue = value;
  setAcpSetting(props.chatId, "effort", value);
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "effort", value });
    acpControlIds.add(rid);
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Failed to set effort: ${e}` });
  }
}

// Re-apply this chat's model / permission mode / effort. The adapter resets its
// selectors to defaults on (re)start AND in the reply to a model switch, which
// is what used to silently drop the effort and permission mode the user picked.
function restoreAcpSelections() {
  const savedModel = getAcpSetting(props.chatId, "model")
    ?? getConfig<Record<string, string>>("chatAcpLastModel", {})[agentKind.value];
  if (savedModel && acpModelOption.value && acpModelOption.value.currentValue !== savedModel && acpRestored.model !== savedModel) {
    acpRestored.model = savedModel;
    acpSelectModel(savedModel, false);
  }
  const savedMode = getAcpSetting(props.chatId, "mode") ?? lastAcpSetting("mode");
  if (savedMode && acpModes.value && acpModes.value.currentModeId !== savedMode && acpRestored.mode !== savedMode) {
    acpRestored.mode = savedMode;
    acpSelectMode(savedMode, false);
  }
  const savedEffort = getAcpSetting(props.chatId, "effort") ?? lastAcpSetting("effort");
  if (savedEffort && acpEffortOption.value && acpEffortOption.value.currentValue !== savedEffort && acpRestored.effort !== savedEffort) {
    acpRestored.effort = savedEffort;
    acpSelectEffort(savedEffort, false);
  }
}

// History picker: list prior sessions for this cwd, then resume the chosen one.
async function openAcpHistory() {
  acpHistoryOpen.value = !acpHistoryOpen.value;
  if (!acpHistoryOpen.value) return;
  if (!sessionId.value) { acpHistoryOpen.value = false; return; } // adapter still starting
  if (acpHistoryBtnEl.value) {
    const r = acpHistoryBtnEl.value.getBoundingClientRect();
    acpHistoryPos.value = { top: Math.round(r.bottom + 6), left: Math.round(Math.max(8, r.right - 280)) };
  }
  try {
    const rid = await invoke<number>("acp_list_sessions", { id: props.chatId, cwd: props.cwd });
    acpControlIds.add(rid);
  } catch (e) {
    console.warn("acp_list_sessions failed:", e); // transient (adapter not ready) — don't pollute the feed
  }
}
async function resumeAcpSession(sid: string) {
  acpHistoryOpen.value = false;
  if (sid === sessionId.value) return;
  suppressNextDone.value = true;
  messages.value = [];
  busy.value = false;
  sessionId.value = sid;
  chats.sync(props.chatId, { claudeSessionId: sid });
  clearMessageHistory(props.chatId); // replayed history repopulates it
  await ensureAcpListeners();
  await invoke("acp_stop", { id: props.chatId }).catch(() => {});
  // emitHistory:true → Rust forwards the session/load replay so old turns render.
  const startErr = await invoke("acp_start", acpStartPayload(true)).catch((e: unknown) => e);
  if (startErr) messages.value.push({ id: nextMsgId++, role: "assistant", text: `Failed to resume: ${startErr}` });
}

// Agent switcher dropdown.
function openProviderSettings() {
  uiStore.openSettings("providers", currentAgent.value?.id);
}

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
function loadModel(): ClaudeModelId {
  const v = getConfig<string | null>(MODEL_CONFIG_KEY, null);
  if (CLAUDE_MODELS.some((m) => m.id === v)) return v as ClaudeModelId;
  if (props.defaultModel && CLAUDE_MODELS.some((m) => m.id === props.defaultModel)) {
    return props.defaultModel as ClaudeModelId;
  }
  return CLAUDE_MODELS[0].id;
}
const selectedModel = ref<ClaudeModelId>(loadModel());
async function selectModel(id: ClaudeModelId) {
  if (id === selectedModel.value) return;
  selectedModel.value = id;
  setConfig(MODEL_CONFIG_KEY, id);
  await restartClaude();
}

const CLAUDE_EFFORTS = [
  { id: "low", label: "Low effort" },
  { id: "medium", label: "Medium effort" },
  { id: "high", label: "High effort" },
  { id: "xhigh", label: "Extra high" },
  { id: "max", label: "Max effort" },
] as const;
type ClaudeEffort = typeof CLAUDE_EFFORTS[number]["id"];
const EFFORT_CONFIG_KEY = props.modelKey ? `chatClaudeEffort:${props.modelKey}` : "chatClaudeEffort";
function loadEffort(): ClaudeEffort {
  const saved = getConfig<string | null>(EFFORT_CONFIG_KEY, null);
  return CLAUDE_EFFORTS.some((option) => option.id === saved) ? saved as ClaudeEffort : "high";
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
  setConfig(EFFORT_CONFIG_KEY, effort);
  await restartClaude();
}

interface ChatMessage {
  id: number;
  role: "user" | "assistant" | "tool" | "thinking" | "permission" | "system-info" | "queued";
  text: string;
  images?: string[]; // data URIs for user messages with attached images
  partial?: boolean;
  toolInput?: Record<string, unknown>; // full tool args for expandable tool calls
  toolOutput?: string;  // captured tool result (first 2000 chars)
  toolUseId?: string;   // matches tool_result blocks back to tool cards
  toolExpanded?: boolean;
  toolFailed?: boolean; // tool_result came back is_error
  toolRawName?: boolean; // true when `text` is a raw tool name (native transport) vs already-human ACP title
  _acpMsgId?: string;   // ACP messageId — identity for incremental chunk append
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
function toolIconFor(msg: ChatMessage): unknown {
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
const displayItems = computed(() => grouping.value.display);
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

interface TurnStats { inputTokens: number; outputTokens: number; costUsd: number }

interface AccountInfo {
  email: string;
  display_name: string;
  organization_type: string;  // "claude_max" | "pro" | ...
  rate_limit_tier: string;    // "default_claude_max_5x" | ...
  status_text: string;        // raw `claude status` stdout
}

// Legacy per-chat localStorage key prefix (kept only for the one-time migration below).

function loadMessages(chatId: number): ChatMessage[] {
  const rec = getConfig<Record<string, unknown>>("chatMessageHistory", {});
  const raw = rec[String(chatId)];
  return Array.isArray(raw) ? (raw as ChatMessage[]) : [];
}

function saveMessages(chatId: number, msgs: ChatMessage[]) {
  // Only persist non-partial messages, cap at 200 to bound storage
  const toSave = msgs.filter((m) => !m.partial).slice(-200);
  const rec = { ...getConfig<Record<string, unknown>>("chatMessageHistory", {}) };
  rec[String(chatId)] = toSave;
  setConfig("chatMessageHistory", rec);
}

function clearMessageHistory(chatId: number) {
  const rec = { ...getConfig<Record<string, unknown>>("chatMessageHistory", {}) };
  delete rec[String(chatId)];
  setConfig("chatMessageHistory", rec);
}

let nextMsgId = 0;
const messages = ref<ChatMessage[]>(loadMessages(props.chatId));
const DRAFT_KEY = computed(() => `burrow.draft.chat.${props.chatId}`);
const inputText = ref(localStorage.getItem(DRAFT_KEY.value) ?? "");
watch(inputText, (val) => {
  if (val) {
    localStorage.setItem(DRAFT_KEY.value, val);
  } else {
    localStorage.removeItem(DRAFT_KEY.value);
  }
});
const busy = ref(false);
const copiedMessageId = ref<number | null>(null);
let copyFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
const messageQueue = ref<string[]>([]);
// Set before an INTENTIONAL claude restart (mode switch / abort) so the `exit`
// event that teardown emits doesn't fire a spurious "Claude finished" toast.
const suppressNextDone = ref(false);
const pendingImages = ref<string[]>([]); // data URIs
const sessionId = ref("");
const turnStats = ref<TurnStats | null>(null);
const sessionCost = ref(0);
const scrollEl = ref<HTMLElement | null>(null);
const inputEl = ref<HTMLTextAreaElement | null>(null);
const suggestionsEl = ref<HTMLElement | null>(null);
let unlisten: UnlistenFn | null = null;
let acpDataUL: UnlistenFn | null = null;
let acpReqUL: UnlistenFn | null = null;

// Attach the acp-data/acp-req listeners if not already. onMounted only attaches
// them when the chat STARTS as an ACP agent; switching to an ACP agent at runtime
// (selectAgent → clearChat → acp_start) must attach them too, or every adapter
// event (model/config + the whole turn) is dropped → no model, stuck "thinking".
async function ensureAcpListeners() {
  if (!acpDataUL) acpDataUL = await listen<string>(`acp-data-${props.chatId}`, (e) => onAcpData(e.payload));
  if (!acpReqUL) acpReqUL = await listen<string>(`acp-req-${props.chatId}`, (e) => onAcpReq(e.payload));
}

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
const PERM_META: Record<PermMode, { label: string; title: string; danger?: boolean }> = {
  default: { label: "Ask", title: "Ask before edits & commands (click to change)" },
  auto: { label: "Auto", title: "Claude decides when to ask (click to change)" },
  acceptEdits: { label: "Accept Edits", title: "Auto-accept file edits; still ask for other tools (click to change)" },
  plan: { label: "Plan Mode", title: "Plan only — no edits or commands until you approve (click to change)" },
  dontAsk: { label: "Don't Ask", title: "Run edits & commands without asking; still blocks dangerous ops (click to change)" },
  bypassPermissions: { label: "Bypass", title: "Skip ALL permission checks (click to change)", danger: true },
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
const permMenuOpen = ref(false);
const permBtnEl = ref<HTMLElement | null>(null);
const permMenuEl = ref<HTMLElement | null>(null);
// The menu is teleported + position:fixed, so anchor it to the button's rect.
const permMenuPos = ref({ top: 0, left: 0 });
function togglePermMenu() {
  if (!permMenuOpen.value && permBtnEl.value) {
    const r = permBtnEl.value.getBoundingClientRect();
    const menuH = PERM_MODES.length * 36 + 12;
    permMenuPos.value = { top: Math.round(r.top - menuH - 6), left: Math.round(r.left) };
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
function watchingNow(): boolean {
  return (props.isWatching ?? false) && document.hasFocus();
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
interface CanUseToolReq {
  requestId: string;
  toolName: string;
  input: Record<string, unknown>;
  description?: string;
  suggestions: Array<Record<string, unknown>>;
  toolUseId?: string;
}
const pendingPermission = ref<CanUseToolReq | null>(null); // Bash / generic tool
const pendingQuestion = ref<CanUseToolReq | null>(null);   // AskUserQuestion
const pendingPlan = ref<CanUseToolReq | null>(null);       // ExitPlanMode
const pendingDiff = ref<CanUseToolReq | null>(null);       // Edit / Write / MultiEdit / NotebookEdit
// Feed marker message IDs — removed when permission is resolved
const pendingPermissionMsgId = ref<number | null>(null);
const pendingQuestionMsgId = ref<number | null>(null);
const pendingPlanMsgId = ref<number | null>(null);
const pendingDiffMsgId = ref<number | null>(null);
// Keep native Claude prompts mounted until the control JSON was accepted by
// stdin. This prevents a failed write from looking like an automatic denial.
const nativeControlResponsePending = ref(false);
// Claude may replay an in-flight control request after a reconnect. Keep a
// small settled-id ledger so an already answered question cannot re-open.
const settledControlRequestIds = new Set<string>();

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
  messageQueue.value = [];
  messages.value = messages.value.filter((m) => m.role !== "queued");
}
function removeQueued(i: number) {
  const text = messageQueue.value[i];
  messageQueue.value.splice(i, 1);
  const qIdx = messages.value.findIndex((m) => m.role === "queued" && m.text === text);
  if (qIdx !== -1) messages.value.splice(qIdx, 1);
}
async function sendQueuedNow(i: number) {
  const text = messageQueue.value.splice(i, 1)[0];
  const qIdx = messages.value.findIndex((m) => m.role === "queued" && m.text === text);
  if (qIdx !== -1) messages.value.splice(qIdx, 1);
  // ACP supports promptQueueing → send now even mid-turn; stream-json must wait.
  if (!busy.value || effectiveTransport.value === "acp") await sendMessage(text);
  else { messageQueue.value.unshift(text); messages.value.unshift({ id: nextMsgId++, role: "queued", text }); }
}

// Split a user message into plain text + @path mention tokens for pill rendering.
function msgParts(text: string): { mention: boolean; v: string }[] {
  const parts: { mention: boolean; v: string }[] = [];
  const re = /(^|\s)(@[^\s@]+)/g;
  let last = 0, m: RegExpExecArray | null;
  while ((m = re.exec(text))) {
    const start = m.index + m[1].length;
    if (start > last) parts.push({ mention: false, v: text.slice(last, start) });
    parts.push({ mention: true, v: m[2] });
    last = start + m[2].length;
  }
  if (last < text.length) parts.push({ mention: false, v: text.slice(last) });
  return parts;
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
    if (m.role === "assistant" || m.role === "thinking") return "Thinking…";
  }
  return "Thinking…";
});

// AskUserQuestion working selection: question text → chosen option label(s).
const questionAnswers = ref<Record<string, string[]>>({});
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
const canSubmitQuestion = computed(() =>
  questionSpecs.value.every((q) => (questionAnswers.value[q.question] ?? []).length > 0));

// Stepped AskUserQuestion — one question shown at a time, mirroring t3code's
// ComposerPendingUserInputPanel (index reset whenever a new request arrives).
const activeQuestionIndex = ref(0);
const activeQuestion = computed(() => questionSpecs.value[activeQuestionIndex.value] ?? null);
const isLastQuestion = computed(() => activeQuestionIndex.value >= questionSpecs.value.length - 1);
const canAdvanceQuestion = computed(() => {
  const q = activeQuestion.value;
  return !!q && (questionAnswers.value[q.question] ?? []).length > 0;
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

// Seed nextMsgId from loaded messages
nextMsgId = messages.value.reduce((max, m) => Math.max(max, m.id + 1), 0);

const cwdDisplay = computed(() => {
  const parts = props.cwd.replace(/^\/Users\/[^/]+/, "~").split("/");
  return parts.slice(-2).join("/") || props.cwd;
});

const hasPartialAssistant = computed(() =>
  messages.value.some((m) => (m.role === "assistant" || m.role === "thinking") && m.partial)
);

function scrollToBottom() {
  nextTick(() => {
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight;
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
// Once Claude sends us a generated title, prefer it and stop overwriting.
const claudeGeneratedTitle = ref(false);
function applyClaudeTitle(raw: unknown) {
  if (typeof raw !== "string" || !raw.trim()) return;
  claudeGeneratedTitle.value = true;
  chats.sync(props.chatId, { title: raw.trim().slice(0, 60) });
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
function applyRuntimeEvent(event: ProviderRuntimeEvent) {
  switch (event.type) {
    case "text.delta": {
      const isAcp = event.messageId.startsWith("acp:");
      const last = isAcp
        ? messages.value.filter((m) => m.role === "assistant" && m.partial && m._acpMsgId === event.messageId).pop()
        : messages.value[messages.value.length - 1];
      if (last?.role === "assistant" && last.partial) last.text += event.text;
      else messages.value.push({ id: nextMsgId++, role: "assistant", text: event.text, partial: true, ...(isAcp ? { _acpMsgId: event.messageId } : {}) });
      scrollToBottom();
      return;
    }
    case "tool.started":
      messages.value.push({ id: nextMsgId++, role: "tool", text: event.name, toolInput: event.input ?? {}, toolUseId: event.toolCallId, toolExpanded: false, toolRawName: Boolean(event.input) });
      scrollToBottom();
      return;
    case "tool.completed": {
      const toolMsg = [...messages.value].reverse().find((m) => m.role === "tool" && m.toolUseId === event.toolCallId);
      if (toolMsg) {
        toolMsg.toolOutput = event.output ? event.output.slice(0, 2000) : "";
        toolMsg.toolFailed = event.failed === true;
      }
      scrollToBottom();
      return;
    }
  }
}

function onLine(line: string) {
  let event: Record<string, unknown>;
  try { event = JSON.parse(line) as Record<string, unknown>; }
  catch { return; }

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
        messages.value.push({ id: nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
        saveMessages(props.chatId, messages.value);
      });
      return;
    }
    if (cr.toolName === "AskUserQuestion") {
      questionAnswers.value = {};
      pendingQuestion.value = cr;
      const qText = ((cr.input.questions as Array<{question: string}>)?.[0]?.question ?? "Question").slice(0, 80);
      const qMid = nextMsgId++;
      pendingQuestionMsgId.value = qMid;
      messages.value.push({ id: qMid, role: "system-info", text: `❓ ${qText}` });
      chats.sendStatusEvent(props.chatId, { type: "WAIT" });
    } else if (cr.toolName === "ExitPlanMode") {
      planFeedback.value = "";
      pendingPlan.value = cr;
      const pMid = nextMsgId++;
      pendingPlanMsgId.value = pMid;
      messages.value.push({ id: pMid, role: "system-info", text: `📋 Plan ready for review` });
      chats.sendStatusEvent(props.chatId, { type: "WAIT" });
    } else if (["Edit", "Write", "MultiEdit", "NotebookEdit"].includes(cr.toolName)) {
      pendingDiff.value = cr;
      const filePath = ((cr.input.file_path ?? cr.input.path ?? "") as string);
      const dMid = nextMsgId++;
      pendingDiffMsgId.value = dMid;
      messages.value.push({ id: dMid, role: "system-info", text: `✏️ ${cr.toolName}: ${filePath.split("/").slice(-2).join("/")}` });
      chats.sendStatusEvent(props.chatId, { type: "PERMISSION_REQUEST" });
    } else {
      pendingPermission.value = cr;
      const pmMid = nextMsgId++;
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
    if (sub === "session_title") applyClaudeTitle(event.title);
    if (sub === "hook_started" || sub === "hook_response") return;
  }

  if (type === "assistant") {
    const content = ((event.message as Record<string, unknown>)?.content ?? []) as Array<Record<string, unknown>>;
    const thinkingParts = content.filter((b) => b.type === "thinking").map((b) => b.thinking as string).join("");

    if (thinkingParts) {
      const last = messages.value[messages.value.length - 1];
      if (last?.role === "thinking" && last.partial) {
        last.text += thinkingParts;
      } else {
        messages.value.push({ id: nextMsgId++, role: "thinking", text: thinkingParts, partial: true });
      }
    }
    for (const runtimeEvent of normalizeClaudeStreamEvent(event)) applyRuntimeEvent(runtimeEvent);
    scrollToBottom();
    return;
  }

  if (type === "user") {
    const content = ((event.message as Record<string, unknown>)?.content ?? []) as Array<Record<string, unknown>>;
    for (const block of content) {
      if (block.type !== "tool_result") continue;
      const toolUseId = block.tool_use_id as string;
      const rc = block.content as Array<Record<string, unknown>> | string | undefined;
      let out = typeof rc === "string" ? rc : (Array.isArray(rc) ? rc.filter((b) => b.type === "text").map((b) => b.text as string).join("\n") : "");
      const toolMsg = [...messages.value].reverse().find((m) => m.role === "tool" && m.toolUseId === toolUseId);
      if (toolMsg) {
        toolMsg.toolOutput = out ? out.slice(0, 2000) : "";
        toolMsg.toolFailed = block.is_error === true;
      }
    }
    return;
  }

  if (type === "result" || type === "exit") {
    busy.value = false;
    // Un-partial ALL messages — tool messages are pushed after assistant text,
    // so checking only `last` would leave the assistant text bubble still partial.
    for (const m of messages.value) { if (m.partial) m.partial = false; }
    // Capture usage/cost from result event
    if (type === "result") {
      const usage = event.usage as Record<string, number> | undefined;
      const cost = (event.cost_usd as number) ?? 0;
      if (usage) {
        const inp = usage.input_tokens ?? 0;
        const out = usage.output_tokens ?? 0;
        turnStats.value = { inputTokens: inp, outputTokens: out, costUsd: cost };
        sessionCost.value += cost;
        chats.recordTurn(inp, out);
      }
      // Claude Code ≥1.x emits session_title in the result event after generating one
      if (!claudeGeneratedTitle.value) applyClaudeTitle(event.session_title);
    }
    saveMessages(props.chatId, messages.value);
    syncStore();
    scrollToBottom();
    // An `exit` from an intentional restart (mode switch / abort) is not a real
    // turn boundary — skip the "finished" toast/notification once.
    if ((type === "exit" || type === "result") && suppressNextDone.value) {
      suppressNextDone.value = false;
    } else {
      chats.sendStatusEvent(props.chatId, { type: "STOP", watching: props.isWatching ?? document.hasFocus() });
      notifyDone();
    }
    // Flush one queued message (next turn will flush the next one).
    if (messageQueue.value.length > 0) {
      const next = messageQueue.value.shift()!;
      // Remove its greyed-out placeholder from the feed
      const qIdx = messages.value.findIndex((m) => m.role === "queued" && m.text === next);
      if (qIdx !== -1) messages.value.splice(qIdx, 1);
      nextTick(() => sendMessage(next));
    }
    return;
  }
}

// ── ACP transport ──────────────────────────────────────────────────────────
// Lines from acp-data-{chatId}: session/update notifications + session/prompt
// responses (turn done) + the {_burrow:"exit"} EOF marker.
function onAcpData(raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { return; }

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
      for (const m of messages.value) m.partial = false;
      saveMessages(props.chatId, messages.value);
      scrollToBottom();
    }
    acpRestored = {};
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
      const result = msg.result as { configOptions?: AcpConfigOption[]; modes?: AcpModes; sessions?: AcpSessionInfo[] } | undefined;
      if (result?.configOptions) acpConfigOptions.value = result.configOptions;
      if (result?.modes) acpModes.value = result.modes;
      if (result?.sessions) acpSessions.value = result.sessions;
      // A model switch comes back with the adapter's whole selector set, effort
      // and permission mode reset to its defaults — put the user's picks back.
      if (result?.configOptions || result?.modes) restoreAcpSelections();
      return;
    }
    if (acpPromptRpcId.value === null || rid !== acpPromptRpcId.value) return;
    acpPromptRpcId.value = null;
    busy.value = false;
    for (const m of messages.value) { if (m.partial) m.partial = false; }
    saveMessages(props.chatId, messages.value);
    syncStore();
    scrollToBottom();
    if (!suppressNextDone.value) {
      chats.sendStatusEvent(props.chatId, { type: "STOP", watching: props.isWatching ?? document.hasFocus() });
      notifyDone();
    }
    suppressNextDone.value = false;
    if (messageQueue.value.length > 0) {
      const next = messageQueue.value.shift()!;
      const qIdx = messages.value.findIndex((m) => m.role === "queued" && m.text === next);
      if (qIdx !== -1) messages.value.splice(qIdx, 1);
      nextTick(() => sendMessage(next));
    }
    return;
  }

  // EOF from the Rust reader thread.
  if (msg._burrow === "exit") {
    // DIAG (remove later): adapter process ended. If this fires mid-turn with no
    // unmount log, the ACP/CLI subprocess died server-side — check acp-debug.log.
    console.warn(`[chat-diag] adapter EXIT chatId=${props.chatId} busy=${busy.value}`);
    if (busy.value) {
      busy.value = false;
      for (const m of messages.value) { if (m.partial) m.partial = false; }
      syncStore();
    }
    return;
  }

  if (msg.method !== "session/update") return;

  // Replayed user turns (session/load history) — render as user bubbles.
  const u = (msg.params as { update?: Record<string, unknown> })?.update;
  if (u?.sessionUpdate === "user_message_chunk") {
    const text = ((u.content as Record<string, unknown>)?.text as string) ?? "";
    const mid = (u.messageId as string) ?? "u";
    const last = messages.value.filter((m) => m.role === "user" && m._acpMsgId === mid).pop();
    if (last) last.text += text;
    else if (text) messages.value.push({ id: nextMsgId++, role: "user", text, _acpMsgId: mid });
    scrollToBottom();
    return;
  }

  const event = parseAcpUpdate(msg.params);
  if (!event) return;

  switch (event.kind) {
    case "thinking_chunk": {
      const last = messages.value[messages.value.length - 1];
      if (last?.role === "thinking" && last.partial) {
        last.text += event.text;
      } else {
        messages.value.push({ id: nextMsgId++, role: "thinking", text: event.text, partial: true });
      }
      scrollToBottom();
      break;
    }
    case "text_chunk":
    case "tool_call":
    case "tool_output":
      for (const runtimeEvent of normalizeAcpRuntimeEvent(event)) applyRuntimeEvent(runtimeEvent);
      break;
  }
}

// Lines from acp-req-{chatId}: blocking session/request_permission requests.
function onAcpReq(raw: string) {
  let msg: Record<string, unknown>;
  try { msg = JSON.parse(raw); } catch { return; }
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
  const pmMid = nextMsgId++;
  acpPermMsgId.value = pmMid;
  messages.value.push({ id: pmMid, role: "system-info", text: isPlan ? "📋 Plan ready for review" : `⚡ Permission: ${perm.title}` });
  chats.sendStatusEvent(props.chatId, { type: "PERMISSION_REQUEST" });
  notifyPermission({ requestId: String(perm.rpcId), toolName: perm.title, input: perm.rawInput, suggestions: [] } as CanUseToolReq);
  syncStore();
  scrollToBottom();
}

// Reply to a rich ACP permission request with the chosen adapter optionId.
async function acpRespond(optionId: string, optName: string, kind: string) {
  const r = acpPermReq.value;
  if (!r || permissionResponsePending.value) return;
  permissionResponsePending.value = true;
  const reject = kind.startsWith("reject");
  try {
    await invoke("acp_respond_permission", { id: props.chatId, rpcId: r.rpcId, optionId });
    messages.value.push({ id: nextMsgId++, role: "permission", text: `${reject ? "✗" : "✓"} ${optName}: ${r.title}` });
    saveMessages(props.chatId, messages.value);
    // serverRequest/resolved closes the prompt and updates the Sidebar state.
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Permission response failed: ${e}` });
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
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Could not copy message: ${e}` });
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
  if (extraImages?.length) pendingImages.value.push(...extraImages);
  // While busy: queue the message instead of sending immediately. ACP adapters
  // support promptQueueing (the agent queues it itself), so send concurrently —
  // pressing Enter force-sends now instead of waiting for the turn to finish.
  if (busy.value && !forcedText && !isAcpRuntime.value) {
    messageQueue.value.push(text);
    messages.value.push({ id: nextMsgId++, role: "queued", text });
    inputText.value = "";
    await nextTick();
    autoResize();
    scrollToBottom();
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
      messages.value.push({ id: nextMsgId++, role: "assistant", text: `Error reading git diff: ${e}` });
      return;
    }
  }

  const msgImages = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined;
  messages.value.push({ id: nextMsgId++, role: "user", text, images: msgImages });
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
    }
  }

  saveMessages(props.chatId, messages.value);
  syncStore();
  scrollToBottom();
  if (usesRpcRuntime.value) {
    try {
      const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined;
      pendingImages.value = [];
      acpPromptRpcId.value = await sendRpcRuntime(text, images);
    } catch (e) {
      messages.value.push({ id: nextMsgId++, role: "assistant", text: `Error: ${e}` });
      busy.value = false;
      chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
      syncStore();
    }
    return;
  }
  try {
    const images = pendingImages.value.length > 0 ? [...pendingImages.value] : undefined;
    pendingImages.value = [];
    await invoke("claude_send", { id: props.chatId, text, sessionId: sessionId.value || null, images });
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Error: ${e}` });
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
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
    saveMessages(props.chatId, messages.value);
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
    messages.value.push({ id: nextMsgId++, role: "permission", text: `${allow ? "✓ Allowed" : "✗ Denied"}: ${cr.toolName}` });
    saveMessages(props.chatId, messages.value);
    invoke("acp_respond_permission", { id: props.chatId, rpcId: acpPermRpcId.value, optionId }).catch((e) => {
      messages.value.push({ id: nextMsgId++, role: "assistant", text: `Permission response failed: ${e}` });
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
    messages.value.push({ id: nextMsgId++, role: "permission", text: `${label}: ${cr.toolName}${detailStr}` });
    saveMessages(props.chatId, messages.value);
  } catch (e) {
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Control response failed: ${e}` });
    saveMessages(props.chatId, messages.value);
  } finally {
    nativeControlResponsePending.value = false;
    syncStore();
  }
}

function toggleOption(question: string, label: string, multi: boolean) {
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

async function submitQuestion() {
  const cr = pendingQuestion.value;
  if (!cr || !canSubmitQuestion.value || nativeControlResponsePending.value) return;
  // The tool reads input.answers keyed by question text. A multi-select answer
  // must stay an array (the CLI expects the same shape it gave options in) —
  // joining it into a comma string here is what made multi-select questions
  // look permanently stuck after Submit.
  const answers: Record<string, string | string[]> = {};
  for (const q of questionSpecs.value) {
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
        id: nextMsgId++,
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
    messageQueue.value = [];
    messages.value = messages.value.filter((m) => m.role !== "queued");
    const lastAcp = messages.value[messages.value.length - 1];
    if (lastAcp?.partial) lastAcp.partial = false;
    chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
    syncStore();
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
  busy.value = false;
  messageQueue.value = [];
  messages.value = messages.value.filter((m) => m.role !== "queued");
  // Drop any in-flight permission/question/plan prompts — the proc backing them is gone.
  pendingPermission.value = null;
  pendingDiff.value = null;
  pendingQuestion.value = null;
  pendingPlan.value = null;
  const last = messages.value[messages.value.length - 1];
  if (last?.partial) last.partial = false;
  chats.sendStatusEvent(props.chatId, { type: "INTERRUPT" });
  syncStore();
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
  messageQueue.value = [];
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
    if (startErr) messages.value.push({ id: nextMsgId++, role: 'assistant', text: `Failed to start ${runtimeLabel.value}: ${startErr}` });
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
  // Switched to a stream-json agent at runtime → ensure the claude-data listener
  // exists (onMounted only attaches it when the chat starts as stream-json).
  if (!unlisten) unlisten = await listen<string>(`claude-data-${props.chatId}`, (ev) => onLine(ev.payload));
}

// `/cmd` token immediately before the cursor — at line start OR after whitespace,
// so command help works mid-message, not only when the input starts with `/`.
function slashQueryBeforeCursor(): { lead: string; q: string; full: string } | null {
  const el = inputEl.value;
  const pos = el?.selectionStart ?? inputText.value.length;
  const upto = inputText.value.slice(0, pos);
  const m = upto.match(/(^|\s)\/([^\s/]*)$/);
  return m ? { lead: m[1], q: m[2], full: m[0] } : null;
}

function updateSuggestions() {
  const m = slashQueryBeforeCursor();
  if (!m) { suggestions.value = []; return; }
  const q = m.q.toLowerCase();
  suggestions.value = allCommands.value.filter(
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

  // chatMessageHistory: burrow.claude.msgs.<id>
  if (getConfig<unknown>("chatMessageHistory", MISSING) === MISSING) {
    const re = /^burrow\.claude\.msgs\.(\d+)$/;
    const rec: Record<string, unknown> = {};
    const keys = Object.keys(localStorage).filter((k) => re.test(k));
    for (const k of keys) {
      const id = k.match(re)![1];
      const raw = localStorage.getItem(k);
      if (raw === null) continue;
      try { rec[id] = JSON.parse(raw); } catch { /* skip malformed entry */ continue; }
    }
    setConfig("chatMessageHistory", rec);
    for (const k of keys) localStorage.removeItem(k);
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

onMounted(async () => {
  // Config must be loaded (and legacy localStorage migrated) before any of the
  // config-backed refs below are trusted — reload them here once configReady settles.
  await configReady;
  migrateLegacyChatConfig();
  messages.value = loadMessages(props.chatId);
  selectedProfileId.value = loadProfileId(props.chatId);
  selectedModel.value = loadModel();
  selectedEffort.value = loadEffort();
  permMode.value = loadPermMode(props.chatId);

  chats.markSeen(props.chatId);
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
  if (usesRpcRuntime.value) {
    await scriptsStore.loadForPath(props.cwd);
    const startErr = await startRpcRuntime().catch((e: unknown) => e);
    if (startErr) messages.value.push({ id: nextMsgId++, role: 'assistant', text: `Failed to start ${runtimeLabel.value}: ${startErr}` });
    else if (props.initialPrompt) sendInitialPrompt(props.initialPrompt, props.initialImages);
    return;
  }
  let startErr: unknown = null;
  await invoke("claude_start", {
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
    messages.value.push({ id: nextMsgId++, role: "assistant", text: `Failed to start Claude CLI: ${e}` });
    return e;
  }).then((e) => { startErr = e; });
  unlisten = await listen<string>(`claude-data-${props.chatId}`, (ev) => onLine(ev.payload));
  if (!startErr && props.initialPrompt) sendInitialPrompt(props.initialPrompt, props.initialImages);

  // Load account info (plan, 5h window) — non-blocking.
  invoke<AccountInfo>("claude_get_account", { cwd: props.cwd })
    .then((info) => { accountInfo.value = info; })
    .catch(() => {});

  // Load installed skills and merge with built-ins. Skills override same-named built-ins.
  // Map-based dedup ensures no duplicates regardless of list_skills returning overlaps.
  try {
    const skills = await invoke<{ name: string; description: string; enabled: boolean }[]>("list_skills");
    const merged = new Map<string, Command>();
    for (const c of BUILTIN_COMMANDS) merged.set(c.name, c);
    for (const s of skills) {
      if (s.enabled) merged.set(s.name, { name: s.name, description: s.description || `/${s.name} skill` });
    }
    allCommands.value = [...merged.values()].sort((a, b) => a.name.localeCompare(b.name));
  } catch { /* browser-only dev without Tauri */ }
});

onBeforeUnmount(() => {
  if (copyFeedbackTimer) clearTimeout(copyFeedbackTimer);
  window.removeEventListener("keydown", onWindowKeydown);
  window.removeEventListener("mousedown", onPermMenuOutside);
  window.removeEventListener("mousedown", onEffortMenuOutside);
  window.removeEventListener("mousedown", onProfileMenuOutside);
  window.removeEventListener("mousedown", onAcpMenuOutside);
  unlisten?.();
  acpDataUL?.();
  acpReqUL?.();
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
  if (activeId === props.chatId) nextTick(() => scrollToBottom());
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
     original dark palette when a var is absent. */
  --chat-bg: var(--bg-base, #0f0f11);
  --chat-surface: var(--bg-panel, #18181c);
  --chat-border: var(--border, rgba(255,255,255,0.08));
  --chat-accent: var(--accent, #ec4899);
  --chat-accent-dim: var(--accent-dim, #6d28d9);
  --chat-text: var(--text-primary, rgba(255,255,255,0.88));
  --chat-muted: var(--text-muted, rgba(255,255,255,0.42));
  --chat-user-bg: color-mix(in srgb, var(--chat-accent) 14%, var(--chat-bg));
  --chat-user-border: color-mix(in srgb, var(--chat-accent) 35%, transparent);
}

.diff-line { line-height: 1.5; }
.diff-add { color: var(--success); }
.diff-del { color: var(--destructive); }

.chat-header-btn { position: relative; }

.chat-runtime-dot {
  box-shadow: 0 0 7px color-mix(in srgb, var(--success, #22c55e) 75%, transparent);
}

.btn-danger-active { color: #ef4444 !important; background: rgba(239,68,68,0.15) !important; }
.btn-active { color: #f472b6 !important; background: rgba(124,58,237,0.15) !important; }

/* Permission-mode dropdown */
.perm-mode-dropdown { position: relative; display: flex; }
.perm-mode-menu {
  position: fixed;
  z-index: 1000;
  min-width: 150px;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  background: #1e1e26;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 8px;
  box-shadow: 0 10px 30px rgba(0,0,0,0.5);
}
.perm-mode-item {
  display: flex;
  align-items: center;
  gap: 7px;
  width: 100%;
  padding: 6px 8px;
  background: none;
  border: none;
  border-radius: 5px;
  color: rgba(255,255,255,0.8);
  font-size: 11px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: color .12s, background .12s;
}
.perm-mode-item:hover { background: rgba(255,255,255,0.06); }
.perm-mode-item-active { color: #f472b6; background: rgba(124,58,237,0.12); }
.perm-mode-item-danger { color: #ef4444; }
.perm-mode-item-danger:hover { background: rgba(239,68,68,0.12); }
.perm-mode-item-danger.perm-mode-item-active { color: #ef4444; background: rgba(239,68,68,0.12); }
.perm-mode-label { font-size: 10px; font-weight: 600; }
.perm-mode-caret { opacity: .6; margin-left: -1px; }

/* Permission-gate banners: shared animation */
.perm-slide-in { animation: perm-slide-in 0.15s ease-out; }
@keyframes perm-slide-in {
  from { opacity: 0; transform: translateY(-4px); }
  to   { opacity: 1; transform: translateY(0); }
}

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
  color: rgba(255,255,255,0.7);
  cursor: pointer;
  text-align: left;
}
.perm-dropdown-item:hover { background: rgba(255,255,255,0.07); color: #fff; }
.perm-pattern {
  font-size: 10px;
  color: rgba(255,255,255,0.45);
  background: rgba(255,255,255,0.07);
  border-radius: 3px;
  padding: 1px 4px;
}

/* ACP permission banner variants (plan / diff icon+border tint) */
.acp-perm-plan .perm-icon { color: #10b981; }
.acp-perm-diff .perm-icon { color: #818cf8; }
.acp-perm-plan .plan-body { max-height: 320px; overflow: auto; }

/* Avatars */
.agent-avatar {
  background: radial-gradient(circle at 30% 25%, color-mix(in srgb, var(--agent-accent, #ec4899) 80%, #fff) 0%, var(--agent-accent, #ec4899) 60%, color-mix(in srgb, var(--agent-accent, #ec4899) 55%, #000) 100%);
}

.mention-pill {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 1px 6px;
  margin: 0 1px;
  background: rgba(124,58,237,0.18);
  border: 1px solid rgba(124,58,237,0.35);
  border-radius: 10px;
  font-size: 0.92em;
  vertical-align: baseline;
}
.mention-pill-icon { color: rgba(167,139,250,0.95); flex-shrink: 0; }

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
.tool-row-running { border-color: color-mix(in srgb, var(--agent-accent, #ec4899) 32%, transparent); }
.tool-row-failed {
  background: color-mix(in srgb, var(--destructive, #ef4444) 8%, transparent);
  border-color: color-mix(in srgb, var(--destructive, #ef4444) 30%, transparent);
}
.tool-caret {
  flex-shrink: 0;
  color: var(--agent-accent, #ec4899);
  transition: transform .15s;
}
.tool-caret-open { transform: rotate(90deg); }
.tool-icon { color: var(--agent-accent, #ec4899); flex-shrink: 0; }
.tool-row-failed .tool-icon { color: var(--destructive, #ef4444); }
.tool-status-icon { flex-shrink: 0; }
.tool-spin { animation: tool-spin 0.9s linear infinite; color: var(--agent-accent, #ec4899); }
@keyframes tool-spin { to { transform: rotate(360deg); } }
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
  background: rgba(124,58,237,0.7);
  animation: thinking 1.2s ease-in-out infinite;
}
.thinking-dot { width: 5px; height: 5px; background: rgba(124,58,237,0.6); }
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
  color: rgba(255,255,255,0.3);
  background: none;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 4px;
  padding: 1px 5px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
}
.queue-item-btn:hover { color: rgba(255,255,255,0.7); border-color: rgba(255,255,255,0.25); }

/* Context usage bar fill colors */
.ctx-usage-bar.ctx-ok { background: rgba(124,58,237,0.5); }
.ctx-usage-bar.ctx-warning { background: rgba(234,179,8,0.7); }
.ctx-usage-bar.ctx-exceeded { background: rgba(239,68,68,0.8); }

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
  background: rgba(22,163,74,0.12);
  border: 1px solid rgba(22,163,74,0.3);
  color: #4ade80;
}
.bubble-permission.perm-rejected {
  background: rgba(185,28,28,0.12);
  border: 1px solid rgba(185,28,28,0.3);
  color: #f87171;
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
.cmd-suggestion.selected { background: rgba(255,255,255,0.05); }

/* Input toolbar buttons */
.toolbar-btn {
  background: none;
  border: none;
  color: rgba(255,255,255,0.45);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 7px;
  border-radius: 7px;
  font-size: 11px;
  font-family: var(--font-ui);
  transition: color .12s, background .12s;
}
.toolbar-btn:hover { color: rgba(255,255,255,0.8); background: rgba(255,255,255,0.06); }
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
  background: #1e1e26;
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 10px;
  box-shadow: 0 10px 30px rgba(0,0,0,0.5);
}
.floating-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 7px 10px;
  background: none;
  border: none;
  border-radius: 7px;
  color: rgba(255,255,255,0.8);
  font-size: 12px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  transition: background .1s;
  gap: 6px;
}
.floating-menu-item:hover { background: rgba(255,255,255,0.06); }
.floating-menu-item-active { color: #f472b6; background: rgba(124,58,237,0.12); }
.model-id-hint {
  font-size: 9px;
  font-family: var(--font-mono);
  color: rgba(255,255,255,0.3);
  margin-left: 6px;
}
.floating-menu-item > .model-id-hint { margin-left: auto; }

.agent-dropdown { position: relative; display: inline-flex; }
.acp-history-menu { min-width: 280px; max-width: 360px; max-height: 320px; overflow-y: auto; }

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
  transition: background .12s, opacity .12s, box-shadow .12s, transform .12s;
  box-shadow: 0 2px 10px color-mix(in srgb, var(--agent-accent, #ec4899) 40%, transparent);
}
.send-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--agent-accent, #ec4899) 80%, #000); transform: translateY(-1px); }
.send-btn:disabled { opacity: 0.35; cursor: default; }
.send-btn-abort { background: #dc2626; }
.send-btn-abort:hover:not(:disabled) { background: #b91c1c; }

.pending-img-remove {
  position: absolute;
  top: -5px;
  right: -5px;
  width: 16px;
  height: 16px;
  background: #1e1e26;
  border: 1px solid rgba(255,255,255,0.15);
  border-radius: 50%;
  color: rgba(255,255,255,0.5);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  transition: color .1s, background .1s;
}
.pending-img-remove:hover { color: #f87171; background: rgba(185,28,28,0.2); }

/* Markdown body inside assistant messages — v-html content, needs real
   selectors (:deep) since these elements aren't authored in this SFC. */
.md-body :deep(p) { margin: 0 0 10px; }
.md-body :deep(p:last-child) { margin-bottom: 0; }
.md-body :deep(ul), .md-body :deep(ol) { margin: 4px 0 10px; padding-left: 20px; }
.md-body :deep(li) { margin: 3px 0; }
.md-body :deep(code) { font-family: var(--font-mono); font-size: 11px; background: rgba(124,58,237,0.14); color: #c4b5fd; padding: 1px 5px; border-radius: 4px; }
.md-body :deep(pre) { background: rgba(0,0,0,0.35); border: 1px solid rgba(255,255,255,0.08); border-radius: 8px; padding: 12px 14px; overflow-x: auto; margin: 8px 0; }
.md-body :deep(pre code) { background: none; padding: 0; font-size: 11px; color: rgba(255,255,255,0.75); }
.md-body :deep(blockquote) { border-left: 3px solid rgba(124,58,237,0.6); margin: 6px 0; padding-left: 12px; color: rgba(255,255,255,0.55); }
.md-body :deep(h1), .md-body :deep(h2), .md-body :deep(h3) { font-weight: 700; margin: 14px 0 6px; color: rgba(255,255,255,0.95); }
.md-body :deep(h1) { font-size: 16px; }
.md-body :deep(h2) { font-size: 14px; }
.md-body :deep(h3) { font-size: 13px; }
.md-body :deep(a) { color: #f472b6; text-decoration: underline; }
.md-body :deep(hr) { border: none; border-top: 1px solid rgba(255,255,255,0.1); margin: 10px 0; }
.md-body :deep(table) { border-collapse: collapse; font-size: 12px; margin: 8px 0; }
.md-body :deep(th), .md-body :deep(td) { border: 1px solid rgba(255,255,255,0.1); padding: 5px 10px; }
.md-body :deep(th) { background: rgba(255,255,255,0.05); font-weight: 600; }
</style>
