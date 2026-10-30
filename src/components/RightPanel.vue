<template>
  <aside
    ref="panelEl"
    class="surface-glow [--glow-base:var(--bg-panel)] flex h-full shrink-0 grow-0 overflow-hidden bg-panel text-xs [backdrop-filter:var(--blur-panels,none)]"
    :class="props.open ? 'w-[var(--right-panel-width,300px)] basis-[var(--right-panel-width,300px)] border-l border-border' : 'w-0 basis-0'"
  >
    <!-- v-show, not v-if: closing the panel must keep XTerm/BrowserPane mounted,
         or reopening respawns a fresh shell over the still-running old one. -->
    <div v-show="props.open" ref="panesEl" class="flex min-w-0 flex-1 flex-col overflow-hidden">
    <!-- Two stacked panes, each with its own surfaces (src/lib/rpLayout.ts).
         Both <section>s always render (v-show) so their slot elements — the
         Teleport targets for Browser/Terminal — never get replaced. -->
    <template v-for="pi in PANES" :key="pi">
    <div
      v-if="pi === 1"
      v-show="paneVisible(0) && paneVisible(1) && !props.forceSurface"
      class="h-[5px] shrink-0 cursor-row-resize border-t border-border transition-colors hover:bg-accent/40"
      title="Drag to resize"
      @pointerdown.prevent="startSplitDrag"
    />
    <section
      v-show="paneVisible(pi) && !(props.forceSurface && pi === 1)"
      class="flex min-h-0 flex-col overflow-hidden"
      :style="paneStyle(pi)"
      @dragover="onSurfaceDragOver"
      @drop="onSurfaceDrop(pi, $event)"
    >
      <div v-show="!props.forceSurface" class="flex h-10 shrink-0 items-center gap-1 border-b border-border px-2">
        <div class="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto hide-scrollbar">
          <button
            v-for="tab in paneTabs(pi)"
            :key="tab.id"
            class="group flex h-7 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-[11.5px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
            :class="act(pi) === tab.id && 'bg-selected text-foreground hover:bg-selected hover:text-foreground'"
            draggable="true"
            :title="`${tab.label} — drag or right-click to move to the ${pi === 0 ? 'bottom' : 'top'} pane`"
            @click="layout.panes[pi].active = tab.id"
            @dragstart="onSurfaceDragStart(tab.id, $event)"
            @contextmenu.prevent="moveSurface(tab.id, pi === 0 ? 1 : 0)"
          >
            <span class="relative flex h-[13px] w-[13px] shrink-0 items-center justify-center">
              <component :is="tab.icon" :size="13" class="group-hover:opacity-0" />
              <span class="absolute inset-0 hidden items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground group-hover:flex" role="button" :aria-label="`Close ${tab.label}`" @click.stop="closeSurface(tab.id)"><PhX :size="12" /></span>
            </span>
            <span class="font-medium">{{ tab.label }}</span>
            <span v-if="surfaceCount(tab.id)" class="tabular-nums text-muted-foreground">{{ surfaceCount(tab.id) }}</span>
          </button>
          <button class="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Open a surface" aria-label="Open a surface" @click="layout.panes[pi].active = null"><PhPlus :size="15" /></button>
        </div>
        <button v-if="pi === 0 && !layout.bottomOpen" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Split the panel" aria-label="Split the panel" @click="openBottom(layout)"><PhRows :size="14" /></button>
        <button v-if="layout.bottomOpen" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" :title="layout.maximized === pi ? 'Restore' : 'Maximize'" :aria-label="layout.maximized === pi ? 'Restore pane' : 'Maximize pane'" @click="toggleMaximize(layout, pi)"><component :is="layout.maximized === pi ? PhArrowsInSimple : PhArrowsOutSimple" :size="13" /></button>
        <button v-if="pi === 1" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-nav)] text-muted-foreground hover:bg-hover hover:text-foreground" title="Close pane (its surfaces move to the top)" aria-label="Close pane" @click="closeBottom(layout)"><PhX :size="13" /></button>
      </div>

    <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
    <div v-if="!act(pi)" class="flex flex-1 items-center justify-center overflow-y-auto p-4">
      <div class="w-full max-w-[440px]">
        <div class="mb-4 px-0.5">
          <h2 class="m-0 text-[15px] font-semibold text-foreground">Open a surface</h2>
          <p class="mt-1 text-[11.5px] text-muted-foreground">Choose what to show in this panel.</p>
        </div>
        <div class="grid gap-2" :class="cq.width.value >= 280 ? 'grid-cols-2' : 'grid-cols-1'">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            class="group relative flex min-h-[84px] flex-col gap-1 rounded-xl border border-border/70 bg-transparent p-3 text-left transition-colors hover:border-border hover:bg-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            @click="pickSurface(pi, tab.id)"
          >
            <span class="flex items-start gap-2 pr-5">
              <component :is="tab.icon" :size="15" class="mt-px shrink-0 text-secondary-foreground group-hover:text-foreground" />
              <span class="min-w-0 break-words text-[12.5px] font-semibold leading-tight text-foreground">{{ tab.label }}</span>
            </span>
            <span class="text-[11px] leading-snug text-muted-foreground">{{ tab.description }}</span>
            <span v-if="surfaceCount(tab.id)" class="absolute right-3 top-3 text-[10.5px] tabular-nums text-muted-foreground/70">{{ surfaceCount(tab.id) }}</span>
          </button>
        </div>
      </div>
    </div>

    <!-- Extension surfaces are host-rendered, never third-party Vue code. -->
    <WorkspacePulseSurface
      v-if="extSurface(pi)?.kind === 'workspace-pulse'"
      :cwd="props.cwd"
      :title="extSurface(pi)!.title"
      :description="extSurface(pi)!.description"
    />

    <ExtensionNativeSurface
      v-else-if="extSurface(pi)?.kind === 'native' && extSurface(pi)!.ui"
      :title="extSurface(pi)!.title"
      :node="extSurface(pi)!.ui!"
    />

    <!-- Explorer tab -->
    <div v-else-if="act(pi) === 'explorer'" class="flex flex-1 flex-col overflow-y-auto">
      <div v-if="!props.cwd" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>
      <div v-else-if="fileTree.rootError" class="p-4 text-center text-[11px] text-destructive">{{ fileTree.rootError }}</div>
      <div v-else class="flex-1 py-1">
        <FileTreeNode v-for="node in fileTree.tree" :key="node.id" :node="node" :depth="0" />
      </div>
    </div>

    <!-- Git tab -->
    <div v-else-if="act(pi) === 'git'" class="flex flex-1 flex-col overflow-hidden overflow-y-auto">
      <!-- Header -->
      <div class="flex shrink-0 items-center justify-between gap-2 border-b border-border px-2.5 py-1.5">
        <div class="flex min-w-0 items-center gap-1 text-[11.5px] text-secondary-foreground">
          <PhGitBranch :size="12" class="shrink-0 text-muted-foreground" />
          <span class="truncate" :title="git.branch">{{ git.branch || "—" }}</span>
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
            class="group mx-1 flex cursor-pointer items-center gap-2 rounded-md px-2 py-[3px] transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, true)"
          >
            <span class="w-3 shrink-0 text-center font-mono text-[10.5px] font-semibold" :class="letterClass(f.x)">{{ f.x }}</span>
            <span class="flex-1 truncate text-[11.5px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <span v-if="fileStats['s:' + f.path]" class="shrink-0 font-mono text-[10.5px] tabular-nums group-hover:hidden"><span class="text-success">+{{ fileStats['s:' + f.path][0] }}</span> <span class="text-destructive">−{{ fileStats['s:' + f.path][1] }}</span></span>
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
            class="group mx-1 flex cursor-pointer items-center gap-2 rounded-md px-2 py-[3px] transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, false)"
          >
            <span class="w-3 shrink-0 text-center font-mono text-[10.5px] font-semibold" :class="letterClass(f.y)">{{ f.y }}</span>
            <span class="flex-1 truncate text-[11.5px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <span v-if="fileStats['u:' + f.path]" class="shrink-0 font-mono text-[10.5px] tabular-nums group-hover:hidden"><span class="text-success">+{{ fileStats['u:' + f.path][0] }}</span> <span class="text-destructive">−{{ fileStats['u:' + f.path][1] }}</span></span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>
          <div
            v-for="f in git.untracked"
            :key="'t:' + f.path"
            class="group mx-1 flex cursor-pointer items-center gap-2 rounded-md px-2 py-[3px] transition-colors hover:bg-hover"
          >
            <span class="w-3 shrink-0 text-center font-mono text-[10.5px] font-semibold text-success">A</span>
            <span class="flex-1 truncate text-[11.5px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <span class="shrink-0 text-[10px] text-muted-foreground group-hover:hidden">new</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>

          <!-- Commit -->
          <div class="m-2 mt-2.5 flex shrink-0 flex-col gap-2">
            <div class="rounded-xl border border-border/70 bg-[color-mix(in_srgb,var(--text-primary)_3%,transparent)] focus-within:border-border">
              <div class="flex items-center gap-1.5 px-2.5 pt-2 text-[10.5px] text-muted-foreground">
                <PhSparkle :size="11" />
                <span class="flex-1">{{ git.generating ? "Generating…" : "Commit message" }}</span>
                <button
                  class="rounded-md px-1.5 py-0.5 text-[10.5px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
                  :disabled="git.generating || !git.hasWorkingTreeChanges"
                  title="Generate a commit message from the changes"
                  @click="git.generateCommitMessage()"
                >{{ git.commitMsg.trim() ? "Regenerate" : "Generate" }}</button>
              </div>
              <textarea
                v-model="git.commitMsg"
                class="commit-input box-border block w-full min-h-[52px] max-h-[120px] resize-none border-0 bg-transparent px-2.5 pb-2 pt-1 font-sans text-[11.5px] leading-normal text-foreground outline-none placeholder:text-muted-foreground placeholder:opacity-60"
                placeholder="Leave empty to generate on commit"
                rows="3"
                @keydown.ctrl.enter="git.commit()"
                @keydown.meta.enter="git.commit()"
              />
            </div>
            <div class="flex gap-1.5">
              <button
                class="flex flex-1 items-center justify-center gap-1.5 rounded-lg border-0 bg-[var(--text-primary)] px-2.5 py-1.5 font-sans text-[11.5px] font-semibold text-[var(--bg-solid,var(--bg-base))] transition-opacity hover:opacity-90 disabled:cursor-default disabled:opacity-35"
                :disabled="!git.hasWorkingTreeChanges || git.committing || git.generating"
                @click="git.commit()"
              >
                <PhGitCommit :size="12" />
                Commit
              </button>
              <button
                class="flex flex-1 items-center justify-center gap-1.5 rounded-lg border border-border bg-transparent px-2.5 py-1.5 font-sans text-[11.5px] font-medium text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
                :disabled="!git.hasWorkingTreeChanges || git.committing || git.generating || git.pushing"
                title="Stage all changes, commit, then push"
                @click="commitAndPush"
              >
                <PhArrowUp :size="12" />
                Commit + push
              </button>
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

    <PullRequestsPanel v-else-if="act(pi) === 'pull-requests'" :cwd="props.cwd" :open-request="prOpen[wsKey]" @opened="delete prOpen[wsKey]" />

    <DevServersSurface v-else-if="act(pi) === 'dev-servers'" :cwd="props.cwd" />

    <BackgroundTasksSurface v-else-if="act(pi) === 'background'" :chat-id="activeChatId" />

    <OutputsSurface v-else-if="act(pi) === 'outputs'" :chat-id="activeChatId" :cwd="props.cwd" />

    <ProgressSurface v-else-if="act(pi) === 'progress'" :chat-id="activeChatId" />

    <SideChatSurface v-else-if="act(pi) === 'side-chat'" :chat-id="activeChatId" :cwd="props.cwd" />

    <MarkdownDocSurface v-else-if="act(pi) === 'doc' && docPath" :path="docPath" :cwd="props.cwd" />

    <MarkdownDocSurface
      v-else-if="act(pi)?.startsWith(FILE_PREFIX) && isMarkdown(act(pi)!) && !mdEditing[act(pi)!]"
      :path="act(pi)!.slice(FILE_PREFIX.length)"
      :cwd="props.cwd"
      inline-edit
      @edit="mdEditing[act(pi)!] = true"
    />

    <div v-else-if="act(pi)?.startsWith(FILE_PREFIX)" class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <button
        v-if="isMarkdown(act(pi)!)"
        class="absolute right-3 top-1.5 z-10 flex h-6 w-6 items-center justify-center rounded-[var(--radius-nav)] bg-panel text-muted-foreground hover:bg-hover hover:text-foreground"
        title="Preview"
        aria-label="Preview"
        @click="mdEditing[act(pi)!] = false"
      >
        <PhEye :size="13" />
      </button>
      <CodeEditor
        :key="act(pi)!"
        :leaf-id="0"
        :path="act(pi)!.slice(FILE_PREFIX.length)"
        :cwd="props.cwd"
        :range="fileRange[act(pi)!]"
        save-on-unmount
        @saved="git.refresh(true)"
      />
    </div>

    <div v-else-if="act(pi) === 'diff'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
        <PhGitCommit :size="13" class="shrink-0 text-secondary-foreground" />
        <select
          aria-label="Diff comparison"
          class="min-w-0 flex-1 rounded-[var(--radius-nav)] border border-border bg-panel py-1 pl-1.5 pr-1 text-[11px] text-secondary-foreground outline-none hover:border-muted-foreground"
          :value="diffScopeKey"
          @change="onDiffScopeChange(($event.target as HTMLSelectElement).value)"
        >
          <option value="last-turn">Last turn</option>
          <option value="workspace">Uncommitted</option>
          <option value="branch">Branch changes</option>
          <optgroup v-if="numberedCheckpoints.length" label="Turns">
            <option v-for="nc in numberedCheckpoints" :key="nc.cp.id" :value="`turn:${nc.cp.id}`">
              Turn {{ nc.turn }}{{ nc.turn === numberedCheckpoints.length ? " (latest)" : "" }}
            </option>
          </optgroup>
        </select>
        <button class="shrink-0 rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh diff" :disabled="scopedDiffLoading" @click="loadScopedDiff(true)"><PhArrowClockwise :size="12" :class="scopedDiffLoading && 'animate-spin'" /></button>
        <button class="flex shrink-0 items-center gap-1 rounded-[var(--radius-nav)] px-1.5 py-1 text-[11px] text-muted-foreground hover:bg-hover hover:text-foreground disabled:opacity-50" :title="diffScope.kind === 'branch' ? 'Have an agent review the branch changes' : 'Have an agent review the uncommitted changes'" :disabled="reviewing || !props.workspaceId" @click="startReview"><PhSparkle :size="12" />Review</button>
      </div>
      <div v-if="scopedDiffError" role="alert" class="p-3 text-[11px] text-destructive">{{ scopedDiffError }}</div>
      <div v-if="scopedDiffLoading && !scopedDiff" class="p-4 text-center text-[11px] text-muted-foreground">Loading changes…</div>
      <div v-else-if="diffScope.kind === 'branch' && !branchBase" class="flex flex-1 flex-col items-center justify-center gap-2 p-4 text-center text-[11px] text-muted-foreground">
        <span>Couldn't find a default branch to diff against.</span>
        <select class="rounded-[var(--radius-nav)] border border-border bg-panel px-1.5 py-1 text-[11px] text-secondary-foreground" @change="pickBranchBase(($event.target as HTMLSelectElement).value)">
          <option value="" disabled selected>Pick a branch…</option>
          <option v-for="b in git.branches.filter((b) => b !== git.branch)" :key="b" :value="b">{{ b }}</option>
        </select>
      </div>
      <div v-else-if="diffScope.kind === 'last-turn' && !lastTurn" class="p-4 text-center text-[11px] leading-relaxed text-muted-foreground">{{ reviewSubject ? "No completed turn in this thread yet." : "Open an agent thread to review its last turn." }}</div>
      <template v-else>
        <div v-if="diffScope.kind === 'last-turn' && lastTurn" class="shrink-0 truncate border-b border-border px-3 py-1.5 text-[10px] text-muted-foreground" :title="lastTurn.label">{{ lastTurn.label || 'Last completed turn' }}</div>
        <div v-else-if="diffScope.kind === 'branch' && branchBase" class="shrink-0 truncate border-b border-border px-3 py-1.5 text-[10px] text-muted-foreground" :title="branchBase">Committed changes since {{ branchBase }}</div>
        <DiffTab
          :diff="scopedDiff"
          :diff-file="diffScopeKey"
          :diff-staged="false"
          :workspace-id="props.workspaceId"
          :review-key="`${props.cwd}:${reviewSubject}:${diffScopeKey}:${diffScope.kind === 'last-turn' ? lastTurn?.id : diffScope.kind === 'branch' ? branchBase : ''}`"
          :send-batch-notes="sendPanelNotes"
          compact
        />
      </template>
    </div>

    <!-- Sub-agents tab: this thread's chat sub-agents + its Task-tool invocations -->
    <div v-else-if="act(pi) === 'agents'" class="flex min-h-0 flex-1 flex-col">
      <!-- Detail: the open child's chat. Only a view: the child's process is
           the Go chat runtime's and runs whether or not this panel is open. -->
      <template v-if="openChildId">
        <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
          <button class="rounded-[var(--radius-nav)] p-1 text-muted-foreground hover:bg-hover hover:text-foreground" aria-label="Back to sub-agents" @click="closeChildDetail"><PhCaretLeft :size="12" /></button>
          <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-semibold text-foreground">{{ chatTitle(openChildId) }}</span>
        </div>
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
            <component
              :is="agentIconComp(childProvider(child)?.icon)"
              :size="12"
              class="shrink-0 text-muted-foreground"
              :style="childProvider(child)?.color ? { color: childProvider(child)!.color } : undefined"
            />
            <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ child.title }}</span>
            <span v-if="child.model" class="max-w-[90px] shrink-0 truncate rounded-[var(--radius-nav)] bg-hover px-1 text-[9.5px] text-muted-foreground" :title="child.model">{{ modelLabel(child.agentKind ?? "", child.model) }}</span>
            <!-- The status slot is separate from the close control so rows do
                 not resize. Done+read intentionally renders nothing; blue is
                 reserved for an unread completion. -->
            <span class="relative flex h-[13px] w-[13px] shrink-0 items-center justify-center">
              <PhWarningCircle
                v-if="childAttentionState(child.id) === 'needs-input' || childAttentionState(child.id) === 'error'"
                :size="11"
                weight="fill"
                class="group-hover:invisible"
                :class="childAttentionState(child.id) === 'error' ? 'text-destructive' : 'text-[var(--yellow)]'"
                :title="childAttentionState(child.id) === 'error' ? 'Sub-agent failed' : 'Sub-agent needs attention'"
                role="status"
              />
              <PhSpinner
                v-else-if="childAttentionState(child.id) === 'working'"
                :size="11"
                class="animate-spin text-accent group-hover:invisible"
                title="Sub-agent working"
                role="status"
              />
              <span
                v-else-if="childAttentionState(child.id) === 'done-unread'"
                class="h-1.5 w-1.5 rounded-full bg-[var(--blue)] group-hover:invisible"
                title="Unread completion — open to mark read"
                aria-label="Unread completion"
                role="status"
              />
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

    <!-- History tab: pre-turn worktree snapshots, newest first -->
    <div v-else-if="act(pi) === 'history'" class="flex flex-1 flex-col overflow-y-auto">
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
    <div v-if="act(pi) === 'terminal' && !terminalWsIds.includes(wsKey)" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>
    <!-- Where this pane's kept-mounted Browser/Terminal get teleported to. -->
    <div :ref="(el) => { slotEls[pi] = el as HTMLElement | null }" v-show="act(pi) === 'browser' || act(pi) === 'terminal'" class="flex min-h-0 flex-1 flex-col" />
    </div>
    </section>
    </template>

    <!-- Browser + Terminal: one per workspace, kept mounted forever, so a page
         being previewed doesn't reload and an app running in the shell keeps
         running across project switches. They are NOT rendered inside a pane:
         a Teleport moves the same instance into whichever pane hosts it, so
         moving a surface between panes never remounts XTerm (a re-attach
         replays the ring into a re-fitted xterm and corrupts scrollback).
         Each one follows its OWN workspace's layout, so switching projects
         moves nothing. Parked in this hidden holder when no pane hosts it.
         (A moved <iframe> does reload — the browser page refreshes when it
         changes pane, nowhere else.) -->
    <div class="hidden">
      <Teleport v-for="id in browserWsIds" :key="'b' + id" :to="hostSlot(id, 'browser')" :disabled="!hostSlot(id, 'browser')">
        <BrowserPane
          v-show="id === wsKey && isShown(layout, 'browser')"
          class="min-h-0 flex-1"
          :request="browserNav[id]"
          :can-ask="id === wsKey && !!activeChatId"
          :agent-name="activeChatAgentName"
          @ask-start="askToStartServer"
        />
      </Teleport>
      <!-- The wrapper is load-bearing: XTerm has a fragment root, so v-show on
           it is ignored and the shell stayed painted over every other surface. -->
      <Teleport v-for="id in terminalWsIds" :key="'t' + id" :to="hostSlot(id, 'terminal')" :disabled="!hostSlot(id, 'terminal')">
        <div v-show="id === wsKey && isShown(layout, 'terminal')" class="flex min-h-0 flex-1 flex-col bg-[var(--terminal-bg)]">
          <!-- One sub-tab per shell: scratch shells and agent-started servers (serve). -->
          <div class="flex h-8 shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-1.5 hide-scrollbar">
            <button
              v-for="t in termsByWs[id].terms"
              :key="t.id"
              class="group flex h-6 max-w-[180px] shrink-0 items-center gap-1.5 rounded px-2 text-[11px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
              :class="termsByWs[id].active === t.id && 'bg-accent/15 text-foreground'"
              :title="t.cmd ? `${t.cmd} — started by an agent` : t.cwd"
              @click="termsByWs[id].active = t.id"
            >
              <component :is="t.cmd ? PhPlay : PhTerminal" :size="12" class="shrink-0" />
              <span class="truncate">{{ t.label }}</span>
              <span
                class="ml-0.5 flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 hover:bg-black/10 hover:text-foreground group-hover:opacity-100"
                role="button"
                :aria-label="`Close ${t.label}`"
                @click.stop="closeTerminal(id, t.id)"
              ><PhX :size="10" /></span>
            </button>
            <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-hover hover:text-foreground" title="New terminal" aria-label="New terminal" @click="addTerminal(id, { cwd: props.cwd, label: 'Terminal' })">
              <PhPlus :size="13" />
            </button>
          </div>
          <!-- The wrapper is load-bearing: XTerm has a fragment root, so v-show
               on it is ignored and the shell stayed painted over everything. -->
          <div v-for="t in termsByWs[id].terms" :key="t.id" v-show="termsByWs[id].active === t.id" class="relative flex min-h-0 flex-1 flex-col">
            <XTerm :pty-id="t.id" :cwd="t.cwd" :initial-cmd="t.cmd" />
          </div>
        </div>
      </Teleport>
    </div>

    <!-- Manual sub-agent spawn dialog — same shape as Sidebar.vue's rename dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="spawnDialogOpen" @click.self="spawnDialogOpen = false">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">Spawn a sub-agent</h3>
        <SubagentProfilePicker v-model="spawnProfile" @configure="configureSpawnProfiles" />
        <textarea
          v-model="spawnTask"
          class="h-24 w-full resize-none rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="What should the sub-agent do?"
          autofocus
          @keydown.esc="spawnDialogOpen = false"
          @keydown.enter.meta.prevent="confirmSpawnDialog"
        />
        <p v-if="spawnError" role="alert" class="m-0 text-xs text-destructive">{{ spawnError }}</p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="spawnDialogOpen = false">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmSpawnDialog" :disabled="!spawnTask.trim() || spawning">{{ spawning ? 'Spawning…' : 'Spawn' }}</button>
        </div>
      </div>
    </div>

    <!-- Closing a sub-agent deletes it: an archived child would be reachable
         from nowhere (archivedSessionsForWs filters children out), so it asks
         rather than hiding the work somewhere the user cannot get it back. -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="closeChildId !== null" @click.self="closeChildId = null" @keydown.esc="closeChildId = null">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">Close this sub-agent?</h3>
        <p class="m-0 text-[12px] leading-relaxed text-muted-foreground">
          <span class="text-secondary-foreground">{{ chatTitle(closeChildId) }}</span> and its transcript are deleted, and its
          agent is stopped if it is still working. What it already reported stays in this thread.
        </p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="closeChildId = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-destructive px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90" v-focus @click="confirmCloseChild">Close</button>
        </div>
      </div>
    </div>


    <!-- Restore confirm — overwrites files on disk, so it always asks first -->
    <Teleport to="body">
      <div class="fixed inset-0 z-[100] flex items-center justify-center bg-base/60" v-if="restoreTarget" @click.self="restoreTarget = null" @keydown.esc="restoreTarget = null">
        <div class="flex w-[430px] flex-col gap-3 rounded-[var(--radius-card)] border border-border bg-[var(--bg-dropdown)] p-6">
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
            <button class="flex items-center gap-[5px] rounded-[var(--radius-chip)] border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" :disabled="restoreBusy" v-focus @click="confirmRestore">
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
import { ref, reactive, computed, watch, inject, provide, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhFiles, PhGitBranch, PhGitCommit,
  PhArrowClockwise, PhWarning, PhX, PhArrowUpRight,
  PhArrowUp, PhArrowDown, PhCaretRight, PhCaretLeft,
  PhClockCounterClockwise, PhArrowUUpLeft, PhArrowsOutSimple, PhPlus, PhGlobe, PhTerminal, PhRobot, PhPlugsConnected,
  PhSpinner, PhWarningCircle, PhRows, PhSparkle, PhArrowsInSimple, PhPlay, PhFileText, PhFileCode, PhEye,
  PhListChecks,
} from "@phosphor-icons/vue";
import {
  closeBottom, closeSurface as closeLayoutSurface, emptyLayout, isShown, loadLayouts, moveTo, openBottom, openIn,
  paneOf, saveLayouts, toggleMaximize, clampSplit, type PaneIndex, type RpLayout,
} from "@/lib/rpLayout";
import { useGitStore, type GitCommit } from "@/stores/git";
import { useFileTreeStore } from "@/stores/fileTree";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useSubagentsStore } from "@/stores/subagents";
import { useChatAttentionStore } from "@/stores/chatAttention";
import { useProvidersStore } from "@/stores/providers";
import { agentIconComp } from "@/lib/agentIcons";
import { modelLabel } from "@/lib/chatModels";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { activeChatIdFor, childrenOf } from "@/stores/chatTree";
import { nextSubAgentView } from "@/lib/subAgentView";
import { perform } from "@/lib/controlBridge";
import { chatSession } from "@/lib/chatSession";
import { getAgentAttentionState, type AgentAttentionState } from "@/lib/terminalStatus";
import FileTreeNode from "./FileTreeNode.vue";
import { useAutoRefresh } from "@/composables/useAutoRefresh";
import { useContainerQuery } from "@/composables/useContainerQuery";
import AutoRefreshButton from "./AutoRefreshButton.vue";
import PullRequestsPanel from "./PullRequestsPanel.vue";
import DevServersSurface from "./DevServersSurface.vue";
import BackgroundTasksSurface from "./BackgroundTasksSurface.vue";
import { PhTerminalWindow } from "@phosphor-icons/vue";
import OutputsSurface from "./OutputsSurface.vue";
import ProgressSurface from "./ProgressSurface.vue";
import SideChatSurface from "./SideChatSurface.vue";
import { PhChatsCircle } from "@phosphor-icons/vue";
import MarkdownDocSurface from "./MarkdownDocSurface.vue";
import CodeEditor from "./CodeEditor.vue";
import AgentChat from "./AgentChat.vue";
import DiffView from "./DiffView.vue";
import { useUIStore } from "@/stores/ui";
import SubagentProfilePicker from "@/components/agents/SubagentProfilePicker.vue";
import DiffTab from "./DiffTab.vue";
import { fetchReviewDiff, type ReviewDiffScope, type TurnReview } from "@/lib/reviewDiff";
import { useTurnReviewRefresh } from "@/composables/useTurnReviewRefresh";
import BrowserPane from "./BrowserPane.vue";
import XTerm from "./XTerm.vue";
import WorkspacePulseSurface from "./WorkspacePulseSurface.vue";
import ExtensionNativeSurface from "./ExtensionNativeSurface.vue";
import { initPtyCounter, nextPtyId } from "@/lib/ptyId";
import { useExtensionSurfaces } from "@/composables/useExtensionSurfaces";
import { addTerm, findServed, installPanelTerminals, onlyShell, removeTerm, type RpTerm, type RpTerms, type ServeRequest, type ServeResult } from "@/lib/rpTerminals";

const props = withDefaults(defineProps<{ cwd: string; workspaceId?: number; isGit?: boolean; open?: boolean; forceSurface?: string }>(), { isGit: true, open: true });
const emit = defineEmits<{ openPanel: []; closePanel: []; openProjectConfig: [] }>();
const git = useGitStore();
// Confirm dialogs focus their action: Enter confirms, Esc cancels.
const vFocus = { mounted: (el: HTMLElement) => el.focus() };
const fileTree = useFileTreeStore();
const chats = useClaudeChatsStore();
const subagents = useSubagentsStore();
const chatAttention = useChatAttentionStore();
const providers = useProvidersStore();
const terminalTabs = useTerminalTabsStore();
const { surfaces: extensionSurfaces, load: loadExtensionSurfaces } = useExtensionSurfaces();
// RightPanel is one shared instance across all workspaces (App.vue doesn't
// key it per workspace), so which surfaces are open / which tab is active
// must be tracked per workspace, not as one global ref — otherwise switching
// projects shows the other project's open tabs.
const NO_WS = -1;
type DiffScope = ReviewDiffScope;
interface WsUiState { diffScope: DiffScope; openChildId: number | null; docPath: string; files: string[] }
const wsUiStates = reactive<Record<number, WsUiState>>({});
const wsKey = computed(() => props.workspaceId ?? NO_WS);
// The open document survives a restart, like the layout that shows it.
const DOC_KEY = "burrow.rpDoc";
function storedDoc(id: number): string {
  try { return JSON.parse(localStorage.getItem(DOC_KEY) ?? "{}")[id] ?? ""; } catch { return ""; }
}
const docPath = computed(() => wsUi(wsKey.value).docPath);
// Files opened from the explorer: one surface per file, id `file:<path>`.
const FILE_PREFIX = "file:";
const FILES_KEY = "burrow.rpFiles";
const isMarkdown = (id: string) => /\.(md|markdown|mdx)$/i.test(id);
/** Markdown file tabs open rendered; true = switched to the editor. Per surface id, not persisted. */
const mdEditing = reactive<Record<string, boolean>>({});
/** Line range a chat link asked for, per file surface id; a fresh object per click re-triggers the editor's reveal. */
const fileRange = reactive<Record<string, { from: number; to: number } | undefined>>({});
function storedFiles(id: number): string[] {
  try { const v = JSON.parse(localStorage.getItem(FILES_KEY) ?? "{}")[id]; return Array.isArray(v) ? v : []; } catch { return []; }
}
function saveFiles(id: number) {
  try {
    const all = JSON.parse(localStorage.getItem(FILES_KEY) ?? "{}");
    localStorage.setItem(FILES_KEY, JSON.stringify({ ...all, [id]: wsUi(id).files }));
  } catch { /* per-device convenience only */ }
}

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
// tool. Those are real chats but not chat-tree children, so they open as a tab.
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
// — nothing writes that slot any more, so opening a chat from the Sidebar or a
// route left it pointing at nothing and this panel said "open a chat to see its
// sub-agents" while a chat was open. The tabs mirror already
// carries `chatId` for chat tabs (Terminal.vue is its writer), so it is the
// same source of truth the user is actually looking at.
const activeChatId = computed(() =>
  activeChatIdFor(terminalTabs.tabsByWs, terminalTabs.activeByWs, props.workspaceId),
);
const childList = computed(() => (activeChatId.value ? childrenOf(chats.sessions, activeChatId.value) : []));

/** The provider instance backing a sub-agent, for its row's logo and colour.
 *  `resolve` falls back to the first configured chat agent, and `agentIconComp`
 *  falls back to the generic robot, so an unknown agentKind still renders. */
function childProvider(child: { agentKind?: string }) {
  return providers.resolve(child.agentKind);
}

// Which child the panel is showing, per workspace (so switching
// projects doesn't carry a detail view over).
const openChildId = computed<number | null>({
  get: () => wsUi(wsKey.value).openChildId,
  set: (v) => { wsUi(wsKey.value).openChildId = v; },
});

watch(openChildId, (current, previous) => {
  if (previous !== null) chatAttention.setWatching(previous, false);
  if (current !== null) chatAttention.setWatching(current, true);
}, { flush: "post" });
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
  if (openChildId.value === null) return;
  openChildId.value = null;
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

function childAttentionState(chatId: number): AgentAttentionState {
  return getAgentAttentionState(chatAttention.statusFor(chatId));
}

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
const spawnProfile = ref("worker");
const spawning = ref(false);
const spawnError = ref("");
function configureSpawnProfiles() { spawnDialogOpen.value = false; useUIStore().openSettings("subagent-profiles"); }
// One-click review: Go creates the chat (a sub-agent of the active thread, else
// a top-level chat) and starts it; Codex runs its native review/start.
const reviewing = ref(false);
async function startReview() {
  if (!props.workspaceId || reviewing.value) return;
  reviewing.value = true;
  scopedDiffError.value = "";
  try {
    const res = await invoke<{ chat_id: number }>("review_changes", {
      workspaceId: props.workspaceId, parentChatId: activeChatId.value ?? 0,
      base: diffScope.value.kind === "branch" ? "auto" : "",
    });
    if (activeChatId.value) openChild(res.chat_id);
    else terminalTabs.openChat(props.workspaceId, res.chat_id);
  } catch (error) { scopedDiffError.value = String(error); }
  finally { reviewing.value = false; }
}
function openSpawnDialog() {
  if (!activeChatId.value || !props.workspaceId) return;
  spawnTask.value = "";
  spawnError.value = "";
  spawnDialogOpen.value = true;
}
async function confirmSpawnDialog() {
  const task = spawnTask.value.trim();
  if (!task || !activeChatId.value || !props.workspaceId || spawning.value) return;
  spawning.value = true;
  spawnError.value = "";
  try {
    await perform("spawn", { task, profile: spawnProfile.value, target: "chat", parent_chat_id: activeChatId.value, cwd: props.cwd });
    spawnDialogOpen.value = false;
  } catch (error) { spawnError.value = String(error); }
  finally { spawning.value = false; }
}

/** Open a sub-agent chat's detail view from outside the panel (a transcript
 *  row's click handler in AgentChat.vue) — shows the panel, opens the Sub-agents
 *  surface, and drives the same slot the list's own row click does. `workspaceId`
 *  is the CHILD's workspace, not necessarily whichever one is currently active. */
function openSubagent(chatId: number, workspaceId: number) {
  const state = wsUi(workspaceId);
  openIn(layoutFor(workspaceId), "agents");
  state.openChildId = chatId;
}
/** A chat link to a dev server: show it in that workspace's Browser surface.
 *  The caller shows the panel. */
const browserNav = reactive<Record<number, { url: string; n: number }>>({});
function openBrowserUrl(url: string, workspaceId: number) {
  openIn(layoutFor(workspaceId), "browser");
  ensureBrowserPane(workspaceId);
  browserNav[workspaceId] = { url, n: (browserNav[workspaceId]?.n ?? 0) + 1 };
}
/** A chat link to one of this repo's pull requests: the PRs surface, on its detail. */
const prOpen = reactive<Record<number, { number: number; url: string; n: number }>>({});
function openPullRequest(number: number, url: string, workspaceId: number) {
  openIn(layoutFor(workspaceId), "pull-requests");
  prOpen[workspaceId] = { number, url, n: (prOpen[workspaceId]?.n ?? 0) + 1 };
}
const activeChatAgentName = computed(() => {
  const chat = activeChatId.value ? chats.sessions.find((s) => s.id === activeChatId.value) : undefined;
  return chat ? providers.resolve(chat.agentKind)?.name : undefined;
});
/** The Browser surface's "Nothing listening" button: ask the chat on screen,
 *  through the same doors as its composer — queued behind a running turn, else sent. */
function askToStartServer(port: number) {
  const id = activeChatId.value;
  if (!id) return;
  const text = `Start the dev server on port ${port} so I can preview it at http://localhost:${port}.`;
  const session = chatSession(id);
  if (session.busy.value) void session.enqueueMessage(text);
  else void session.send({ text });
}
/** Open a surface (Background, Progress, Outputs, …) in a workspace's panel
 *  from outside it — a transcript row that names one. The caller shows the panel. */
function openPanelSurface(id: string, workspaceId: number) {
  openIn(layoutFor(workspaceId), id);
}
function wsUi(id: number): WsUiState {
  return (wsUiStates[id] ??= { diffScope: { kind: "workspace" }, openChildId: null, docPath: storedDoc(id), files: storedFiles(id) });
}
// ── Layout: two stacked panes per workspace, persisted per device ─────────────
const PANES = [0, 1] as const;
const layouts = reactive<Record<number, RpLayout>>(loadLayouts());
function layoutFor(id: number): RpLayout {
  return (layouts[id] ??= emptyLayout());
}
const layout = computed(() => layoutFor(wsKey.value));
// Debounced: a split drag mutates `split` on every pointermove.
let saveTimer: ReturnType<typeof setTimeout> | undefined;
watch(layouts, () => {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    const { [NO_WS]: _welcome, ...persisted } = layouts;
    saveLayouts(persisted);
  }, 300);
}, { deep: true });
/** Is `id` on screen in some pane of the current workspace? */
const shown = (id: string) => isShown(layout.value, id);
/** The pane's active surface — null (picker) if it names one that doesn't exist (yet). */
function act(pi: PaneIndex): string | null {
  const id = layout.value.panes[pi].active;
  return id && tabs.value.some((t) => t.id === id) ? id : null;
}
function paneVisible(pi: PaneIndex): boolean {
  const l = layout.value;
  return pi === 0 ? l.maximized !== 1 : l.bottomOpen && l.maximized !== 0;
}
function paneStyle(pi: PaneIndex) {
  if (props.forceSurface || !(paneVisible(0) && paneVisible(1))) return { flex: "1 1 0" };
  const share = pi === 0 ? layout.value.split : 1 - layout.value.split;
  return { flex: `${share} 1 0` };
}
const slotEls = reactive<Record<number, HTMLElement | null>>({});
/** The pane slot a workspace's Browser/Terminal is teleported into, per that
 *  workspace's own layout (so a project switch moves nothing). */
function hostSlot(wsId: number, id: "browser" | "terminal"): HTMLElement | null {
  const pane = paneOf(layoutFor(wsId), id);
  return pane === -1 ? null : slotEls[pane] ?? null;
}
const panesEl = ref<HTMLElement | null>(null);
function startSplitDrag(e: PointerEvent) {
  const handle = e.currentTarget as HTMLElement;
  // Pointer capture keeps the drag alive over the browser's iframe.
  handle.setPointerCapture(e.pointerId);
  const onMove = (ev: PointerEvent) => {
    const rect = panesEl.value?.getBoundingClientRect();
    if (rect?.height) layout.value.split = clampSplit((ev.clientY - rect.top) / rect.height);
  };
  const onUp = () => {
    handle.removeEventListener("pointermove", onMove);
    handle.removeEventListener("pointerup", onUp);
  };
  handle.addEventListener("pointermove", onMove);
  handle.addEventListener("pointerup", onUp);
}
const SURFACE_MIME = "application/x-burrow-rp-surface";
function onSurfaceDragStart(id: string, e: DragEvent) {
  e.dataTransfer?.setData(SURFACE_MIME, id);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
}
function onSurfaceDragOver(e: DragEvent) {
  if (e.dataTransfer?.types.includes(SURFACE_MIME)) e.preventDefault();
}
function onSurfaceDrop(pi: PaneIndex, e: DragEvent) {
  const id = e.dataTransfer?.getData(SURFACE_MIME);
  if (!id) return;
  e.preventDefault();
  moveSurface(id, pi);
}
function moveSurface(id: string, to: PaneIndex) {
  moveTo(layout.value, id, to);
}
/** From a pane's picker: an already-open surface moves here rather than
 *  just being focused in the other pane, which the picker would hide. */
function pickSurface(pi: PaneIndex, id: string) {
  const at = paneOf(layout.value, id);
  if (at !== -1 && at !== pi) moveSurface(id, pi);
  else void openSurface(id, pi);
}

watch(() => shown("agents"), (on) => { if (!on) closeChildDetail(); });
watch(() => props.open, (open) => { if (!open) closeChildDetail(); });
watch(wsKey, (_cur, old) => {
  if (old !== undefined && old !== NO_WS && wsUiStates[old]?.openChildId != null) wsUiStates[old].openChildId = null;
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
const scopedDiffError = ref("");
const lastTurn = ref<TurnReview | null>(null);
let diffRequest = 0;
// Branch base is auto-detected per workspace (git.ts has no default-branch
// concept), so it lives here rather than in a store; a manual pick overrides
// detection until the workspace changes.
const branchBase = ref("");
const manualBranchBase = ref("");
const showHistory = ref(false);
const activeTerm = inject<() => any>('activeTerm', () => undefined);
const reviewSubject = computed<string>(() => activeTerm()?.reviewSubject ?? (activeChatId.value ? `chat:${activeChatId.value}` : ""));
async function sendPanelNotes(markdown: string): Promise<boolean> {
  return await activeTerm()?.sendReviewNotes(markdown) ?? false;
}

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
    { id: "git", label: "Git", icon: PhGitBranch, description: "Inspect, stage and commit workspace changes." },
    { id: "pull-requests", label: "PRs", icon: PhGitBranch, description: "Check pull requests for this workspace." },
    { id: "explorer", label: "Files", icon: PhFiles, description: "Browse the current workspace." },
    { id: "diff", label: "Diff", icon: PhGitCommit, description: "Review the complete workspace diff." },
    { id: "history", label: "Checkpoints", icon: PhClockCounterClockwise, description: "Review and restore agent-turn snapshots." },
    { id: "agents", label: "Agents", icon: PhRobot, description: "Sub-agents spawned by this chat." },
    { id: "progress", label: "Progress", icon: PhListChecks, description: "The active chat's live todo list." },
    { id: "side-chat", label: "Side chat", icon: PhChatsCircle, description: "Ask a quick question about the active chat without disturbing it." },
    { id: "browser", label: "Browser", icon: PhGlobe, description: "Preview a dev server without leaving Burrow." },
    { id: "dev-servers", label: "Dev servers", icon: PhPlugsConnected, description: "See and kill dev servers running in this workspace." },
    { id: "background", label: "Background", icon: PhTerminalWindow, description: "Background shell tasks the active chat launched." },
    { id: "outputs", label: "Outputs", icon: PhFileCode, description: "Files this chat created or edited." },
    { id: "terminal", label: "Terminal", icon: PhTerminal, description: "Run shell commands next to your changes." },
  ];
  // Only exists once something opened a document (chat link, ⌘-click…).
  if (docPath.value) all.push({ id: "doc", label: docPath.value.split("/").pop()!, icon: PhFileText, description: "Rendered markdown document." });
  for (const p of wsUi(wsKey.value).files) all.push({ id: FILE_PREFIX + p, label: p.split("/").pop()!, icon: PhFileCode, description: p });
  return [...all, ...extensionSurfaces.value];
});

// Push the repo the commit landed in (git.commit returns it), not whichever
// one is selected once an auto-generated message has come back.
async function commitAndPush() {
  const dir = await git.commit().catch(() => undefined); // the store already toasted the failure
  if (dir) await git.push(dir);
}

function letterClass(code: string): string {
  if (code === "A" || code === "?") return "text-success";
  if (code === "D") return "text-destructive";
  return "text-warning";
}

// Per-file +/− for the Git surface rows, keyed "s:<path>" (staged) / "u:<path>".
// ponytail: two numstat calls per status refresh; untracked files show "new".
const fileStats = ref<Record<string, [number, number]>>({});
async function loadFileStats() {
  const dir = props.cwd;
  if (!dir || git.error || !shown("git")) return;
  const run = (args: string[]) =>
    invoke<{ stdout: string; code: number }>("run_git", { cwd: dir, args }).then((o) => (o.code === 0 ? o.stdout : "")).catch(() => "");
  const [staged, unstaged] = await Promise.all([run(["diff", "--cached", "--numstat"]), run(["diff", "--numstat"])]);
  const out: Record<string, [number, number]> = {};
  for (const [prefix, text] of [["s:", staged], ["u:", unstaged]] as const) {
    for (const line of text.split("\n")) {
      const [ins, del, path] = line.split("\t");
      if (path && ins !== "-") out[prefix + path] = [Number(ins) || 0, Number(del) || 0];
    }
  }
  if (dir === props.cwd) fileStats.value = out;
}
watch(() => [git.staged, git.unstaged, props.cwd, shown("git")], () => void loadFileStats(), { immediate: true });

/** The muted number beside a surface's label ("Changes 5"); 0 renders nothing. */
function surfaceCount(id: string): number {
  if (id === "git") return git.error ? 0 : git.staged.length + git.unstaged.length + git.untracked.length;
  if (id === "agents") return childList.value.length;
  return 0;
}

function extSurface(pi: PaneIndex) {
  const id = act(pi);
  return id ? extensionSurfaces.value.find((surface) => surface.tabId === id) : undefined;
}

// Terminals per workspace — scratch shells plus the processes agents start via
// `serve` — kept mounted forever once opened, like the main Terminal.vue
// leaves, so an app running in one keeps running across project switches.
const termsByWs = reactive<Record<number, RpTerms>>({});
const terminalWsIds = computed(() => Object.keys(termsByWs).map(Number).filter((id) => termsByWs[id].terms.length));
const scratchStarts = new Map<number, Promise<void>>();

// The main terminal restores its saved/daemon sessions asynchronously. The RP
// can be opened before that restore has advanced the shared id counter, which
// used to let it reuse a live id. The daemon treats that as an attach, so the
// fresh RP xterm had no shell output and looked like a permanently blank pane.
// An XTerm only mounts once its id is in termsByWs, i.e. after this check.
async function addTerminal(ws: number, t: Omit<RpTerm, "id">): Promise<number> {
  const sessions = await invoke<Array<{ pty_id: number }>>("list_pty_sessions").catch(() => []);
  // Keep the retired Mission Control id range from pushing ordinary tabs up.
  initPtyCounter(sessions.reduce((max, s) => (s.pty_id < 1_000_000 ? Math.max(max, s.pty_id) : max), 0));
  const id = nextPtyId();
  addTerm((termsByWs[ws] ??= { terms: [], active: null }), { id, ...t });
  return id;
}

/** A shown Terminal surface with nothing in it gets one scratch shell. */
async function ensureScratch(ws: number) {
  if (termsByWs[ws]?.terms.length) return;
  const running = scratchStarts.get(ws);
  if (running) return running;
  const start = addTerminal(ws, { cwd: props.cwd, label: "Terminal" }).then(() => {});
  scratchStarts.set(ws, start);
  try { await start; } finally { scratchStarts.delete(ws); }
}

function killPty(ptyId: number) {
  void invoke("kill_pty", { id: ptyId }).catch((error) => console.warn("[right-panel] could not close terminal", error));
}

// Closing a terminal means close it, not merely hide a live shell forever.
// Remove it from the render list first so its XTerm listener is disposed; the
// daemon kill then makes the PTY unrecoverably gone.
function closeTerminal(ws: number, ptyId: number) {
  const s = termsByWs[ws];
  if (!s) return;
  removeTerm(s, ptyId);
  killPty(ptyId);
  if (!s.terms.length) closeLayoutSurface(layoutFor(ws), "terminal");
}

// Two serve calls for the same thing in flight (parallel tool calls, MCP and
// CLI at once) must share one terminal: dedup alone runs before the await.
const serving = new Map<string, Promise<ServeResult>>();
function serve(req: ServeRequest): Promise<ServeResult> {
  const key = `${req.workspaceId}\0${req.cwd}\0${req.cmd.trim().replace(/\s+/g, " ")}`;
  let p = serving.get(key);
  if (!p) {
    p = serveOnce(req).finally(() => serving.delete(key));
    serving.set(key, p);
  }
  return p;
}

async function serveOnce(req: ServeRequest): Promise<ServeResult> {
  const ws = req.workspaceId;
  // Bottom pane unless the user already put the Terminal surface elsewhere.
  const reveal = () => {
    openIn(layoutFor(ws), "terminal", 1);
    if (ws === wsKey.value && !props.open) emit("openPanel");
  };
  const hit = findServed(termsByWs[ws], req.cmd, req.cwd);
  const fg = hit ? await invoke<string>("get_pty_foreground", { id: hit.id }).catch(() => "?") : "";
  // "" = the daemon no longer has that PTY (its shell exited): drop the dead
  // tab and start fresh rather than typing into nothing.
  if (hit && fg === "") removeTerm(termsByWs[ws], hit.id);
  else if (hit) {
    termsByWs[ws].active = hit.id;
    reveal();
    // Same server asked for again: if it was stopped (only the shell is left
    // in the foreground), start it again in the same terminal. Ctrl+U first
    // clears whatever the user left half-typed on the prompt.
    const restart = onlyShell(fg);
    if (restart) await invoke("write_pty", { id: hit.id, data: Array.from(new TextEncoder().encode("\x15" + req.cmd + "\n")) });
    return { pty_id: hit.id, workspace_id: ws, reused: true, restarted: restart };
  }
  const id = await addTerminal(ws, { cwd: req.cwd, label: req.name || req.cmd, cmd: req.cmd });
  reveal();
  return { pty_id: id, workspace_id: ws, reused: false };
}

function closePanelTerminal(ptyId: number): boolean {
  for (const [ws, s] of Object.entries(termsByWs)) {
    if (s.terms.some((t) => t.id === ptyId)) {
      closeTerminal(Number(ws), ptyId);
      return true;
    }
  }
  return false;
}
onMounted(() => installPanelTerminals({ serve, close: closePanelTerminal }));
onBeforeUnmount(() => installPanelTerminals(null));

// Same idea for the browser preview: one BrowserPane per workspace, kept
// mounted so its URL/history survives switching projects.
const browserWsIds = ref<number[]>([]);
function ensureBrowserPane(id: number) {
  if (!browserWsIds.value.includes(id)) browserWsIds.value.push(id);
}

function paneTabs(pi: PaneIndex) {
  return layout.value.panes[pi].tabs
    .map((id) => tabs.value.find((tab) => tab.id === id))
    .filter((tab): tab is NonNullable<typeof tab> => Boolean(tab));
}

async function openSurface(id: string, pane: PaneIndex = 0) {
  if (!props.open) emit("openPanel");
  if (id === "terminal") await ensureScratch(wsKey.value);
  openIn(layout.value, id, pane);
  if (id === "browser") ensureBrowserPane(wsKey.value);
}
// A layout restored from storage (or a tab clicked after a restart) can show
// Browser/Terminal before anything allocated them — allocate on first show.
watch([wsKey, () => shown("terminal")], ([ws, on]) => { if (on) void ensureScratch(ws); }, { immediate: true });
watch([wsKey, () => shown("browser")], ([ws, on]) => { if (on) ensureBrowserPane(ws); }, { immediate: true });

function closeSurface(id: string) {
  if (id === "terminal") {
    const s = termsByWs[wsKey.value];
    s?.terms.forEach((t) => killPty(t.id));
    delete termsByWs[wsKey.value];
  }
  if (id.startsWith(FILE_PREFIX)) {
    const state = wsUi(wsKey.value);
    state.files = state.files.filter((p) => FILE_PREFIX + p !== id);
    saveFiles(wsKey.value);
  }
  closeLayoutSurface(layout.value, id);
}

function openGitTab() {
  openSurface("git");
}

/** Render a markdown file (plan, spec, README) in the panel. */
function openDoc(path: string) {
  wsUi(wsKey.value).docPath = path;
  try {
    const all = JSON.parse(localStorage.getItem(DOC_KEY) ?? "{}");
    localStorage.setItem(DOC_KEY, JSON.stringify({ ...all, [wsKey.value]: path }));
  } catch { /* per-device convenience only */ }
  void openSurface("doc");
}

/** Open a file from the explorer as its own panel tab. A new one joins the
 *  pane that already holds file tabs, so dragging them to the bottom pane
 *  keeps the tree visible on top. */
function openFile(path: string, range?: { from: number; to: number }) {
  const state = wsUi(wsKey.value);
  if (range) {
    fileRange[FILE_PREFIX + path] = { ...range };
    // A line range needs the editor, not the rendered preview.
    if (isMarkdown(path)) mdEditing[FILE_PREFIX + path] = true;
  }
  const pane = state.files.map((p) => paneOf(layout.value, FILE_PREFIX + p)).find((i) => i !== -1) ?? 0;
  if (!state.files.includes(path)) {
    state.files.push(path);
    saveFiles(wsKey.value);
  }
  void openSurface(FILE_PREFIX + path, pane);
}
provide("openFileInPanel", openFile);

defineExpose({ openGitTab, openSubagent, openPanelSurface, openBrowserUrl, openPullRequest, openDoc, openFile });

// Phone shell (/project/:surface): one surface, full screen, no tab strip or
// second pane — the phone's own header and back arrow replace them.
watch([() => props.forceSurface, wsKey], ([id]) => { if (id) pickSurface(0, id); }, { immediate: true });

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
  const cwd = props.cwd;
  if (!cwd) return (checkpoints.value = []);
  const rows = await invoke<Checkpoint[]>("list_checkpoints", { cwd, limit: 50 });
  if (props.cwd === cwd) checkpoints.value = rows;
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

watch([() => shown("history"), () => props.cwd], () => { if (shown("history")) loadCheckpoints(); });
watch([() => shown("diff"), () => props.cwd, diffScopeKey, reviewSubject], () => {
  // Invalidate in-flight requests even when the diff surface is closed.
  diffRequest++;
  if (!shown("diff")) return;
  void loadScopedDiff();
}, { immediate: true });

// checkpoints are listed newest-first; turn numbers count up from the oldest,
// so "Turn 1" is stable as new turns land instead of shifting every time.
const numberedCheckpoints = computed(() =>
  checkpoints.value.map((cp, i) => ({ cp, turn: checkpoints.value.length - i })));

function onDiffScopeChange(v: string) {
  if (v.startsWith("turn:")) {
    const id = Number(v.slice(5));
    if (!Number.isInteger(id) || !checkpoints.value.some((cp) => cp.id === id)) return;
    diffScope.value = { kind: "turn", checkpointId: id };
  } else if (v === "workspace" || v === "branch" || v === "last-turn") {
    diffScope.value = { kind: v };
  }
}

function pickBranchBase(b: string) {
  if (!b) return;
  manualBranchBase.value = b;
  loadScopedDiff();
}

async function loadScopedDiff(preserveCurrent = false) {
  const request = ++diffRequest;
  const cwd = props.cwd;
  const scope = diffScope.value;
  const subjectId = reviewSubject.value;
  const manualBase = manualBranchBase.value;
  const isCurrent = () => request === diffRequest && cwd === props.cwd;
  if (!preserveCurrent) {
    scopedDiff.value = "";
    branchBase.value = "";
    lastTurn.value = null;
  }
  scopedDiffError.value = "";
  scopedDiffLoading.value = true;
  try {
    const rows = cwd ? await invoke<Checkpoint[]>("list_checkpoints", { cwd, limit: 50 }) : [];
    if (!isCurrent()) return;
    checkpoints.value = rows ?? [];
    const checkpointCommit = scope.kind === "turn" ? checkpoints.value.find((c) => c.id === scope.checkpointId)?.commit : undefined;
    const result = await fetchReviewDiff({ cwd, scope, subjectId, checkpointCommit, manualBase }, invoke);
    if (!isCurrent()) return;
    scopedDiff.value = result.diff;
    branchBase.value = result.base;
    lastTurn.value = result.turn;
    if (scope.kind === "branch" && !result.base) await git.fetchBranches();
  } catch (error) {
    if (isCurrent()) scopedDiffError.value = `Could not load changes: ${error}`;
  } finally {
    if (isCurrent()) scopedDiffLoading.value = false;
  }
}

watch(() => props.cwd, () => { manualBranchBase.value = ""; checkpoints.value = []; }, { flush: "sync" });

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
    if (shown("diff") && !scopedDiffLoading.value) void loadScopedDiff(true);
  }
}

const ar = useAutoRefresh(autoRefresh, "burrow-git-refresh-interval");

useTurnReviewRefresh(reviewSubject, () => {
  if (shown("diff") && diffScope.value.kind === "last-turn") void loadScopedDiff(true);
});
function onFocus() { autoRefresh(); }
function onVisible() { if (!document.hidden) autoRefresh(); }

watch(() => shown("git"), (on) => { if (on) autoRefresh(); });

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
