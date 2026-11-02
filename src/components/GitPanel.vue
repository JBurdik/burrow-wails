<template>
  <div class="flex h-full flex-col overflow-hidden bg-base text-xs" :data-size="props.size">

    <!-- Top bar: title + workspace selector + actions -->
    <div class="flex h-[38px] shrink-0 items-center gap-1.5 border-b border-border bg-panel px-3">
      <span class="shrink-0 text-[11px] font-semibold uppercase tracking-[0.05em] text-muted-foreground opacity-70">Git</span>

      <!-- Workspace selector -->
      <div class="flex h-6 items-center gap-1 rounded border border-border bg-[color-mix(in_srgb,var(--border)_25%,var(--bg-panel))] py-0 pl-[7px] pr-1.5">
        <PhFolder :size="11" class="shrink-0 text-muted-foreground" />
        <Select v-model="selectedWsIdModel" :options="workspaceOptions" class="h-5 min-w-0 border-0 bg-transparent px-0 py-0 font-sans text-[11px] text-secondary-foreground" />
      </div>

      <!-- Branch chip -->
      <button
        class="branch-chip flex h-6 max-w-[220px] items-center gap-1 rounded border border-border bg-transparent px-[7px] py-[3px] font-mono text-[11px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-40"
        :class="showBranchDropdown && 'bg-hover text-foreground'"
        @click="toggleBranchDropdown"
        :title="`Branch: ${git.branch}`"
        :disabled="!!git.error"
      >
        <PhGitBranch :size="11" class="shrink-0 text-yellow-400" style="color:var(--yellow)" />
        <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ git.branch || "—" }}</span>
        <span v-if="git.ahead > 0" class="shrink-0 text-[10px] text-success">↑{{ git.ahead }}</span>
        <span v-if="git.behind > 0" class="shrink-0 text-[10px] text-warning">↓{{ git.behind }}</span>
        <PhCaretDown :size="8" class="shrink-0 text-muted-foreground transition-transform" :class="showBranchDropdown && 'rotate-180'" />
      </button>

      <div class="flex-1" />

      <button
        v-if="!git.error && git.hasWorkingTreeChanges"
        class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30"
        :disabled="git.loading"
        @click="git.stashPush()"
        title="Stash all changes (incl. untracked)"
      >
        <PhArchiveBox :size="13" />
        Stash
      </button>

      <!-- Network actions -->
      <button class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" :disabled="git.fetching || git.loading" @click="git.fetch()" title="Fetch">
        <PhArrowsClockwise :size="13" :class="git.fetching && 'animate-spin'" />
        Fetch
      </button>
      <button
        v-if="!git.error && git.hasUpstream && git.behind > 0"
        class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30"
        :disabled="git.pulling || git.pushing"
        @click="git.pull()"
        title="Pull (ff-only)"
      >
        <PhArrowDown :size="13" :class="git.pulling && 'animate-spin'" />
        Pull <span class="rounded-lg bg-accent/20 px-1 text-[9px] font-semibold text-accent">{{ git.behind }}</span>
      </button>
      <button
        v-if="!git.error"
        class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30"
        :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
        @click="git.push()"
        :title="git.hasUpstream ? 'Push' : 'Publish branch'"
      >
        <PhArrowUp :size="13" :class="git.pushing && 'animate-spin'" />
        {{ git.hasUpstream ? "Push" : "Publish" }}
        <span v-if="git.ahead > 0" class="rounded-lg bg-accent/20 px-1 text-[9px] font-semibold text-accent">{{ git.ahead }}</span>
      </button>
      <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" :disabled="git.loading" @click="git.refresh()" title="Refresh">
        <PhArrowClockwise :size="14" :class="git.loading && 'animate-spin'" />
      </button>
      <button v-if="!isPopout" class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" @click="popout()" title="Pop out to window">
        <PhArrowSquareOut :size="14" />
      </button>
    </div>

    <!-- Branch dropdown -->
    <template v-if="showBranchDropdown">
      <div class="fixed inset-0 z-[99]" @click="showBranchDropdown = false" />
      <div class="absolute left-[240px] top-[42px] z-[100] max-h-[260px] min-w-[220px] overflow-y-auto rounded-[5px] border border-border bg-[var(--bg-dropdown)] py-[3px] shadow-[0_6px_20px_rgba(0,0,0,0.35)]">
        <div
          v-for="b in git.branches"
          :key="b"
          class="group/br flex cursor-pointer items-center gap-[7px] px-3 py-[5px] font-mono text-xs text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="b === git.branch && 'text-foreground'"
          @click="selectBranch(b)"
        >
          <PhCheck v-if="b === git.branch" :size="10" class="w-3 shrink-0 text-success" />
          <span v-else class="w-3 shrink-0 text-success" />
          <span class="flex-1 truncate">{{ b }}</span>
          <span v-if="b !== git.branch && git.branch" class="hidden shrink-0 gap-0.5 font-sans group-hover/br:flex">
            <button class="rounded px-1.5 py-px text-[10px] text-muted-foreground hover:bg-accent/15 hover:text-accent" :title="`Merge ${b} into ${git.branch}`" @click.stop="askMerge(b)">Merge</button>
            <button class="rounded px-1.5 py-px text-[10px] text-muted-foreground hover:bg-accent/15 hover:text-accent" :title="`Rebase ${git.branch} onto ${b}`" @click.stop="askRebase(b)">Rebase</button>
          </span>
        </div>
        <div class="my-[3px] h-px bg-border" />
        <div v-if="!newBranchMode" class="flex cursor-pointer items-center gap-[7px] px-3 py-[5px] text-xs text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click.stop="startNewBranch">
          <PhPlus :size="10" /> New branch…
        </div>
        <div v-else class="px-2.5 py-[5px]">
          <input
            ref="newBranchInputRef"
            v-model="newBranchName"
            class="box-border w-full rounded-[3px] border border-border bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-[7px] py-1 font-mono text-xs text-foreground outline-none focus:border-accent/50"
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
      class="shrink-0 border-b px-3 py-2"
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
        <button class="rounded border border-transparent bg-accent/80 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-accent disabled:cursor-default disabled:opacity-40" :disabled="git.conflicted.length > 0" @click="git.continueOp()">Continue {{ opLabel.toLowerCase() }}</button>
        <button class="rounded border border-border px-2.5 py-1 text-[11px] text-secondary-foreground hover:bg-hover hover:text-foreground" @click="git.abortOp()">Abort</button>
      </div>
    </div>

    <!-- No repo error -->
    <div v-if="git.error" class="flex flex-1 items-center justify-center gap-2.5 text-[13px] text-muted-foreground">
      <PhWarning :size="20" />
      <span>Not a git repository</span>
      <button class="flex items-center gap-[5px] rounded-[5px] border border-border bg-hover px-3 py-[5px] text-xs text-foreground hover:border-warning hover:bg-warning hover:text-black" :disabled="git.loading" @click="git.gitInit()">
        <PhGitBranch :size="12" /> Git Init
      </button>
    </div>

    <!-- Main two-column layout -->
    <div v-else class="flex flex-1 overflow-hidden">

      <!-- LEFT: file lists + commit -->
      <div class="flex w-[280px] shrink-0 grow-0 basis-[280px] flex-col overflow-hidden border-r border-border">

        <!-- COMMIT FILES MODE -->
        <template v-if="selectedCommit">
          <div class="flex min-w-0 shrink-0 items-center gap-1.5 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] py-1.5 pl-1.5 pr-2">
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="clearSelectedCommit" title="Back to changes">
              <PhArrowLeft :size="12" />
            </button>
            <span class="shrink-0 font-mono text-[10px] text-warning">{{ selectedCommit.shortHash }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-secondary-foreground" :title="selectedCommit.subject">{{ selectedCommit.subject }}</span>
            <span v-if="commitFiles.length" class="shrink-0 rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium text-accent">{{ commitFiles.length }}</span>
          </div>
          <div class="flex-1 overflow-y-auto py-2">
            <div v-if="commitFilesLoading" class="pt-3 px-3 text-[11px] text-muted-foreground opacity-55">Loading…</div>
            <div v-else-if="!commitFiles.length" class="pt-3 px-3 text-[11px] text-muted-foreground opacity-55">No files changed</div>
            <div
              v-for="f in commitFiles"
              :key="f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="commitDiff?.filePath === f.path && 'bg-accent/[0.09]'"
              @click="openCommitFileDiff(f)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold" :class="commitFileStatusClass(f.status)">{{ f.status }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="commitDiff?.filePath === f.path && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <span class="flex shrink-0 gap-[3px] font-mono text-[10px]">
                <span v-if="f.added" class="text-success">+{{ f.added }}</span>
                <span v-if="f.deleted" class="text-destructive">-{{ f.deleted }}</span>
              </span>
            </div>
          </div>
        </template>

        <!-- NORMAL MODE: staged + changes + commit area -->
        <template v-else>
          <div class="flex-1 overflow-y-auto py-2">

            <!-- CONFLICTS -->
            <template v-if="git.conflicted.length">
              <div class="flex items-center justify-between px-3 py-[3px]">
                <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-destructive">Conflicts <span class="rounded-lg bg-destructive/15 px-[5px] text-[10px] font-medium normal-case tracking-normal">{{ git.conflicted.length }}</span></span>
              </div>
              <div
                v-for="f in git.conflicted"
                :key="'c:' + f.path"
                class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
                :class="activeDiff?.path === f.path && 'bg-accent/[0.09]'"
                @click="toggleDiff(f.path, false)"
              >
                <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-destructive">!</span>
                <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
                <div class="hidden items-center gap-0.5 group-hover:flex">
                  <button class="rounded px-1 text-[10px] text-muted-foreground hover:bg-hover hover:text-foreground" title="Keep my version (ours)" @click.stop="git.resolveWith(f.path, 'ours')">Ours</button>
                  <button class="rounded px-1 text-[10px] text-muted-foreground hover:bg-hover hover:text-foreground" title="Take their version (theirs)" @click.stop="git.resolveWith(f.path, 'theirs')">Theirs</button>
                  <button class="rounded px-1 text-muted-foreground hover:text-success" title="Mark resolved (git add)" @click.stop="git.markResolved(f.path)"><PhCheck :size="11" /></button>
                </div>
              </div>
              <div class="mb-2.5" />
            </template>

            <!-- STAGED -->
            <div class="flex items-center justify-between px-3 py-[3px]">
              <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">Staged <span v-if="git.staged.length" class="rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ git.staged.length }}</span></span>
              <button v-if="git.staged.length" class="rounded-sm border border-border bg-transparent px-1.5 py-px text-[10px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.unstageAll()">−All</button>
            </div>
            <div v-if="git.staged.length === 0" class="px-3 pb-[5px] pt-0.5 text-[11px] text-muted-foreground opacity-55">Nothing staged</div>
            <div
              v-for="f in git.staged"
              :key="'s:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="activeDiff?.path === f.path && activeDiff?.staged && 'bg-accent/[0.09]'"
              @click="toggleDiff(f.path, true)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-success">{{ f.x }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="activeDiff?.path === f.path && activeDiff?.staged && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <button class="hidden shrink-0 rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-foreground group-hover:block" @click.stop="git.unstageFile(f.path)" title="Unstage">−</button>
            </div>

            <!-- CHANGES -->
            <div class="mt-2.5 flex items-center justify-between px-3 py-[3px]">
              <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">Changes <span v-if="changesCount" class="rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ changesCount }}</span></span>
              <button v-if="changesCount" class="rounded-sm border border-border bg-transparent px-1.5 py-px text-[10px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.stageAll()">+All</button>
            </div>
            <div v-if="!changesCount" class="px-3 pb-[5px] pt-0.5 text-[11px] text-muted-foreground opacity-55">Working tree clean</div>
            <div
              v-for="f in git.unstaged"
              :key="'u:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="activeDiff?.path === f.path && !activeDiff?.staged && 'bg-accent/[0.09]'"
              @click="toggleDiff(f.path, false)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-warning">{{ f.y }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="activeDiff?.path === f.path && !activeDiff?.staged && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <div class="hidden items-center gap-0.5 group-hover:flex">
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-success" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-destructive" @click.stop="git.discardFile(f.path)" title="Discard">✕</button>
              </div>
            </div>
            <div
              v-for="f in git.untracked"
              :key="'t:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-muted-foreground">?</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
              <div class="hidden items-center gap-0.5 group-hover:flex">
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-success" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
              </div>
            </div>

            <!-- STASHES -->
            <template v-if="git.stashes.length">
              <div class="mt-2.5 flex items-center px-3 py-[3px]">
                <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">Stashes <span class="rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ git.stashes.length }}</span></span>
              </div>
              <div v-for="st in git.stashes" :key="st.ref" class="group flex items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover" :title="`${st.ref}\n${st.message}`">
                <PhArchiveBox :size="11" class="shrink-0 text-muted-foreground" />
                <span class="flex-1 truncate text-[11px] text-secondary-foreground group-hover:text-foreground">{{ st.message }}</span>
                <span class="shrink-0 text-[10px] text-muted-foreground group-hover:hidden">{{ st.relTime }}</span>
                <div class="hidden items-center gap-0.5 group-hover:flex">
                  <button class="rounded px-1 text-[10px] text-muted-foreground hover:bg-hover hover:text-foreground" title="Apply and drop" @click="git.stashPop(st.ref)">Pop</button>
                  <button class="rounded px-1 text-[10px] text-muted-foreground hover:bg-hover hover:text-foreground" title="Apply, keep the stash" @click="git.stashApply(st.ref)">Apply</button>
                  <button class="rounded px-1 text-muted-foreground hover:text-destructive" title="Drop" @click="askDropStash(st.ref)"><PhTrash :size="11" /></button>
                </div>
              </div>
            </template>
          </div>

          <!-- Commit area (pinned to bottom of left column) -->
          <div class="flex shrink-0 flex-col gap-1.5 border-t border-border bg-panel px-3 py-[9px]">
            <div class="relative">
              <textarea
                v-model="git.commitMsg"
                class="gp-commit-input box-border w-full resize-none rounded border border-border bg-[color-mix(in_srgb,var(--border)_15%,var(--bg-panel))] px-2 py-1.5 pr-6 font-sans text-xs leading-normal text-foreground outline-none transition-colors placeholder:text-muted-foreground placeholder:opacity-60 focus:border-accent/55"
                placeholder="Commit message…"
                rows="3"
                @keydown.ctrl.enter="git.commit()"
                @keydown.meta.enter="git.commit()"
              />
              <button
                class="absolute right-1 top-1 rounded p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-accent disabled:cursor-default disabled:opacity-30"
                :disabled="!git.hasWorkingTreeChanges || git.generating"
                title="Generate commit message (stages all changes first if needed)"
                @click="git.generateCommitMessage()"
              >
                <PhSparkle :size="12" :class="git.generating && 'animate-pulse'" />
              </button>
            </div>
            <div v-if="git.generateError" class="text-[10px] text-destructive">{{ git.generateError }}</div>
            <div class="flex flex-wrap gap-[3px]">
              <button
                v-for="t in COMMIT_TYPES"
                :key="t"
                class="rounded-[3px] border border-border bg-transparent px-[5px] py-0.5 font-mono text-[9.5px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
                :class="activeType === t && '!border-accent/45 !bg-accent/[0.14] !text-accent'"
                @click="applyType(t)"
              >{{ t }}</button>
            </div>
            <div class="flex gap-1.5">
              <button
                class="flex flex-1 items-center justify-center gap-1 rounded border border-border bg-hover px-2 py-[5px] font-sans text-[11px] font-medium text-secondary-foreground transition-colors hover:bg-[color-mix(in_srgb,var(--border)_60%,var(--bg-hover))] hover:text-foreground disabled:cursor-default disabled:opacity-30"
                :disabled="!git.hasWorkingTreeChanges || gitBusy"
                @click="git.commit()"
                title="⌘↵ (auto-generates a message if the box is empty)"
              >
                <PhGitCommit :size="12" :class="(git.generating || git.committing) && 'animate-spin'" />
                {{ git.generating ? "Generating…" : git.committing ? "Committing…" : "Commit" }}
              </button>
              <button
                class="flex flex-1 items-center justify-center gap-1 rounded border border-transparent bg-accent/80 px-2 py-[5px] font-sans text-[11px] font-medium text-white transition-colors hover:bg-accent disabled:cursor-default disabled:opacity-30"
                :disabled="!git.hasWorkingTreeChanges || gitBusy"
                @click="commitAndPush()"
              >
                <PhArrowUp :size="12" :class="(git.generating || git.committing || git.pushing) && 'animate-spin'" />
                {{ git.generating ? "Generating…" : git.committing ? "Committing…" : git.pushing ? "Pushing…" : "Commit & Push" }}
              </button>
            </div>
          </div>
        </template>
      </div>

      <!-- RIGHT: diff + history -->
      <div class="flex min-w-0 flex-1 flex-col overflow-hidden">

        <!-- Diff view: file diff -->
        <div v-if="git.diffFile" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="flex shrink-0 items-center gap-2 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] px-3 py-1.5">
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground">{{ git.diffFile }}</span>
            <span class="shrink-0 text-[10px] text-muted-foreground">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.clearDiff(); activeDiff = null" title="Close diff"><PhX :size="11" /></button>
          </div>
          <div v-if="hunks.length" class="min-h-0 flex-1 overflow-auto">
            <div v-for="(h, i) in hunks" :key="git.diffFile + ':' + i + ':' + h.header" class="border-b border-border">
              <div class="sticky top-0 z-10 flex items-center gap-2 border-b border-border bg-panel px-3 py-1">
                <span class="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-foreground" :title="h.header">{{ h.header }}</span>
                <span class="shrink-0 font-mono text-[10px]"><span class="text-success">+{{ h.adds }}</span> <span class="text-destructive">−{{ h.dels }}</span></span>
                <template v-if="git.diffStaged">
                  <button class="hunk-btn" @click="git.unstageHunk(h.patch)"><PhMinus :size="10" /> Unstage</button>
                </template>
                <template v-else>
                  <button class="hunk-btn hover:!text-destructive" @click="askDiscardHunk(h.patch)"><PhArrowCounterClockwise :size="10" /> Discard</button>
                  <button class="hunk-btn" @click="git.stageHunk(h.patch)"><PhPlus :size="10" /> Stage</button>
                </template>
              </div>
              <DiffView :diff="h.patch" :diff-key="git.diffFile + ':' + i" no-header />
            </div>
          </div>
          <DiffView v-else :diff="git.diff" :diff-key="git.diffFile" />
        </div>

        <!-- Diff view: commit diff -->
        <div v-else-if="commitDiff" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="flex shrink-0 items-center gap-2 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] px-3 py-1.5">
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground">{{ commitDiff.subject }}</span>
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="commitDiff = null" title="Close diff"><PhX :size="11" /></button>
          </div>
          <DiffView :diff="commitDiff.text" :diff-key="commitDiff.hash + (commitDiff.filePath ?? '')" />
        </div>

        <div v-else class="flex flex-1 items-center justify-center gap-2 text-xs text-muted-foreground opacity-50">
          <PhArrowLeft :size="16" />
          Click a file to view diff
        </div>

        <!-- History (commit graph) -->
        <div class="flex shrink-0 flex-col border-t border-border bg-panel" :class="props.size === 'lg' ? 'max-h-[45%]' : 'max-h-[260px]'">
          <div class="flex shrink-0 cursor-pointer select-none items-center gap-[5px] px-3 py-[5px]" @click="showHistory = !showHistory">
            <PhCaretRight :size="10" class="text-muted-foreground transition-transform" :class="showHistory && 'rotate-90'" />
            <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">History</span>
            <span v-if="git.ahead > 0" class="ml-1 rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ git.ahead }} unpushed</span>
          </div>
          <div v-if="showHistory" class="flex-1 overflow-y-auto pb-1">
            <div v-if="graph.length === 0" class="px-3 py-1.5 text-[11px] text-muted-foreground opacity-55">No commits</div>
            <div
              v-for="(c, i) in graph"
              :key="c.hash"
              class="flex cursor-pointer items-center gap-2 pr-3 transition-colors hover:bg-hover"
              :class="selectedCommit?.hash === c.hash && '!bg-accent/[0.09]'"
              :style="{ height: ROW_H + 'px' }"
              :title="c.subject + '\n' + c.author + ' · ' + c.relTime"
              @click="openCommitDiff(c)"
              @contextmenu.prevent="openCommitMenu(c, $event)"
            >
              <svg :width="graphW" :height="ROW_H" class="shrink-0 overflow-visible">
                <g fill="none" stroke-width="1.6" stroke-linecap="round">
                  <path v-for="(e, k) in layout.rows[i].up" :key="'u' + k" :d="curve(laneX(e.from), 0, laneX(e.to), e.from === e.to && e.to !== layout.rows[i].lane ? MID + 0.5 : MID)" :stroke="laneColor(e.color)" />
                  <path v-for="(e, k) in layout.rows[i].down" :key="'d' + k" :d="curve(laneX(e.from), e.from === e.to && e.from !== layout.rows[i].lane ? MID - 0.5 : MID, laneX(e.to), ROW_H)" :stroke="laneColor(e.color)" />
                </g>
                <!-- HEAD wears a paw (GitPaw's mark); merges are hollow -->
                <g v-if="c.hash === headHash" :fill="laneColor(layout.rows[i].color)">
                  <circle :cx="laneX(layout.rows[i].lane)" :cy="MID" r="8" opacity="0.18" />
                  <ellipse :cx="laneX(layout.rows[i].lane)" :cy="MID + 1.4" rx="3.3" ry="2.9" stroke="var(--bg-panel)" stroke-width="1" />
                  <circle v-for="([dx, dy], k) in PAW_TOES" :key="k" :cx="laneX(layout.rows[i].lane) + dx" :cy="MID + dy" r="1.3" />
                </g>
                <circle v-else-if="layout.rows[i].isMerge" :cx="laneX(layout.rows[i].lane)" :cy="MID" r="3.5" fill="var(--bg-panel)" :stroke="laneColor(layout.rows[i].color)" stroke-width="1.8" />
                <circle v-else :cx="laneX(layout.rows[i].lane)" :cy="MID" r="4" :fill="laneColor(layout.rows[i].color)" stroke="var(--bg-panel)" stroke-width="1.8" />
              </svg>
              <span class="flex min-w-0 flex-1 items-center gap-1.5">
                <span
                  v-for="r in refPills(c.refs)"
                  :key="r.label"
                  class="flex h-[17px] shrink-0 items-center gap-1 rounded-[4px] border px-1.5 text-[10px] font-medium"
                  :style="r.kind === 'tag' ? tagStyle : r.kind === 'remote' ? remoteStyle : pillStyle(laneColor(layout.rows[i].color))"
                >
                  <span v-if="r.head" class="size-1.5 rounded-full bg-accent" />
                  <PhTag v-if="r.kind === 'tag'" :size="9" />
                  <PhCloud v-else-if="r.kind === 'remote' || r.synced" :size="9" class="opacity-70" />
                  {{ r.label }}
                </span>
                <span class="min-w-0 flex-1 truncate text-[11px] text-secondary-foreground" :class="i < git.ahead && '!text-foreground'">{{ c.subject }}</span>
              </span>
              <span v-if="props.size === 'lg'" class="max-w-[120px] shrink-0 truncate text-[10px] text-muted-foreground">{{ c.author }}</span>
              <span class="shrink-0 text-[10px] tabular-nums text-muted-foreground">{{ c.relTime }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Commit context menu -->
    <template v-if="commitMenu">
      <div class="fixed inset-0 z-[999]" @click="commitMenu = null" @contextmenu.prevent="commitMenu = null" />
      <div class="git-menu fixed z-[1000] w-[210px] rounded-lg border border-border bg-[var(--bg-dropdown)] p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="{ left: commitMenu.x + 'px', top: commitMenu.y + 'px' }">
        <div class="truncate px-2 pb-1 pt-0.5 font-mono text-[10px] text-muted-foreground">{{ commitMenu.c.shortHash }} · {{ commitMenu.c.subject }}</div>
        <template v-for="b in menuBranches" :key="b">
          <button class="git-menu-item" @click="menu(() => git.switchBranch(b))">Checkout <b class="font-mono">{{ b }}</b></button>
          <button v-if="git.branch" class="git-menu-item" @click="menu(() => askMerge(b))">Merge <b class="font-mono">{{ b }}</b> into current</button>
          <button v-if="git.branch" class="git-menu-item" @click="menu(() => askRebase(b))">Rebase current onto <b class="font-mono">{{ b }}</b></button>
          <div class="git-menu-sep" />
        </template>
        <button class="git-menu-item" @click="menu((c) => git.checkoutCommit(c.hash))">Checkout (detached)</button>
        <button class="git-menu-item" @click="menu((c) => askName('New branch', `From ${c.shortHash}`, (n) => git.branchAt(n, c.hash)))">New branch here…</button>
        <button class="git-menu-item" @click="menu((c) => askName('New tag', `On ${c.shortHash}`, (n) => git.tagAt(n, c.hash)))">Tag here…</button>
        <div class="git-menu-sep" />
        <button class="git-menu-item" @click="menu((c) => git.cherryPick(c.hash))">Cherry-pick</button>
        <button class="git-menu-item" @click="menu((c) => git.revertCommit(c.hash))">Revert</button>
        <div class="git-menu-sep" />
        <button class="git-menu-item" @click="menu((c) => askReset(c, 'soft'))">Reset here (soft)</button>
        <button class="git-menu-item" @click="menu((c) => askReset(c, 'mixed'))">Reset here (mixed)</button>
        <button class="git-menu-item !text-destructive" @click="menu((c) => askReset(c, 'hard'))">Reset here (hard)…</button>
        <div class="git-menu-sep" />
        <button class="git-menu-item" @click="menu((c) => copyHash(c.hash))">Copy hash</button>
      </div>
    </template>

    <!-- Confirm / name prompt (window.confirm is a no-op in the Wails webview) -->
    <div v-if="dialog" class="fixed inset-0 z-[1001] flex items-center justify-center bg-black/60" @click.self="dialog = null" @keydown.esc="dialog = null">
      <div class="flex w-[380px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-5">
        <h3 class="text-sm font-semibold text-foreground">{{ dialog.title }}</h3>
        <p v-if="dialog.body" class="text-[12px] leading-[1.6] text-secondary-foreground">{{ dialog.body }}</p>
        <input
          v-if="dialog.input"
          v-model="dialog.value"
          v-focus
          class="w-full rounded border border-border bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-2 py-1.5 font-mono text-xs text-foreground outline-none focus:border-accent/50"
          @keydown.enter.prevent="runDialog"
        />
        <div class="flex justify-end gap-2">
          <button class="rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:text-foreground" @click="dialog = null">Cancel</button>
          <button
            v-focus="!dialog.input"
            class="rounded-md border-0 px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-40"
            :class="dialog.danger ? 'bg-destructive' : 'bg-accent'"
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
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit, type GraphCommit } from "@/stores/git";
import { layoutGraph } from "@/lib/gitGraph";
import { splitHunks } from "@/lib/gitHunks";
import { useWorkspaceStore } from "@/stores/workspace";
import DiffView from "./DiffView.vue";
import { Select } from "@/components/ui/select";

// "lg" is the full-screen Git dialog; the default is the narrow right panel.
const props = withDefaults(defineProps<{ size?: "sm" | "lg" }>(), { size: "sm" });

const git = useGitStore();
const wsStore = useWorkspaceStore();
const isPopout = (window as any).__TAURI_INTERNALS__?.metadata?.currentWindow?.label === "gitpanel";
const gitBusy = computed(() => git.generating || git.committing || git.pushing);

async function popout() {
  await invoke("open_git_panel_window");
}
const selectedWsId = ref<number | null>(wsStore.active?.id ?? null);
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

function commitFileStatusClass(s: string) {
  if (s === "A") return "cf-added";
  if (s === "D") return "cf-deleted";
  if (s === "R" || s === "C") return "cf-renamed";
  return "cf-modified";
}
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

.hunk-btn {
  display: flex; align-items: center; gap: 3px; flex-shrink: 0;
  border: 1px solid var(--border); border-radius: 4px; padding: 1px 6px;
  font-size: 10px; color: var(--text-secondary); background: transparent;
}
.hunk-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.git-menu-item {
  display: block; width: 100%; text-align: left; border-radius: 5px;
  padding: 5px 8px; font-size: 12px; color: var(--text-secondary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.git-menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.git-menu-sep { margin: 4px 0; height: 1px; background: var(--border); }

.cf-added    { color: var(--green); }
.cf-deleted  { color: var(--red); }
.cf-renamed  { color: var(--accent); }
.cf-modified { color: var(--yellow); }
</style>
