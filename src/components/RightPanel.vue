<template>
  <aside
    ref="panelEl"
    class="flex h-full shrink-0 grow-0 overflow-hidden bg-panel text-xs [backdrop-filter:var(--blur-panels,none)]"
    :class="props.open ? 'w-[var(--right-panel-width,300px)] basis-[var(--right-panel-width,300px)] border-l border-border' : 'w-0 basis-0'"
  >
    <!-- v-show, not v-if: closing the panel must keep XTerm/BrowserPane mounted,
         or reopening respawns a fresh shell over the still-running old one. -->
    <div v-show="props.open" class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <div class="flex h-10 shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-2 hide-scrollbar">
        <button
          v-for="tab in openedTabs"
          :key="tab.id"
          class="group flex h-7 shrink-0 items-center gap-1.5 rounded-[var(--radius-nav)] px-2 text-[11px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
          :class="activeTab === tab.id && 'bg-accent/15 text-foreground'"
          @click="activeTab = tab.id"
        >
          <span class="relative flex h-[13px] w-[13px] shrink-0 items-center justify-center">
            <component :is="tab.icon" :size="13" class="group-hover:opacity-0" />
            <span class="absolute inset-0 hidden items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground group-hover:flex" role="button" :aria-label="`Close ${tab.label}`" @click.stop="closeSurface(tab.id)"><PhX :size="12" /></span>
          </span>
          <span class="font-medium">{{ tab.label }}</span>
        </button>
        <button class="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Open a surface" aria-label="Open a surface" @click="showSurfacePicker"><PhPlus :size="15" /></button>
      </div>

    <div v-if="!activeTab" class="flex flex-1 items-center justify-center overflow-y-auto p-5">
      <div class="w-full max-w-[420px]">
        <div class="mb-5 text-center">
          <h2 class="m-0 text-sm font-semibold text-foreground">Open a surface</h2>
          <p class="mt-1 text-[11px] text-muted-foreground">Choose what to show in the right panel.</p>
        </div>
        <div class="grid gap-2" :class="cq.is.md ? 'grid-cols-2' : 'grid-cols-1'">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            class="group flex min-h-[66px] items-start gap-3 rounded-[var(--radius-card)] border border-border/70 bg-panel px-3 py-3 text-left transition-colors hover:border-accent/45 hover:bg-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            @click="openSurface(tab.id)"
          >
            <component :is="tab.icon" :size="16" class="mt-0.5 shrink-0 text-secondary-foreground group-hover:text-accent" />
            <span class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">{{ tab.label }}</span>
              <span class="text-[10.5px] leading-relaxed text-muted-foreground">{{ tab.description }}</span>
            </span>
          </button>
        </div>
      </div>
    </div>

    <!-- Extension surfaces are host-rendered, never third-party Vue code. -->
    <WorkspacePulseSurface
      v-if="activeExtensionSurface?.kind === 'workspace-pulse'"
      :cwd="props.cwd"
      :title="activeExtensionSurface.title"
      :description="activeExtensionSurface.description"
    />

    <ExtensionNativeSurface
      v-else-if="activeExtensionSurface?.kind === 'native' && activeExtensionSurface.ui"
      :title="activeExtensionSurface.title"
      :node="activeExtensionSurface.ui"
    />

    <!-- Explorer tab -->
    <div v-else-if="activeTab === 'explorer'" class="flex flex-1 flex-col overflow-y-auto">
      <div v-if="!props.cwd" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>
      <div v-else-if="fileTree.rootError" class="p-4 text-center text-[11px] text-destructive">{{ fileTree.rootError }}</div>
      <div v-else class="flex-1 py-1">
        <FileTreeNode v-for="node in fileTree.tree" :key="node.id" :node="node" :depth="0" />
      </div>
    </div>

    <!-- Git tab -->
    <div v-else-if="activeTab === 'git'" class="flex flex-1 flex-col overflow-hidden overflow-y-auto">
      <!-- Header -->
      <div class="flex shrink-0 items-center justify-between border-b border-border px-2 py-[5px]">
        <div class="flex items-center gap-1 font-mono text-[11px] text-secondary-foreground">
          <PhGitBranch :size="12" class="shrink-0 text-warning" />
          <span>{{ git.branch || "—" }}</span>
          <span v-if="git.ahead > 0" class="text-[10px] text-success" title="Commits ahead of upstream">↑{{ git.ahead }}</span>
          <span v-if="git.behind > 0" class="text-[10px] text-warning" title="Commits behind upstream">↓{{ git.behind }}</span>
        </div>
        <div class="flex items-center gap-[3px]">
          <button
            v-if="!git.error && git.hasUpstream && git.behind > 0"
            class="flex items-center gap-[3px] rounded-[var(--radius-chip)] border border-border bg-transparent px-[7px] py-0.5 font-sans text-[10px] font-medium text-secondary-foreground transition-colors hover:border-accent/40 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.pulling || git.pushing || git.loading"
            @click="git.pull()"
            title="git pull --ff-only"
          >
            <PhArrowDown :size="11" :class="git.pulling && 'animate-spin'" />
            Pull
            <span>({{ git.behind }})</span>
          </button>
          <button
            v-if="!git.error"
            class="flex items-center gap-[3px] rounded-[var(--radius-chip)] border border-border bg-transparent px-[7px] py-0.5 font-sans text-[10px] font-medium text-secondary-foreground transition-colors hover:border-accent/40 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
            @click="git.push()"
            :title="git.hasUpstream ? 'git push' : 'git push -u origin ' + git.branch"
          >
            <PhArrowUp :size="11" :class="git.pushing && 'animate-spin'" />
            {{ git.hasUpstream ? "Push" : "Publish" }}
            <span v-if="git.ahead > 0">({{ git.ahead }})</span>
          </button>
          <AutoRefreshButton
            :current-interval="ar.currentInterval.value"
            :is-running="ar.isRunning.value"
            :next-refresh-in="ar.nextRefreshIn.value"
            :toggle="ar.toggle"
            :set-refresh-interval="ar.setRefreshInterval"
          />
          <button
            class="flex items-center rounded-[var(--radius-nav)] p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
            @click="activeTerm()?.openGitTab()"
            title="Open the full git manager as a tab"
          >
            <PhArrowsOutSimple :size="13" />
          </button>
          <button
            class="flex items-center rounded-[var(--radius-nav)] p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.loading"
            @click="git.refresh()"
            title="Refresh"
          >
            <PhArrowClockwise :size="13" :class="git.loading && 'animate-spin'" />
          </button>
        </div>
      </div>

      <!-- Push/pull loader -->
      <div v-if="git.pushing || git.pulling" class="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1">
        <div class="relative h-0.5 flex-1 overflow-hidden rounded-full bg-border">
          <div class="git-progress-bar" />
        </div>
        <span class="text-[10px] text-muted-foreground">{{ git.pushing ? "Pushing…" : "Pulling…" }}</span>
      </div>

      <div class="flex flex-1 flex-col overflow-y-auto py-1.5">
        <!-- Error -->
        <div v-if="git.error" class="flex flex-wrap items-center gap-1.5 px-2.5 py-4 text-[11px] text-secondary-foreground">
          <PhWarning :size="13" />
          Not a git repository
          <button
            class="ml-auto flex items-center gap-1 rounded-[var(--radius-chip)] border border-border bg-hover px-2 py-[3px] text-[11px] text-foreground hover:border-warning hover:bg-warning hover:text-black disabled:cursor-default disabled:opacity-35"
            :disabled="git.loading"
            @click="git.gitInit()"
          >
            <PhGitBranch :size="12" />
            Git Init
          </button>
        </div>

        <template v-else>
          <!-- Staged -->
          <div class="flex items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65">
            Staged
            <button
              v-if="git.staged.length > 0"
              class="flex items-center gap-0.5 rounded-[var(--radius-chip)] border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100"
              @click="openAllDiffInTab(true)"
              title="Open all staged diffs in new tab"
            ><PhArrowUpRight :size="10" /> View</button>
          </div>
          <div v-if="git.staged.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-[var(--blue)]/70">Nothing staged</div>
          <div
            v-for="f in git.staged"
            :key="'s:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, true)"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-success">{{ f.x }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-foreground group-hover:block" @click.stop="git.unstageFile(f.path)" title="Unstage">−</button>
          </div>

          <!-- Unstaged + untracked -->
          <div class="mt-2 flex items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65">
            Changes
            <div class="flex items-center gap-[3px]">
              <button
                v-if="git.unstaged.length > 0"
                class="flex items-center gap-0.5 rounded-[var(--radius-chip)] border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100"
                @click="openAllDiffInTab(false)"
                title="Open all unstaged diffs in new tab"
              ><PhArrowUpRight :size="10" /> View</button>
              <button
                v-if="git.unstaged.length > 0 || git.untracked.length > 0"
                class="rounded-[var(--radius-chip)] border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100 disabled:cursor-default disabled:opacity-30"
                :disabled="git.loading"
                @click="git.stageAll()"
                title="Stage all"
              >+ All</button>
            </div>
          </div>
          <div v-if="git.unstaged.length === 0 && git.untracked.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-[var(--blue)]/70">
            Working tree clean
          </div>
          <div
            v-for="f in git.unstaged"
            :key="'u:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, false)"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-warning">{{ f.y }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>
          <div
            v-for="f in git.untracked"
            :key="'t:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-muted-foreground">?</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>

          <!-- Commit -->
          <div class="mt-1.5 flex shrink-0 flex-col gap-[5px] border-t border-border p-2">
            <textarea
              v-model="git.commitMsg"
              class="commit-input box-border w-full min-h-[52px] max-h-[100px] resize-none rounded-[var(--radius-chip)] border border-border bg-[color-mix(in_srgb,var(--border)_15%,var(--bg-panel))] px-2 py-1.5 font-sans text-[11px] leading-normal text-foreground outline-none transition-colors placeholder:text-muted-foreground placeholder:opacity-60 focus:border-accent/60"
              placeholder="Commit message…"
              rows="3"
              @keydown.ctrl.enter="git.commit()"
              @keydown.meta.enter="git.commit()"
            />
            <div class="flex gap-[5px]">
              <button
                class="flex flex-1 items-center justify-center gap-[5px] rounded-[var(--radius-chip)] border-0 bg-accent px-2.5 py-[5px] font-sans text-[11px] font-semibold text-white transition-colors hover:bg-accent-dim disabled:cursor-default disabled:opacity-35"
                :disabled="!git.commitMsg.trim() || git.staged.length === 0"
                @click="git.commit()"
              >
                <PhGitCommit :size="12" />
                Commit
              </button>
              <CommitPushMenu />
            </div>
          </div>

          <!-- Diff (hidden when panel is too narrow — use cq.is.md = ≥320px) -->
          <div v-if="git.diffFile && cq.is.md" class="flex max-h-[220px] shrink-0 flex-col border-t border-border">
            <div class="flex shrink-0 items-center gap-1.5 bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-2 py-1">
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground">{{ git.diffFile }}</span>
              <span class="shrink-0 text-[10px] text-muted-foreground">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
              <button class="flex items-center rounded-[var(--radius-nav)] p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.clearDiff()" title="Close">
                <PhX :size="11" />
              </button>
            </div>
            <DiffView :diff="git.diff" :diff-key="git.diffFile" />
          </div>

          <!-- History -->
          <div class="mt-1.5 shrink-0 border-t border-border pt-1">
            <div
              class="flex cursor-pointer select-none items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65"
              @click="showHistory = !showHistory"
            >
              <span class="flex items-center gap-1"><PhCaretRight :size="9" class="transition-transform" :class="showHistory && 'rotate-90'" /> History</span>
            </div>
            <template v-if="showHistory">
              <div v-if="git.log.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-muted-foreground opacity-60">No commits</div>
              <div
                v-for="(c, i) in git.log"
                :key="c.hash"
                class="mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
                :class="i < git.ahead && 'bg-accent/[0.06] hover:bg-accent/[0.12]'"
                :title="c.subject + '\n' + c.author + (i < git.ahead ? '\n↑ Not pushed' : '')"
                @click="openCommitDiff(c)"
              >
                <span class="shrink-0 font-mono text-[10px] text-warning" :class="i < git.ahead && 'text-accent'">{{ c.shortHash }}</span>
                <span v-if="i < git.ahead" class="shrink-0 text-[9px] font-bold text-accent" title="Not pushed">↑</span>
                <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-secondary-foreground transition-colors hover:text-foreground">{{ c.subject }}</span>
                <span class="shrink-0 text-[10px] text-muted-foreground">{{ c.relTime }}</span>
              </div>
            </template>
          </div>
        </template>
      </div>
    </div>

    <PullRequestsPanel v-else-if="activeTab === 'pull-requests'" :cwd="props.cwd" />

    <div v-else-if="activeTab === 'diff'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
        <PhGitCommit :size="13" class="shrink-0 text-secondary-foreground" />
        <select
          class="min-w-0 flex-1 rounded-[var(--radius-nav)] border border-border bg-panel py-1 pl-1.5 pr-1 text-[11px] text-secondary-foreground outline-none hover:border-muted-foreground"
          :value="diffScopeKey"
          @change="onDiffScopeChange(($event.target as HTMLSelectElement).value)"
        >
          <option value="workspace">Working tree</option>
          <option value="branch">Branch changes</option>
          <optgroup v-if="numberedCheckpoints.length" label="Turns">
            <option v-for="nc in numberedCheckpoints" :key="nc.cp.id" :value="`turn:${nc.cp.id}`">
              Turn {{ nc.turn }}{{ nc.turn === numberedCheckpoints.length ? " (latest)" : "" }}
            </option>
          </optgroup>
        </select>
        <button class="shrink-0 rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh diff" :disabled="scopedDiffLoading" @click="loadScopedDiff"><PhArrowClockwise :size="12" :class="scopedDiffLoading && 'animate-spin'" /></button>
      </div>
      <div v-if="scopedDiffLoading" class="p-4 text-center text-[11px] text-muted-foreground">Loading changes…</div>
      <div v-else-if="diffScope.kind === 'branch' && !branchBase" class="flex flex-1 flex-col items-center justify-center gap-2 p-4 text-center text-[11px] text-muted-foreground">
        <span>Couldn't find a default branch to diff against.</span>
        <select class="rounded-[var(--radius-nav)] border border-border bg-panel px-1.5 py-1 text-[11px] text-secondary-foreground" @change="pickBranchBase(($event.target as HTMLSelectElement).value)">
          <option value="" disabled selected>Pick a branch…</option>
          <option v-for="b in git.branches.filter((b) => b !== git.branch)" :key="b" :value="b">{{ b }}</option>
        </select>
      </div>
      <div v-else-if="!scopedDiff" class="p-4 text-center text-[11px] leading-relaxed text-muted-foreground">No changes.</div>
      <DiffView v-else :diff="scopedDiff" :diff-key="diffScopeKey" />
    </div>

    <ManagerPanel
      v-else-if="activeTab === 'manager' && props.workspaceId"
      :cwd="props.cwd"
      :ws-id="props.workspaceId"
    />
    <div v-else-if="activeTab === 'manager'" class="flex flex-1 items-center justify-center p-6 text-center text-[11px] leading-relaxed text-muted-foreground">
      Open a project to start a Manager thread.
    </div>

    <!-- Sub-agents tab: this thread's chat sub-agents + its Task-tool invocations -->
    <div v-else-if="activeTab === 'agents'" class="flex min-h-0 flex-1 flex-col">
      <!-- Detail: the open child's chat. It is NOT mounted here — SubAgentHost.vue
           keeps one AgentChat alive per child for its whole life (a CLI process
           starts on mount, and it must run whether or not this panel is open),
           and teleports the open one into this slot. -->
      <template v-if="openChildId">
        <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
          <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" aria-label="Back to sub-agents" @click="closeChildDetail"><PhCaretLeft :size="12" /></button>
          <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-semibold text-foreground">{{ chatTitle(openChildId) }}</span>
        </div>
        <!-- The panel owns the open child's view; SubAgentHost keeps every
             OTHER child alive so their CLIs keep running with the panel shut.
             Exactly one of the two renders a given chat id, so their reducers
             cannot overwrite each other in the chat-session registry. -->
        <AgentChat
          v-if="openChildSession"
          :key="openChildSession.id"
          compact
          class="min-h-0 flex-1"
          :chat-id="openChildSession.id"
          :workspace-id="openChildSession.workspaceId"
          :cwd="props.cwd"
          :agent-kind="openChildSession.agentKind"
          is-watching
        />
      </template>

      <template v-else>
        <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
          <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Sub-agents</span>
          <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Spawn a sub-agent under this thread" aria-label="Spawn a sub-agent" @click="openSpawnDialog"><PhPlus :size="12" /></button>
        </div>
        <div v-if="!activeChatId" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
          Open a chat to see its sub-agents.
        </div>
        <template v-else>
          <div v-if="!childList.length" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
            No sub-agents yet.<br />This thread's agent can spawn one, or use +.
          </div>
          <div
            v-for="child in childList"
            :key="child.id"
            class="group flex cursor-pointer items-center gap-1.5 border-b border-border/40 px-2 py-[6px] transition-colors hover:bg-hover"
            @click="openChild(child.id)"
          >
            <PhRobot :size="12" class="shrink-0 text-muted-foreground" />
            <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ child.title }}</span>
            <!-- Phase dot reserves the slot's size (invisible, not hidden, so the box stays); the
                 close button overlays it via absolute + opacity, so it never resizes the row and stays
                 focusable (unlike display:none) for keyboard/AX users. -->
            <span class="relative flex h-[13px] w-[13px] shrink-0 items-center justify-center">
              <span
                v-if="childPhase[child.id] && childPhase[child.id] !== 'idle'"
                class="status-dot group-hover:invisible"
                :class="`status-${childPhase[child.id]}`"
                :title="statusLabel(childPhase[child.id])"
                role="status"
              >{{ childPhase[child.id] === "running" ? spinnerFrame : "" }}</span>
              <button
                class="absolute inset-0 flex items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground opacity-0 pointer-events-none transition-opacity hover:bg-hover hover:text-destructive group-hover:pointer-events-auto group-hover:opacity-100 focus-visible:pointer-events-auto focus-visible:opacity-100"
                :title="`Close ${child.title}`"
                :aria-label="`Close ${child.title}`"
                @click.stop="askCloseChild(child.id)"
              ><PhX :size="11" /></button>
            </span>
          </div>

          <!-- Task-tool invocations by this thread, newest first -->
          <div class="flex shrink-0 items-center gap-1.5 border-y border-border px-2 py-1.5">
            <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Task tool</span>
          </div>
          <div v-if="!subagentList.length" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
            No Task-tool calls yet.<br />Shows up when this chat uses the Task tool.
          </div>
          <div
            v-for="entry in subagentList"
            :key="entry.toolUseId"
            class="flex cursor-pointer items-start gap-1.5 border-b border-border/40 px-2 py-[6px] transition-colors hover:bg-hover"
            @click="openSubagentChat(entry.chatId)"
          >
            <PhRobot :size="12" class="mt-[1px] shrink-0 text-muted-foreground" />
            <div class="flex min-w-0 flex-1 flex-col">
              <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ entry.description }}</span>
              <span class="font-mono text-[9.5px] text-muted-foreground">{{ chatTitle(entry.chatId) }}{{ entry.subagentType ? ` · ${entry.subagentType}` : "" }} · {{ subagentTime(entry.startedAt) }}</span>
            </div>
            <span
              class="mt-0.5 shrink-0 rounded-full px-1.5 py-[1px] text-[9px] font-medium"
              :class="{
                'bg-accent/15 text-accent': entry.status === 'running',
                'bg-success/15 text-success': entry.status === 'done',
                'bg-destructive/15 text-destructive': entry.status === 'failed',
              }"
            >{{ entry.status }}</span>
          </div>
        </template>
      </template>
    </div>

    <!-- Manual sub-agent spawn dialog — same shape as Sidebar.vue's rename dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="spawnDialogOpen" @click.self="spawnDialogOpen = false">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-sm font-semibold text-foreground">Spawn a sub-agent</h3>
        <textarea
          v-model="spawnTask"
          class="h-24 w-full resize-none rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="What should the sub-agent do?"
          autofocus
          @keydown.esc="spawnDialogOpen = false"
          @keydown.enter.meta.prevent="confirmSpawnDialog"
        />
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="spawnDialogOpen = false">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmSpawnDialog" :disabled="!spawnTask.trim()">Spawn</button>
        </div>
      </div>
    </div>

    <!-- Closing a sub-agent deletes it: an archived child would be reachable
         from nowhere (archivedSessionsForWs filters children out), so it asks
         rather than hiding the work somewhere the user cannot get it back. -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="closeChildId !== null" @click.self="closeChildId = null">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-sm font-semibold text-foreground">Close this sub-agent?</h3>
        <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
          <span class="text-secondary-foreground">{{ chatTitle(closeChildId) }}</span> and its transcript are deleted, and its
          agent is stopped if it is still working. What it already reported stays in this thread.
        </p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="closeChildId = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-destructive px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90" @click="confirmCloseChild">Close</button>
        </div>
      </div>
    </div>

    <!-- History tab: pre-turn worktree snapshots, newest first -->
    <div v-else-if="activeTab === 'history'" class="flex flex-1 flex-col overflow-y-auto">
      <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
        <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Checkpoints</span>
        <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh" @click="loadCheckpoints">
          <PhArrowClockwise :size="12" />
        </button>
      </div>

      <div v-if="!checkpoints.length" class="m-2 rounded-[var(--radius-card)] border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
        No checkpoints yet.<br />One is taken before every agent turn.
      </div>

      <div
        v-for="cp in checkpoints"
        :key="cp.id"
        class="group/cp flex cursor-pointer items-center gap-1.5 border-b border-border/40 px-2 py-[6px] transition-colors hover:bg-hover"
        @click="openCheckpointDiff(cp)"
      >
        <PhClockCounterClockwise :size="11" class="shrink-0 text-muted-foreground" />
        <div class="flex min-w-0 flex-1 flex-col">
          <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ cp.label || "Checkpoint" }}</span>
          <span class="font-mono text-[9.5px] text-muted-foreground">{{ cpTime(cp.createdAt) }} · {{ cp.commit.slice(0, 7) }}</span>
        </div>
        <button
          class="shrink-0 rounded-[var(--radius-nav)] p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-hover hover:text-foreground group-hover/cp:opacity-100"
          title="Open diff in panel"
          @click.stop="openCheckpointDiffInPanel(cp)"
        >
          <PhGitCommit :size="12" />
        </button>
        <button
          class="shrink-0 rounded-[var(--radius-nav)] p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-hover hover:text-foreground group-hover/cp:opacity-100"
          title="Restore the working tree to this checkpoint"
          @click.stop="restoreTarget = cp"
        >
          <PhArrowUUpLeft :size="12" />
        </button>
      </div>
    </div>
    <!-- Browser: one per workspace, outside the v-if chain and kept mounted, so
         switching to Changes (or another project) and back doesn't reload the
         page being previewed. -->
    <BrowserPane
      v-for="id in browserWsIds"
      :key="id"
      v-show="id === wsKey && activeTab === 'browser'"
      class="min-h-0 flex-1"
    />

    <!-- Terminal: same kept-mounted treatment, one per workspace, so an app running
         in it keeps running when you switch projects and back. -->
    <XTerm
      v-for="id in terminalWsIds"
      :key="id"
      v-show="id === wsKey && activeTab === 'terminal'"
      :pty-id="terminalPtyByWs[id]"
      :cwd="terminalCwdByWs[id]"
      class="min-h-0 flex-1"
    />
    <div v-if="activeTab === 'terminal' && !terminalWsIds.includes(wsKey)" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>


    <!-- Restore confirm — overwrites files on disk, so it always asks first -->
    <Teleport to="body">
      <div class="fixed inset-0 z-[100] flex items-center justify-center bg-base/60" v-if="restoreTarget" @click.self="restoreTarget = null">
        <div class="flex w-[430px] flex-col gap-3 rounded-[var(--radius-card)] border border-border bg-panel p-6">
          <h3 class="text-sm font-semibold text-foreground">Restore checkpoint “{{ restoreTarget!.label || restoreTarget!.commit.slice(0, 7) }}”?</h3>
          <p class="text-[11.5px] leading-[1.7] text-secondary-foreground">
            Every file in this workspace goes back to how it looked at
            <strong>{{ cpTime(restoreTarget!.createdAt) }}</strong> — later edits are overwritten and files
            created since are deleted. Your commits and the staging area are untouched.
          </p>
          <p class="text-[11.5px] leading-[1.7] text-secondary-foreground">
            The current state is saved as a new checkpoint first, so this is undoable.
          </p>
          <p v-if="restoreError" class="whitespace-pre-wrap break-words text-[11px] text-destructive">{{ restoreError }}</p>
          <div class="flex justify-end gap-2">
            <button class="flex items-center gap-[5px] rounded-[var(--radius-chip)] border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-muted-foreground hover:text-foreground" @click="restoreTarget = null">Cancel</button>
            <button class="flex items-center gap-[5px] rounded-[var(--radius-chip)] border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" :disabled="restoreBusy" @click="confirmRestore">
              {{ restoreBusy ? "Restoring…" : "Restore" }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, inject, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import {
  PhFiles, PhGitBranch, PhGitCommit,
  PhArrowClockwise, PhWarning, PhX, PhArrowUpRight,
  PhArrowUp, PhArrowDown, PhCaretRight, PhCaretLeft,
  PhClockCounterClockwise, PhArrowUUpLeft, PhArrowsOutSimple, PhSparkle, PhPlus, PhGlobe, PhTerminal, PhRobot,
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit } from "@/stores/git";
import { useFileTreeStore } from "@/stores/fileTree";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useSubagentsStore } from "@/stores/subagents";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { activeChatIdFor, childrenOf } from "@/stores/chatTree";
import { subAgentViewTarget, nextSubAgentView } from "@/lib/subAgentView";
import { perform } from "@/lib/controlBridge";
import { displayStatus, type Phase } from "@/runtime/displayStatus";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import { statusLabel, type TermStatus } from "@/lib/terminalStatus";
import { spinnerFrame } from "@/lib/spinner";
import FileTreeNode from "./FileTreeNode.vue";
import { useAutoRefresh } from "@/composables/useAutoRefresh";
import { useContainerQuery } from "@/composables/useContainerQuery";
import AutoRefreshButton from "./AutoRefreshButton.vue";
import PullRequestsPanel from "./PullRequestsPanel.vue";
import ManagerPanel from "./ManagerPanel.vue";
import AgentChat from "./AgentChat.vue";
import DiffView from "./DiffView.vue";
import CommitPushMenu from "./CommitPushMenu.vue";
import BrowserPane from "./BrowserPane.vue";
import XTerm from "./XTerm.vue";
import WorkspacePulseSurface from "./WorkspacePulseSurface.vue";
import ExtensionNativeSurface from "./ExtensionNativeSurface.vue";
import { initPtyCounter, nextPtyId } from "@/lib/ptyId";
import { useExtensionSurfaces } from "@/composables/useExtensionSurfaces";

const props = withDefaults(defineProps<{ cwd: string; workspaceId?: number; isGit?: boolean; open?: boolean }>(), { isGit: true, open: true });
const emit = defineEmits<{ openPanel: []; closePanel: []; openProjectConfig: []; managerOpen: [] }>();
const git = useGitStore();
const fileTree = useFileTreeStore();
const chats = useClaudeChatsStore();
const subagents = useSubagentsStore();
const terminalTabs = useTerminalTabsStore();
const { surfaces: extensionSurfaces, load: loadExtensionSurfaces } = useExtensionSurfaces();
// RightPanel is one shared instance across all workspaces (App.vue doesn't
// key it per workspace), so which surfaces are open / which tab is active
// must be tracked per workspace, not as one global ref — otherwise switching
// projects shows the other project's open tabs.
const NO_WS = -1;
type DiffScope = { kind: "workspace" } | { kind: "branch" } | { kind: "turn"; checkpointId: number };
interface WsUiState { openedTabIds: string[]; activeTab: string | null; diffScope: DiffScope; openChildId: number | null }
const wsUiStates = reactive<Record<number, WsUiState>>({});
const wsKey = computed(() => props.workspaceId ?? NO_WS);

// Task-tool invocations, narrowed to the thread that's actually open (was:
// every chat in the workspace) — both this and the Sub-agents list below
// answer "what did THIS thread delegate".
const subagentList = computed(() => {
  if (!activeChatId.value) return [];
  return subagents.forChats([activeChatId.value]);
});
function chatTitle(chatId: number): string {
  return chats.sessions.find((s) => s.id === chatId)?.title ?? `Chat ${chatId}`;
}
// A Task-tool row names a chat this thread spawned through Claude's own Task
// tool. Those are real chats but not chat-tree children, so they have no
// SubAgentHost instance to teleport — opening one as a tab is still right.
function openSubagentChat(chatId: number) {
  if (!props.workspaceId) return;
  // A chat-tree child belongs to the panel; only a Task-tool chat becomes a tab.
  if (chats.sessions.find((s) => s.id === chatId)?.parentChatId) {
    openChild(chatId);
    return;
  }
  terminalTabs.openChat(props.workspaceId, chatId);
}
function subagentTime(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// ── Sub-agents section (chat-tree children of the active thread) ──────────────
// The thread on screen is whichever TAB is active, not `claudeChats.activeByWs`
// — that slot is only ever written by ManagerPanel, so opening a chat from the
// Sidebar or a route left it pointing at nothing and this panel said "open a
// chat to see its sub-agents" while a chat was open. The tabs mirror already
// carries `chatId` for chat tabs (Terminal.vue is its writer), so it is the
// same source of truth the user is actually looking at.
const activeChatId = computed(() =>
  activeChatIdFor(terminalTabs.tabsByWs, terminalTabs.activeByWs, props.workspaceId),
);
const childList = computed(() => (activeChatId.value ? childrenOf(chats.sessions, activeChatId.value) : []));

// Which child the panel is showing, per workspace (so switching
// projects doesn't carry a detail view over).
const openChildId = computed<number | null>({
  get: () => wsUi(wsKey.value).openChildId,
  set: (v) => { wsUi(wsKey.value).openChildId = v; },
});

// subAgentViewTarget tells SubAgentHost which child NOT to render, because
// this panel is rendering it. `flush: "post"` so the host gives the child up
// only once this panel's own AgentChat has been patched in — handing over in
// the same tick would unmount the host's instance before the panel's exists.
watch(openChildId, (v) => { subAgentViewTarget.value = v; }, { flush: "post" });
/** The open child's session — the panel renders its AgentChat itself. */
const openChildSession = computed(() =>
  openChildId.value === null ? undefined : chats.sessions.find((s) => s.id === openChildId.value),
);

function openChild(id: number) {
  openChildId.value = id;
}
// A stale target pointing at a child of a workspace nobody is looking at
// must not leave it teleported into this (possibly different) workspace's
// slot — called on the back button, on leaving the agents surface, on the
// panel closing, and on a workspace switch, below.
function closeChildDetail() {
  if (openChildId.value !== null) openChildId.value = null;
}

// Closing a sub-agent removes it. `remove()` stops its process, drops its
// stream session and deletes the row (Go cascades any of its own children),
// so the detail view has to let go of it first or it would sit on an id that
// no longer exists.
const closeChildId = ref<number | null>(null);
function askCloseChild(id: number) {
  closeChildId.value = id;
}
async function confirmCloseChild() {
  const id = closeChildId.value;
  closeChildId.value = null;
  if (id === null) return;
  if (openChildId.value === id) closeChildDetail();
  await chats.remove(id);
}

// Phase per child, straight off the bus — the same event Terminal.vue's
// applyPhase listens to for tabs. First frontend consumer of phase-chat:.
//
// Unlike AgentChat.vue's own subagentPhase (safe because a transcript's list
// of spawn rows only ever grows), childList shrinks on every chat and
// workspace switch — so listeners are torn down when a child leaves the
// list, not just on RightPanel's own unmount (which never happens; it is
// mounted once for the app's lifetime).
const childPhase = reactive<Record<number, TermStatus>>({});
const phaseUnsubs = new Map<number, () => void>();
// Ids whose phase has already arrived LIVE. The seed below is a round trip, so
// it can resolve after the first `phase-chat:` event for the same child and put
// a stale snapshot back over a newer state; this is what makes the seed lose.
const phaseFromEvent = new Set<number>();
watch(childList, (list) => {
  const liveIds = new Set(list.map((c) => c.id));
  for (const [id, un] of phaseUnsubs) {
    if (liveIds.has(id)) continue;
    un();
    phaseUnsubs.delete(id);
    phaseFromEvent.delete(id);
    delete childPhase[id];
  }
  const fresh = list.filter((c) => !phaseUnsubs.has(c.id));
  if (fresh.length) {
    // PhaseStore only emits on a CHANGE (phasestore.go), so a child whose
    // phase already flipped to running/done before this panel subscribed
    // would otherwise sit on the "idle" placeholder until its next
    // transition. Seed it from the server's current state instead of guessing.
    invoke<ShellSnapshotData>("shell_snapshot").then((snap) => {
      for (const child of fresh) {
        const phase = snap.phases[`chat:${child.id}`];
        if (phase && !phaseFromEvent.has(child.id)) childPhase[child.id] = displayStatus(phase, 0, true);
      }
    });
  }
  for (const child of fresh) {
    childPhase[child.id] = childPhase[child.id] ?? "idle";
    listen<Phase>(`phase-chat:${child.id}`, (ev) => {
      phaseFromEvent.add(child.id);
      childPhase[child.id] = ev.payload ? displayStatus(ev.payload, 0, true) : "idle";
    }).then((un) => {
      // The child may have left the list again while listen() was still
      // resolving — don't resurrect a subscription for one already torn down.
      if (!childList.value.some((c) => c.id === child.id)) { un(); return; }
      phaseUnsubs.set(child.id, un);
    });
  }
}, { immediate: true });
onBeforeUnmount(() => phaseUnsubs.forEach((un) => un()));

// The open child must never survive a thread switch, or point at a child
// that no longer exists in the store (deleted directly, or cascade-deleted
// with its parent thread) — see nextSubAgentView's own doc for the three
// rules. Runs on every activeChatId/childList change; closeChildDetail()
// below covers the other close triggers (back button, leaving the surface,
// panel closing, workspace switching).
let prevActiveChatIdForView: number | null = activeChatId.value;
watch([activeChatId, childList], ([cur, list]) => {
  openChildId.value = nextSubAgentView(openChildId.value, cur, prevActiveChatIdForView, list.map((c) => c.id));
  prevActiveChatIdForView = cur;
});

// Manual spawn dialog — the app has no window.prompt() anywhere else
// (a Wails window has no native prompt chrome), so this follows Sidebar.vue's
// own small rename-dialog pattern instead of introducing a browser dialog.
const spawnDialogOpen = ref(false);
const spawnTask = ref("");
function openSpawnDialog() {
  if (!activeChatId.value || !props.workspaceId) return;
  spawnTask.value = "";
  spawnDialogOpen.value = true;
}
async function confirmSpawnDialog() {
  const task = spawnTask.value.trim();
  if (!task || !activeChatId.value || !props.workspaceId) return;
  spawnDialogOpen.value = false;
  // Same door as the agent's own spawn verb: one implementation, two callers.
  await perform("spawn", { task, target: "chat", parent_chat_id: activeChatId.value, cwd: props.cwd });
}

/** Open a sub-agent chat's detail view from outside the panel (a transcript
 *  row's click handler in AgentChat.vue) — shows the panel, opens the Sub-agents
 *  surface, and drives the same slot the list's own row click does. `workspaceId`
 *  is the CHILD's workspace, not necessarily whichever one is currently active. */
function openSubagent(chatId: number, workspaceId: number) {
  const state = wsUi(workspaceId);
  if (!state.openedTabIds.includes("agents")) state.openedTabIds.push("agents");
  state.activeTab = "agents";
  // No subAgentViewTarget here: the watcher above carries it across once the
  // surface and the slot have actually rendered. Setting it now would be the
  // same too-early Teleport the watcher exists to avoid — and this path is
  // the worse one, because it may also be switching the surface to "agents".
  state.openChildId = chatId;
}
function wsUi(id: number): WsUiState {
  return (wsUiStates[id] ??= { openedTabIds: [], activeTab: null, diffScope: { kind: "workspace" }, openChildId: null });
}
const activeTab = computed<string | null>({
  get: () => wsUi(wsKey.value).activeTab,
  set: (v) => { wsUi(wsKey.value).activeTab = v; },
});
const openedTabIds = computed<string[]>({
  get: () => wsUi(wsKey.value).openedTabIds,
  set: (v) => { wsUi(wsKey.value).openedTabIds = v; },
});

watch(activeTab, (cur, prev) => { if (prev === "agents" && cur !== "agents") closeChildDetail(); });
watch(() => props.open, (open) => { if (!open) closeChildDetail(); });
watch(wsKey, (_cur, old) => {
  if (old !== undefined && old !== NO_WS && wsUiStates[old]?.openChildId != null) wsUiStates[old].openChildId = null;
  subAgentViewTarget.value = null;
});

const diffScope = computed<DiffScope>({
  get: () => wsUi(wsKey.value).diffScope,
  set: (v) => { wsUi(wsKey.value).diffScope = v; },
});
const diffScopeKey = computed(() => {
  const s = diffScope.value;
  return s.kind === "turn" ? `turn:${s.checkpointId}` : s.kind;
});
const scopedDiff = ref("");
const scopedDiffLoading = ref(false);
// Branch base is auto-detected per workspace (git.ts has no default-branch
// concept), so it lives here rather than in a store; a manual pick overrides
// detection until the workspace changes.
const branchBase = ref("");
const manualBranchBase = ref("");
const showHistory = ref(false);
const activeTerm = inject<() => any>('activeTerm', () => undefined);

const panelEl = ref<HTMLElement | null>(null);
// sm=220: show tab labels; md=320: show inline diff; lg=440: not used yet
const cq = useContainerQuery(panelEl, { sm: 220, md: 320, lg: 440 });

async function openAllDiffInTab(staged: boolean) {
  const diff = await git.fetchAllDiff(staged);
  if (!diff) return;
  activeTerm()?.openDiffInTab(staged ? "Staged changes" : "Unstaged changes", staged, diff);
}

async function openCommitDiff(c: GitCommit) {
  const out = await invoke<{ stdout: string; stderr: string; code: number }>("run_git", {
    cwd: props.cwd,
    args: ["show", c.hash],
  });
  if (out.code !== 0 || !out.stdout) return;
  activeTerm()?.openDiffInTab(`${c.shortHash} ${c.subject}`, false, out.stdout);
}

// Keep Git surfaces visible even before a repository exists. Changes already
// explains that state and offers Git Init; hiding Diff made the surface picker
// look incomplete for newly opened folders.
const tabs = computed(() => {
  const all = [
    { id: "git", label: "Changes", icon: PhGitBranch, description: "Inspect, stage and commit workspace changes." },
    { id: "pull-requests", label: "Pull requests", icon: PhGitBranch, description: "Check pull requests for this workspace." },
    { id: "explorer", label: "Files", icon: PhFiles, description: "Browse the current workspace." },
    { id: "diff", label: "Diff", icon: PhGitCommit, description: "Review the complete workspace diff." },
    { id: "history", label: "Checkpoints", icon: PhClockCounterClockwise, description: "Review and restore agent-turn snapshots." },
    { id: "manager", label: "Manager", icon: PhSparkle, description: "Plan and coordinate agent work for this project." },
    { id: "agents", label: "Sub-agents", icon: PhRobot, description: "Sub-agents spawned by this chat." },
    { id: "browser", label: "Browser", icon: PhGlobe, description: "Preview a dev server without leaving Burrow." },
    { id: "terminal", label: "Terminal", icon: PhTerminal, description: "Run shell commands next to your changes." },
  ];
  return [...all, ...extensionSurfaces.value];
});

const activeExtensionSurface = computed(() =>
  extensionSurfaces.value.find((surface) => surface.tabId === activeTab.value),
);

// One scratch terminal (and its cwd) per workspace, kept mounted forever once
// opened — like the main Terminal.vue leaves — so an app running in it keeps
// running when you switch to another project and back.
const terminalPtyByWs = reactive<Record<number, number>>({});
const terminalCwdByWs = reactive<Record<number, string>>({});
const terminalWsIds = computed(() => Object.keys(terminalPtyByWs).map(Number));
const terminalPtyStarts = new Map<number, Promise<void>>();

// The main terminal restores its saved/daemon sessions asynchronously. The RP
// can be opened before that restore has advanced the shared id counter, which
// used to let it reuse a live id. The daemon treats that as an attach, so the
// fresh RP xterm had no shell output and looked like a permanently blank pane.
async function ensureTerminalPty(id: number) {
  if (id in terminalPtyByWs) return;
  const running = terminalPtyStarts.get(id);
  if (running) return running;

  const start = (async () => {
    const sessions = await invoke<Array<{ pty_id: number }>>("list_pty_sessions").catch(() => []);
    // Keep the retired Mission Control id range from pushing ordinary tabs up.
    const maxLiveId = sessions.reduce(
      (max, session) => session.pty_id < 1_000_000 ? Math.max(max, session.pty_id) : max,
      0,
    );
    initPtyCounter(maxLiveId);
    // Another open may have completed while the daemon request was in flight.
    if (id in terminalPtyByWs) return;
    terminalPtyByWs[id] = nextPtyId();
    terminalCwdByWs[id] = props.cwd;
  })();
  terminalPtyStarts.set(id, start);
  try {
    await start;
  } finally {
    terminalPtyStarts.delete(id);
  }
}

// Same idea for the browser preview: one BrowserPane per workspace, kept
// mounted so its URL/history survives switching projects.
const browserWsIds = ref<number[]>([]);
function ensureBrowserPane(id: number) {
  if (!browserWsIds.value.includes(id)) browserWsIds.value.push(id);
}

const openedTabs = computed(() => openedTabIds.value
  .map((id) => tabs.value.find((tab) => tab.id === id))
  .filter((tab): tab is NonNullable<typeof tab> => Boolean(tab)));

async function openSurface(id: string) {
  if (!props.open) emit("openPanel");
  // Allocate the PTY before activating the surface. Otherwise Vue can mount
  // XTerm with an id that has not been checked against the daemon yet.
  if (id === "terminal") await ensureTerminalPty(wsKey.value);
  if (!openedTabIds.value.includes(id)) openedTabIds.value.push(id);
  activeTab.value = id;
  if (id === "browser") ensureBrowserPane(wsKey.value);
  if (id === "manager") emit("managerOpen");
}

function closeSurface(id: string) {
  if (id === "terminal") {
    const workspaceId = wsKey.value;
    const ptyId = terminalPtyByWs[workspaceId];
    // Closing the RP Terminal means close it, not merely hide a live scratch
    // shell forever. Remove it from the render list first so its XTerm listener
    // is disposed; the daemon kill then makes the PTY unrecoverably gone.
    if (ptyId !== undefined) {
      delete terminalPtyByWs[workspaceId];
      delete terminalCwdByWs[workspaceId];
      void invoke("kill_pty", { id: ptyId }).catch((error) =>
        console.warn("[right-panel] could not close terminal", error),
      );
    }
  }
  openedTabIds.value = openedTabIds.value.filter((openedId) => openedId !== id);
  if (activeTab.value === id) activeTab.value = openedTabs.value[0]?.id ?? null;
}

function showSurfacePicker() {
  activeTab.value = null;
}

function openManager() {
  openSurface("manager");
}

function openGitTab() {
  openSurface("git");
}

defineExpose({ openManager, openGitTab, openSubagent });

// --- Checkpoints (History tab) ---
interface Checkpoint {
  id: number;
  cwd: string;
  ptyId: string;
  label: string;
  commit: string;
  tree: string;
  createdAt: number;
}

const checkpoints = ref<Checkpoint[]>([]);
const restoreTarget = ref<Checkpoint | null>(null);
const restoreError = ref("");
const restoreBusy = ref(false);

async function loadCheckpoints() {
  if (!props.cwd) return (checkpoints.value = []);
  checkpoints.value = await invoke<Checkpoint[]>("list_checkpoints", { cwd: props.cwd, limit: 50 });
}

function cpTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// What has changed in the workspace since this checkpoint was taken.
async function openCheckpointDiff(cp: Checkpoint) {
  const diff = await invoke<string>("checkpoint_diff", { cwd: props.cwd, commit: cp.commit });
  if (!diff.trim()) return;
  activeTerm()?.openDiffInTab(`Since “${cp.label || cp.commit.slice(0, 7)}”`, false, diff);
}

// Same diff, but shown in the panel's own Diff tab (as "Turn N") instead of a
// new terminal tab.
function openCheckpointDiffInPanel(cp: Checkpoint) {
  diffScope.value = { kind: "turn", checkpointId: cp.id };
  openSurface("diff");
}

async function confirmRestore() {
  if (!restoreTarget.value) return;
  restoreBusy.value = true;
  restoreError.value = "";
  try {
    await invoke("restore_checkpoint", { cwd: props.cwd, commit: restoreTarget.value.commit });
    restoreTarget.value = null;
    await loadCheckpoints();
    git.refresh(true);
  } catch (e) {
    restoreError.value = String(e);
  } finally {
    restoreBusy.value = false;
  }
}

watch([activeTab, () => props.cwd], () => { if (activeTab.value === "history") loadCheckpoints(); });
watch([activeTab, () => props.cwd], () => {
  if (activeTab.value !== "diff") return;
  loadCheckpoints(); // needed for the Turn N options, not just the Checkpoints tab
  loadScopedDiff();
});

// checkpoints are listed newest-first; turn numbers count up from the oldest,
// so "Turn 1" is stable as new turns land instead of shifting every time.
const numberedCheckpoints = computed(() =>
  checkpoints.value.map((cp, i) => ({ cp, turn: checkpoints.value.length - i })));

function onDiffScopeChange(v: string) {
  diffScope.value = v.startsWith("turn:")
    ? { kind: "turn", checkpointId: Number(v.slice(5)) }
    : { kind: v as "workspace" | "branch" };
  if (diffScope.value.kind === "branch") manualBranchBase.value = "";
  loadScopedDiff();
}

function pickBranchBase(b: string) {
  if (!b) return;
  manualBranchBase.value = b;
  loadScopedDiff();
}

async function loadScopedDiff() {
  if (!props.cwd) { scopedDiff.value = ""; return; }
  const scope = diffScope.value;
  scopedDiffLoading.value = true;
  try {
    if (scope.kind === "workspace") {
      const [unstaged, staged] = await Promise.all([git.fetchAllDiff(false), git.fetchAllDiff(true)]);
      scopedDiff.value = [
        unstaged && "# Unstaged changes\n" + unstaged,
        staged && "# Staged changes\n" + staged,
      ].filter(Boolean).join("\n\n");
    } else if (scope.kind === "branch") {
      if (manualBranchBase.value) {
        const out = await invoke<{ stdout: string; code: number }>("run_git", { cwd: props.cwd, args: ["diff", `${manualBranchBase.value}...HEAD`] });
        branchBase.value = manualBranchBase.value;
        scopedDiff.value = out.code === 0 ? out.stdout : "";
      } else {
        branchBase.value = await invoke<string>("branch_diff_base", { cwd: props.cwd });
        if (!branchBase.value) { scopedDiff.value = ""; await git.fetchBranches(); }
        else scopedDiff.value = await invoke<string>("branch_diff", { cwd: props.cwd });
      }
    } else {
      const cp = checkpoints.value.find((c) => c.id === scope.checkpointId);
      scopedDiff.value = cp ? await invoke<string>("checkpoint_diff", { cwd: props.cwd, commit: cp.commit }) : "";
    }
  } finally {
    scopedDiffLoading.value = false;
  }
}

watch(() => props.cwd, (p) => {
  if (p) {
    git.setCwd(p);
    fileTree.loadRoot(p);
  } else {
    fileTree.clearTree();
  }
}, { immediate: true });

// --- Auto-refresh: window focus + configurable interval ---
// Runs regardless of which tab is active — the titlebar's commit/push
// button reads git.hasWorkingTreeChanges too, and it stayed stale (only
// updating once someone opened the Changes tab) if this only refreshed
// while that tab was already the one showing.
function autoRefresh() {
  if (props.cwd && !document.hidden) {
    git.refresh(true);
  }
}

const ar = useAutoRefresh(autoRefresh, "burrow-git-refresh-interval");

function onFocus() { autoRefresh(); }
function onVisible() { if (!document.hidden) autoRefresh(); }

watch(activeTab, (t) => { if (t === "git") autoRefresh(); });

onMounted(() => {
  loadExtensionSurfaces();
  window.addEventListener("focus", onFocus);
  document.addEventListener("visibilitychange", onVisible);
});

onBeforeUnmount(() => {
  window.removeEventListener("focus", onFocus);
  document.removeEventListener("visibilitychange", onVisible);
});
</script>

<style scoped>
.git-progress-bar::after {
  content: "";
  position: absolute;
  top: 0; left: 0;
  height: 100%;
  width: 40%;
  border-radius: 2px;
  background: var(--accent);
  animation: git-indeterminate 1.1s ease-in-out infinite;
}
@keyframes git-indeterminate {
  0%   { left: -40%; }
  100% { left: 100%; }
}

.commit-input::-webkit-scrollbar { display: none; }
</style>
