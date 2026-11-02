<template>
  <div class="flex h-full flex-col overflow-hidden bg-base text-xs" :data-size="props.size">

    <!-- Top bar: title + workspace selector + actions (GitPaw toolbar: ghost buttons, no borders) -->
    <div class="flex h-[38px] shrink-0 items-center gap-1 border-b border-border bg-panel px-2">
      <!-- Workspace selector -->
      <div class="flex h-6 min-w-0 items-center gap-1 rounded-md bg-foreground/[0.05] py-0 pl-[7px] pr-1.5">
        <PhFolder :size="11" class="shrink-0 text-muted-foreground" />
        <Select v-model="selectedWsIdModel" :options="workspaceOptions" class="h-5 min-w-0 border-0 bg-transparent px-0 py-0 font-sans text-[11px] text-foreground" />
      </div>

      <!-- Branch chip -->
      <button
        class="gp-btn max-w-[200px] !text-foreground"
        :class="showBranchDropdown && 'bg-foreground/[0.08]'"
        @click="toggleBranchDropdown"
        :title="`Branch: ${git.branch}`"
        :disabled="!!git.error"
      >
        <PhGitBranch :size="12" class="shrink-0" style="color:var(--accent)" />
        <span class="truncate">{{ git.branch || "—" }}</span>
        <span v-if="git.behind > 0" class="shrink-0 tabular-nums text-warning">↓{{ git.behind }}</span>
        <span v-if="git.ahead > 0" class="shrink-0 tabular-nums text-success">↑{{ git.ahead }}</span>
        <PhCaretDown :size="9" class="shrink-0 text-muted-foreground transition-transform" :class="showBranchDropdown && 'rotate-180'" />
      </button>

      <div class="flex-1" />

      <!-- Network actions -->
      <button class="gp-btn" :disabled="git.fetching || git.loading" @click="git.fetch()" title="Fetch">
        <PhArrowsClockwise :size="13" :class="git.fetching && 'animate-spin'" />
        Fetch
      </button>
      <button
        v-if="!git.error && git.hasUpstream && git.behind > 0"
        class="gp-btn"
        :disabled="git.pulling || git.pushing"
        @click="git.pull()"
        title="Pull (ff-only)"
      >
        <PhArrowDown :size="13" :class="git.pulling && 'animate-spin'" />
        Pull <span class="tabular-nums text-warning">{{ git.behind }}</span>
      </button>
      <button
        v-if="!git.error"
        class="gp-btn"
        :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
        @click="git.push()"
        :title="git.hasUpstream ? 'Push' : 'Publish branch'"
      >
        <PhArrowUp :size="13" :class="git.pushing && 'animate-spin'" />
        {{ git.hasUpstream ? "Push" : "Publish" }}
        <span v-if="git.ahead > 0" class="tabular-nums text-success">{{ git.ahead }}</span>
      </button>
      <span v-if="!git.error && git.hasWorkingTreeChanges" class="mx-0.5 h-4 w-px bg-border" />
      <button
        v-if="!git.error && git.hasWorkingTreeChanges"
        class="gp-btn"
        :disabled="git.loading"
        @click="git.stashPush()"
        title="Stash all changes (incl. untracked)"
      >
        <PhArchiveBox :size="13" />
        Stash
      </button>
      <button class="gp-icon-btn" :disabled="git.loading" @click="git.refresh()" title="Refresh">
        <PhArrowClockwise :size="13" :class="git.loading && 'animate-spin'" />
      </button>
      <button v-if="!isPopout && canOpenWindows()" class="gp-icon-btn" @click="popout()" title="Pop out to window">
        <PhArrowSquareOut :size="13" />
      </button>
    </div>

    <!-- Branch dropdown -->
    <template v-if="showBranchDropdown">
      <div class="fixed inset-0 z-[99]" @click="showBranchDropdown = false" />
      <div class="gp-menu absolute left-[120px] top-[40px] z-[100] max-h-[260px] min-w-[220px] overflow-y-auto">
        <div
          v-for="b in git.branches"
          :key="b"
          class="gp-menu-item group/br cursor-pointer gap-[7px]"
          :class="b === git.branch && '!text-foreground'"
          @click="selectBranch(b)"
        >
          <PhCheck v-if="b === git.branch" :size="11" class="w-3 shrink-0 text-accent" />
          <span v-else class="w-3 shrink-0" />
          <span class="flex-1 truncate">{{ b }}</span>
          <span v-if="b !== git.branch && git.branch" class="hidden shrink-0 gap-0.5 group-hover/br:flex">
            <button class="gp-btn-sm" :title="`Merge ${b} into ${git.branch}`" @click.stop="askMerge(b)">Merge</button>
            <button class="gp-btn-sm" :title="`Rebase ${git.branch} onto ${b}`" @click.stop="askRebase(b)">Rebase</button>
          </span>
        </div>
        <div class="gp-menu-sep" />
        <div v-if="!newBranchMode" class="gp-menu-item cursor-pointer gap-[7px]" @click.stop="startNewBranch">
          <PhPlus :size="11" class="w-3" /> New branch…
        </div>
        <div v-else class="px-1.5 py-1">
          <input
            ref="newBranchInputRef"
            v-model="newBranchName"
            class="gp-input h-7 w-full px-2 font-mono text-xs"
            placeholder="branch-name"
            @keydown.enter.prevent="confirmNewBranch"
            @keydown.esc="newBranchMode = false"
          />
        </div>
      </div>
    </template>

    <!-- Push/pull/commit/generate progress bar -->
    <div v-if="gitBusy || git.pulling" class="flex shrink-0 items-center gap-2.5 border-b border-border px-3 py-1">
      <div class="gp-progress-bar relative h-0.5 flex-1 overflow-hidden rounded-sm bg-border" />
      <span class="text-[11px] text-muted-foreground">{{ git.generating ? "Generating message…" : git.committing ? "Committing…" : git.pushing ? "Pushing…" : "Pulling…" }}</span>
    </div>

    <!-- Stopped merge / rebase / cherry-pick / revert -->
    <div
      v-if="!git.error && git.opState"
      class="shrink-0 border-b px-3 py-2.5"
      :class="git.conflicted.length ? 'border-destructive/30 bg-destructive/10' : 'border-accent/25 bg-accent/10'"
    >
      <div class="flex items-center gap-1.5 text-[12px] font-medium text-foreground">
        <PhWarning v-if="git.conflicted.length" :size="13" class="shrink-0 text-destructive" />
        {{ git.conflicted.length ? `${opLabel} stopped on a conflict` : `${opLabel} in progress` }}
      </div>
      <div class="mt-0.5 text-[11px] text-muted-foreground">
        {{ git.conflicted.length
          ? `Resolve ${git.conflicted.length} file${git.conflicted.length === 1 ? "" : "s"}, then continue.`
          : "Everything is resolved. Continue when ready." }}
      </div>
      <div class="mt-2 flex gap-1.5">
        <button class="gp-solid h-6 px-2" :disabled="git.conflicted.length > 0" @click="git.continueOp()">Continue {{ opLabel.toLowerCase() }}</button>
        <button class="gp-btn" @click="git.abortOp()">Abort</button>
      </div>
    </div>

    <!-- No repo error -->
    <div v-if="git.error" class="flex flex-1 flex-col items-center justify-center gap-3 text-[13px] text-muted-foreground">
      <PhWarning :size="22" />
      <span>Not a git repository</span>
      <button class="gp-solid h-7 px-3" :disabled="git.loading" @click="git.gitInit()">
        <PhGitBranch :size="12" /> Git Init
      </button>
    </div>

    <!-- Main two-column layout -->
    <div v-else class="flex flex-1 overflow-hidden">

      <!-- LEFT: file lists + commit -->
      <div class="flex w-[280px] shrink-0 grow-0 basis-[280px] flex-col overflow-hidden border-r border-border bg-panel">

        <!-- COMMIT FILES MODE -->
        <template v-if="selectedCommit">
          <div class="flex min-w-0 shrink-0 items-center gap-1.5 border-b border-border py-1.5 pl-1.5 pr-2.5">
            <button class="gp-icon-btn" @click="clearSelectedCommit" title="Back to changes">
              <PhArrowLeft :size="12" />
            </button>
            <span class="min-w-0 flex-1 truncate text-[12px] font-medium text-foreground" :title="selectedCommit.subject">{{ selectedCommit.subject }}</span>
            <span class="shrink-0 font-mono text-[10.5px] text-muted-foreground">{{ selectedCommit.shortHash }}</span>
          </div>
          <div class="flex-1 overflow-y-auto p-1.5">
            <div v-if="commitFilesLoading" class="gp-empty">Loading…</div>
            <div v-else-if="!commitFiles.length" class="gp-empty">Empty commit.</div>
            <div
              v-for="f in commitFiles"
              :key="f.path"
              class="gp-row"
              :class="commitDiff?.filePath === f.path && 'gp-row-active'"
              @click="openCommitFileDiff(f)"
            >
              <span class="gp-badge" :class="badge(f.status).cls">{{ badge(f.status).label }}</span>
              <span class="gp-path" :title="f.path"><span class="text-foreground">{{ baseName(f.path) }}</span><span v-if="dirName(f.path)" class="ml-1.5 text-muted-foreground">{{ dirName(f.path) }}</span></span>
              <span class="shrink-0 text-[10.5px] tabular-nums">
                <span v-if="f.added" class="text-success">+{{ f.added }}</span>
                <span v-if="f.deleted" class="ml-1.5 text-destructive">−{{ f.deleted }}</span>
              </span>
            </div>
          </div>
        </template>

        <!-- NORMAL MODE: staged + changes + commit area -->
        <template v-else>
          <div class="flex-1 overflow-y-auto p-1.5">

            <!-- CONFLICTS -->
            <div v-if="git.conflicted.length" class="mb-2">
              <div class="gp-section">
                <span class="font-medium text-destructive">Conflicts</span>
                <span class="tabular-nums text-muted-foreground">{{ git.conflicted.length }}</span>
              </div>
              <div
                v-for="f in git.conflicted"
                :key="'c:' + f.path"
                class="gp-row"
                :class="activeDiff?.path === f.path && 'gp-row-active'"
                @click="toggleDiff(f.path, false)"
              >
                <span class="gp-badge text-destructive">!</span>
                <span class="gp-path" :title="f.path"><span class="text-foreground">{{ baseName(f.path) }}</span><span v-if="dirName(f.path)" class="ml-1.5 text-muted-foreground">{{ dirName(f.path) }}</span></span>
                <div class="gp-row-actions">
                  <button class="gp-btn-sm" title="Keep my version (ours)" @click.stop="git.resolveWith(f.path, 'ours')">Ours</button>
                  <button class="gp-btn-sm" title="Take their version (theirs)" @click.stop="git.resolveWith(f.path, 'theirs')">Theirs</button>
                  <button class="gp-row-btn" title="Mark resolved (git add)" @click.stop="git.markResolved(f.path)"><PhCheck :size="12" /></button>
                </div>
              </div>
            </div>

            <!-- STAGED -->
            <div class="mb-2">
              <div class="gp-section">
                <span class="font-medium text-success">Staged</span>
                <span class="tabular-nums text-muted-foreground">{{ git.staged.length }}</span>
                <span class="flex-1" />
                <button v-if="git.staged.length" class="gp-btn-sm" @click="git.unstageAll()"><PhCaretDoubleUp :size="11" /> Unstage all</button>
              </div>
              <div v-if="git.staged.length === 0" class="gp-empty">Nothing staged yet.</div>
              <div
                v-for="f in git.staged"
                :key="'s:' + f.path"
                class="gp-row"
                :class="activeDiff?.path === f.path && activeDiff?.staged && 'gp-row-active'"
                @click="toggleDiff(f.path, true)"
              >
                <span class="gp-badge" :class="badge(f.x).cls">{{ badge(f.x).label }}</span>
                <span class="gp-path" :title="f.path"><span class="text-foreground">{{ baseName(f.path) }}</span><span v-if="dirName(f.path)" class="ml-1.5 text-muted-foreground">{{ dirName(f.path) }}</span></span>
                <div class="gp-row-actions">
                  <button class="gp-row-btn" @click.stop="git.unstageFile(f.path)" title="Unstage"><PhMinus :size="12" /></button>
                </div>
              </div>
            </div>

            <!-- CHANGES -->
            <div class="mb-2">
              <div class="gp-section">
                <span class="font-medium text-muted-foreground">Changes</span>
                <span class="tabular-nums text-muted-foreground">{{ changesCount }}</span>
                <span class="flex-1" />
                <button v-if="changesCount" class="gp-btn-sm" @click="git.stageAll()"><PhCaretDoubleDown :size="11" /> Stage all</button>
              </div>
              <div v-if="!changesCount" class="gp-empty">Working tree clean.</div>
              <div
                v-for="f in git.unstaged"
                :key="'u:' + f.path"
                class="gp-row"
                :class="activeDiff?.path === f.path && !activeDiff?.staged && 'gp-row-active'"
                @click="toggleDiff(f.path, false)"
              >
                <span class="gp-badge" :class="badge(f.y).cls">{{ badge(f.y).label }}</span>
                <span class="gp-path" :title="f.path"><span class="text-foreground">{{ baseName(f.path) }}</span><span v-if="dirName(f.path)" class="ml-1.5 text-muted-foreground">{{ dirName(f.path) }}</span></span>
                <div class="gp-row-actions">
                  <button class="gp-row-btn hover:!text-destructive" @click.stop="git.discardFile(f.path)" title="Discard"><PhArrowCounterClockwise :size="12" /></button>
                  <button class="gp-row-btn" @click.stop="git.stageFile(f.path)" title="Stage"><PhPlus :size="12" /></button>
                </div>
              </div>
              <div
                v-for="f in git.untracked"
                :key="'t:' + f.path"
                class="gp-row"
              >
                <span class="gp-badge" :class="badge('?').cls">{{ badge('?').label }}</span>
                <span class="gp-path" :title="f.path"><span class="text-foreground">{{ baseName(f.path) }}</span><span v-if="dirName(f.path)" class="ml-1.5 text-muted-foreground">{{ dirName(f.path) }}</span></span>
                <div class="gp-row-actions">
                  <button class="gp-row-btn" @click.stop="git.stageFile(f.path)" title="Stage"><PhPlus :size="12" /></button>
                </div>
              </div>
            </div>

            <!-- STASHES -->
            <div v-if="git.stashes.length" class="mb-2">
              <div class="gp-section">
                <span class="font-medium text-muted-foreground">Stashes</span>
                <span class="tabular-nums text-muted-foreground">{{ git.stashes.length }}</span>
              </div>
              <div v-for="st in git.stashes" :key="st.ref" class="gp-row" :title="`${st.ref}\n${st.message}`">
                <PhArchiveBox :size="12" class="w-3 shrink-0 text-muted-foreground" />
                <span class="min-w-0 flex-1 truncate text-foreground">{{ st.message }}</span>
                <span class="shrink-0 text-[10.5px] tabular-nums text-muted-foreground gp-row-meta">{{ st.relTime }}</span>
                <div class="gp-row-actions">
                  <button class="gp-btn-sm" title="Apply and drop" @click="git.stashPop(st.ref)">Pop</button>
                  <button class="gp-btn-sm" title="Apply, keep the stash" @click="git.stashApply(st.ref)">Apply</button>
                  <button class="gp-row-btn hover:!text-destructive" title="Drop" @click="askDropStash(st.ref)"><PhTrash :size="12" /></button>
                </div>
              </div>
            </div>
          </div>

          <!-- Commit area (pinned to bottom of left column) -->
          <div class="flex shrink-0 flex-col gap-2 border-t border-border p-2.5">
            <div class="relative">
              <textarea
                v-model="git.commitMsg"
                class="gp-commit-input gp-input w-full resize-none px-2.5 py-2 pr-7 font-sans text-[12px] leading-relaxed"
                placeholder="Commit message…"
                rows="3"
                @keydown.ctrl.enter="git.commit()"
                @keydown.meta.enter="git.commit()"
              />
              <button
                class="gp-icon-btn absolute right-1 top-1 hover:!text-accent"
                :disabled="!git.hasWorkingTreeChanges || git.generating"
                title="Generate commit message (stages all changes first if needed)"
                @click="git.generateCommitMessage()"
              >
                <PhSparkle :size="13" :class="git.generating && 'animate-pulse'" />
              </button>
            </div>
            <div v-if="git.generateError" class="text-[10.5px] text-destructive">{{ git.generateError }}</div>
            <div class="flex flex-wrap gap-1">
              <button
                v-for="t in COMMIT_TYPES"
                :key="t"
                class="h-5 rounded-[5px] px-1.5 font-mono text-[10px] text-muted-foreground transition-colors hover:bg-foreground/[0.06] hover:text-foreground"
                :class="activeType === t && '!bg-accent/[0.14] !text-accent'"
                @click="applyType(t)"
              >{{ t }}</button>
            </div>
            <div class="flex gap-1.5">
              <button
                class="gp-btn flex-1 justify-center bg-foreground/[0.05]"
                :disabled="!git.hasWorkingTreeChanges || gitBusy"
                @click="commitAndPush()"
              >
                <PhArrowUp :size="12" :class="(git.generating || git.committing || git.pushing) && 'animate-spin'" />
                {{ git.generating ? "Generating…" : git.committing ? "Committing…" : git.pushing ? "Pushing…" : "Commit & Push" }}
              </button>
              <button
                class="gp-solid h-6 flex-1 px-2"
                :disabled="!git.hasWorkingTreeChanges || gitBusy"
                @click="git.commit()"
                title="⌘↵ (auto-generates a message if the box is empty)"
              >
                <PhGitCommit :size="12" :class="(git.generating || git.committing) && 'animate-spin'" />
                {{ git.generating ? "Generating…" : git.committing ? "Committing…" : `Commit${git.staged.length ? ` (${git.staged.length})` : ""}` }}
                <span class="font-sans text-[10px] opacity-60">⌘↵</span>
              </button>
            </div>
          </div>
        </template>
      </div>

      <!-- RIGHT: diff + history -->
      <div class="flex min-w-0 flex-1 flex-col overflow-hidden">

        <!-- Diff view: file diff -->
        <div v-if="git.diffFile" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="gp-diff-head">
            <span class="gp-path text-[12px]" :title="git.diffFile"><span class="text-foreground">{{ baseName(git.diffFile) }}</span><span v-if="dirName(git.diffFile)" class="ml-1.5 text-muted-foreground">{{ dirName(git.diffFile) }}</span></span>
            <span class="shrink-0 rounded px-1.5 text-[10px]" :class="git.diffStaged ? 'bg-success/15 text-success' : 'bg-foreground/[0.06] text-muted-foreground'">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
            <button class="gp-icon-btn" @click="git.clearDiff(); activeDiff = null" title="Close diff"><PhX :size="12" /></button>
          </div>
          <div v-if="hunks.length" class="min-h-0 flex-1 overflow-auto p-2">
            <div v-for="(h, i) in hunks" :key="git.diffFile + ':' + i + ':' + h.header" class="gp-hunk">
              <div class="gp-hunk-head">
                <span class="min-w-0 flex-1 truncate font-mono text-[10.5px] text-muted-foreground" :title="h.header">{{ hunkTitle(h.header) }}</span>
                <span class="shrink-0 text-[10.5px] tabular-nums"><span class="text-success">+{{ h.adds }}</span> <span class="ml-1 text-destructive">−{{ h.dels }}</span></span>
                <template v-if="git.diffStaged">
                  <button class="gp-btn-sm" @click="git.unstageHunk(h.patch)"><PhMinus :size="11" /> Unstage</button>
                </template>
                <template v-else>
                  <button class="gp-btn-sm hover:!text-destructive" @click="askDiscardHunk(h.patch)"><PhArrowCounterClockwise :size="11" /> Discard</button>
                  <button class="gp-btn-sm" @click="git.stageHunk(h.patch)"><PhPlus :size="11" /> Stage</button>
                </template>
              </div>
              <DiffView :diff="h.patch" :diff-key="git.diffFile + ':' + i" no-header />
            </div>
          </div>
          <DiffView v-else :diff="git.diff" :diff-key="git.diffFile" />
        </div>

        <!-- Diff view: commit diff -->
        <div v-else-if="commitDiff" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="gp-diff-head">
            <span class="gp-path text-[12px]" :title="commitDiff.subject"><span class="text-foreground">{{ baseName(commitDiff.subject) }}</span><span v-if="dirName(commitDiff.subject)" class="ml-1.5 text-muted-foreground">{{ dirName(commitDiff.subject) }}</span></span>
            <button class="gp-icon-btn" @click="commitDiff = null" title="Close diff"><PhX :size="12" /></button>
          </div>
          <DiffView :diff="commitDiff.text" :diff-key="commitDiff.hash + (commitDiff.filePath ?? '')" />
        </div>

        <div v-else class="flex flex-1 items-center justify-center px-6 text-center text-[12px] text-muted-foreground">
          Select a file to see its diff.
        </div>

        <!-- History (commit graph) -->
        <div class="flex shrink-0 flex-col border-t border-border bg-panel" :class="props.size === 'lg' ? 'max-h-[45%]' : 'max-h-[260px]'">
          <div class="flex h-7 shrink-0 cursor-pointer select-none items-center gap-1.5 px-2.5" @click="showHistory = !showHistory">
            <PhCaretRight :size="10" class="text-muted-foreground transition-transform" :class="showHistory && 'rotate-90'" />
            <span class="text-[11.5px] font-medium text-muted-foreground">History</span>
            <span v-if="git.ahead > 0" class="text-[11px] tabular-nums text-success">{{ git.ahead }} unpushed</span>
          </div>
          <div v-if="showHistory" class="flex-1 overflow-y-auto pb-1">
            <div v-if="graph.length === 0" class="gp-empty px-3">No commits yet.</div>
            <div
              v-for="(c, i) in graph"
              :key="c.hash"
              class="flex cursor-pointer items-center pr-2.5 hover:bg-foreground/[0.035]"
              :class="selectedCommit?.hash === c.hash && '!bg-accent/[0.14]'"
              :style="{ height: ROW_H + 'px' }"
              :title="c.subject + '\n' + c.author + ' · ' + c.relTime"
              @click="openCommitDiff(c)"
              @contextmenu.prevent="openCommitMenu(c, $event)"
            >
              <svg :width="graphW" :height="ROW_H" class="shrink-0 overflow-visible">
                <g fill="none" stroke-width="1.8" stroke-linecap="round">
                  <path v-for="(e, k) in layout.rows[i].up" :key="'u' + k" :d="curve(laneX(e.from), 0, laneX(e.to), e.from === e.to && e.to !== layout.rows[i].lane ? MID + 0.5 : MID)" :stroke="laneColor(e.color)" />
                  <path v-for="(e, k) in layout.rows[i].down" :key="'d' + k" :d="curve(laneX(e.from), e.from === e.to && e.from !== layout.rows[i].lane ? MID - 0.5 : MID, laneX(e.to), ROW_H)" :stroke="laneColor(e.color)" />
                </g>
                <!-- HEAD wears a paw (GitPaw's mark); merges are hollow -->
                <g v-if="c.hash === headHash" :fill="laneColor(layout.rows[i].color)">
                  <circle :cx="laneX(layout.rows[i].lane)" :cy="MID" r="8.5" opacity="0.18" />
                  <ellipse :cx="laneX(layout.rows[i].lane)" :cy="MID + 1.5" rx="3.6" ry="3.1" stroke="var(--bg-panel)" stroke-width="1" />
                  <circle v-for="([dx, dy], k) in PAW_TOES" :key="k" :cx="laneX(layout.rows[i].lane) + dx" :cy="MID + dy" r="1.4" />
                </g>
                <circle v-else-if="layout.rows[i].isMerge" :cx="laneX(layout.rows[i].lane)" :cy="MID" r="3.8" fill="var(--bg-panel)" :stroke="laneColor(layout.rows[i].color)" stroke-width="2" />
                <circle v-else :cx="laneX(layout.rows[i].lane)" :cy="MID" r="4.3" :fill="laneColor(layout.rows[i].color)" stroke="var(--bg-panel)" stroke-width="2" />
              </svg>
              <span class="flex min-w-0 flex-1 items-center gap-1.5">
                <span class="flex min-w-0 max-w-[60%] shrink items-center gap-1 overflow-hidden empty:hidden">
                  <span
                    v-for="r in refPills(c.refs)"
                    :key="r.label"
                    class="flex h-[18px] shrink-0 items-center gap-1 rounded-[5px] border px-1.5 text-[10.5px] font-medium"
                    :class="r.head && 'font-semibold'"
                    :style="r.kind === 'tag' ? tagStyle : r.kind === 'remote' ? remoteStyle : pillStyle(laneColor(layout.rows[i].color))"
                  >
                    <span v-if="r.head" class="size-1.5 rounded-full bg-accent" />
                    <PhTag v-if="r.kind === 'tag'" :size="10" />
                    <PhCloud v-else-if="r.kind === 'remote'" :size="10" class="opacity-70" />
                    {{ r.label }}
                    <PhCloud v-if="r.synced" :size="10" class="opacity-70" />
                  </span>
                </span>
                <span
                  class="min-w-0 flex-1 truncate text-[11.5px]"
                  :class="[
                    selectedCommit?.hash === c.hash || c.hash === headHash || i < git.ahead ? 'text-foreground' : 'text-foreground/85',
                    layout.rows[i].isMerge && '!text-muted-foreground',
                  ]"
                >{{ c.subject }}</span>
              </span>
              <span v-if="props.size === 'lg'" class="ml-3 max-w-[120px] shrink-0 truncate text-[10.5px] text-muted-foreground">{{ c.author }}</span>
              <span class="ml-2 shrink-0 text-[10.5px] tabular-nums text-muted-foreground">{{ c.relTime }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Commit context menu -->
    <template v-if="commitMenu">
      <div class="fixed inset-0 z-[999]" @click="commitMenu = null" @contextmenu.prevent="commitMenu = null" />
      <div class="gp-menu fixed z-[1000] w-[210px]" :style="{ left: commitMenu.x + 'px', top: commitMenu.y + 'px' }">
        <div class="truncate px-2.5 pb-1 pt-0.5 font-mono text-[10.5px] text-muted-foreground">{{ commitMenu.c.shortHash }} · {{ commitMenu.c.subject }}</div>
        <template v-for="b in menuBranches" :key="b">
          <button class="gp-menu-item" @click="menu(() => git.switchBranch(b))">Checkout <b class="ml-1 font-mono">{{ b }}</b></button>
          <button v-if="git.branch" class="gp-menu-item" @click="menu(() => askMerge(b))">Merge <b class="mx-1 font-mono">{{ b }}</b> into current</button>
          <button v-if="git.branch" class="gp-menu-item" @click="menu(() => askRebase(b))">Rebase current onto <b class="ml-1 font-mono">{{ b }}</b></button>
          <div class="gp-menu-sep" />
        </template>
        <button class="gp-menu-item" @click="menu((c) => git.checkoutCommit(c.hash))">Checkout (detached)</button>
        <button class="gp-menu-item" @click="menu((c) => askName('New branch', `From ${c.shortHash}`, (n) => git.branchAt(n, c.hash)))">New branch here…</button>
        <button class="gp-menu-item" @click="menu((c) => askName('New tag', `On ${c.shortHash}`, (n) => git.tagAt(n, c.hash)))">Tag here…</button>
        <div class="gp-menu-sep" />
        <button class="gp-menu-item" @click="menu((c) => git.cherryPick(c.hash))">Cherry-pick</button>
        <button class="gp-menu-item" @click="menu((c) => git.revertCommit(c.hash))">Revert</button>
        <div class="gp-menu-sep" />
        <button class="gp-menu-item" @click="menu((c) => askReset(c, 'soft'))">Reset here (soft)</button>
        <button class="gp-menu-item" @click="menu((c) => askReset(c, 'mixed'))">Reset here (mixed)</button>
        <button class="gp-menu-item gp-menu-danger" @click="menu((c) => askReset(c, 'hard'))">Reset here (hard)…</button>
        <div class="gp-menu-sep" />
        <button class="gp-menu-item" @click="menu((c) => copyHash(c.hash))">Copy hash</button>
      </div>
    </template>

    <!-- Confirm / name prompt (window.confirm is a no-op in the Wails webview) -->
    <div v-if="dialog" class="fixed inset-0 z-[1001] flex items-start justify-center bg-black/45 pt-[20vh]" @click.self="dialog = null" @keydown.esc="dialog = null">
      <div class="flex w-[380px] max-w-[calc(100%-24px)] flex-col rounded-xl border border-border bg-[var(--bg-dropdown)] p-5 shadow-[0_24px_60px_rgba(0,0,0,0.55)]">
        <h3 class="text-[14px] font-semibold text-foreground">{{ dialog.title }}</h3>
        <p v-if="dialog.body" class="mt-2 text-[12.5px] leading-relaxed text-muted-foreground">{{ dialog.body }}</p>
        <input
          v-if="dialog.input"
          v-model="dialog.value"
          v-focus
          class="gp-input mt-3 h-8 w-full px-2.5 font-mono text-[12.5px]"
          @keydown.enter.prevent="runDialog"
        />
        <div class="mt-5 flex justify-end gap-2">
          <button class="gp-btn h-7 px-2.5" @click="dialog = null">Cancel</button>
          <button
            v-focus="!dialog.input"
            class="gp-solid h-7 px-3"
            :class="dialog.danger && 'gp-solid-danger'"
            :disabled="dialog.input && !dialog.value.trim()"
            @click="runDialog"
          >{{ dialog.confirm }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhGitBranch, PhGitCommit, PhFolder,
  PhArrowUp, PhArrowDown, PhArrowLeft, PhArrowClockwise, PhArrowsClockwise,
  PhArrowSquareOut,
  PhCaretDown, PhCaretRight,
  PhWarning, PhX, PhCheck, PhPlus, PhSparkle,
  PhArchiveBox, PhTrash, PhMinus, PhArrowCounterClockwise, PhTag, PhCloud,
  PhCaretDoubleUp, PhCaretDoubleDown,
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit, type GraphCommit } from "@/stores/git";
import { layoutGraph } from "@/lib/gitGraph";
import { splitHunks } from "@/lib/gitHunks";
import { useWorkspaceStore } from "@/stores/workspace";
import { canOpenWindows, isSecondaryWindow, openWindow } from "@/lib/windows";
import DiffView from "./DiffView.vue";
import { Select } from "@/components/ui/select";

// "lg" is the full-screen Git dialog; the default is the narrow right panel.
// initialWsId: the workspace a popped-out Git window was opened for.
const props = withDefaults(defineProps<{ size?: "sm" | "lg"; initialWsId?: number }>(), { size: "sm" });

const git = useGitStore();
const wsStore = useWorkspaceStore();
const isPopout = isSecondaryWindow();
const gitBusy = computed(() => git.generating || git.committing || git.pushing);

async function popout() {
  const id = selectedWsId.value;
  await openWindow("git", `/window/git${id != null ? `?ws=${id}` : ""}`, "Git", 1100, 780);
}
const selectedWsId = ref<number | null>(props.initialWsId ?? wsStore.active?.id ?? null);
const selectedWsIdModel = computed<string | undefined>({ get: () => selectedWsId.value?.toString(), set: (id) => { selectedWsId.value = id ? Number(id) : null; } });
// Worktrees belong in here too: they are separate checkouts with their own
// status, and listing only topLevel meant that whenever the active workspace
// was a worktree the picker showed nothing, the lookups below found nothing,
// and the panel kept displaying the previously selected repo's git.
const workspaceOptions = computed(() =>
  wsStore.topLevel.flatMap((w) => [
    { value: String(w.id), label: w.name },
    ...(wsStore.worktreesByParent[w.id] ?? []).map((t) => ({
      value: String(t.id),
      label: `${w.name} / ${t.worktree_branch || t.name}`,
    })),
  ]),
);
const selectedWs = computed(() => wsStore.workspaces.find((w) => w.id === selectedWsId.value) ?? null);
const showBranchDropdown = ref(false);
const newBranchMode = ref(false);
const newBranchName = ref("");
const newBranchInputRef = ref<HTMLInputElement | null>(null);
const showHistory = ref(true);
const activeDiff = ref<{ path: string; staged: boolean } | null>(null);
const commitDiff = ref<{ hash: string; subject: string; text: string; filePath?: string } | null>(null);

interface CommitFile { path: string; status: string; added: number; deleted: number }
const selectedCommit = ref<GitCommit | null>(null);
const commitFiles = ref<CommitFile[]>([]);
const commitFilesLoading = ref(false);

const COMMIT_TYPES = ["feat", "fix", "docs", "chore", "refactor", "test", "style"] as const;

const changesCount = computed(() => git.unstaged.length + git.untracked.length);

const activeType = computed(() => {
  const m = git.commitMsg.match(/^([a-z]+)(\([^)]+\))?:\s/);
  return m ? m[1] : null;
});

// When workspace selection changes, point git store at that workspace
watch(selectedWsId, () => {
  if (selectedWs.value) git.setCwd(selectedWs.value.path);
}, { immediate: true });

// When active workspace changes externally, follow it
watch(() => wsStore.active?.id, (id) => {
  if (id != null) selectedWsId.value = id;
});

function toggleBranchDropdown() {
  showBranchDropdown.value = !showBranchDropdown.value;
  if (showBranchDropdown.value) newBranchMode.value = false;
}

async function selectBranch(name: string) {
  showBranchDropdown.value = false;
  if (name === git.branch) return;
  await git.switchBranch(name);
}

async function startNewBranch() {
  newBranchMode.value = true;
  newBranchName.value = "";
  await nextTick();
  newBranchInputRef.value?.focus();
}

async function confirmNewBranch() {
  const name = newBranchName.value.trim();
  if (!name) return;
  newBranchMode.value = false;
  showBranchDropdown.value = false;
  await git.createBranch(name);
}

function toggleDiff(path: string, staged: boolean) {
  commitDiff.value = null;
  if (activeDiff.value?.path === path && activeDiff.value?.staged === staged) {
    activeDiff.value = null;
    git.clearDiff();
  } else {
    activeDiff.value = { path, staged };
    git.showDiff(path, staged);
  }
}

function applyType(t: string) {
  const current = git.commitMsg;
  const typePrefix = /^[a-z]+(\([^)]+\))?:\s/;
  if (typePrefix.test(current)) {
    git.commitMsg = current.replace(typePrefix, `${t}: `);
  } else {
    git.commitMsg = `${t}: ${current}`;
  }
}

async function commitAndPush() {
  await git.push(await git.commit());
}

async function openCommitDiff(c: GitCommit) {
  const w = selectedWs.value;
  if (!w) return;

  if (selectedCommit.value?.hash === c.hash) {
    selectedCommit.value = null;
    commitFiles.value = [];
    commitDiff.value = null;
    return;
  }

  selectedCommit.value = c;
  commitDiff.value = null;
  activeDiff.value = null;
  git.clearDiff();
  commitFilesLoading.value = true;
  try {
    const [nsOut, numOut] = await Promise.all([
      invoke<{ stdout: string; code: number }>("run_git", {
        cwd: w.path, args: ["show", "--name-status", "--format=", c.hash],
      }),
      invoke<{ stdout: string; code: number }>("run_git", {
        cwd: w.path, args: ["show", "--numstat", "--format=", c.hash],
      }),
    ]);
    const nsLines = nsOut.stdout.split("\n").filter((l) => l.trim() && l.includes("\t"));
    const numLines = numOut.stdout.split("\n").filter((l) => l.trim() && l.includes("\t"));
    commitFiles.value = nsLines.map((line, i) => {
      const nsParts = line.split("\t");
      const status = nsParts[0][0];
      const path = nsParts[nsParts.length - 1];
      const numLine = numLines[i] || "";
      const numParts = numLine.split("\t");
      const added = parseInt(numParts[0]) || 0;
      const deleted = parseInt(numParts[1]) || 0;
      return { path, status, added, deleted };
    });
  } finally {
    commitFilesLoading.value = false;
  }
}

async function openCommitFileDiff(f: CommitFile) {
  if (!selectedCommit.value) return;
  const w = selectedWs.value;
  if (!w) return;
  if (commitDiff.value?.filePath === f.path) { commitDiff.value = null; return; }
  const out = await invoke<{ stdout: string; code: number }>("run_git", {
    cwd: w.path, args: ["show", selectedCommit.value.hash, "--", f.path],
  });
  commitDiff.value = { hash: selectedCommit.value.hash, subject: f.path, text: out.stdout, filePath: f.path };
}

function clearSelectedCommit() {
  selectedCommit.value = null;
  commitFiles.value = [];
  commitDiff.value = null;
}

// ── hunks ── conflicted files show a combined diff: no per-hunk actions there
const hunks = computed(() => {
  const f = git.diffFile;
  if (!f || git.conflicted.some((c) => c.path === f)) return [];
  return splitHunks(git.diff);
});

// ── commit graph ──
const ROW_H = 24;
const MID = ROW_H / 2;
const LANE_W = 12;
const MAX_LANES = 12;
const PAW_TOES = [[-3.8, -1.6], [-1.4, -4], [1.4, -4], [3.8, -1.6]] as const;
const LANE_COLORS = [
  "var(--accent)", "var(--green)", "var(--yellow)", "var(--blue)",
  "color-mix(in oklab, var(--accent) 55%, var(--red))", "color-mix(in oklab, var(--green) 50%, var(--blue))",
];
const laneColor = (i: number) => LANE_COLORS[i % LANE_COLORS.length];
const laneX = (i: number) => 9 + Math.min(i, MAX_LANES) * LANE_W;
const curve = (x1: number, y1: number, x2: number, y2: number) =>
  x1 === x2 ? `M${x1} ${y1}V${y2}` : `M${x1} ${y1}C${x1} ${(y1 + y2) / 2} ${x2} ${(y1 + y2) / 2} ${x2} ${y2}`;

const graph = ref<GraphCommit[]>([]);
const layout = computed(() => layoutGraph(graph.value));
const graphW = computed(() => laneX(Math.min(layout.value.maxLanes, MAX_LANES)) + 4);
const headHash = computed(() => graph.value.find((c) => c.refs.some((r) => r === "HEAD" || r.startsWith("HEAD -> ")))?.hash);
// git.log is re-read on every refresh, so it is the "history may have moved" signal
let graphGen = 0;
watch(() => [git.cwd, git.log], async () => {
  const g = ++graphGen;
  const rows = await git.fetchGraph();
  if (g === graphGen) graph.value = rows;
}, { immediate: true });

interface RefPill { label: string; kind: "local" | "remote" | "tag"; head: boolean; synced: boolean }
// local + its same-named remote collapse into one pill with a cloud
function refPills(refs: string[]): RefPill[] {
  const locals = new Map<string, RefPill>();
  const remotes: string[] = [];
  const tags: RefPill[] = [];
  const known = new Set(git.branches);
  for (const r of refs) {
    if (r.startsWith("tag: ")) tags.push({ label: r.slice(5), kind: "tag", head: false, synced: false });
    else if (r.startsWith("HEAD -> ")) locals.set(r.slice(8), { label: r.slice(8), kind: "local", head: true, synced: false });
    else if (r === "HEAD" || r.endsWith("/HEAD")) continue;
    else if (known.has(r)) locals.set(r, { label: r, kind: "local", head: false, synced: false });
    else remotes.push(r);
  }
  const out: RefPill[] = [];
  for (const r of remotes) {
    const l = locals.get(r.split("/").slice(1).join("/"));
    if (l) l.synced = true;
    else out.push({ label: r, kind: "remote", head: false, synced: false });
  }
  return [...locals.values(), ...out, ...tags];
}
const pillStyle = (color: string) => ({
  color,
  borderColor: `color-mix(in oklab, ${color} 45%, transparent)`,
  background: `color-mix(in oklab, ${color} 13%, transparent)`,
});
const tagStyle = { color: "var(--yellow)", borderColor: "transparent", background: "color-mix(in oklab, var(--yellow) 15%, transparent)" };
const remoteStyle = { color: "var(--text-muted)", borderColor: "var(--border)", background: "transparent" };

// ── commit context menu ──
const commitMenu = ref<{ c: GraphCommit; x: number; y: number } | null>(null);
function openCommitMenu(c: GraphCommit, e: MouseEvent) {
  // keep the 210px-wide, ~420px-tall menu on screen
  commitMenu.value = { c, x: Math.min(e.clientX, window.innerWidth - 220), y: Math.min(e.clientY, window.innerHeight - 440) };
}
const menuBranches = computed(() =>
  commitMenu.value ? refPills(commitMenu.value.c.refs).filter((r) => r.kind === "local" && r.label !== git.branch).map((r) => r.label) : [],
);
function menu(fn: (c: GraphCommit) => unknown) {
  const c = commitMenu.value?.c;
  commitMenu.value = null;
  if (c) fn(c);
}

// ── confirm / prompt dialog ──
const dialog = ref<{ title: string; body?: string; input?: boolean; value: string; confirm: string; danger?: boolean; run: (v: string) => unknown } | null>(null);
function runDialog() {
  const d = dialog.value;
  if (!d || (d.input && !d.value.trim())) return;
  dialog.value = null;
  d.run(d.value.trim());
}
const askName = (title: string, body: string, run: (name: string) => unknown) =>
  (dialog.value = { title, body, input: true, value: "", confirm: "Create", run });
const askMerge = (b: string) => {
  showBranchDropdown.value = false;
  dialog.value = { title: `Merge ${b} into ${git.branch}?`, body: "Creates a merge commit unless it can fast-forward.", value: "", confirm: "Merge", run: () => git.mergeBranch(b) };
};
const askRebase = (b: string) => {
  showBranchDropdown.value = false;
  // ponytail: "has upstream" ≈ "already shared"; an exact check would compare with the upstream's merge-base
  const shared = git.hasUpstream;
  dialog.value = {
    title: `Rebase ${git.branch} onto ${b}?`,
    body: shared
      ? `${git.branch} is already pushed. Rebasing rewrites its history and the next push will need --force.`
      : `Replays the commits of ${git.branch} on top of ${b}.`,
    value: "", confirm: "Rebase", danger: shared, run: () => git.rebaseOnto(b),
  };
};
const RESET_BODY = {
  soft: "Commits after this point go back to staged changes. Nothing is lost.",
  mixed: "Commits after this point come back as unstaged changes. Nothing is lost.",
  hard: "Commits after this point AND every uncommitted change are thrown away. This cannot be undone.",
} as const;
const askReset = (c: GraphCommit, mode: "soft" | "mixed" | "hard") =>
  (dialog.value = { title: `Reset ${git.branch || "HEAD"} to ${c.shortHash} (${mode})?`, body: RESET_BODY[mode], value: "", confirm: "Reset", danger: mode === "hard", run: () => git.resetTo(c.hash, mode) });
const askDropStash = (ref: string) =>
  (dialog.value = { title: `Drop ${ref}?`, body: "The stashed changes will be gone.", value: "", confirm: "Drop", danger: true, run: () => git.stashDrop(ref) });
const askDiscardHunk = (patch: string) =>
  (dialog.value = { title: "Discard this hunk?", body: "The changes in this block are lost.", value: "", confirm: "Discard", danger: true, run: () => git.discardHunk(patch) });

const copyHash = (h: string) => navigator.clipboard.writeText(h);
const opLabel = computed(() => ({ merge: "Merge", rebase: "Rebase", "cherry-pick": "Cherry-pick", revert: "Revert" })[git.opState ?? "merge"]);
const vFocus = { mounted: (el: HTMLElement, b: { value?: boolean }) => { if (b.value !== false) el.focus(); } };

// GitPaw's StatusBadge: one letter, colour per kind ("?" reads as N = new)
const BADGES: Record<string, [string, string]> = {
  M: ["M", "text-warning"], A: ["A", "text-success"], "?": ["N", "text-success"], D: ["D", "text-destructive"],
  R: ["R", "text-accent"], C: ["C", "text-accent"], T: ["T", "text-accent"], U: ["!", "text-destructive"],
};
const badge = (s: string) => { const [label, cls] = BADGES[s] ?? [s, "text-muted-foreground"]; return { label, cls }; };
// GitPaw's FilePath: file name first, its directory dimmed after it
const baseName = (p: string) => p.slice(p.lastIndexOf("/") + 1);
const dirName = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf("/")));
const hunkTitle = (h: string) => h.replace(/^@@[^@]*@@\s?/, "") || h;
</script>

<style>
/* Global resets — needed when GitPanel is mounted as the root component in the
   popout window (where App.vue's :root block is not loaded). Harmless when
   embedded in the main app since the values are identical. */
:root {
  --bg-base: #0d0d0d;
  --bg-panel: #111111;
  --bg-hover: #1a1a1a;
  --border: #2a2a2a;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent: #3b82f6;
  --green: #22c55e;
  --yellow: #eab308;
  --red: #ef4444;
  --font-mono: "JetBrains Mono", "Fira Code", "Cascadia Code", monospace;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
body {
  background-color: var(--bg-base);
  color: var(--text-primary);
  font-family: var(--font-ui);
  margin: 0;
  overflow: hidden;
  user-select: none;
  -webkit-font-smoothing: antialiased;
}
</style>

<style scoped>
/* Every type size in this panel is a hard-coded px utility, tuned for the ~300px
   right panel. `size="lg"` (the full-screen Git dialog) re-maps those same
   utilities one step up instead of duplicating 80 class attributes. */
[data-size="lg"] { font-size: 13px; }
[data-size="lg"] :deep(:is(.text-\[9px\], .text-\[9\.5px\])) { font-size: 11px; }
[data-size="lg"] :deep(.text-\[10px\]) { font-size: 12px; }
[data-size="lg"] :deep(:is(.text-\[11px\], .text-\[11\.5px\])) { font-size: 13px; }
[data-size="lg"] :deep(.text-\[13px\]) { font-size: 15px; }
[data-size="lg"] :deep(.text-xs) { font-size: 13px; }

.gp-progress-bar::after {
  content: "";
  position: absolute;
  inset-block: 0;
  left: -40%;
  width: 40%;
  background: var(--accent);
  animation: progress-slide 1.1s ease-in-out infinite;
}
@keyframes progress-slide { to { left: 100%; } }

.gp-commit-input::-webkit-scrollbar { display: none; }

/* GitPaw look, mapped onto Burrow theme tokens */
.gp-btn {
  display: inline-flex; align-items: center; gap: 5px; height: 24px; padding: 0 7px;
  border-radius: 6px; white-space: nowrap; font-size: 11.5px; font-weight: 500;
  color: var(--text-muted); transition: background-color .1s, color .1s;
}
.gp-btn:hover { color: var(--text-primary); background: color-mix(in oklab, var(--text-primary) 6%, transparent); }
.gp-btn:active { background: color-mix(in oklab, var(--text-primary) 9%, transparent); }
.gp-btn:disabled, .gp-icon-btn:disabled, .gp-solid:disabled { opacity: .4; pointer-events: none; }
.gp-btn-sm {
  display: inline-flex; align-items: center; gap: 3px; flex-shrink: 0; height: 20px; padding: 0 6px;
  border-radius: 5px; font-size: 10.5px; font-weight: 500; color: var(--text-muted);
}
.gp-btn-sm:hover { color: var(--text-primary); background: color-mix(in oklab, var(--text-primary) 8%, transparent); }
.gp-icon-btn {
  display: grid; place-items: center; width: 24px; height: 24px; flex-shrink: 0; border-radius: 6px; color: var(--text-muted);
}
.gp-icon-btn:hover { color: var(--text-primary); background: color-mix(in oklab, var(--text-primary) 6%, transparent); }
.gp-solid {
  display: inline-flex; align-items: center; justify-content: center; gap: 5px; white-space: nowrap;
  border-radius: 6px; font-size: 11.5px; font-weight: 600; color: var(--bg-base); background: var(--accent);
  transition: background-color .1s;
}
.gp-solid:hover { background: color-mix(in oklab, var(--accent) 88%, white); }
.gp-solid:active { background: color-mix(in oklab, var(--accent) 88%, black); }
.gp-solid-danger { background: var(--red); color: white; }
.gp-solid-danger:hover { background: color-mix(in oklab, var(--red) 88%, white); }
.gp-input {
  box-sizing: border-box; border-radius: 8px; outline: none; color: var(--text-primary);
  border: 1px solid color-mix(in oklab, var(--text-primary) 8%, transparent); background: var(--bg-base);
  transition: border-color .1s;
}
.gp-input::placeholder { color: var(--text-muted); }
.gp-input:focus { border-color: color-mix(in oklab, var(--accent) 60%, transparent); }

.gp-section { display: flex; align-items: center; gap: 7px; height: 26px; padding: 0 6px; font-size: 11px; }
.gp-empty { padding: 0 6px 4px; font-size: 11.5px; color: var(--text-muted); opacity: .8; }
.gp-row {
  display: flex; align-items: center; gap: 7px; height: 24px; padding: 0 3px 0 6px;
  border-radius: 6px; cursor: pointer; font-size: 11.5px;
}
.gp-row:hover { background: color-mix(in oklab, var(--text-primary) 4%, transparent); }
.gp-row-active, .gp-row-active:hover { background: color-mix(in oklab, var(--accent) 14%, transparent); }
.gp-row-actions { display: none; flex-shrink: 0; align-items: center; gap: 1px; }
.gp-row:hover .gp-row-actions, .gp-row-active .gp-row-actions { display: flex; }
.gp-row:hover .gp-row-meta { display: none; }
.gp-row-btn {
  display: grid; place-items: center; width: 20px; height: 20px; border-radius: 4px; color: var(--text-muted);
}
.gp-row-btn:hover { color: var(--text-primary); background: color-mix(in oklab, var(--text-primary) 10%, transparent); }
.gp-badge { width: 12px; flex-shrink: 0; text-align: center; font-family: var(--font-mono); font-size: 10.5px; font-weight: 600; }
.gp-path { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.gp-diff-head {
  display: flex; flex-shrink: 0; align-items: center; gap: 8px; height: 34px; padding: 0 6px 0 12px;
  border-bottom: 1px solid var(--border);
}
.gp-hunk {
  margin-bottom: 8px; overflow: hidden; border-radius: 8px;
  border: 1px solid color-mix(in oklab, var(--text-primary) 6%, transparent);
  background: color-mix(in oklab, black 15%, transparent);
}
.gp-hunk-head {
  position: sticky; top: 0; z-index: 10; display: flex; align-items: center; gap: 6px; height: 28px; padding: 0 4px 0 10px;
  border-bottom: 1px solid color-mix(in oklab, var(--text-primary) 5%, transparent);
  background: color-mix(in oklab, var(--text-primary) 2.5%, var(--bg-base));
}

.gp-menu {
  border-radius: 8px; padding: 4px; border: 1px solid color-mix(in oklab, var(--text-primary) 7%, transparent);
  background: color-mix(in oklab, var(--bg-dropdown) 95%, transparent); backdrop-filter: blur(20px);
  box-shadow: 0 18px 44px rgba(0,0,0,0.5);
}
.gp-menu-item {
  display: flex; align-items: center; width: 100%; height: 26px; padding: 0 9px; text-align: left; border-radius: 6px;
  font-size: 12px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.gp-menu-item:hover { background: color-mix(in oklab, var(--accent) 25%, transparent); }
.gp-menu-danger { color: var(--red); }
.gp-menu-danger:hover { background: color-mix(in oklab, var(--red) 25%, transparent); }
.gp-menu-sep { margin: 4px 0; height: 1px; background: color-mix(in oklab, var(--text-primary) 7%, transparent); }
</style>
