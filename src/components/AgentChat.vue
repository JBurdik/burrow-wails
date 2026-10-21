<template>
  <div class="claude-chat surface-glow [--glow-base:var(--bg-base)] drop-zone flex h-full flex-row overflow-hidden bg-base" :style="{ '--agent-accent': agentAccentColor }" @mousedown.capture="onWindowFocus">
    <div class="chat-main flex min-w-0 flex-1 flex-col overflow-hidden">

    <!-- Chat title bar -->
    <!-- Only when embedded (Right Panel sub-agent): a full-size chat shows its title in the window's top row. -->
    <div v-if="compact && activeSession?.title" class="chat-title-bar flex flex-shrink-0 items-center gap-2 border-b border-border px-3 py-1.5 text-[12px] font-medium text-foreground">
      <span class="min-w-0 flex-1 truncate">{{ activeSession.title }}</span>
      <span v-if="turnCount > 0" class="flex-shrink-0 text-[11px] font-normal text-muted-foreground">{{ turnCount }} turn{{ turnCount === 1 ? '' : 's' }}</span>
      <span v-if="activeModelLabel" class="flex flex-shrink-0 items-center gap-1 text-[11px] font-normal text-muted-foreground">
        <component :is="agentIconComp(currentAgent?.icon)" :size="11" />
        {{ activeModelLabel }}
      </span>
    </div>

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
          <div v-if="permDropdownOpen" class="absolute bottom-[calc(100%+4px)] right-0 z-[100] min-w-[200px] rounded-lg border border-[var(--chat-border)] bg-[var(--chat-dropdown)] p-1">
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

    <div class="relative flex min-h-0 flex-1">
    <div ref="scrollEl" @scroll.passive="onScroll" class="chat-messages relative flex flex-1 flex-col overflow-y-auto py-6 pb-2 [scroll-behavior:smooth] [-webkit-user-select:text] [user-select:text]">
      <div class="mx-auto flex w-full max-w-[760px] flex-1 flex-col gap-0.5">
      <!-- A remount starts with no messages until the transcript is read back, which on
           a slow link (remote client) takes long enough to read as an empty conversation. -->
      <div v-if="messages.length === 0 && transcriptLoading" class="flex flex-col gap-6 px-4 py-4" aria-busy="true" aria-label="Loading conversation">
        <div class="ml-auto h-9 w-2/5 animate-pulse rounded-2xl bg-hover" />
        <div class="flex flex-col gap-2">
          <div class="h-3 w-full animate-pulse rounded bg-hover" />
          <div class="h-3 w-11/12 animate-pulse rounded bg-hover" />
          <div class="h-3 w-3/5 animate-pulse rounded bg-hover" />
        </div>
        <div class="ml-auto h-9 w-1/3 animate-pulse rounded-2xl bg-hover" />
        <div class="flex flex-col gap-2">
          <div class="h-3 w-10/12 animate-pulse rounded bg-hover" />
          <div class="h-3 w-2/3 animate-pulse rounded bg-hover" />
        </div>
      </div>
      <div v-else-if="messages.length === 0" class="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-10 text-center">
        <div class="chat-empty-avatar mb-2 flex h-11 w-11 items-center justify-center rounded-[var(--radius-avatar)] border border-border" style="background: var(--surface-elevated);" aria-hidden="true">
          <component :is="currentAgentIcon" :size="28" :style="{ color: 'var(--agent-accent)' }" />
        </div>
        <span class="text-[9px] font-semibold uppercase tracking-[.08em] text-muted-foreground">New conversation</span>
        <span class="text-base font-semibold text-foreground">Start a focused conversation</span>
        <span class="mt-0.5 font-mono text-[11px] text-muted-foreground">Working in {{ cwdDisplay }}</span>
      </div>

      <div v-if="hiddenBefore > 0" class="flex justify-center py-2">
        <button
          class="rounded-md border border-border px-3 py-1 text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-50"
          :disabled="loadingEarlier"
          @click="loadEarlierKeepingScroll"
        >{{ loadingEarlier ? "Loading…" : `Load earlier (${hiddenBefore} more)` }}</button>
      </div>

      <div
        v-for="(msg, msgIdx) in displayItems"
        :key="msg.id"
        :data-message-id="msg.id"
        class="chat-message"
        :class="[`role-${msg.role}`, { partial: msg.partial }]"
      >
        <!-- Collapsed tool-call group pill ("Ran N commands" / "Used N tools") -->
        <template v-if="msg.role === 'tool-group-header'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
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
            <button class="worked-bar" @click="foldedTurns[msg.turnKey] = !msg.folded">
              <PhCaretRight :size="12" weight="bold" class="tool-caret" :class="{ 'tool-caret-open': !msg.folded }" />
              <span class="font-semibold text-foreground">{{ msg.label }}</span>
              <span class="ml-auto flex items-center gap-1.5">
                <span v-if="msg.toolCount" class="worked-pill"><PhWrench :size="12" />{{ msg.toolCount }} tool {{ msg.toolCount === 1 ? 'call' : 'calls' }}</span>
                <span v-if="msg.fileCount" class="worked-pill"><PhPencilSimple :size="12" />{{ msg.fileCount }} {{ msg.fileCount === 1 ? 'file' : 'files' }}</span>
              </span>
            </button>
          </div>
        </template>

        <!-- Files this turn edited: a diffstat card whose rows expand into the
             edit's own before/after lines (Ghostex-style inline diff). -->
        <template v-else-if="msg.role === 'changed-files'">
          <div class="flex items-start gap-2.5 px-4 py-1">
            <div class="diff-card min-w-0 max-w-[720px] flex-1">
              <div class="diff-card-head">
                <span class="diff-card-icon"><PhPencilSimple :size="14" /></span>
                <div class="min-w-0">
                  <div class="text-[13px] font-semibold text-foreground">Edited {{ msg.files.length }} {{ msg.files.length === 1 ? 'file' : 'files' }}</div>
                  <div class="truncate text-[11px] text-muted-foreground">{{ diffDirs(msg.files) }}</div>
                </div>
                <span class="ml-auto flex-shrink-0 font-mono text-[12px]"><span class="diff-add">+{{ msg.added }}</span> <span class="diff-del">−{{ msg.removed }}</span></span>
                <span class="diff-ratio"><span class="diff-ratio-add" :style="{ width: diffRatio(msg.added, msg.removed) + '%' }" /></span>
              </div>
              <div v-for="(f, fi) in msg.files" :key="f.path">
                <div class="diff-file-row" :title="f.path" @click="toggleDiff(msg.id, f.path, fi)">
                  <PhCaretRight :size="10" class="tool-caret" :class="{ 'tool-caret-open': diffOpen(msg.id, f.path, fi) }" />
                  <PhFileText :size="13" class="flex-shrink-0 text-[var(--chat-info)]" />
                  <span class="max-w-[55%] flex-shrink-0 truncate font-semibold text-foreground">{{ basename(f.path) }}</span>
                  <span class="min-w-0 truncate text-muted-foreground" style="direction: rtl; text-align: left;">{{ dirOf(f.path) }}</span>
                  <span class="ml-auto flex-shrink-0 font-mono text-[11px]"><span class="diff-add">+{{ f.added }}</span> <span class="diff-del">−{{ f.removed }}</span></span>
                  <span class="diff-blocks"><i v-for="(b, bi) in diffBlocks(f.added, f.removed)" :key="bi" :class="b" /></span>
                  <button class="diff-open" title="Open the file's diff" @click.stop="openChangedFileDiff(f.path)"><PhArrowSquareOut :size="12" /></button>
                </div>
                <div v-if="diffOpen(msg.id, f.path, fi)" class="diff-lines">
                  <template v-for="(l, li) in hunkLines(f.hunks).lines" :key="li">
                    <div v-if="l.kind === 'sep'" class="diff-sep">⋯</div>
                    <div v-else class="diff-line" :class="l.kind === 'add' ? 'diff-line-add' : 'diff-line-del'"><span class="diff-sign">{{ l.kind === 'add' ? '+' : '−' }}</span>{{ l.text || ' ' }}</div>
                  </template>
                  <div v-if="hunkLines(f.hunks).hidden" class="diff-more" @click="openChangedFileDiff(f.path)">{{ hunkLines(f.hunks).hidden }} more lines — open diff</div>
                </div>
              </div>
            </div>
          </div>
        </template>

        <!-- User message -->
        <template v-else-if="msg.role === 'user'">
          <div class="group flex items-end justify-end gap-1.5 px-4" :class="isFirstOfRun(msgIdx) ? 'pt-2.5 pb-[3px]' : 'py-[1px]'">
            <div class="flex flex-shrink-0 items-center self-center">
              <button class="message-copy-btn" :aria-label="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" :title="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" @click="copyMessage(msg)">
                <PhCheck v-if="copiedMessageId === msg.id" :size="13" weight="bold" />
                <PhCopy v-else :size="13" />
              </button>
              <button class="message-copy-btn" aria-label="Edit" title="Edit — put this text back in the composer" @click="editMessage(msg)">
                <PhPencilSimple :size="13" />
              </button>
              <template v-if="canBranch">
                <button class="message-copy-btn" aria-label="Rewind to here" title="Rewind to here — drop this message and everything after it, then edit and resend" @click="rewindTo(msg)">
                  <PhArrowCounterClockwise :size="13" />
                </button>
                <button class="message-copy-btn" aria-label="Fork from here" title="Fork from here — continue this conversation in a new chat" @click="forkFrom(msg)">
                  <PhGitFork :size="13" />
                </button>
              </template>
            </div>
            <div class="bubble-user max-w-[72%] rounded-[18px] px-4 py-2.5 text-[13px] leading-[1.55]" style="background: var(--bg-selected); color: var(--text-primary);">
              <div v-if="msg.images && msg.images.length > 0" class="mb-1.5 flex flex-wrap gap-1.5">
                <img
                  v-for="(img, i) in msg.images"
                  :key="i"
                  :src="img"
                  class="block max-h-40 max-w-[200px] cursor-pointer rounded object-cover"
                  :alt="`Image ${i + 1}`"
                  @click="showImage(img)"
                />
              </div>
              <div class="md-body">
                <template v-for="(seg, segIdx) in userMsgSegments(msg)" :key="segIdx">
                  <PasteChip v-if="seg.paste" :text="seg.text" />
                  <span v-else v-html="renderUserMd(seg.text)" />
                </template>
              </div>
            </div>
          </div>
        </template>

        <!-- Tool call — compact row, expandable; hidden while its parent group pill is collapsed -->
        <template v-else-if="msg.role === 'tool'">
          <template v-if="!groupIdOf(msg.id) || isGroupExpanded(groupIdOf(msg.id))">
            <div class="group flex items-start gap-2.5 px-4 py-[3px]">
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

        <!-- Sub-agent spawn marker: a pointer into the Right Panel, not just a note -->
        <template v-else-if="msg.role === 'system-info' && msg.subagentChatId">
          <div class="flex justify-center px-4 py-1">
            <button
              class="flex max-w-[min(440px,85%)] items-center gap-1.5 rounded-[20px] border border-border bg-hover px-2.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:border-accent/45 hover:text-foreground"
              :title="msg.text"
              @click="openSubagentFromTranscript(msg.subagentChatId)"
            >
              <PhRobot :size="11" class="shrink-0" />
              <!-- A spawn prompt is a paragraph, not a title: clamp it to one
                   line so the marker stays a marker. Full text is the tooltip. -->
              <span class="truncate">{{ subagentLabel(msg.text) }}</span>
              <span v-if="msg.subagentAgent" class="shrink-0 text-secondary-foreground">· {{ msg.subagentAgent }}</span>
              <span class="status-dot shrink-0" :class="subagentDotClass(msg.subagentChatId)" />
            </button>
          </div>
        </template>

        <!-- System info marker (permission requested, plan ready, etc.) -->
        <template v-else-if="msg.role === 'system-info'">
          <div class="flex justify-center px-4 py-1">
            <span class="rounded-[20px] border border-border bg-hover px-2.5 py-0.5 text-[11px] text-muted-foreground">{{ msg.text }}</span>
          </div>
        </template>

        <!-- Permission log -->
        <template v-else-if="msg.role === 'permission'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
            <div class="bubble-permission" :class="msg.text.startsWith('✓') ? 'perm-granted' : 'perm-rejected'">
              <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ msg.text }}</span>
            </div>
          </div>
        </template>

        <!-- Thinking -->
        <template v-else-if="msg.role === 'thinking'">
          <div class="flex items-start gap-2.5 px-4 py-[3px]">
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
          <div class="group flex items-start gap-2.5 px-4" :class="isFirstOfRun(msgIdx) ? 'pt-3 pb-1' : 'py-[1px]'">
            <div class="min-w-0 flex-1 pt-1 text-[14.5px] leading-[1.7] text-foreground">
              <TranscriptComments
                :message-id="msg.id"
                :text="msg.text"
                :partial="msg.partial"
                :notes="transcriptFeedback.notes.value.filter((note) => note.messageId === msg.id)"
                @add="(quote, body) => transcriptFeedback.add(msg.id, msg.text, quote, body)"
                @remove="transcriptFeedback.remove"
                @resolve="transcriptFeedback.setResolved"
                @follow-up="jumpToFeedback"
              >
                <!-- eslint-disable-next-line vue/no-v-html -->
                <div class="md-body" v-html="renderAssistantMd(msg)" @click="onRichBlockClick" />
              </TranscriptComments>
            </div>
            <div class="mt-0.5 flex flex-shrink-0 items-center">
              <button class="message-copy-btn" :aria-label="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" :title="copiedMessageId === msg.id ? 'Copied' : 'Copy message'" @click="copyMessage(msg)">
                <PhCheck v-if="copiedMessageId === msg.id" :size="13" weight="bold" />
                <PhCopy v-else :size="13" />
              </button>
              <button v-if="canBranch && finalReplyIds.has(msg.id)" class="message-copy-btn" aria-label="Fork from here" title="Fork from here — continue this conversation in a new chat" @click="forkFrom(msg)">
                <PhGitFork :size="13" />
              </button>
            </div>
          </div>
        </template>
      </div>

      <!-- Working line (Ghostex-style): a slowly spinning spark, a playful verb, the clock. -->
      <div v-if="busy" class="flex items-center gap-2 px-4 py-2">
        <PhSparkle :size="15" weight="fill" class="working-spark" />
        <span class="text-[13px] text-secondary-foreground">{{ workingVerb }}…</span>
        <span class="text-[12px] tabular-nums text-muted-foreground">{{ workingElapsed }}</span>
      </div>
      </div>
    </div>
    <!-- Minimap (after Ghostex): one dash per prompt you sent, longer for
         longer prompts; hover previews it, click jumps to it. -->
    <div v-if="minimapPrompts.length >= 2" class="minimap-rail" @mouseleave="minimapHover = null">
      <button
        v-for="(p, i) in minimapPrompts"
        :key="p.id"
        class="minimap-dash"
        :class="{ 'is-current': p.id === minimapCurrent, 'is-hover': minimapHover?.id === p.id }"
        :style="{ width: p.width + 'px' }"
        :aria-label="`Jump to prompt ${i + 1}`"
        @mouseenter="minimapHover = { ...p, index: i }"
        @click="jumpToMessage(p.id)"
      />
      <div v-if="minimapHover" class="minimap-preview">
        <div class="mb-1 text-[11px] text-muted-foreground">Prompt {{ minimapHover.index + 1 }} of {{ minimapPrompts.length }}</div>
        <div class="line-clamp-6 whitespace-pre-wrap text-[12.5px] leading-[1.5] text-foreground">{{ minimapHover.text }}</div>
      </div>
    </div>
    </div>

    <!-- Jump back to live: only while scrolled up (autoscroll paused) -->
    <div class="relative h-0">
      <button
        v-if="!atBottom"
        class="absolute bottom-2 left-1/2 z-10 flex -translate-x-1/2 items-center gap-1 rounded-full border border-border bg-[var(--bg-dropdown)] px-2.5 py-1 text-[11px] text-secondary-foreground transition-colors hover:bg-hover"
        title="Jump to latest"
        @click="scrollToBottom(true)"
      >
        <PhArrowDown :size="12" weight="bold" /> Latest
      </button>
    </div>

    <!-- New-style input bar -->
    <div class="flex-shrink-0 px-[18px] pb-2 pt-2.5">
      <div class="mx-auto w-full max-w-[760px]">
      <!-- Branch-changed banner: session.branch (created-on snapshot) vs. current workspace branch -->
      <div v-if="branchChanged && !branchBannerDismissed" class="mb-1.5 flex items-center gap-2 rounded-lg border border-border bg-hover px-3 py-1.5 text-[11px] text-muted-foreground">
        <PhGitBranch :size="13" weight="bold" class="flex-shrink-0" />
        <span class="flex-1 truncate">Branch changed — was <span class="font-mono text-foreground">{{ activeSession?.branch }}</span></span>
        <span v-if="restoreBranchError" class="flex-shrink-0 text-destructive">{{ restoreBranchError }}</span>
        <button class="flex-shrink-0 font-medium text-foreground hover:underline disabled:opacity-50" :disabled="restoringBranch" @click="restoreBranch">{{ restoringBranch ? 'Restoring…' : 'Restore branch' }}</button>
        <button class="flex-shrink-0 text-muted-foreground hover:text-foreground" title="Dismiss" @click="branchBannerDismissed = true"><PhX :size="11" weight="bold" /></button>
      </div>
      <div v-if="catchUp.open" class="catchup-card mb-2">
        <div class="flex items-center gap-2">
          <PhSparkle :size="14" class="text-foreground" />
          <span class="text-[13px] font-semibold text-foreground">Catch me up</span>
          <span v-if="catchUp.loading" class="text-[11.5px] text-muted-foreground">Reading the last {{ CATCH_UP_MESSAGES }} messages…</span>
          <span class="ml-auto flex items-center gap-3 text-[11.5px] text-secondary-foreground">
            <button class="hover:text-foreground disabled:opacity-40" :disabled="catchUp.loading" @click="runCatchUp">Refresh</button>
            <button class="hover:text-foreground disabled:opacity-40" :disabled="!catchUp.summary" @click="copyCatchUp">{{ catchUp.copied ? 'Copied' : 'Copy' }}</button>
            <button class="hover:text-foreground" title="Close" @click="catchUp.open = false"><PhX :size="12" /></button>
          </span>
        </div>
        <p v-if="catchUp.error" class="mt-1.5 text-[12.5px] text-[var(--red)]">{{ catchUp.error }}</p>
        <template v-else-if="catchUp.summary">
          <p class="mt-1.5 text-[13px] leading-[1.5] text-foreground">{{ catchUp.summary }}</p>
          <div v-for="(it, i) in catchUp.items" :key="i" class="mt-1.5 flex items-start gap-2 text-[12.5px] leading-[1.5] text-foreground">
            <span class="catchup-tag" :class="'tag-' + it.tag">{{ CATCH_UP_LABELS[it.tag] ?? it.tag }}</span>
            <span>{{ it.text }}</span>
          </div>
        </template>
      </div>
      <div v-if="subagentChildren.length" class="subagents-card mb-2">
        <button class="subagents-head" @click="subagentsCollapsed = !subagentsCollapsed">
          <PhUsersThree :size="14" class="flex-shrink-0 text-secondary-foreground" />
          <span class="font-semibold text-foreground">Sub-agents</span>
          <span class="text-muted-foreground">{{ subagentChildren.length }} {{ subagentChildren.length === 1 ? 'thread' : 'threads' }}</span>
          <PhCaretDown :size="11" class="ml-auto text-muted-foreground transition-transform" :class="{ '-rotate-90': subagentsCollapsed }" />
        </button>
        <template v-if="!subagentsCollapsed">
          <button v-for="c in subagentChildren" :key="c.id" class="subagent-row" :title="c.title" @click="openSubagentFromTranscript(c.id)">
            <span class="status-dot shrink-0" :class="subagentDotClass(c.id)" />
            <span class="shrink-0 font-medium text-foreground">{{ c.title }}</span>
            <span class="subagent-badge" :class="'tone-' + subagentState(c.id).tone">{{ subagentState(c.id).label }}</span>
            <span class="min-w-0 truncate text-muted-foreground">{{ subagentDetail[c.id] }}</span>
          </button>
        </template>
      </div>
      <TranscriptFeedbackBar
        :count="transcriptFeedback.pending.value.length"
        :sending="sendingFeedback"
        :busy="busy"
        :error="feedbackSendError || transcriptFeedback.error.value"
        @send="sendTranscriptNotes"
      />
      <div class="relative">
      <!-- @file / $skill / /command picker. Floats above the frame rather than
           inside it: .chat-input-box is overflow-hidden, and an inline list
           pushed the whole toolbar down as the user typed. -->
      <ComposerSuggestions :items="suggestions" :active-index="activeIndex" @pick="completion.apply" />
      <div class="chat-input-box overflow-hidden rounded-[18px] border border-border transition-[border-color,box-shadow]" :class="{ 'input-queued': busy && inputText.trim() }" style="background: var(--bg-panel);">
        <!-- Queued follow-ups live inside the composer (Ghostex-style): one row
             each, drag to reorder, play = interrupt and send now, pencil = take
             it back into the input. -->
        <div v-if="messageQueue.length" class="flex flex-col gap-1.5 px-2.5 pt-2.5">
          <div
            v-for="(q, qi) in messageQueue"
            :key="q.id"
            draggable="true"
            class="queue-row group flex cursor-grab items-center gap-2 rounded-[9px] border border-border px-2.5 py-1.5 text-[13px] text-secondary-foreground"
            :class="{ 'opacity-40': draggingQueuedId === q.id, 'queue-row-drop': dropQueuedIndex === qi && draggingQueuedId !== q.id }"
            @dragstart="draggingQueuedId = q.id"
            @dragend="draggingQueuedId = null; dropQueuedIndex = null"
            @dragover.prevent="draggingQueuedId != null && (dropQueuedIndex = qi)"
            @drop.prevent="dropQueued(qi)"
          >
            <PhDotsSixVertical :size="13" class="flex-shrink-0 text-muted-foreground" />
            <span class="min-w-0 flex-1 truncate">{{ q.text }}</span>
            <PhImage v-if="q.images?.length" :size="12" class="flex-shrink-0 text-muted-foreground" />
            <button class="queue-act" title="Interrupt the current turn and send this now" @click="sendQueuedNow(q.id)"><PhPlay :size="12" weight="fill" /></button>
            <button class="queue-act" title="Take it back into the composer" @click="editQueued(q)"><PhPencilSimple :size="12" /></button>
            <button class="queue-act" title="Remove from queue" @click="removeQueued(q.id)"><PhX :size="12" /></button>
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
        <button
          v-if="showCatchUpBtn"
          class="catchup-btn"
          title="Summarize where this chat is at (text-generation model, nothing is sent to the agent)"
          @click="runCatchUp"
        ><PhSparkle :size="13" /> Catch me up</button>
        <ComposerTextInput
          ref="inputEl"
          v-model="inputText"
          class="chat-input composer-input box-border block max-h-40 min-h-12 w-full border-none bg-transparent px-4 pb-1 pt-3 font-sans text-[14px] leading-[1.55] text-foreground outline-none"
          :class="showCatchUpBtn && 'pr-[132px]'"
          :placeholder="busy ? 'Keep typing — Enter queues it for when the agent is free.\nUse @ to mention a file and $ for skills.' : 'Press Enter to send a message.\nUse @ to mention a file and $ for skills.'"
          :skills="completion.skills.value"
          :commands="allCommands"
          :files="completion.files.value"
          @keydown="onKeydown"
          @input="onInput"
          @paste="onPaste"
        />
        </div>
          <ComposerImages v-model="pendingImages" class="px-3 pb-1.5" />
          <div class="composer-toolbar px-2.5 pb-2.5 pt-2">
          <!-- Left: share selection, model dropdown, perm mode -->
          <div class="composer-pillbar">
            <button
              v-if="editorCtx.selection"
              class="composer-pill"
              :title="`Add selection: ${relPath(editorCtx.selection.path)}#L${editorCtx.selection.startLine}-L${editorCtx.selection.endLine}`"
              @click="shareSelection"
            >
              <PhTextAa :size="13" />
            </button>
            <span v-if="asleep" class="composer-pill pointer-events-none opacity-70" title="Sleeping: the agent process was stopped to free memory. Sending a message wakes it."><PhMoon :size="13" weight="fill" /></span>
            <!-- Agent + model in one popover (the old header bar is gone) -->
            <ModelPicker
              :agent-id="agentKind"
              :model-id="effectiveTransport === 'claude-cli' ? selectedModel : acpActiveModelId"
              :models="liveModels.length ? liveModels : undefined"
              :cwd="cwd"
              :settings="pickerSettings"
              @select="onPickModel"
              @setting="onPickerSetting"
            />
            <!-- Claude Agent SDK effort is forwarded to the local Claude Code CLI. -->
            <ComposerPill
              v-if="effectiveTransport === 'claude-cli'"
              :label="selectedEffortLabel.replace(/ effort$/, '')"
              :items="CLAUDE_EFFORTS"
              :active="selectedEffort"
              title="Claude reasoning effort"
              @select="selectEffort($event as ClaudeEffort)"
            />
            <!-- Profile switcher (only shown when more than one profile exists) -->
            <ComposerPill
              v-if="effectiveTransport === 'claude-cli' && claudeProfiles.length > 1"
              :icon="PhUserGear"
              :label="selectedProfile?.name ?? 'Default'"
              :items="profileItems"
              :active="selectedProfileId"
              :highlight="selectedProfileId !== defaultProfileId"
              :title="selectedProfile?.configDir ? `CLAUDE_CONFIG_DIR: ${selectedProfile.configDir}` : 'Claude profile'"
              @select="selectProfile"
            />
            <!-- Permission mode switcher (native Claude only) -->
            <ComposerPill
              v-if="effectiveTransport === 'claude-cli'"
              :icon="PERM_ICON[permMode]"
              :items="permItems"
              :active="permMode"
              :danger="permMeta.danger"
              :highlight="permMode === 'acceptEdits'"
              :title="permMeta.title"
              detailed
              @select="selectPermMode($event as PermMode)"
            />

            <!-- ACP effort switcher (driven by the adapter's configOptions) -->
            <ComposerPill
              v-if="isAcpRuntime && acpEffortOption"
              :label="acpEffortLabel"
              :items="acpEffortItems"
              :active="acpEffortOption.currentValue"
              @select="acpSelectEffort"
            />

            <!-- ACP permission-mode switcher (driven by the adapter's session modes) -->
            <ComposerPill
              v-if="isAcpRuntime && acpModes"
              :icon="PhShieldCheck"
              :label="acpModeLabel"
              :items="acpModeItems"
              :active="acpModes.currentModeId"
              :title="`Permission mode: ${acpModeLabel}`"
              detailed
              @select="acpSelectMode"
            />
          </div>

          <!-- Right: cost badge + abort/send -->
          <div class="composer-sendgroup">
            <HoverCardRoot v-if="contextUsageRatio > 0" v-model:open="ctxCardOpen" :open-delay="120" :close-delay="120">
              <HoverCardTrigger as-child>
                <div class="ctx-ring" :class="contextUsageClass">
                  <svg viewBox="0 0 16 16" class="h-4 w-4 -rotate-90">
                    <circle cx="8" cy="8" :r="CTX_R" fill="none" stroke="currentColor" stroke-width="2" class="opacity-20" />
                    <circle cx="8" cy="8" :r="CTX_R" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" :stroke-dasharray="contextDash" />
                  </svg>
                  <span class="ctx-ring-pct font-mono text-[10px]">{{ contextPct }}%</span>
                </div>
              </HoverCardTrigger>
              <HoverCardPortal>
                <HoverCardContent
                  side="top"
                  align="end"
                  :side-offset="6"
                  class="z-50 w-[248px] rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md"
                >
                  <div class="flex items-baseline justify-between">
                    <span class="text-[11px] font-medium">Context</span>
                    <span class="font-mono text-[11px]" :class="contextUsageClass">{{ contextPct }}%</span>
                  </div>
                  <div class="mt-1 font-mono text-[10px] text-muted-foreground">
                    {{ contextTokens.toLocaleString() }} / {{ contextMax.toLocaleString() }} tokens
                  </div>
                  <div class="mt-2 h-1 overflow-hidden rounded-full bg-hover">
                    <div class="ctx-card-bar h-full rounded-full" :class="contextUsageClass" :style="{ width: (contextUsageRatio * 100) + '%' }" />
                  </div>
                  <!-- The CLI's own breakdown once asked for, ours until then -->
                  <dl v-if="contextReport" class="mt-2.5 space-y-1">
                    <div v-for="row in contextReport" :key="row.label" class="flex items-baseline justify-between gap-2">
                      <dt class="truncate text-[10px] text-muted-foreground">{{ row.label }}</dt>
                      <dd class="whitespace-nowrap font-mono text-[10px]">{{ row.tokens }} <span class="opacity-50">{{ row.pct }}</span></dd>
                    </div>
                  </dl>
                  <dl v-else-if="contextRows.length" class="mt-2.5 space-y-1">
                    <div v-for="row in contextRows" :key="row.label" class="flex items-baseline justify-between gap-2">
                      <dt class="text-[10px] text-muted-foreground" :title="row.hint">{{ row.label }}</dt>
                      <dd class="font-mono text-[10px]">{{ row.value.toLocaleString() }}</dd>
                    </div>
                  </dl>
                  <div class="mt-3 flex gap-1.5">
                    <button class="ctx-card-btn" :disabled="!canCompact || busy || contextReportPending" @click="loadContextReport">
                      {{ contextReportPending ? "Loading…" : contextReport ? "Refresh" : "Breakdown" }}
                    </button>
                    <button class="ctx-card-btn" :disabled="!canCompact || busy" @click="compactContext">Compact</button>
                  </div>
                  <p v-if="!contextReport" class="mt-1.5 text-[9px] leading-tight text-muted-foreground">
                    Breakdown asks the CLI's /context — local, no tokens.
                  </p>
                </HoverCardContent>
              </HoverCardPortal>
            </HoverCardRoot>
            <span v-if="sessionCost > 0 && !busy" class="px-1 font-mono text-[10px] text-muted-foreground">${{ sessionCost.toFixed(4) }}</span>
            <button
              v-if="busy"
              class="composer-send composer-send-abort"
              :class="{ 'composer-send-stalled': stalled }"
              :title="stalled ? 'No response for a while — looks stuck. Click to restart (Esc)' : 'Abort (Esc)'"
              @click="abortTurn"
            >
              <PhStop :size="14" weight="bold" />
            </button>
            <button v-else class="composer-send" :disabled="!inputText.trim()" @click="sendMessage()">
              <PhArrowUp :size="14" weight="bold" />
            </button>
          </div>
        </div>
      </div>
      </div>
      <!-- Where this chat works, as one quiet line (Ghostex-style) — it was a
           read-only target bar, which looked like a control and wasn't. -->
      <div class="mt-1.5 flex items-center justify-center gap-1.5 text-[11px] text-muted-foreground">
        <PhGitBranch v-if="chatWorkspace?.parent_id" :size="11" />
        <span class="truncate">{{ chatRepoName }} · {{ chatBranch }}</span>
      </div>
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

    <HtmlPageModal v-if="pageView" v-bind="pageView" @close="pageView = null" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, nextTick, onMounted, onBeforeUnmount, onUpdated, watch, inject } from "vue";
import { PhArrowDown, PhArrowUp, PhWrench, PhStop, PhShieldWarning, PhShieldCheck, PhPencilSimple, PhGitDiff, PhGitBranch, PhListChecks, PhTextAa, PhCaretDown, PhCaretRight, PhX, PhUserGear, PhSparkle, PhFastForward, PhFileText, PhTerminalWindow, PhMagnifyingGlass, PhGlobe, PhRobot, PhWarningCircle, PhCopy, PhCheck, PhImage, PhPlay, PhDotsSixVertical, PhArrowSquareOut, PhUsersThree, PhArrowCounterClockwise, PhGitFork, PhMoon } from "@phosphor-icons/vue";
import { invoke } from "@tauri-apps/api/core";
import { useFileDrop, appendRef } from "@/lib/useFileDrop";
import { listen } from "@tauri-apps/api/event";
import type { Phase } from "@/runtime/displayStatus";
import { chatAsleep, useClaudeChatsStore } from "@/stores/claudeChats";
import { childrenOf } from "@/stores/chatTree";
import { useChatAttentionStore } from "@/stores/chatAttention";
import { useEditorContextStore } from "@/stores/editorContext";
import { useImageLightbox } from "@/composables/useImageLightbox";
import { useGitStore } from "@/stores/git";
import { useWorkspaceStore } from "@/stores/workspace";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useProvidersStore, chatTransportFor, type ChatTransport } from "@/stores/providers";
import { agentIconComp } from "@/lib/agentIcons";
import { HoverCardRoot, HoverCardTrigger, HoverCardPortal, HoverCardContent } from "reka-ui";
import ModelPicker, { type PickerSetting } from "@/components/ModelPicker.vue";
import ComposerTextInput from "@/components/ComposerTextInput.vue";
import { stripPasteMarkers, hasPasteMarkers, splitPasteSegments } from "@/lib/composerDom";
import PasteChip from "@/components/composer/PasteChip.vue";
import ComposerSuggestions from "@/components/composer/ComposerSuggestions.vue";
import ComposerImages from "@/components/composer/ComposerImages.vue";
import ComposerPill, { type ComposerPillItem } from "@/components/composer/ComposerPill.vue";
import { useComposerCompletion } from "@/lib/composerCompletion";
import CodexUserInputPanel from "@/components/CodexUserInputPanel.vue";
import { chatSession, replayChatStream, TRANSCRIPT_PAGE, type ChatMessagesPage } from "@/lib/chatSession";
import { setLaunchSettings, type LaunchSettingsPatch } from "@/lib/chatRuntime";
import type { ChatMessage, QueuedChatMessage } from "@/lib/chatTypes";
import { modelsFor, learnModels, modelLabel, type ModelEntry } from "@/lib/chatModels";
import { chatSettingKey } from "@/lib/chatSettings";
import { splitMentions } from "@/lib/mentionTokens";
import { editOf, fmtDuration, hunkLines, isProjectPath, mergeEdits, type FileEdit } from "@/lib/chatTurns";
import { useUIStore } from "@/stores/ui";
import { marked } from "marked";
import DOMPurify from "dompurify";
import { escapeHtml, formatAnswers, renderChatMd } from "@/lib/chatBlocks";
import { cachedMermaid, renderMermaid } from "@/lib/mermaidRender";
import { useDiagram } from "@/composables/useDiagram";
import HtmlPageModal from "./HtmlPageModal.vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "@/lib/config";
import {
  getAcpCapabilities, getAcpChatSetting, getLastAcpSetting,
  setAcpCapabilities, setAcpChatSetting, type AcpChatSettings,
} from "@/lib/acpSettings";
import { smartTitle, isDefaultTitle } from "@/lib/chatTitle";
import TranscriptComments from "./review/TranscriptComments.vue";
import TranscriptFeedbackBar from "./review/TranscriptFeedbackBar.vue";
import { useTranscriptFeedback } from "@/composables/useTranscriptFeedback";

function renderMd(text: string): string {
  return DOMPurify.sanitize(marked.parse(text) as string);
}

// An assistant reply may carry ```visual / ```questions blocks (chatBlocks.ts).
// A questions form counts as answered once any user message follows it.
const answeredMsgIds = computed(() => {
  const ids = new Set<number>();
  let seenUser = false;
  for (let i = messages.value.length - 1; i >= 0; i--) {
    const m = messages.value[i];
    if (m.role === "user") seenUser = true;
    else if (seenUser && m.role === "assistant") ids.add(m.id);
  }
  return ids;
});
function renderAssistantMd(msg: ChatMessage): string {
  return DOMPurify.sanitize(renderChatMd(msg.text, answeredMsgIds.value.has(msg.id)));
}

// The questions form is v-html, so its clicks are delegated: selection state
// lives in the DOM (aria-pressed) until Send reads it back.
function onRichBlockClick(event: MouseEvent) {
  const target = event.target as HTMLElement;
  const cm = target.closest<HTMLElement>("[data-cm-action]");
  if (cm) {
    const src = decodeURIComponent(cm.closest<HTMLElement>(".cm-mermaid")?.dataset.mermaidSrc ?? "");
    if (cm.dataset.cmAction === "open") useDiagram().showDiagram(src);
    else void navigator.clipboard.writeText(src);
    return;
  }
  const page = target.closest<HTMLElement>("[data-cv-page]");
  if (page) { void openPage(page.dataset.cvPage ?? "", page.dataset.cvPageTitle ?? ""); return; }
  const opt = target.closest<HTMLButtonElement>(".cq-opt");
  if (opt && !opt.disabled) {
    const q = opt.closest<HTMLElement>(".cq-q");
    const on = opt.getAttribute("aria-pressed") !== "true";
    if (q?.dataset.cqMulti !== "1") q?.querySelectorAll(".cq-opt").forEach((o) => o.setAttribute("aria-pressed", "false"));
    opt.setAttribute("aria-pressed", String(on));
    return;
  }
  const action = target.closest<HTMLElement>("[data-cq-action]");
  const form = action?.closest<HTMLElement>("[data-chat-questions]");
  if (!action || !form) return;
  const skip = action.dataset.cqAction === "skip";
  const answers = [...form.querySelectorAll<HTMLElement>(".cq-q")].map((q) => ({
    question: q.dataset.cqQuestion ?? "",
    picked: skip ? [] : [...q.querySelectorAll<HTMLElement>('.cq-opt[aria-pressed="true"]')].map((o) => o.dataset.cqOpt ?? ""),
    other: skip ? "" : q.querySelector<HTMLInputElement>(".cq-other")?.value ?? "",
  }));
  const notes = skip ? "" : form.querySelector<HTMLTextAreaElement>(".cq-notes")?.value ?? "";
  form.querySelectorAll<HTMLButtonElement | HTMLInputElement | HTMLTextAreaElement>("button, input, textarea").forEach((el) => (el.disabled = true));
  void sendMessage(formatAnswers(answers, notes));
}

// ```mermaid blocks arrive as placeholders (a plain code block carrying the
// source); swap in the rendered SVG after every patch. A cached diagram swaps
// synchronously — v-html re-inserts the placeholder on every streamed token,
// and this runs before paint, so it doesn't flicker. Invalid source rejects
// and stays a code block.
function hydrateMermaid() {
  for (const el of scrollEl.value?.querySelectorAll<HTMLElement>(".cm-mermaid:not([data-cm-state])") ?? []) {
    el.dataset.cmState = "pending";
    const src = decodeURIComponent(el.dataset.mermaidSrc ?? "");
    const show = (svg: string) => {
      const box = el.querySelector(".cm-diagram");
      if (!box) return;
      box.innerHTML = svg;
      el.classList.add("cm-ok");
      el.dataset.cmState = "ok";
    };
    const hit = cachedMermaid(src);
    if (hit) show(hit);
    else renderMermaid(src).then(show, () => (el.dataset.cmState = "failed"));
  }
}
onMounted(hydrateMermaid);
onUpdated(hydrateMermaid);
// A theme switch doesn't re-run v-html; re-render the diagrams already shown.
// (useUIStore() here: the `uiStore` const is declared further down.)
watch(() => [useUIStore().theme, useUIStore().themeMode], () => nextTick(() => {
  scrollEl.value?.querySelectorAll<HTMLElement>(".cm-mermaid[data-cm-state='ok']").forEach((el) => delete el.dataset.cmState);
  hydrateMermaid();
}));

// Page cards (```visual {"page": …}): the agent's HTML file opens in a
// sandboxed iframe modal, never inline in the transcript.
const pageView = ref<{ title: string; path: string; html: string } | null>(null);
async function openPage(path: string, title: string) {
  const abs = path.startsWith("/") ? path : `${props.cwd}/${path}`;
  let html: string;
  try {
    html = await invoke<string>("read_text_file_checked", { path: abs });
  } catch (e) {
    html = `<p style="font:13px system-ui;color:#b91c1c;padding:16px">Could not read ${escapeHtml(abs)}: ${escapeHtml(e)}</p>`;
  }
  pageView.value = { title: title || abs, path: abs, html };
}

// A sent message that still carries a `wrapPaste` block (see composerDom.ts)
// renders that block as the same collapsed "Pasted text (N lines)" chip the
// composer showed while it was still being typed, instead of dumping the raw
// paste inline — which is what used to overflow the bubble on a large paste.
// `displayText` is the only field that can hold markers; `text` (what actually
// reached the agent) never does, by the time it's pushed.
function userMsgSegments(msg: ChatMessage) {
  return splitPasteSegments(msg.displayText ?? msg.text);
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
  // Use a dedicated localStorage key for the model selection instead of the
  // shared global one, so this chat's model is independent of every other
  // chat. Falls back to the global key.
  modelKey?: string;
  // Initial model when nothing is stored under modelKey yet.
  defaultModel?: string;
  // Optional runtime override, otherwise the selected agent defines it.
  transport?: ChatTransport;
  // Which agent to run — a chatAgents store id (default 'claude').
  agentKind?: string;
  // Whether this chat's tab is actually the one on screen (its workspace active,
  // terminal mode, this tab selected) — passed by Terminal.vue via its isWatching()
  // helper. Callers that don't track tab-level visibility (float chat) omit it
  // and fall back to plain window focus.
  isWatching?: boolean;
  // First message to send automatically once this chat's runtime is up (used by
  // the welcome-screen composer, which creates the chat and its prompt at once).
  initialPrompt?: string;
  // Images paired with the first prompt from the welcome-screen composer.
  initialImages?: string[];
}>();
const transcriptFeedback = useTranscriptFeedback(() => props.chatId);
const sendingFeedback = ref(false);
const feedbackSendError = ref("");

function jumpToFeedback(deliveryId: string) {
  const delivery = transcriptFeedback.deliveries.value[deliveryId];
  const message = delivery && messages.value.find((msg) => (msg.role === "user" || msg.role === "queued") && msg.text === delivery.text);
  const target = message && scrollEl.value?.querySelector<HTMLElement>(`[data-message-id="${message.id}"]`);
  if (target) target.scrollIntoView({ behavior: "smooth", block: "center" });
  else feedbackSendError.value = "This follow-up is no longer in the visible transcript.";
}

async function sendTranscriptNotes() {
  if (sendingFeedback.value || busy.value || !transcriptFeedback.pending.value.length) return;
  sendingFeedback.value = true;
  feedbackSendError.value = "";
  try {
    const result = await sendMessage();
    if (!result) feedbackSendError.value = "Comments were not sent. They are still pending; check the agent connection and retry.";
  } catch (error) {
    feedbackSendError.value = `Comments were not sent: ${error}`;
  } finally {
    sendingFeedback.value = false;
  }
}


const emit = defineEmits<{ (e: "prompt-sent"): void }>();

const chats = useClaudeChatsStore();
const asleep = computed(() => chatAsleep(chats.sessions.find((s) => s.id === props.chatId)));
const chatAttention = useChatAttentionStore();
// Whether this instance holds one of chatAttention's watch counts. Tracked so
// mount/unmount and isWatching flips stay balanced.
let attentionHeld = false;
function holdAttention(value: boolean) {
  if (value === attentionHeld) return;
  attentionHeld = value;
  chatAttention.setWatching(props.chatId, value);
}
const workspaces = useWorkspaceStore();
const git = useGitStore();
const uiStore = useUIStore();
const chatAgents = useProvidersStore();
const editorCtx = useEditorContextStore();
const { showImage } = useImageLightbox();
const chatWorkspace = computed(() => workspaces.workspaces.find((workspace) => workspace.id === props.workspaceId));
const chatRepoName = computed(() => {
  const ws = chatWorkspace.value;
  const repo = ws?.parent_id ? workspaces.workspaces.find((w) => w.id === ws.parent_id) : ws;
  return repo?.name ?? "";
});
const chatBranch = computed(() => chatWorkspace.value?.worktree_branch || git.branchByWs[props.workspaceId] || "HEAD");

// Branch-changed banner: the branch a chat was created on (session.branch,
// a one-time snapshot) vs. the workspace's current branch.
const activeSession = computed(() => chats.sessions.find((s) => s.id === props.chatId));
const branchChanged = computed(() => {
  const created = activeSession.value?.branch;
  return !!created && created !== chatBranch.value;
});
const branchBannerDismissed = ref(false);
const restoringBranch = ref(false);
const restoreBranchError = ref("");
// Purely informational per product decision — dismiss on close or on send,
// and reappear if the branch drifts again (e.g. after a restore, then a
// further switch away).
watch(() => [props.chatId, chatBranch.value], () => {
  branchBannerDismissed.value = false;
  restoreBranchError.value = "";
});
async function restoreBranch() {
  const created = activeSession.value?.branch;
  const ws = chatWorkspace.value;
  if (!created || !ws || restoringBranch.value) return;
  restoringBranch.value = true;
  restoreBranchError.value = "";
  try {
    await git.checkoutBranchForWorkspace(ws.id, ws.path, created);
  } catch (e) {
    restoreBranchError.value = e instanceof Error ? e.message : "checkout failed";
  } finally {
    restoringBranch.value = false;
  }
}

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
  messages, hiddenBefore, loadingEarlier, busy, lastActivityAt, turnStartedAt, messageQueue,
  turnStats, sessionCost,
  pendingPermission, pendingQuestion, pendingPlan, pendingDiff,
  acpPermReq, acpModes, acpConfigOptions,
  permissionResponsePending, codexUserInput, codexUserInputPending,
  nativeControlResponsePending,
  contextTokens, contextWindow, contextSplit, contextReport, contextReportPending,
  claudeGeneratedTitle,
  enqueueMessage, removeQueuedMessage, clearQueuedMessages, moveQueuedMessage,
} = S;
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
const acpModelOption = computed(() => acpConfigOptions.value.find((o) => o.id === "model"));
const acpEffortOption = computed(() => acpConfigOptions.value.find((o) => o.id === "effort"));
const acpModeLabel = computed(() => acpModes.value?.availableModes.find((m) => m.id === acpModes.value?.currentModeId)?.name ?? "Mode");
const acpActiveModelId = computed(() => acpModelOption.value?.currentValue ?? "");
const acpEffortLabel = computed(() => { const o = acpEffortOption.value; return o?.options.find((c) => c.value === o.currentValue)?.name ?? "Effort"; });

// The adapter reports its own selector sets, so the pill items are derived
// rather than declared. Same shape as Claude's below — one pill component.
const acpEffortItems = computed<ComposerPillItem[]>(() =>
  (acpEffortOption.value?.options ?? []).map((c) => ({ id: c.value, label: c.name, title: c.description })),
);
const acpModeItems = computed<ComposerPillItem[]>(() =>
  (acpModes.value?.availableModes ?? []).map((m) => ({
    id: m.id, label: m.name, description: m.description, icon: acpModeIcon(m.id),
  })),
);

// The runtime only publishes selector metadata during its handshake. Persist
// the latest useful snapshot so an idle session eviction or view hand-off does
// not make the effort and permission pills disappear until the next start.
watch([acpModes, acpConfigOptions], ([modes, configOptions]) => {
  if (!isAcpRuntime.value || (!modes && configOptions.length === 0)) return;
  setAcpCapabilities(props.chatId, { agentId: agentKind.value, modes, configOptions });
}, { deep: true });
// trackAcpControl(rid, restore) (the session's): request ids of OUR OWN restore
// pushes. The reply to a restore carries the adapter's selector set again, so
// re-restoring from it is what would ping-pong forever — skipping just those
// replies breaks the loop, while every OTHER reset (session start, or the
// reply to a user's mode/effort/model pick) still gets repaired.

// A pick is also recorded on the row (row_only — this push applies it live):
// the row is what the chat runtime re-applies after its next start.
async function acpSelectMode(modeId: string, userPick = true) {
  S.showAcpSelection("mode", modeId);
  setAcpChatSetting(props.chatId, agentKind.value, "mode", modeId);
  if (userPick) void persistLaunch({ permission_mode: modeId, row_only: true });
  try {
    const rid = await invoke<number>("acp_set_mode", { id: props.chatId, modeId });
    S.trackAcpControl(rid, !userPick);
  } catch (e) {
    S.notice(`Failed to set mode: ${e}`);
  }
}
async function acpSelectModel(value: string, userPick = true) {
  S.showAcpSelection("model", value);
  setAcpChatSetting(props.chatId, agentKind.value, "model", value);
  if (userPick) void persistLaunch({ model: value, row_only: true });
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "model", value });
    S.trackAcpControl(rid, !userPick);
  } catch (e) {
    S.notice(`Failed to set model: ${e}`);
  }
}
async function acpSelectEffort(value: string, userPick = true) {
  S.showAcpSelection("effort", value);
  setAcpChatSetting(props.chatId, agentKind.value, "effort", value);
  if (userPick) void persistLaunch({ effort: value, row_only: true });
  try {
    const rid = await invoke<number>("acp_set_config", { id: props.chatId, configId: "effort", value });
    S.trackAcpControl(rid, !userPick);
  } catch (e) {
    S.notice(`Failed to set effort: ${e}`);
  }
}

// Re-apply this chat's model / permission mode / effort. The adapter resets its
// selectors to defaults on (re)start AND in the reply to a model switch, which
// is what used to silently drop the effort and permission mode the user picked.
function restoreAcpSelections() {
  const savedModel = getAcpChatSetting(props.chatId, "model") ?? getLastAcpSetting(agentKind.value, "model");
  const modelOffered = acpModelOption.value?.options.some((o) => o.value === savedModel);
  if (savedModel && modelOffered && acpModelOption.value && acpModelOption.value.currentValue !== savedModel) {
    acpSelectModel(savedModel, false);
  }
  const savedMode = getAcpChatSetting(props.chatId, "mode") ?? getLastAcpSetting(agentKind.value, "mode");
  const modeOffered = acpModes.value?.availableModes.some((m) => m.id === savedMode);
  if (savedMode && modeOffered && acpModes.value && acpModes.value.currentModeId !== savedMode) {
    acpSelectMode(savedMode, false);
  }
  // Only push a value the adapter actually offers — a saved effort can be stale
  // after a model switch (Codex publishes its efforts per model).
  const savedEffort = getAcpChatSetting(props.chatId, "effort") ?? getLastAcpSetting(agentKind.value, "effort");
  const effortOffered = acpEffortOption.value?.options.some((o) => o.value === savedEffort);
  if (savedEffort && effortOffered && acpEffortOption.value && acpEffortOption.value.currentValue !== savedEffort) {
    acpSelectEffort(savedEffort, false);
  }
}

// Agent switcher dropdown.
async function selectAgent(id: string) {
  if (id === agentKind.value) return;
  // chat_clear (below) stops whichever process the chat has, by id.
  agentKind.value = id;
  chats.sync(props.chatId, { agentKind: id, transport: currentAgent.value ? chatTransportFor(currentAgent.value) : 'claude-cli' });
  await clearChat();
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
  nextTick(() => inputEl.value?.focus());
}

// Launch settings live on the chat row (docs/plans/006-chat-runtime.md): the
// Go chat runtime starts the process from it. The legacy per-chat config.json
// keys are still read as a fallback (the PR1 backfill copied them over).
function chatRow() {
  return chats.sessions.find((s) => s.id === props.chatId);
}
async function persistLaunch(patch: LaunchSettingsPatch) {
  await setLaunchSettings(props.chatId, patch).catch((e: unknown) => {
    S.notice(`Couldn't save the chat settings: ${e}`);
  });
}
function seedLaunchRow() {
  const row = chatRow();
  if (!row) return;
  const patch: LaunchSettingsPatch = { row_only: true };
  if (!row.model) patch.model = selectedModel.value;
  if (!row.effort) patch.effort = selectedEffort.value;
  if (!row.permissionMode) patch.permission_mode = permMode.value;
  if (!row.profileId && selectedProfileId.value) patch.profile_id = selectedProfileId.value;
  if (Object.keys(patch).length > 1) void persistLaunch(patch);
}

// Profile switcher — the Claude provider instances (each one is a config dir).
const claudeProfiles = computed(() => chatAgents.active.filter((a) => a.providerId === "claude"));
const defaultProfileId = computed(() => claudeProfiles.value[0]?.id ?? "claude");
// Legacy per-chat localStorage key prefix (kept only for the one-time migration below).
function loadProfileId(id: number): string {
  const row = chatRow()?.profileId;
  if (row) return row;
  const rec = getConfig<Record<string, string>>("chatProfileSelection", {});
  return rec[String(id)] ?? defaultProfileId.value;
}
const selectedProfileId = ref<string>(loadProfileId(props.chatId));
const selectedProfile = computed(() => chatAgents.byId(selectedProfileId.value) ?? claudeProfiles.value[0]);
const profileItems = computed<ComposerPillItem[]>(() =>
  claudeProfiles.value.map((p) => ({ id: p.id, label: p.name, hint: p.configDir || undefined })),
);
async function selectProfile(id: string) {
  if (id === selectedProfileId.value) return;
  selectedProfileId.value = id;
  // Go restarts a live process with the new profile; a cold one starts with it.
  await persistLaunch({ profile_id: id });
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
// Called, not captured: custom model ids land in config.json after this module
// is evaluated, and a chat pinned to one must still validate.
const claudeModels = () => modelsFor("claude");
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
  const v = chatRow()?.model || getConfig<Record<string, string>>(MODEL_BY_CHAT_KEY, {})[chatSettingId.value];
  return claudeModels().some((m) => m.id === v) ? (v as ClaudeModelId) : null;
}
/** The model this chat starts with: its own pick, else the caller's explicit
 *  default (e.g. what the composer's ModelPicker chose for this new thread),
 *  else last-used-anywhere, else the catalog head. The caller's default has
 *  to outrank last-used-anywhere, or picking a model for THIS chat can be
 *  silently overridden by whatever some OTHER chat's picker set more
 *  recently in the shared "seed for the next new chat" config key. */
function loadModel(): ClaudeModelId {
  const own = storedChatModel();
  if (own) return own;
  if (props.defaultModel && claudeModels().some((m) => m.id === props.defaultModel)) {
    return props.defaultModel as ClaudeModelId;
  }
  const v = getConfig<string | null>(MODEL_CONFIG_KEY, null);
  if (claudeModels().some((m) => m.id === v)) return v as ClaudeModelId;
  return claudeModels()[0].id;
}
const selectedModel = ref<ClaudeModelId>(loadModel());
async function selectModel(id: ClaudeModelId) {
  if (id === selectedModel.value) return;
  selectedModel.value = id;
  setConfig(MODEL_CONFIG_KEY, id); // seed for the NEXT new chat only
  // Go applies it in place (set_model) when Claude takes it, else restarts.
  await persistLaunch({ model: id });
}

// Mirror the live model into the session so the Sidebar can badge the thread
// with what's actually running it.
watch(
  () => (effectiveTransport.value === "claude-cli" ? selectedModel.value : acpActiveModelId.value),
  (m) => { if (m) chats.setModel(props.chatId, m); },
  { immediate: true },
);

const activeModelLabel = computed(() => {
  const id = effectiveTransport.value === "claude-cli" ? selectedModel.value : acpActiveModelId.value;
  if (!id) return "";
  return liveModels.value.find((m) => m.id === id)?.label ?? modelLabel(agentKind.value, id);
});

const turnCount = computed(() => messages.value.filter((m) => m.role === "user").length);

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
  const v = chatRow()?.effort || getConfig<Record<string, string>>(EFFORT_BY_CHAT_KEY, {})[chatSettingId.value];
  return isEffort(v) ? v : null;
}
function loadEffort(): ClaudeEffort {
  const own = storedChatEffort();
  if (own) return own;
  const saved = getConfig<string | null>(EFFORT_CONFIG_KEY, null);
  return isEffort(saved) ? saved : "high";
}
const selectedEffort = ref<ClaudeEffort>(loadEffort());
const selectedEffortLabel = computed(() => CLAUDE_EFFORTS.find((option) => option.id === selectedEffort.value)?.label ?? "High effort");
async function selectEffort(effort: ClaudeEffort) {
  if (effort === selectedEffort.value) return;
  selectedEffort.value = effort;
  setConfig(EFFORT_CONFIG_KEY, effort); // seed for the NEXT new chat only
  await persistLaunch({ effort });
}


// Per-tool icon + one-line human summary for the compact tool-call row.
const TOOL_ICONS: Record<string, unknown> = {
  Read: PhFileText, Edit: PhPencilSimple, Write: PhPencilSimple, MultiEdit: PhPencilSimple,
  Bash: PhTerminalWindow, Grep: PhMagnifyingGlass, Glob: PhMagnifyingGlass,
  TodoWrite: PhListChecks, WebFetch: PhGlobe, WebSearch: PhGlobe, Task: PhRobot, Agent: PhRobot,
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

const openRightPanelGitTab = inject<() => void>("openRightPanelGitTab", () => {});
function openChangedFileDiff(path: string) {
  git.showDiff(path, false);
  openRightPanelGitTab();
}

// The panel is where a sub-agent lives; a transcript row recording a spawn is
// just a pointer to it.
const openRightPanelSubagent = inject<(chatId: number, workspaceId: number) => void>("openRightPanelSubagent", () => {});
function openSubagentFromTranscript(chatId: number) {
  openRightPanelSubagent(chatId, props.workspaceId);
}

// Live dot on a transcript's spawn row, straight off the bus — same event and
// field RightPanel's Sub-agents list reads. Listeners are set up lazily, one
// per distinct subagentChatId this transcript has ever shown (a spawn row
// never disappears, so there is nothing to unsubscribe until unmount).
// This thread's live children (the Sub-agents card above the composer).
const subagentChildren = computed(() => (props.chatId ? childrenOf(chats.sessions, props.chatId) : []));
const subagentPhase = reactive<Record<number, string>>({});
const subagentDetail = reactive<Record<number, string>>({});
const subagentPhaseUnsubs: Array<() => void> = [];
// `listen()` is async; a component unmounted before it resolves would have
// already run onBeforeUnmount's forEach below by the time the `.then()`
// finally pushes an unsub function in — nothing left to call it, so that
// listener (and the closure it holds) leaks for the app's remaining life.
// Same hazard RightPanel.vue's own phase-chat: subscription guards against,
// just a different shape: there, a child can leave the list while listen()
// is in flight; here, it's this component itself going away.
let subagentPhaseUnmounted = false;
watch(
  () => [
    ...messages.value.filter((m) => m.role === "system-info" && m.subagentChatId).map((m) => m.subagentChatId as number),
    ...subagentChildren.value.map((c) => c.id),
  ],
  (ids) => {
    for (const id of ids) {
      if (id in subagentPhase) continue;
      subagentPhase[id] = "idle";
      listen<Phase>(`phase-chat:${id}`, (ev) => {
        subagentPhase[id] = ev.payload?.state ?? "idle";
        subagentDetail[id] = ev.payload?.detail ?? "";
      }).then((un) => {
        if (subagentPhaseUnmounted) { un(); return; }
        subagentPhaseUnsubs.push(un);
      });
    }
  },
  { immediate: true, deep: true },
);
watch(() => props.isWatching, (value) => holdAttention(value ?? true));
useFileDrop(() => inputEl.value?.element?.closest(".claude-chat"), {
  image: (uri) => pendingImages.value.push(uri),
  ref: (r) => { inputText.value = appendRef(inputText.value, r); nextTick(() => inputEl.value?.focus()); },
});
onBeforeUnmount(() => {
  holdAttention(false);
  subagentPhaseUnmounted = true;
  subagentPhaseUnsubs.forEach((un) => un());
});

// Sub-agents card above the composer: this thread's live children, one row
// each, with the state that matters to the parent (Ghostex "Threads" card).
const subagentsCollapsed = ref(false);
function subagentState(chatId: number): { label: string; tone: string } {
  const state = subagentPhase[chatId];
  if (state === "waiting_input" || state === "waiting_approval") return { label: "Needs you", tone: "needs" };
  if (state === "running") return { label: "Running", tone: "running" };
  if (state === "failed") return { label: "Failed", tone: "failed" };
  if (state === "done") return { label: "Done", tone: "done" };
  return { label: "Idle", tone: "idle" };
}

// The robot icon already says "sub-agent"; the stored prefix only eats width.
function subagentLabel(text: string): string {
  return text.replace(/^Spawned sub-agent:\s*/, "");
}

function subagentDotClass(chatId: number): string {
  const state = subagentPhase[chatId];
  if (state === "running") return "status-running";
  if (state === "waiting_input" || state === "waiting_approval") return "status-waiting";
  if (state === "done") return "status-done";
  if (state === "failed") return "status-error";
  return ""; // idle/stale: plain dot, nothing to flag
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
    case "Agent":
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
  // Queued follow-ups render pinned below the live turn (like Claude desktop),
  // not at their array position — streamed output lands after them.
  const msgs = messages.value.filter((m) => m.role !== "queued");
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
// Inline diff card. The first file starts open; a click flips it.
const diffToggles = reactive<Record<string, boolean>>({});
function diffOpen(msgId: string, path: string, index: number): boolean {
  return diffToggles[`${msgId}:${path}`] ?? index === 0;
}
function toggleDiff(msgId: string, path: string, index: number) {
  diffToggles[`${msgId}:${path}`] = !diffOpen(msgId, path, index);
}
function dirOf(path: string): string {
  const rel = props.cwd && path.startsWith(props.cwd + "/") ? path.slice(props.cwd.length + 1) : path;
  return rel.includes("/") ? rel.slice(0, rel.lastIndexOf("/") + 1) : "";
}
function diffDirs(files: FileEdit[]): string {
  const dirs = [...new Set(files.map((f) => dirOf(f.path).replace(/\/$/, "")).filter(Boolean))];
  return dirs.slice(0, 3).join(" · ") + (dirs.length > 3 ? ` · +${dirs.length - 3}` : "");
}
function diffRatio(added: number, removed: number): number {
  return added + removed ? Math.round((added / (added + removed)) * 100) : 0;
}
/** GitHub-style five blocks: green/red in proportion, grey for the rest of a small change. */
function diffBlocks(added: number, removed: number): string[] {
  const total = added + removed;
  const filled = Math.min(5, Math.max(1, Math.ceil(total / 10)));
  const green = total ? Math.round((added / total) * filled) : 0;
  return Array.from({ length: 5 }, (_, i) => (i < green ? "b-add" : i < filled ? "b-del" : "b-off"));
}

// Turn fold + per-turn changed-files summary. A turn starts at a user message
// and ends at the next one (or at the end of an idle chat). Once it's settled,
// its thinking/tool rows collapse behind one "Worked for Xs" row and the files
// it touched are summed into a diffstat card — the transcript stays readable
// when scrolling back over a long session.
interface TurnFoldRow { role: "turn-fold"; id: string; turnKey: number; label: string; folded: boolean; toolCount: number; fileCount: number; partial?: false }
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
    const label = user.turnMs ? `Worked ${fmtDuration(user.turnMs)}` : "Worked";
    const toolCount = turnRows.filter((r) => r.role === "tool").length;
    const projectEdit = (r: ChatMessage | ToolGroupHeader) => {
      if (r.role !== "tool") return null;
      const e = editOf((r as ChatMessage).toolInput);
      return e && isProjectPath(e.path, props.cwd) ? e : null;
    };
    const fileCount = new Set(turnRows.map((r) => projectEdit(r)?.path).filter(Boolean)).size;

    const edits: FileEdit[] = [];
    let foldEmitted = false;
    for (const r of turnRows) {
      const e = projectEdit(r);
      if (e) edits.push(e);
      if (settled && WORK_ROLES.has(r.role)) {
        if (!foldEmitted) {
          out.push({ role: "turn-fold", id: `fold-${user.id}`, turnKey: user.id, label, folded, toolCount, fileCount });
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
async function loadMessages(chatId: number): Promise<{ messages: ChatMessage[]; hiddenBefore: number }> {
  // The newest window only — the rest is one "Load earlier" away. An older
  // backend without the paged call falls back to reading everything.
  try {
    const page = await invoke<ChatMessagesPage>("load_chat_messages_page", { chatId, before: -1, limit: TRANSCRIPT_PAGE });
    if (Array.isArray(page?.messages)) return { messages: page.messages, hiddenBefore: page.start };
  } catch { /* fall through */ }
  try {
    const raw = await invoke<string>("load_chat_messages", { chatId });
    const parsed = JSON.parse(raw);
    return { messages: Array.isArray(parsed) ? (parsed as ChatMessage[]) : [], hiddenBefore: 0 };
  } catch {
    return { messages: [], hiddenBefore: 0 };
  }
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
// The session stamps the turn (turnStartedAt, turnMs); the view only ticks.
watch(busy, (val) => { if (val) nowTick.value = Date.now(); }, { flush: "sync" });
const copiedMessageId = ref<number | null>(null);
let copyFeedbackTimer: ReturnType<typeof setTimeout> | null = null;
// Set before an INTENTIONAL claude restart (mode switch / abort) so the `exit`
// event that teardown emits doesn't fire a spurious "Claude finished" toast.
const pendingImages = ref<string[]>([]); // data URIs
const scrollEl = ref<HTMLElement | null>(null);
const inputEl = ref<InstanceType<typeof ComposerTextInput> | null>(null);

// @file / $skill / /command completion and the skill pills, shared with the
// welcome composer (lib/composerCompletion.ts) so the two cannot drift.
const completion = useComposerCompletion({
  text: inputText,
  input: () => inputEl.value,
  cwd: () => props.cwd,
  commands: allCommands,
});
const { suggestions, activeIndex } = completion;

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
  const v = chatRow()?.permissionMode || cfg.byChat[String(id)];
  if (isPermMode(v)) return v;
  // Migrate the old boolean "dangerous mode" flag → bypassPermissions.
  if (cfg.dangerousByChat[String(id)]) return "bypassPermissions";
  // New chat: inherit the last-used mode so the user doesn't have to re-pick every time.
  if (isPermMode(cfg.last)) return cfg.last;
  return "default";
}
/** `last` is this device's default for the next new chat; the chat's own mode
 *  is its row's for claude-cli (persistLaunch), still config.json for the RPC
 *  runtimes until they move into the Go chat runtime. */
function savePermMode(id: number, mode: PermMode) {
  const cfg = { ...getConfig<ChatPermissionModeConfig>("chatPermissionMode", { byChat: {}, dangerousByChat: {} }) };
  if (usesRpcRuntime.value) cfg.byChat = { ...cfg.byChat, [String(id)]: mode };
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
const permItems = computed<ComposerPillItem[]>(() => PERM_MODES.map((m) => ({
  id: m, ...PERM_META[m], icon: PERM_ICON[m],
})));

// The model picker's quick-settings row: the same state and handlers as the
// composer pills, only what this transport supports.
const pickerSettings = computed<PickerSetting[]>(() => {
  if (effectiveTransport.value === "claude-cli") {
    return [
      { key: "effort", label: "Effort", items: CLAUDE_EFFORTS.map((o) => ({ id: o.id, label: o.label.replace(/ effort$/, "") })), active: selectedEffort.value },
      { key: "plan", label: "Plan", toggle: true, items: [{ id: "default", label: "Off" }, { id: "plan", label: "On" }], active: permMode.value },
      { key: "perm", label: "Permissions", items: permItems.value, active: permMode.value },
    ];
  }
  const out: PickerSetting[] = [];
  if (isAcpRuntime.value && acpEffortOption.value) {
    out.push({ key: "effort", label: "Effort", items: acpEffortItems.value, active: acpEffortOption.value.currentValue });
  }
  if (isAcpRuntime.value && acpModes.value) {
    const modes = acpModeItems.value;
    if (modes.some((m) => m.id === "plan")) {
      out.push({ key: "acp-plan", label: "Plan", toggle: true, items: [{ id: modes.find((m) => m.id !== "plan")!.id, label: "Off" }, { id: "plan", label: "On" }], active: acpModes.value.currentModeId });
    }
    out.push({ key: "mode", label: "Permissions", items: modes, active: acpModes.value.currentModeId });
  }
  return out;
});
function onPickerSetting(key: string, value: string) {
  const claude = effectiveTransport.value === "claude-cli";
  if (key === "effort") void (claude ? selectEffort(value as ClaudeEffort) : acpSelectEffort(value));
  else if (key === "perm" || key === "plan") void selectPermMode(value as PermMode);
  else if (key === "mode" || key === "acp-plan") void acpSelectMode(value);
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








// Queued follow-ups
function removeQueued(id: number) {
  removeQueuedMessage(id);
  S.save();
}
const draggingQueuedId = ref<number | null>(null);
const dropQueuedIndex = ref<number | null>(null);
function dropQueued(toIndex: number) {
  if (draggingQueuedId.value != null) moveQueuedMessage(draggingQueuedId.value, toIndex);
  draggingQueuedId.value = null;
  dropQueuedIndex.value = null;
  S.save();
}
function editQueued(q: QueuedChatMessage) {
  removeQueued(q.id);
  inputText.value = inputText.value.trim() ? `${q.text}\n${inputText.value}` : q.text;
  if (q.images?.length) pendingImages.value = [...q.images, ...pendingImages.value];
  nextTick(() => inputEl.value?.focus());
}
function sendQueuedNow(id: number) {
  // Interrupt, don't steer: a follow-up has to remain its own turn for every
  // provider (Codex, generic ACP). The abort clears `busy`, and the session's
  // drain then sends the queue head — which this message just became.
  moveQueuedMessage(id, 0);
  S.save();
  void abortTurn();
}

// The working line's verb rotates every few seconds — a small sign of life
// that reads better than three dots (Ghostex does the same).
const WORKING_VERBS = ["Working", "Thinking", "Reading", "Tinkering", "Pondering", "Wrangling", "Skedaddling", "Noodling", "Untangling", "Cooking"];
const workingVerbIndex = ref(0);
let workingVerbTimer: number | undefined;
watch(busy, (on) => {
  window.clearInterval(workingVerbTimer);
  if (!on) return;
  workingVerbIndex.value = Math.floor(Math.random() * WORKING_VERBS.length);
  workingVerbTimer = window.setInterval(() => { workingVerbIndex.value = (workingVerbIndex.value + 1) % WORKING_VERBS.length; }, 4000);
}, { immediate: true });
onBeforeUnmount(() => window.clearInterval(workingVerbTimer));
const workingVerb = computed(() => WORKING_VERBS[workingVerbIndex.value]);

// Context meter. The window is whatever the CLI says the model had on the last
// turn (a [1m] model is five times a 200k one); 200k until it has said.
const CONTEXT_FALLBACK = 200_000;
// contextTokens / contextWindow / contextSplit are the session's (remembered
// per chat there, so the ring survives a remount).
const contextMax = computed(() => contextWindow.value || CONTEXT_FALLBACK);
const contextUsageRatio = computed(() => Math.min(contextTokens.value / contextMax.value, 1));
const contextUsageClass = computed(() => {
  const r = contextUsageRatio.value;
  if (r >= 0.9) return "ctx-exceeded";
  if (r >= 0.75) return "ctx-warning";
  return "ctx-ok";
});

const contextPct = computed(() => Math.round(contextUsageRatio.value * 100));
const contextFree = computed(() => Math.max(contextMax.value - contextTokens.value, 0));
const contextRows = computed(() => {
  const sp = contextSplit.value;
  if (!sp) return [];
  return [
    { label: "Cached prompt", value: sp.cached, hint: "served from the prompt cache" },
    { label: "New this turn", value: sp.fresh, hint: "sent uncached or written to the cache" },
    { label: "Model output", value: sp.output, hint: "" },
    { label: "Free", value: contextFree.value, hint: "" },
  ].filter((r) => r.value > 0);
});
const ctxCardOpen = ref(false);

// `/context` is the CLI's own accounting of what fills the window, and it costs
// nothing to ask (local command, num_turns 0). It answers with a markdown
// report whose useful part is one small table and whose bulk is a row per MCP
// tool — well over a thousand of them — so it is sent out of band and the
// table is lifted into the card instead of landing in the transcript.
// contextReport / contextReportPending: the session's.

async function loadContextReport() {
  if (canCompact.value) await S.requestContextReport();
}
const CTX_R = 6;
const CTX_C = 2 * Math.PI * CTX_R;
const contextDash = computed(() => `${contextUsageRatio.value * CTX_C} ${CTX_C}`);
// ponytail: /compact is the CLI's own command, so nothing here has to know how
// compaction works — it is only supported by the native Claude transport.
const canCompact = computed(() => effectiveTransport.value === "claude-cli");
// Not a `disabled` button: a disabled element gets no hover events, so the
// tooltip — the whole point of the ring — would vanish exactly when it cannot
// be clicked. It stays hoverable and refuses the click instead.
function compactContext() {
  if (!canCompact.value || busy.value) return;
  ctxCardOpen.value = false;
  void sendMessage("/compact");
}


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
  updateMinimapCurrent();
}

// ── minimap ──
const minimapPrompts = computed(() =>
  messages.value
    .filter((m) => m.role === "user" && m.text?.trim())
    .map((m) => ({ id: m.id, text: m.text.slice(0, 480), width: m.text.length > 400 ? 24 : m.text.length > 150 ? 16 : m.text.length > 50 ? 11 : 8 })),
);
const minimapHover = ref<{ id: number; text: string; index: number } | null>(null);
const minimapCurrent = ref<number | null>(null);
/** The prompt whose message sits nearest the top of the viewport (last one above it). */
function updateMinimapCurrent() {
  const el = scrollEl.value;
  if (!el || minimapPrompts.value.length < 2) return;
  const top = el.getBoundingClientRect().top + 40;
  let current: number | null = minimapPrompts.value[0].id;
  for (const p of minimapPrompts.value) {
    const node = el.querySelector(`[data-message-id="${p.id}"]`);
    if (node && node.getBoundingClientRect().top <= top) current = p.id;
  }
  minimapCurrent.value = current;
}
function jumpToMessage(id: number) {
  scrollEl.value?.querySelector(`[data-message-id="${id}"]`)?.scrollIntoView({ block: "start", behavior: "smooth" });
}
// Older messages land ABOVE the viewport: without compensating, the view would
// jump to the top of the newly inserted block instead of staying on what you read.
async function loadEarlierKeepingScroll() {
  const el = scrollEl.value;
  const prevHeight = el?.scrollHeight ?? 0;
  const prevTop = el?.scrollTop ?? 0;
  await S.loadEarlier();
  await nextTick();
  if (el) el.scrollTop = prevTop + (el.scrollHeight - prevHeight);
}

function scrollToBottom(force = false) {
  if (!force && !atBottom.value) return;
  nextTick(() => {
    if (scrollEl.value) scrollEl.value.scrollTop = scrollEl.value.scrollHeight;
    atBottom.value = true;
  });
}

// Catch me up (after Ghostex): the text-generation model reads the chat's tail
// and answers in a card above the composer. Nothing reaches the agent.
const CATCH_UP_MESSAGES = 30;
const CATCH_UP_LABELS: Record<string, string> = {
  needs_you: "Needs you", blocked: "Blocked", in_progress: "In progress", done: "Done", untested: "Untested",
};
const showCatchUpBtn = computed(() => !inputText.value.trim() && messages.value.length > 2 && !catchUp.open);
const catchUp = reactive({
  open: false, loading: false, error: "", copied: false, summary: "",
  items: [] as { tag: string; text: string }[],
});
async function runCatchUp() {
  const chatAtStart = props.chatId;
  Object.assign(catchUp, { open: true, loading: true, error: "", copied: false });
  const transcript = messages.value
    .filter((m) => (m.role === "user" || m.role === "assistant") && m.text?.trim())
    .slice(-CATCH_UP_MESSAGES)
    .map((m) => `${m.role === "user" ? "User" : "Agent"}: ${m.text.slice(0, 1500)}`)
    .join("\n\n");
  try {
    const res = await invoke<{ summary: string; items: { tag: string; text: string }[] }>("generate_catch_up", {
      cwd: props.cwd, model: uiStore.textGenerationModel, transcript,
    });
    if (props.chatId !== chatAtStart) return;
    Object.assign(catchUp, { summary: res.summary, items: res.items ?? [] });
  } catch (e) {
    if (props.chatId === chatAtStart) catchUp.error = `Couldn't catch up: ${e}`;
  } finally {
    if (props.chatId === chatAtStart) catchUp.loading = false;
  }
}
async function copyCatchUp() {
  const text = [catchUp.summary, ...catchUp.items.map((it) => `- ${CATCH_UP_LABELS[it.tag] ?? it.tag}: ${it.text}`)].join("\n");
  await navigator.clipboard.writeText(text).catch(() => {});
  catchUp.copied = true;
  setTimeout(() => { catchUp.copied = false; }, 1500);
}
watch(() => props.chatId, () => Object.assign(catchUp, { open: false, summary: "", items: [], error: "" }));

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



function syncStore() {
  S.sync();
}






// The stream reducers (transcript, turn boundary, blocking requests, queue
// drain, notifications) live in the chat session — lib/chatSession.ts — so they
// keep running while this component is unmounted. This view reads the session
// and reaches back only through the ChatViewHooks installed in onMounted.





async function respondCodexUserInput(answers: Record<string, string[]>) {
  await S.respond({ kind: "codexInput", answers });
}

function cancelCodexUserInput() {
  const request = codexUserInput.value;
  if (!request) return;
  const answers = Object.fromEntries(request.questions.map((question) => [question.id, [] as string[]]));
  void respondCodexUserInput(answers);
}

// Reply to a rich ACP permission request with the chosen adapter optionId.
async function acpRespond(optionId: string, optName: string, kind: string) {
  await S.respond({ kind: "acpOption", optionId, label: optName, reject: kind.startsWith("reject") });
}

// Rewind / fork ride Claude's --resume-session-at (chatrewind.go); Codex and
// generic ACP have nothing like it, so they only get Copy / Edit.
const canBranch = computed(() => !isAcpRuntime.value);
// The reply that closes each settled turn: the last assistant bubble before the
// next prompt, with no tool call or thinking after it.
const finalReplyIds = computed(() => {
  const ids = new Set<number>();
  let last: number | null = null;
  for (const m of messages.value) {
    if (m.role === "assistant") last = m.partial ? null : m.id;
    else if (m.role === "tool" || m.role === "thinking") last = null;
    else if (m.role === "user" || m.role === "queued") {
      if (last !== null) ids.add(last);
      last = null;
    }
  }
  if (last !== null && !busy.value) ids.add(last);
  return ids;
});
const termTabs = useTerminalTabsStore();

function putInComposer(msg: ChatMessage) {
  inputText.value = msg.displayText ?? msg.text;
  pendingImages.value = msg.images ? [...msg.images] : [];
  nextTick(() => { inputEl.value?.focus(); inputEl.value?.setCaret(inputText.value.length); });
}

function editMessage(msg: ChatMessage) {
  putInComposer(msg);
}

async function rewindTo(msg: ChatMessage) {
  const snapshot = { ...msg };
  if (await S.rewind(msg)) putInComposer(snapshot);
}

async function forkFrom(msg: ChatMessage) {
  const id = await S.fork(msg);
  if (id === null) return;
  // The tab opener looks the chat up in the store; an unknown id would mint a
  // new empty chat instead.
  await chats.reload();
  termTabs.openChat(props.workspaceId, id);
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
    S.notice(`Could not copy message: ${e}`);
    S.save();
  }
}

// The welcome-screen composer creates a chat and its first prompt together.
// chat_send starts the process itself, whatever the transport.
async function sendInitialPrompt(prompt: string, images?: string[]) {
  emit("prompt-sent");
  await sendMessage(prompt, images);
}

async function sendMessage(forcedText?: string, extraImages?: string[]): Promise<{ id: number; text: string } | null> {
  const feedbackBatch = forcedText === undefined && !busy.value ? [...transcriptFeedback.pending.value] : [];
  const sendingChatId = props.chatId;
  const raw = forcedText ?? inputText.value;
  // The stripped copy is what goes on the wire and what an echoed user.delta
  // is matched against (`pendingSends`); `raw` still carries any `wrapPaste`
  // markers, kept below only to give the TRANSCRIPT'S OWN rendering of this
  // message something to collapse into a chip — never sent anywhere.
  const strippedBaseline = stripPasteMarkers(raw).trim();
  let text = strippedBaseline;
  if (!text && !feedbackBatch.length) return null;
  branchBannerDismissed.value = true;
  // /pr: build a PR description prompt from git diff
  if (text.match(/^\/pr\b/)) {
    try {
      const stat = await invoke<{ stdout: string }>("run_git", { cwd: props.cwd, args: ["diff", "HEAD~1", "--stat", "--no-color"] });
      const diff = await invoke<{ stdout: string }>("run_git", { cwd: props.cwd, args: ["diff", "HEAD~1", "--no-color"] });
      text = `Write a PR description for these changes:\n\n${stat.stdout}\n\`\`\`diff\n${diff.stdout.slice(0, 8000)}\n\`\`\``;
    } catch (e) {
      S.notice(`Error reading git diff: ${e}`);
      return null;
    }
  }

  text = transcriptFeedback.compose(feedbackBatch, text);
  const acceptFeedback = () => {
    if (feedbackBatch.length && props.chatId === sendingChatId) transcriptFeedback.markSent(feedbackBatch, text);
    feedbackSendError.value = "";
  };
  const images = [...pendingImages.value, ...(extraImages ?? [])];
  // A follow-up is always a separate turn.  In particular, do not rely on an
  // ACP adapter's prompt queueing: Codex can reinterpret a second turn/start
  // as steering, and generic adapters have no negotiated queue capability.
  if (busy.value) {
    const queued = enqueueMessage(text, images);
    acceptFeedback();
    pendingImages.value = [];
    inputText.value = "";
    S.save();
    await nextTick();
    scrollToBottom(true);
    return { id: queued.id, text };
  }
  if (!forcedText) {
    inputText.value = "";
    await nextTick();
  }

  const msgImages = images.length > 0 ? images : undefined;
  // Only set when the /pr rewrite above didn't replace `text` with a generated
  // prompt — that one has no paste chip of its own to show.
  const displayText = text === strippedBaseline && hasPasteMarkers(raw) ? raw.trim() : undefined;
  // This is the durable start receipt. It captures the recovery checkpoint and
  // creates one audit row; provider completion freezes its final diff.
  await invoke("start_turn_audit", {
    cwd: props.cwd,
    subjectId: `chat:${props.chatId}`,
    label: text.slice(0, 60),
  }).catch(() => {});

  // Auto-title from first user message (only if still at default and Claude hasn't set one yet)
  if (!claudeGeneratedTitle.value) {
    const session = chats.sessions.find((s) => s.id === props.chatId);
    if (session && isDefaultTitle(session.title)) {
      chats.sync(props.chatId, { title: smartTitle(text) });
      void refineTitle(text);
    }
  }

  pendingImages.value = [];
  syncStore();
  const sentId = await S.send({ text, images: msgImages, displayText });
  if (sentId === null) return null;
  acceptFeedback();
  return { id: sentId, text };
}



// Generic tool permission + diff Accept/Reject (both pull from pendingPermission|pendingDiff).
async function respondPermission(allow: boolean, opts?: { always?: boolean; updatedInput?: Record<string, unknown>; message?: string }) {
  await S.respond({ kind: "permission", allow, ...opts });
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
  if (!pendingQuestion.value || !canSubmitQuestion.value) return;
  // A multi-select answer must stay an array (the CLI expects the same shape it
  // gave options in) — joining it into a comma string here is what made
  // multi-select questions look permanently stuck after Submit.
  const answers: Record<string, string | string[]> = {};
  for (const q of questionSpecs.value) {
    const custom = (questionCustomAnswers.value[q.question] ?? "").trim();
    if (custom) { answers[q.question] = custom; continue; }
    const labels = questionAnswers.value[q.question] ?? [];
    if (!labels.length) continue;
    answers[q.question] = q.multiSelect ? labels : labels[0];
  }
  await S.respond({ kind: "question", answers });
}
async function cancelQuestion() {
  await S.respond({ kind: "question", answers: {} });
}

async function respondPlan(approve: boolean) {
  const resolved = await S.respond({ kind: "plan", approve, feedback: planFeedback.value });
  // ExitPlanMode changes Claude's live session in place. Mirror that transition
  // locally without restarting the process, or the picker and next resume keep
  // claiming/re-applying plan mode after the approved implementation has begun.
  if (approve && resolved && permMode.value === "plan") {
    permMode.value = "auto";
    savePermMode(props.chatId, "auto");
    if (!usesRpcRuntime.value) void persistLaunch({ permission_mode: "auto", row_only: true });
  }
}

// Pick a permission mode from the header dropdown (default / acceptEdits / bypassPermissions).
// RPC runtimes update the active thread in place; restarting them loses the
// pending approval context and was the source of "unknown agent session" errors.
async function selectPermMode(mode: PermMode) {
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
      S.trackAcpControl(requestId);
      void persistLaunch({ permission_mode: mode, row_only: true });
    } catch (err) {
      // Do not leave the picker claiming a policy that the server rejected.
      permMode.value = previousMode;
      savePermMode(props.chatId, previousMode);
      S.notice(`Couldn't update the permission mode: ${String(err)}`);
      syncStore();
    }
    return;
  }

  await persistLaunch({ permission_mode: mode });
}

// Restart / abort / clear are the chat runtime's (lib/chatSession.ts intents →
// chat_restart / chat_interrupt / chat_clear): Go stops the process and
// publishes session.reset, whose reducer drops the dead prompts, closes the
// partial and clears busy — for every client, not just this view. The next
// send starts a fresh process, resuming the session unless it was cleared.
async function abortTurn() {
  // A stalled turn gets the restart its button promises; otherwise the
  // provider stops the turn in place where it can (Claude, Codex), and Go
  // falls back to a restart where it cannot or does not answer.
  await (stalled.value ? S.restart() : S.interrupt());
}

async function clearChat() {
  // The draft side is this view's own; the transcript side is Go's.
  clearQueuedMessages();
  pendingImages.value = [];
  chats.sync(props.chatId, { claudeSessionId: "", busy: false, messageCount: 0, title: `Chat` });
  await S.clear();
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
    const recalled = S.recallLastUserMessage();
    if (recalled !== null) {
      e.preventDefault();
      inputText.value = recalled;
      nextTick(() => { inputEl.value?.focus(); inputEl.value?.setCaret(inputText.value.length); });
      return;
    }
  }
  // Suggestion nav claims Arrow/Tab/Enter/Escape while the list is open, so it
  // has to run before the Enter-sends check below.
  if (completion.handleKeydown(e)) return;
  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); sendMessage(); }
}

function onInput() {
  void completion.update();
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

function onWindowKeydown(e: KeyboardEvent) {
  if (!pendingPermission.value && !pendingDiff.value) return;
  if (document.activeElement === inputEl.value?.element) return; // handled by onKeydown
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

// Set on unmount so the long async onMounted() below can stop writing into
// this instance's (possibly by-then-evicted) session once nothing is
// watching — see the guards after each `await` inside onMounted.
let unmounted = false;

// Coming back to the window counts as seeing this chat — mirrors Terminal.vue's
// onWindowFocus/seeActiveTab. Without this, a turn that finished while the app
// was backgrounded left the review dot up even after refocusing on this exact
// chat, since finishTurn() only checks document.hasFocus() at the instant STOP fires.
//
// Also bound to `mousedown` on the chat root: the `window` "focus" DOM event is
// not a reliable signal that the user is back on THIS thread specifically — the
// user may return to the app via Cmd+Tab/Dock without the event firing before
// they start reading, or with this same tab already active so nothing else
// re-checks it. A click anywhere in the pane is unambiguous proof of "looking at
// it" and clears a dot that would otherwise sit lit until the next tab switch.
//
// Asks watchingNow(), NOT props.isWatching. Terminal.isWatching() reads
// document.hasFocus() inside a plain function feeding a template prop, and
// focus is not reactive — so the prop is a snapshot of whatever focus was at
// the last render, not what it is now. Anything reactive that re-rendered
// while the user was away (a streaming message, a tab title, a phase dot)
// froze `isWatching` at false, and this guard then refused to mark the chat
// seen on the very focus event that exists to clear it. The mousedown
// fallback shared the guard, so clicking into the thread did not help either:
// the dot sat lit until a tab switch. watchingNow() reads the session's own
// refCount plus a LIVE document.hasFocus(), which is what finishTurn already
// trusts to tell done from review.
function onWindowFocus() {
  if (watchingNow()) chatAttention.markSeen(props.chatId);
}

// True until this mount has read the transcript back (see the skeleton in the template).
const transcriptLoading = ref(true);
onMounted(async () => {
  // Point the session's view hooks at this mount and take a reference. The
  // session already holds the listeners and reduces the stream itself, so no
  // stream is ever torn down and re-attached on a remount.
  S.attachView({
    scrollToBottom,
    onQuestionOpened: () => { questionAnswers.value = {}; },
    onPlanOpened: () => { planFeedback.value = ""; },
    onAcpSession: () => learnModels(agentKind.value, liveModels.value),
    restoreAcpSelections,
    usesRpcRuntime: () => usesRpcRuntime.value,
    send: async (text, images) => { await sendMessage(text, images); },
  });
  // The transcript arrives on this channel for BOTH transports, so it is
  // attached unconditionally — unlike the raw ones, which are per-runtime.
  await S.listenEvents();
  S.retain();
  // Config must be loaded (and legacy localStorage migrated) before any of the
  // config-backed refs below are trusted — reload them here once configReady settles.
  await configReady;
  // Bail if this instance was torn down (chat/workspace switched away) while
  // awaiting above: release() can evict+delete an idle session synchronously,
  // so a late write here would land on an orphaned session nobody reads, or —
  // if the user switched back in the meantime — race a fresh mount's own load.
  if (unmounted) return;
  migrateLegacyChatConfig();
  if (isAcpRuntime.value && !acpModes.value && acpConfigOptions.value.length === 0) {
    const cached = getAcpCapabilities(props.chatId, agentKind.value);
    if (cached) S.seedAcpCapabilities(cached.modes, cached.configOptions);
  }
  // Only read the transcript back from SQLite when the session has none. A
  // non-empty session is the LIVE copy — it kept receiving while this view was
  // unmounted, so it is ahead of the DB, and assigning over it would throw the
  // newer part away (exactly the hole this plan is about).
  if (messages.value.length === 0) {
    try {
      const loaded = await loadMessages(props.chatId);
      if (unmounted) return;
      S.hydrate({ messages: loaded.messages, hiddenBefore: loaded.hiddenBefore });
    } finally {
      transcriptLoading.value = false;
    }
    // Catch up on anything the agent said after that transcript was written —
    // i.e. a turn that was in flight when the app was last closed or crashed.
    await replayChatStream(props.chatId);
    if (unmounted) return;
  }
  transcriptLoading.value = false;
  // A remount is how you come back to a chat now (fáze 3 unmounts hidden chat
  // leaves), so land on the newest message. The activeByWs watcher below cannot
  // do it: the active id is already this chat before the fresh instance mounts,
  // so it never fires and the view opened scrolled to the top of the history.
  scrollToBottom(true);
  // A queue parked by an older build (or by a relaunch — the placeholders are
  // persisted with the transcript) has no busy transition left to release it.
  S.drain();
  selectedProfileId.value = loadProfileId(props.chatId);
  selectedModel.value = loadModel();
  selectedEffort.value = loadEffort();
  permMode.value = loadPermMode(props.chatId);
  // Pin what this device resolved (its "last used" defaults) onto the row on
  // first mount: the row is what Go starts the process with, and it must not
  // start a different model than the picker shows.
  if (!usesRpcRuntime.value) seedLaunchRow();

  // Mounted normally means visible (Terminal.isChatVisible), but a chat spawned
  // with a prompt can mount unwatched — marking that one seen would clear a dot
  // nobody looked at.
  holdAttention(props.isWatching ?? true);
  window.addEventListener("focus", onWindowFocus);
  window.addEventListener("keydown", onWindowKeydown);
  // Float (compact) control chat: pre-allow `burrow` Bash commands so routine
  // control calls (focus/list/new-tab/spawn) don't prompt every time. User can
  // still tighten via the perm-mode switch / Deny.
  if (props.compact) chats.addPermissionRule("Bash:burrow");
  const stored = chats.sessions.find((s) => s.id === props.chatId)?.claudeSessionId ?? "";
  if (stored) S.hydrate({ sessionId: stored });
  S.sync();
  // The raw listeners are JS-only and free — attach both, so a cold chat
  // (its process is started by chat_send, in Go) and an agent switch stream
  // immediately.
  await S.listenClaude();
  await S.listenAcp();
  // No drain here: it already ran above, unconditionally. Two drains in one
  // mount each take their own message and send both turns at once.
  if (props.initialPrompt) sendInitialPrompt(props.initialPrompt, props.initialImages);

  // Load account info (plan, 5h window) — non-blocking.
  invoke<AccountInfo>("claude_get_account", { cwd: props.cwd })
    .then((info) => { accountInfo.value = info; })
    .catch(() => {});

});

onBeforeUnmount(() => {
  unmounted = true;
  if (copyFeedbackTimer) clearTimeout(copyFeedbackTimer);
  if (stallTimer) clearInterval(stallTimer);
  window.removeEventListener("focus", onWindowFocus);
  window.removeEventListener("keydown", onWindowKeydown);
  // Hand the session back instead of unsubscribing: it drops its listeners only
  // when nothing is in flight (lib/chatSession.ts). A turn running in a tab the
  // user navigated away from keeps streaming into the session and is simply
  // there on the next mount.
  S.release();
  // NOTE: deliberately do NOT stop the adapter/CLI here. The backend process
  // lifetime is tied to the SESSION, not this component's mount — a background
  // chat gets unmounted whenever its host tears down (FloatChat when ws.active
  // flips, a workspace leaving `opened`),
  // and killing the proc there halted live agents mid-turn ("blik"). Teardown
  // now happens on explicit close (Terminal.closeTab / closePane → stopChatSession)
  // and on `remove()` in the claudeChats store.
});

watch(() => props.chatId, () => nextTick(() => inputEl.value?.focus()));

// Scroll to bottom when this chat becomes the active one (user clicked it in sidebar).
watch(() => chats.activeByWs[props.workspaceId], (activeId) => {
  if (activeId === props.chatId) nextTick(() => scrollToBottom(true));
});

// Exposed for host shells that drive this chat from an external compact
// input: send a message and focus the textarea.
function focusInput() {
  nextTick(() => inputEl.value?.focus());
}
function getPermMode(): PermMode {
  return permMode.value;
}
defineExpose({ sendMessage, focusInput, selectModel, selectedModel, getPermMode, selectPermMode, permMode });
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
  /* Semantic status hues for gate banners — warn/success ride the theme's own
     yellow/green so they shift with it; info rides the new status-blue token
     ("done"/finished feel) rather than a hardcoded hex, so it shifts with
     every theme too. */
  --chat-warn: var(--yellow, #f59e0b);
  --chat-success: var(--green, #10b981);
  --chat-info: var(--blue, #60a5fa);
}

.diff-line { line-height: 1.5; }
.diff-add { color: var(--green); }
.diff-del { color: var(--red); }

.chat-header-btn { position: relative; }

.chat-runtime-dot {
  box-shadow: 0 0 7px color-mix(in srgb, var(--green, #22c55e) 75%, transparent);
}

.btn-danger-active { color: var(--red, #ef4444) !important; background: color-mix(in srgb, var(--red, #ef4444) 15%, transparent) !important; }
.btn-active { color: var(--chat-accent) !important; background: color-mix(in srgb, var(--chat-accent) 15%, transparent) !important; }


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
.perm-allow { background: var(--green); color: #fff; }
.perm-always { background: color-mix(in srgb, var(--green) 22%, var(--bg-panel)); color: var(--text-primary); }
.perm-deny  { background: var(--red); color: #fff; }
.perm-neutral { background: color-mix(in srgb, var(--agent-accent, var(--accent)) 20%, var(--bg-panel)); color: var(--text-primary); border: 1px solid color-mix(in srgb, var(--agent-accent, var(--accent)) 35%, transparent); }
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
  background: var(--surface-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius-avatar);
}

/* :deep() — this span is built by pillifyMentions() via DOMParser/createElement
   and injected through v-html, so it never gets the scoped data-v-* attribute
   a template-authored element would; a plain scoped selector silently never
   matches it and the pill renders as unstyled text. */
:deep(.mention-pill) {
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
:deep(.mention-pill::before) {
  content: "";
  width: 0.85em;
  height: 0.85em;
  flex-shrink: 0;
  background: var(--chat-info);
  -webkit-mask: var(--mention-pill-icon) center / contain no-repeat;
  mask: var(--mention-pill-icon) center / contain no-repeat;
}
:deep(.mention-pill) {
  --mention-pill-icon: url('data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256"><path d="M213.66,82.34l-56-56A8,8,0,0,0,152,24H56A16,16,0,0,0,40,40V216a16,16,0,0,0,16,16H200a16,16,0,0,0,16-16V88A8,8,0,0,0,213.66,82.34ZM160,51.31,188.69,80H160ZM200,216H56V40h88V88a8,8,0,0,0,8,8h48V216Z"/></svg>');
}

/* The user bubble is a quiet neutral surface (--bg-selected), so fences and
   chips inside it only need a faint tint of the text color. */
.bubble-user .md-body :deep(pre) {
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-color: color-mix(in srgb, var(--text-primary) 12%, transparent);
}
.bubble-user .md-body :deep(code) { background: color-mix(in srgb, var(--text-primary) 10%, transparent); color: inherit; }
.bubble-user .md-body :deep(a) { color: var(--chat-info); }
.bubble-user .md-body :deep(.composer-inline-chip) {
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--text-primary) 18%, transparent);
  color: inherit;
  cursor: pointer;
}

/* Tool row — quiet activity-log line, expandable */
/* Tool rows read as quiet text lines (Ghostex): "› Read chatSession.ts". No
   chip chrome; a running or failed row only changes its colour. */
.tool-row {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 3px 6px;
  background: transparent;
  border: none;
  border-radius: 7px;
  font-size: 13px;
  color: var(--text-secondary, rgba(255,255,255,0.55));
  cursor: pointer;
  user-select: none;
  max-width: 100%;
  overflow: hidden;
  transition: background .1s, color .1s;
}
.tool-row:hover {
  background: color-mix(in srgb, var(--text-primary) 5%, transparent);
  color: var(--text-primary);
}
.tool-row-running { color: var(--text-primary); }
.tool-row-failed { color: var(--red, #ef4444); }
.tool-caret {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: transform .15s;
}
.tool-caret-open { transform: rotate(90deg); }
.tool-icon { color: var(--agent-accent, var(--accent)); flex-shrink: 0; }
.tool-row-failed .tool-icon { color: var(--red, #ef4444); }
.tool-status-icon { flex-shrink: 0; }
.tool-pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--agent-accent, var(--accent));
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
  background: color-mix(in srgb, var(--green, #16a34a) 5%, transparent);
  border: 1px solid color-mix(in srgb, var(--green, #16a34a) 16%, transparent);
  border-radius: 8px;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--green, var(--text-primary));
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow-y: auto;
  max-width: min(560px, 90vw);
}
.tool-output-failed {
  background: color-mix(in srgb, var(--red, #ef4444) 6%, transparent);
  border-color: color-mix(in srgb, var(--red, #ef4444) 20%, transparent);
  color: var(--red, #ef4444);
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


/* Context usage bar fill colors */
/* Context ring — the meter is the button that empties it */
.ctx-ring {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 5px 2px 3px;
  border-radius: 999px;
  transition: background 120ms, color 120ms;
}
.ctx-ring:hover { background: var(--hover); }
.ctx-ring-pct { opacity: 0.75; }
.ctx-ring.ctx-ok { color: color-mix(in srgb, var(--chat-accent) 75%, var(--muted-foreground)); }
.ctx-ring.ctx-warning { color: var(--chat-warn); }
.ctx-ring.ctx-exceeded { color: var(--red, #ef4444); }
.ctx-card-bar.ctx-ok { background: color-mix(in srgb, var(--chat-accent) 75%, transparent); }
.ctx-card-bar.ctx-warning { background: var(--chat-warn); }
.ctx-card-bar.ctx-exceeded { background: var(--red, #ef4444); }
.ctx-card-btn {
  flex: 1;
  padding: 3px 6px;
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 10px;
  color: var(--muted-foreground);
  transition: background 120ms, color 120ms;
}
.ctx-card-btn:hover:not(:disabled) { background: var(--hover); color: var(--foreground); }
.ctx-card-btn:disabled { opacity: 0.4; cursor: default; }

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

/* Ghostex-style "Worked 42s" bar and the working spark. */
.worked-bar { display: flex; width: 100%; align-items: center; gap: 9px; padding: 9px 14px; border: none; border-radius: 12px; font-size: 13.5px; text-align: left; cursor: pointer; background: color-mix(in srgb, var(--chat-text) 4%, var(--chat-bg)); }
.worked-bar:hover { background: color-mix(in srgb, var(--chat-text) 7%, var(--chat-bg)); }
.worked-pill { display: inline-flex; align-items: center; gap: 5px; padding: 2px 9px; border-radius: 999px; border: 1px solid var(--chat-border); font-size: 11.5px; color: var(--chat-text-secondary); }
.working-spark { color: #E0876A; animation: working-spin 9s linear infinite, working-pulse 1.6s ease-in-out infinite; }
@keyframes working-spin { to { transform: rotate(360deg); } }
@keyframes working-pulse { 50% { opacity: .55; } }

/* Inline diff card (changed-files row). */
.diff-card { border: 1px solid var(--chat-border); border-radius: 14px; overflow: hidden; background: color-mix(in srgb, var(--chat-text) 2%, var(--chat-bg)); }
.diff-card-head { display: flex; align-items: center; gap: 10px; padding: 11px 14px; }
.diff-card-icon { display: flex; align-items: center; justify-content: center; width: 30px; height: 30px; flex-shrink: 0; border-radius: 9px; color: var(--chat-accent); background: color-mix(in srgb, var(--chat-accent) 16%, transparent); }
.diff-ratio { width: 90px; height: 5px; flex-shrink: 0; border-radius: 3px; overflow: hidden; background: var(--red, #D9827A); }
.diff-ratio-add { display: block; height: 100%; background: var(--chat-success); }
.diff-file-row { display: flex; align-items: center; gap: 8px; padding: 8px 14px; font-size: 12.5px; cursor: pointer; border-top: 1px solid var(--chat-border); background: color-mix(in srgb, var(--chat-text) 3%, transparent); }
.diff-file-row:hover { background: color-mix(in srgb, var(--chat-text) 6%, transparent); }
.diff-blocks { display: flex; gap: 2px; flex-shrink: 0; }
.diff-blocks i { width: 7px; height: 7px; border-radius: 2px; }
.diff-blocks .b-add { background: var(--chat-success); }
.diff-blocks .b-del { background: var(--red, #D9827A); }
.diff-blocks .b-off { background: var(--chat-border); }
.diff-open { display: flex; padding: 3px; border: none; border-radius: 5px; background: transparent; color: var(--chat-text-secondary); cursor: pointer; }
.diff-open:hover { color: var(--chat-text); background: color-mix(in srgb, var(--chat-text) 8%, transparent); }
.diff-lines { border-top: 1px solid var(--chat-border); padding: 4px 0; font-family: var(--font-mono); font-size: 11.5px; line-height: 1.6; overflow-x: auto; }
.diff-line { padding: 0 14px; white-space: pre; color: var(--chat-text-secondary); }
.diff-line-add { background: color-mix(in srgb, var(--chat-success) 14%, transparent); color: color-mix(in srgb, var(--chat-success) 55%, var(--chat-text)); }
.diff-line-del { background: color-mix(in srgb, var(--red, #D9827A) 14%, transparent); color: color-mix(in srgb, var(--red, #D9827A) 55%, var(--chat-text)); }
.diff-sign { display: inline-block; width: 16px; opacity: 0.7; user-select: none; }
.diff-sep { padding: 0 14px; color: var(--chat-muted); user-select: none; }
.diff-more { padding: 4px 14px; font-family: var(--font-sans); font-size: 11.5px; color: var(--chat-text-secondary); cursor: pointer; }
.diff-more:hover { color: var(--chat-text); }

/* Transcript minimap rail. */
.minimap-rail { position: absolute; top: 72px; right: 10px; z-index: 5; display: flex; flex-direction: column; align-items: flex-end; gap: 9px; padding: 6px 4px; max-height: calc(100% - 140px); overflow: hidden; }
.minimap-dash { height: 2px; border: none; border-radius: 1px; padding: 0; background: color-mix(in srgb, var(--chat-text) 28%, transparent); cursor: pointer; transition: width .12s, background-color .12s; }
.minimap-dash::before { content: ""; display: block; height: 10px; margin-top: -4px; }
.minimap-dash.is-current { background: var(--chat-text); }
.minimap-dash.is-hover { background: var(--chat-accent); width: 28px !important; }
.minimap-preview { position: absolute; top: 0; right: 40px; width: 320px; padding: 10px 12px; border: 1px solid var(--chat-border); border-radius: 12px; background: var(--chat-surface); box-shadow: 0 12px 32px rgba(0,0,0,.35); pointer-events: none; }

/* Catch me up: button in the empty composer, card above it. */
.catchup-btn { position: absolute; top: 8px; right: 10px; z-index: 1; display: flex; align-items: center; gap: 6px; padding: 4px 11px; border-radius: 999px; border: 1px solid var(--chat-border); background: var(--chat-surface); color: var(--chat-text); font-size: 12px; cursor: pointer; }
.catchup-btn:hover { background: color-mix(in srgb, var(--chat-text) 6%, var(--chat-surface)); }
.catchup-card { border: 1px solid var(--chat-border); border-radius: 14px; padding: 12px 14px; background: color-mix(in srgb, var(--chat-text) 2%, var(--chat-bg)); }
.catchup-tag { flex-shrink: 0; margin-top: 1px; padding: 0 6px; border-radius: 5px; font-size: 10.5px; font-weight: 600; letter-spacing: .02em; text-transform: uppercase; }
.catchup-tag.tag-needs_you { color: var(--chat-warn); background: color-mix(in srgb, var(--chat-warn) 16%, transparent); }
.catchup-tag.tag-blocked { color: var(--red, #D9827A); background: color-mix(in srgb, var(--red, #D9827A) 14%, transparent); }
.catchup-tag.tag-in_progress { color: var(--chat-info); background: color-mix(in srgb, var(--chat-info) 14%, transparent); }
.catchup-tag.tag-done { color: var(--chat-success); background: color-mix(in srgb, var(--chat-success) 14%, transparent); }
.catchup-tag.tag-untested { color: var(--chat-text-secondary); background: color-mix(in srgb, var(--chat-text) 8%, transparent); }

/* Sub-agents card above the composer. */
.subagents-card { border: 1px solid var(--chat-border); border-radius: 14px; overflow: hidden; background: color-mix(in srgb, var(--chat-text) 2%, var(--chat-bg)); }
.subagents-head { display: flex; width: 100%; align-items: center; gap: 8px; padding: 9px 14px; font-size: 12.5px; border: none; background: transparent; cursor: pointer; }
.subagent-row { display: flex; width: 100%; align-items: center; gap: 9px; padding: 6px 14px; font-size: 12.5px; text-align: left; border: none; border-top: 1px solid color-mix(in srgb, var(--chat-border) 60%, transparent); background: transparent; cursor: pointer; }
.subagent-row:hover { background: color-mix(in srgb, var(--chat-text) 5%, transparent); }
.subagent-badge { flex-shrink: 0; padding: 1px 7px; border-radius: 6px; font-size: 11px; font-weight: 500; }
.subagent-badge.tone-needs { color: var(--chat-warn); background: color-mix(in srgb, var(--chat-warn) 16%, transparent); }
.subagent-badge.tone-running { color: var(--chat-info); background: color-mix(in srgb, var(--chat-info) 14%, transparent); }
.subagent-badge.tone-done { color: var(--chat-success); background: color-mix(in srgb, var(--chat-success) 14%, transparent); }
.subagent-badge.tone-failed { color: var(--red, #D9827A); background: color-mix(in srgb, var(--red, #D9827A) 14%, transparent); }
.subagent-badge.tone-idle { color: var(--chat-text-secondary); background: color-mix(in srgb, var(--chat-text) 6%, transparent); }

/* Queued follow-up rows inside the composer. */
.queue-row { background: color-mix(in srgb, var(--chat-text) 3%, transparent); }
.queue-row-drop { border-color: var(--chat-accent); }
.queue-act { display: flex; align-items: center; justify-content: center; width: 22px; height: 22px; border-radius: 6px; border: none; background: transparent; color: var(--chat-text-secondary); cursor: pointer; }
.queue-act:hover { background: color-mix(in srgb, var(--chat-text) 8%, transparent); color: var(--chat-text); }

/* Chat input box: quiet by default, agent-accent ring on focus so the active
   composer is unambiguous without a heavy persistent border. */
.chat-input-box:focus-within {
  border-color: color-mix(in srgb, var(--text-primary) 22%, var(--border));
}

.chat-input-box { position: relative; z-index: 1; }
</style>
