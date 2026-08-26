<!--
  MissionControl.vue — a tank-style task dashboard, in its own Tauri window.

  Mounted by main.ts when the window label is `mission`. Each "task" is a
  headless `claude` PTY (Burrow's native PTY, not tmux): we spawn it from a
  prompt, watch its status dot via the global hook server (pty-hook-{id}),
  attach a live xterm on demand, and read the final assistant reply straight
  from Claude's JSONL transcript when the Stop hook fires.

  POC scope: spawn + list + live terminal + result capture + sequential queue.
  Task metadata is mirrored to localStorage so reopening the window keeps the
  history; live attach only works while the daemon PTY is still alive.
-->
<template>
  <div class="grid h-full w-full min-h-0 bg-base font-sans text-sm text-foreground" :class="activeWs ? 'grid-cols-[300px_1fr]' : 'flex items-center justify-center'">
   <template v-if="activeWs">
    <!-- ── Left rail: task list grouped by project (cwd) ───────────────── -->
    <aside class="flex flex-col overflow-y-auto border-r border-border bg-panel [-webkit-backdrop-filter:var(--blur-content,none)] [backdrop-filter:var(--blur-content,none)]">
      <header class="flex items-center gap-[9px] px-4 pb-2.5 pt-4">
        <PhCrosshair :size="17" weight="bold" class="shrink-0 text-accent" />
        <h1 class="m-0 flex-1 text-sm font-semibold tracking-[0.01em]">Mission Control</h1>
      </header>

      <!-- Scope = the active workspace. Dropdown switches it (also reflected in the sidebar). New tasks target it. -->
      <div class="dd relative mx-3 mb-2.5">
        <button type="button" class="flex w-full items-center gap-2.5 rounded-[10px] border border-border bg-[var(--terminal-bg)] px-[11px] py-[9px] text-left font-inherit text-sm text-foreground transition-colors duration-150 hover:border-[color-mix(in_srgb,var(--accent)_35%,var(--border))]" :class="{ '!border-accent': wsMenuOpen }" @click="wsMenuOpen = !wsMenuOpen">
          <img v-if="wsIcon(activeWs.id)" class="h-5 w-5 shrink-0 rounded-[5px] object-cover" :src="wsIcon(activeWs.id)" alt="" />
          <PhGitBranch v-else-if="activeWs.parent_id" :size="15" class="shrink-0 text-secondary-foreground" />
          <PhFolder v-else :size="15" weight="fill" class="shrink-0 text-secondary-foreground" />
          <span class="flex min-w-0 flex-1 flex-col gap-px">
            <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[13px] font-semibold">{{ activeWs.name }}</span>
            <span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-muted-foreground">{{ shortCwd(activeWs.path) }}</span>
          </span>
          <PhCaretDown :size="13" class="ml-auto shrink-0 text-muted-foreground transition-transform duration-200" :class="{ 'rotate-180': wsMenuOpen }" />
        </button>
        <div v-if="wsMenuOpen" class="fixed inset-0 z-[100]" @click="wsMenuOpen = false" />
        <ul v-if="wsMenuOpen" class="absolute left-0 right-0 top-[calc(100%+5px)] z-[101] m-0 max-h-[260px] list-none overflow-y-auto rounded-[9px] border border-border bg-panel p-[5px] shadow-[0_14px_34px_-10px_#000d]">
          <li
            v-for="w in wsStore.topLevel"
            :key="w.id"
            class="flex cursor-pointer items-center justify-between gap-2 rounded-md px-[9px] py-[7px] text-sm text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="{ 'text-accent [&_svg]:text-accent': w.id === activeWs.id }"
            @click="pickWorkspace(w)"
          >
            <span class="flex min-w-0 flex-row items-center gap-[9px]">
              <img v-if="wsIcon(w.id)" class="h-[18px] w-[18px] shrink-0 rounded-[5px] object-cover" :src="wsIcon(w.id)" alt="" />
              <PhFolder v-else :size="14" weight="fill" class="shrink-0 text-secondary-foreground" />
              <span class="flex min-w-0 flex-col gap-px">
                <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ w.name }}</span>
                <code class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-muted-foreground">{{ shortCwd(w.path) }}</code>
              </span>
            </span>
            <PhCheck v-if="w.id === activeWs.id" :size="13" weight="bold" />
          </li>
        </ul>
      </div>

      <button class="mx-3 mb-3 flex items-center justify-center gap-[7px] rounded-lg border border-accent bg-accent px-3 py-[9px] text-[13px] font-semibold text-white hover:bg-accent-dim" @click="openComposer"><PhPlus :size="14" weight="bold" /> New task</button>

      <div class="flex flex-wrap items-center gap-1.5 border-b border-border px-3.5 pb-3 text-xs text-secondary-foreground">
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('running') }"><em class="dot running" />{{ countBy('running') }}<small class="text-[10px] text-muted-foreground">running</small></span>
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('waiting') }"><em class="dot waiting" />{{ countBy('waiting') }}<small class="text-[10px] text-muted-foreground">waiting</small></span>
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('permission') }"><em class="dot permission" />{{ countBy('permission') }}<small class="text-[10px] text-muted-foreground">permission</small></span>
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('error') }"><em class="dot error" />{{ countBy('error') }}<small class="text-[10px] text-muted-foreground">error</small></span>
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('review') }"><em class="dot review" />{{ countBy('review') }}<small class="text-[10px] text-muted-foreground">review</small></span>
        <span class="inline-flex items-center gap-[5px] rounded-full border border-border bg-[var(--terminal-bg)] px-2 py-[3px] text-[11px] tabular-nums opacity-55" :class="{ 'opacity-100': countBy('done') }"><em class="dot done" />{{ countBy('done') }}<small class="text-[10px] text-muted-foreground">done</small></span>
        <span class="flex-1" />
        <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-transparent px-1.5 py-0.5 text-[10px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" v-if="tasks.some(t => !t.alive)" @click="clearDead">clear finished</button>
      </div>

      <section class="px-4 pb-4 pt-2" v-if="queue.length">
        <h2 class="m-0 text-[11px] uppercase tracking-[0.04em] text-secondary-foreground">Queue · {{ queue.length }}</h2>
        <ul class="m-0 mt-2 flex list-none flex-col gap-1.5 p-0">
          <li v-for="(q, i) in queue" :key="q.qid" class="flex items-center gap-2 rounded-lg border border-border bg-[var(--terminal-bg)] px-2 py-1.5">
            <span class="w-3.5 text-[10px] text-muted-foreground">{{ i + 1 }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs">{{ q.prompt }}</span>
            <button class="border-none bg-none text-muted-foreground" title="remove" @click="queue.splice(i, 1)"><PhX :size="11" /></button>
          </li>
        </ul>
      </section>

      <div v-if="!tasks.length" class="px-4 py-[30px] text-center text-xs leading-[1.7] text-muted-foreground">
        No tasks yet.<br />Hit <strong>＋ New</strong> to spawn one.
      </div>

      <!-- Projects = tasks grouped by working dir -->
      <nav class="flex-1 overflow-y-auto py-2">
        <div v-for="proj in projects" :key="proj.key" class="mb-1.5">
          <div class="px-3.5 pb-1 pt-1.5 font-mono text-[10px] uppercase tracking-[0.05em] text-muted-foreground">{{ proj.label }}</div>
          <ul class="m-0 list-none p-0">
            <li
              v-for="t in proj.tasks"
              :key="t.id"
              class="flex cursor-pointer items-center gap-2 border-l-2 border-l-transparent px-3.5 py-2 hover:bg-hover"
              :class="{ 'border-l-accent bg-selected': t.id === selectedId }"
              @click="selectTask(t)"
            >
              <em class="dot" :class="t.status" />
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[13px]">{{ t.title }}</span>
              <PhArrowSquareOut v-if="t.handedOff" class="shrink-0 text-accent" :size="12" weight="bold" title="handed off to a terminal tab" />
              <span class="whitespace-nowrap text-[10px] text-muted-foreground">{{ statusLabel(t) }}</span>
            </li>
          </ul>
        </div>
      </nav>
    </aside>

    <!-- ── Center: selected-task conversation + continue bar ───────────── -->
    <main class="flex h-full min-h-0 flex-col overflow-hidden">
      <template v-if="selected">
        <header class="flex items-center gap-2.5 border-b border-border px-5 py-3.5">
          <em class="dot" :class="selected.status" :title="selected.status" />
          <h2 class="m-0 max-w-[40vw] overflow-hidden text-ellipsis whitespace-nowrap text-[15px] font-semibold">{{ selected.title }}</h2>
          <span class="rounded-[5px] bg-accent/[0.18] px-1.5 py-0.5 text-[10px] text-accent" v-if="selected.model && selected.model !== 'default'">{{ selected.model }}</span>
          <span class="font-mono text-[11px] text-muted-foreground">{{ shortCwd(selected.cwd) }}</span>
          <span class="text-xs text-muted-foreground">· {{ statusLabel(selected) }}</span>
          <span class="inline-flex items-center gap-1 rounded-[5px] bg-accent/[0.14] px-[7px] py-0.5 text-[10px] font-semibold text-accent" v-if="selected.handedOff" title="this task's live session was handed off to a terminal tab"><PhArrowSquareOut :size="11" weight="bold" /> in tab</span>
          <span class="flex-1" />
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" :class="{ '!text-accent !border-[color-mix(in_srgb,var(--accent)_50%,transparent)]': ui.missionShowActivity }" @click="ui.missionShowActivity = !ui.missionShowActivity" :title="ui.missionShowActivity ? 'hide thinking + tool activity in the feed' : 'show thinking + tool activity in the feed'"><PhWrench :size="12" /> Activity</button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="openTranscript(selected)" title="view the full session transcript"><PhArticle :size="12" /> Transcript</button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))] disabled:cursor-not-allowed disabled:opacity-40" @click="openTerminal(selected)" :disabled="!selected.alive || selected.handedOff" title="attach live terminal"><PhTerminal :size="12" /> Terminal</button>
          <button v-if="selected.handedOff" class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="focusHandoff(selected)" title="jump to the terminal tab running this task"><PhArrowSquareOut :size="12" /> Focus tab</button>
          <button v-else class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))] disabled:cursor-not-allowed disabled:opacity-40" @click="handoffToTab(selected)" :disabled="!selected.alive" title="hand this live session off to a real terminal tab (keeps tracking here)"><PhArrowRight :size="12" /> Hand off</button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-[color-mix(in_srgb,var(--red)_30%,var(--border))] bg-hover px-2 py-1 text-[11px] text-destructive hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="deleteTask(selected)" title="kill + remove"><PhTrash :size="12" /> Delete</button>
        </header>

        <div class="flex flex-1 flex-col gap-4 overflow-y-auto p-5" ref="convoEl">
          <div v-for="(turn, i) in visibleTurns" :key="i" class="turn flex gap-2.5 text-[13px] leading-[1.55]" :class="[turn.role, turn.kind || 'text']">
            <span class="flex w-[18px] shrink-0 justify-center pt-0.5 text-muted-foreground">
              <PhBrain v-if="turn.kind === 'thinking'" :size="13" />
              <PhWrench v-else-if="turn.kind === 'tool_use'" :size="13" />
              <PhArrowBendDownRight v-else-if="turn.kind === 'tool_result'" :size="13" />
              <PhCaretRight v-else-if="turn.role === 'user'" :size="13" weight="bold" />
              <PhRobot v-else :size="14" />
            </span>
            <!-- thinking: muted italic stream -->
            <div v-if="turn.kind === 'thinking'" class="ttext thinking flex-1 whitespace-pre-wrap font-sans italic text-muted-foreground opacity-75">{{ turn.text }}</div>
            <!-- tool call: name + collapsible input -->
            <details v-else-if="turn.kind === 'tool_use'" class="tool-entry flex-1 rounded-lg border border-[color-mix(in_srgb,var(--border)_70%,transparent)] bg-[color-mix(in_srgb,var(--bg-hover)_45%,transparent)] px-2.5 py-1.5 font-mono text-[11px]">
              <summary class="flex select-none list-none items-center gap-[5px] text-secondary-foreground [&::-webkit-details-marker]:hidden"><PhWrench :size="11" /> <span class="font-semibold text-accent">{{ turn.tool || 'tool' }}</span></summary>
              <pre v-if="turn.text" class="mt-1.5 max-h-[220px] overflow-auto whitespace-pre-wrap break-words text-muted-foreground">{{ turn.text }}</pre>
            </details>
            <!-- tool result: collapsible, error-tinted on failure -->
            <details v-else-if="turn.kind === 'tool_result'" class="tool-entry result flex-1 rounded-lg border border-[color-mix(in_srgb,var(--border)_70%,transparent)] bg-[color-mix(in_srgb,var(--bg-hover)_45%,transparent)] px-2.5 py-1.5 font-mono text-[11px]" :class="{ 'border-[color-mix(in_srgb,var(--red)_50%,transparent)]': turn.isError }">
              <summary class="select-none list-none text-muted-foreground [&::-webkit-details-marker]:hidden" :class="{ '!text-destructive': turn.isError }">{{ turn.isError ? 'result · error' : 'result' }}</summary>
              <pre class="mt-1.5 max-h-[220px] overflow-auto whitespace-pre-wrap break-words text-muted-foreground">{{ turn.text }}</pre>
            </details>
            <div v-else-if="turn.role === 'user'" class="ttext flex-1 whitespace-pre-wrap rounded-lg border border-border bg-[color-mix(in_srgb,var(--bg-hover)_70%,transparent)] px-3 py-2 font-mono text-foreground [backdrop-filter:blur(12px)] [-webkit-backdrop-filter:blur(12px)]">{{ turn.text }}</div>
            <!-- eslint-disable-next-line vue/no-v-html -->
            <div v-else class="ttext md-body flex-1 whitespace-normal rounded-lg border border-[color-mix(in_srgb,var(--border)_80%,transparent)] bg-[color-mix(in_srgb,var(--terminal-bg)_55%,transparent)] px-3 py-2.5 leading-[1.6] text-secondary-foreground [backdrop-filter:blur(12px)] [-webkit-backdrop-filter:blur(12px)]" v-html="renderMd(turn.text)"></div>
          </div>
          <div v-if="selected.status === 'running'" class="flex animate-[pulse_1.4s_infinite] items-center gap-1.5 pl-7 text-xs text-warning"><PhRobot :size="13" /> working…</div>
          <div v-if="visibleTurns.length === 1 && selected.status !== 'running'" class="pl-7 text-xs italic text-muted-foreground">no result captured yet</div>
        </div>

        <!-- Handed off → a terminal tab owns input; lock the bar to avoid two writers. -->
        <div class="flex flex-row items-center gap-2 border-t border-border bg-panel px-5 pb-4 pt-3 text-xs text-accent" v-if="selected.handedOff">
          <PhArrowSquareOut :size="13" />
          <span>Handed off to a terminal tab — input lives there now.</span>
          <span class="flex-1" />
          <button class="flex items-center justify-center gap-1.5 rounded-lg border border-accent bg-accent px-3 py-2 text-[13px] font-semibold text-white hover:bg-accent-dim" @click="focusHandoff(selected)"><PhArrowSquareOut :size="13" /> Focus tab</button>
        </div>
        <!-- Persistent continue bar (tank's "send & continue") -->
        <div class="flex flex-col gap-2 border-t border-border bg-panel px-5 pb-4 pt-3" v-else-if="selected.alive"
             @drop.prevent="onFollowupDrop($event, selected)" @dragover.prevent>
          <!-- pasted/dropped image thumbnails -->
          <div v-if="selected.followupImages?.length" class="flex flex-wrap gap-2">
            <div v-for="(p, i) in selected.followupImages" :key="`fu-${i}`" class="img-thumb group relative h-[60px] w-[60px] shrink-0 overflow-hidden rounded-lg border border-border bg-hover" :title="p.split('/').pop()">
              <img v-if="imagePreviews[p]" class="block h-full w-full object-cover" :src="imagePreviews[p]" :alt="p.split('/').pop()" />
              <span v-else class="flex h-full w-full items-center justify-center text-muted-foreground"><PhImage :size="18" /></span>
              <button class="absolute right-0.5 top-0.5 flex h-[18px] w-[18px] items-center justify-center rounded-[5px] border-none bg-black/[0.67] text-white opacity-0 transition-opacity group-hover:opacity-100 hover:bg-black/[0.83]" @click="removeFollowupImage(selected, i)" title="Remove"><PhX :size="11" weight="bold" /></button>
            </div>
          </div>
          <div class="fu-input relative flex rounded-xl border border-border bg-[var(--terminal-bg)] transition-[border-color,box-shadow] duration-150 focus-within:border-accent focus-within:shadow-[0_0_0_3px_color-mix(in_srgb,var(--accent)_18%,transparent)]" :class="{ 'opacity-60': selected.status === 'running' }">
            <textarea
              class="min-h-[52px] max-h-[200px] flex-1 resize-none rounded-xl border-none bg-transparent px-[13px] py-[11px] pr-[56px] font-mono text-[13px] leading-[1.45] text-foreground outline-none disabled:cursor-not-allowed"
              v-model="selected.followup"
              rows="2"
              placeholder="Continue the conversation — paste or drop images · Enter to send · Shift+Enter for newline"
              :disabled="selected.status === 'running'"
              @paste="onFollowupPaste($event, selected)"
              @keydown.enter.exact.prevent="sendFollowup(selected)"
            ></textarea>
            <div class="absolute bottom-2 right-2 flex gap-1.5">
              <button
                v-if="selected.status === 'running'"
                class="flex h-8 w-8 items-center justify-center rounded-[9px] border-none bg-[color-mix(in_srgb,#ef4444_22%,var(--bg-hover))] text-[#ef4444] transition-[transform,background,opacity] duration-100 hover:bg-[#ef4444] hover:text-white active:scale-[0.92]"
                @click="stopGeneration(selected)"
                title="Interrupt the current turn (Esc)"
              ><PhStop :size="13" weight="fill" /></button>
              <button
                v-else
                class="flex h-8 w-8 items-center justify-center rounded-[9px] border-none bg-accent text-white transition-[transform,background,opacity] duration-100 hover:brightness-110 active:scale-[0.92] disabled:cursor-default disabled:bg-hover disabled:text-muted-foreground"
                :disabled="!selected.followup.trim() && !selected.followupImages?.length"
                @click="sendFollowup(selected)"
                title="Send & continue"
              ><PhArrowUp :size="15" weight="bold" /></button>
            </div>
          </div>
        </div>
        <div class="flex flex-row items-center gap-2 border-t border-border bg-panel px-5 pb-4 pt-3 text-xs text-muted-foreground" v-else-if="!selected.alive">
          <span>PTY finished — this task is read-only.</span>
          <span class="flex-1" />
          <button class="flex items-center justify-center gap-1.5 rounded-lg border border-accent bg-accent px-3 py-2 text-[13px] font-semibold text-white hover:bg-accent-dim" @click="resumeTask(selected)" title="respawn claude --resume and continue"><PhArrowClockwise :size="13" /> Resume</button>
        </div>
      </template>

      <div v-else class="flex h-full items-center justify-center">
        <div class="text-center text-muted-foreground">
          <h2 class="mb-1.5 text-base text-secondary-foreground">No task selected</h2>
          <p class="text-[13px]">Pick a task on the left, or hit <strong>＋ New task</strong> to spawn one.</p>
        </div>
      </div>
    </main>
   </template>

    <!-- ── Blank state: no active workspace → nothing to scope a task to ──── -->
    <div v-else class="flex w-full items-center justify-center p-10">
      <div class="flex max-w-[380px] flex-col items-center gap-2.5 text-center">
        <PhCrosshair :size="34" weight="duotone" class="mb-1 text-accent opacity-90" />
        <h2 class="m-0 text-lg font-semibold text-foreground">Pick a workspace</h2>
        <p class="m-0 text-[13px] leading-[1.6] text-secondary-foreground">Mission Control runs Claude tasks against a workspace. Select one in the sidebar on the left to spawn and track agent tasks here.</p>
        <span class="mt-1.5 inline-flex items-center gap-1.5 font-mono text-[11px] text-accent"><PhArrowLeft :size="13" /> choose a workspace to begin</span>
      </div>
    </div>

    <!-- ── New-task composer (modal) ───────────────────────────────────── -->
    <div v-if="composerOpen" class="fixed inset-0 z-[90] flex items-center justify-center bg-black/[0.73]" @click.self="composerOpen = false">
      <div class="flex w-[520px] max-w-[92vw] flex-col gap-3 rounded-[14px] border border-border bg-panel p-[18px] [-webkit-backdrop-filter:var(--blur-overlay,none)] [backdrop-filter:var(--blur-overlay,none)]" @drop.prevent="onComposerDrop" @dragover.prevent>
        <header class="flex items-center gap-2 text-sm font-semibold">
          <PhCrosshair :size="14" weight="bold" class="shrink-0 text-accent" />
          <span class="flex-1">New task</span>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover p-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="composerOpen = false"><PhX :size="12" /></button>
        </header>

        <div class="relative flex flex-col gap-1">
          <div class="mb-1 flex items-baseline justify-between">
            <span class="text-[11px] uppercase tracking-[0.04em] text-secondary-foreground">Prompt</span>
            <span class="text-[10px] text-muted-foreground">Type <kbd class="rounded-[3px] border border-border bg-hover px-1 py-px font-mono text-[10px] text-secondary-foreground">@</kbd> to reference a file · <kbd class="rounded-[3px] border border-border bg-hover px-1 py-px font-mono text-[10px] text-secondary-foreground">⌘↵</kbd> to run</span>
          </div>
          <div class="relative">
            <textarea
              class="box-border w-full resize-y rounded-lg border border-border bg-[var(--terminal-bg)] px-2.5 py-2 font-mono text-[13px] text-foreground focus:border-accent focus:outline-none"
              ref="promptTextarea"
              v-model="draft.prompt"
              rows="5"
              placeholder="What should Claude do?"
              @keydown.meta.enter="runDraft"
              @paste="onComposerPaste"
              @input="onPromptInput"
              @keydown.escape="showFilePicker = false"
            ></textarea>
            <!-- @-trigger file picker dropdown -->
            <div v-if="showFilePicker" class="absolute left-0 right-0 top-[calc(100%+4px)] z-[200] overflow-hidden rounded-[9px] border border-border bg-panel shadow-[0_12px_32px_-8px_#000c]">
              <div class="flex items-center gap-1.5 border-b border-border px-2.5 py-1.5 font-mono text-[10px] text-muted-foreground"><PhMagnifyingGlass :size="11" /> <span>{{ fileSearchQuery || "files in workspace" }}</span></div>
              <ul class="m-0 list-none p-1">
                <li v-for="p in fileSearchResults" :key="p" @click="selectFileFromPicker(p)" class="flex cursor-pointer items-center gap-2 rounded-md px-[9px] py-[7px] text-xs hover:bg-hover">
                  <PhFile :size="12" />
                  <span class="whitespace-nowrap font-medium text-foreground">{{ fileBasename(p) }}</span>
                  <span class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-muted-foreground">{{ shortCwd(p) }}</span>
                </li>
              </ul>
            </div>
          </div>
        </div>

        <!-- Image attachments: thumbnail previews -->
        <div v-if="draft.images.length" class="flex flex-wrap gap-2">
          <div v-for="(p, i) in draft.images" :key="`img-${i}`" class="img-thumb group relative h-[60px] w-[60px] shrink-0 overflow-hidden rounded-lg border border-border bg-hover" :title="p.split('/').pop()">
            <img v-if="imagePreviews[p]" class="block h-full w-full object-cover" :src="imagePreviews[p]" :alt="p.split('/').pop()" />
            <span v-else class="flex h-full w-full items-center justify-center text-muted-foreground"><PhImage :size="18" /></span>
            <button class="absolute right-0.5 top-0.5 flex h-[18px] w-[18px] items-center justify-center rounded-[5px] border-none bg-black/[0.67] text-white opacity-0 transition-opacity group-hover:opacity-100 hover:bg-black/[0.83]" @click="removeImage(i)" title="Remove"><PhX :size="11" weight="bold" /></button>
          </div>
        </div>

        <!-- Tagged context files -->
        <div v-if="draft.files.length" class="flex flex-wrap gap-1.5">
          <span v-for="(p, i) in draft.files" :key="`file-${i}`" class="inline-flex items-center gap-1.5 rounded-md border border-[color-mix(in_srgb,var(--accent)_25%,var(--border))] bg-hover px-2 py-[3px] text-[11px] text-[color-mix(in_srgb,var(--accent)_90%,var(--text-secondary))] [&_svg]:text-accent">
            <PhFile :size="12" /> {{ fileBasename(p) }}
            <button class="border-none bg-none p-0 text-muted-foreground" @click="removeFile(i)"><PhX :size="10" /></button>
          </span>
        </div>

        <!-- Attach buttons -->
        <div class="flex items-center gap-2.5">
          <button class="inline-flex items-center gap-[5px] rounded-lg border border-border bg-transparent px-1.5 py-0.5 text-[10px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="pickFiles" type="button" title="Attach context files (sent as @path references)">
            <PhPaperclip :size="13" /> Attach files
          </button>
          <span class="text-[10.5px] text-muted-foreground">or drop images · type <kbd class="rounded-[3px] border border-border bg-hover px-1 py-px font-mono text-[10px] text-secondary-foreground">@</kbd> for inline ref</span>
        </div>

        <div class="flex flex-col gap-1">
          <span class="text-[11px] uppercase tracking-[0.04em] text-secondary-foreground">Model</span>
          <div class="dd relative">
            <button type="button" class="flex w-full items-center gap-2 rounded-lg border border-border bg-[var(--terminal-bg)] px-2.5 py-2 text-left font-inherit text-[13px] text-foreground transition-colors duration-150 hover:border-[color-mix(in_srgb,var(--accent)_35%,var(--border))]" :class="{ '!border-accent': modelMenuOpen }" @click="modelMenuOpen = !modelMenuOpen">
              <span class="flex flex-1 items-center gap-1.5 text-left">{{ modelLabel }}</span>
              <PhCaretDown :size="12" class="text-muted-foreground transition-transform duration-200" :class="{ 'rotate-180': modelMenuOpen }" />
            </button>
            <div v-if="modelMenuOpen" class="fixed inset-0 z-[100]" @click="modelMenuOpen = false" />
            <ul v-if="modelMenuOpen" class="absolute left-0 right-0 top-[calc(100%+5px)] z-[101] m-0 max-h-[260px] list-none overflow-y-auto rounded-[9px] border border-border bg-panel p-[5px] shadow-[0_14px_34px_-10px_#000d]">
              <li
                v-for="m in MODELS"
                :key="m.value"
                class="flex cursor-pointer items-center justify-between gap-2 rounded-md px-[9px] py-2 text-[13px] text-secondary-foreground hover:bg-hover hover:text-foreground"
                :class="{ 'text-accent [&_svg]:text-accent': m.value === draft.model }"
                @click="draft.model = m.value; modelMenuOpen = false"
              >
                <span>{{ m.label }}</span>
                <PhCheck v-if="m.value === draft.model" :size="13" weight="bold" />
              </li>
            </ul>
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <span class="text-[11px] uppercase tracking-[0.04em] text-secondary-foreground">Profile</span>
          <div class="dd relative">
            <button type="button" class="flex w-full items-center gap-2 rounded-lg border border-border bg-[var(--terminal-bg)] px-2.5 py-2 text-left font-inherit text-[13px] text-foreground transition-colors duration-150 hover:border-[color-mix(in_srgb,var(--accent)_35%,var(--border))]" :class="{ '!border-accent': profileMenuOpen }" @click="profileMenuOpen = !profileMenuOpen">
              <span class="flex flex-1 items-center gap-1.5 text-left"><PhUserGear :size="12" class="shrink-0 text-muted-foreground" /> {{ profileLabel }}</span>
              <PhCaretDown :size="12" class="text-muted-foreground transition-transform duration-200" :class="{ 'rotate-180': profileMenuOpen }" />
            </button>
            <div v-if="profileMenuOpen" class="fixed inset-0 z-[100]" @click="profileMenuOpen = false" />
            <ul v-if="profileMenuOpen" class="absolute left-0 right-0 top-[calc(100%+5px)] z-[101] m-0 max-h-[260px] list-none overflow-y-auto rounded-[9px] border border-border bg-panel p-[5px] shadow-[0_14px_34px_-10px_#000d]">
              <li
                v-for="p in profilesStore.profiles"
                :key="p.id"
                class="flex cursor-pointer items-center justify-between gap-2 rounded-md px-[9px] py-2 text-[13px] text-secondary-foreground hover:bg-hover hover:text-foreground"
                :class="{ 'text-accent [&_svg]:text-accent': p.id === draft.profileId }"
                @click="draft.profileId = p.id; profileMenuOpen = false"
              >
                <span class="flex min-w-0 flex-col gap-0.5">
                  <span>{{ p.name }}</span>
                  <code v-if="p.configDir" class="overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-muted-foreground">{{ p.configDir }}</code>
                </span>
                <PhCheck v-if="p.id === draft.profileId" :size="13" weight="bold" />
              </li>
            </ul>
          </div>
        </div>
        <p class="-mt-1 flex items-center gap-1.5 overflow-hidden whitespace-nowrap text-[11.5px] text-secondary-foreground">
          <PhFolder :size="12" weight="fill" class="shrink-0 text-accent" /> {{ activeWs?.name }}
          <span class="overflow-hidden text-ellipsis font-mono text-[10.5px] text-muted-foreground">{{ shortCwd(draft.cwd) }}</span>
        </p>

        <!-- Feature 5 — isolate in a git worktree -->
        <label class="flex cursor-pointer items-start gap-2 text-xs text-secondary-foreground">
          <input type="checkbox" v-model="draft.isolate" />
          <span>Isolate in a git worktree <em class="font-normal not-italic text-muted-foreground">(off the active repo — parallel-safe)</em></span>
        </label>
        <div v-if="draft.isolate" class="ml-6 flex items-center gap-1.5 text-xs text-muted-foreground">
          <PhGitBranch :size="13" />
          <span class="font-mono opacity-70">mission/</span>
          <input v-model="draft.branch" class="min-w-0 flex-1 rounded-md border border-border bg-[var(--terminal-bg)] px-1.5 py-1 font-mono text-xs text-foreground focus:border-[color-mix(in_srgb,var(--accent,#6ab)_60%,var(--border))] focus:outline-none" type="text" placeholder="m-a1b2c3 (auto)" spellcheck="false" />
        </div>
        <div v-if="worktreeError" class="text-xs text-destructive">{{ worktreeError }}</div>

        <!-- Skip permission prompts — claude runs unattended (no interactive gate) -->
        <label class="flex cursor-pointer items-start gap-2 text-xs text-secondary-foreground">
          <input type="checkbox" v-model="draft.skipPerms" />
          <span><PhWarning :size="13" class="align-[-2px] text-warning" /> Skip permissions <em class="font-normal not-italic text-[color-mix(in_srgb,var(--red)_70%,var(--text-muted))]">(<code>--dangerously-skip-permissions</code> — no approval prompts; claude can run any tool)</em></span>
        </label>

        <div class="flex items-center gap-2">
          <button class="flex items-center justify-center gap-1.5 rounded-lg border border-accent bg-accent px-3 py-2 text-[13px] font-semibold text-white hover:bg-accent-dim disabled:cursor-not-allowed disabled:opacity-40" :disabled="!canRun" @click="runDraft">Run now</button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-3 py-2 text-[13px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))] disabled:cursor-not-allowed disabled:opacity-40" :disabled="!canRun" @click="enqueueDraft">Add to queue</button>
          <span class="flex-1" />
          <label class="flex items-center gap-2 text-[11px] text-secondary-foreground"><span>concurrent</span><input class="w-[52px] rounded-md border border-border bg-[var(--terminal-bg)] px-1.5 py-1 text-foreground" v-model.number="maxConcurrent" type="number" min="1" max="6" /></label>
        </div>
      </div>
    </div>

    <!-- ── Terminal modal (attach-on-demand) ───────────────────────────── -->
    <div v-if="termTaskId" class="fixed inset-0 z-[100] flex items-center justify-center bg-black/[0.63]" @click.self="closeTerminal">
      <div class="flex h-[min(680px,78vh)] w-[min(1000px,80vw)] flex-col overflow-hidden rounded-xl border border-border bg-[var(--terminal-bg)] shadow-[0_24px_64px_-16px_#000c] [-webkit-backdrop-filter:var(--blur-overlay,none)] [backdrop-filter:var(--blur-overlay,none)]">
        <header class="flex items-center gap-2 border-b border-border bg-panel px-3 py-2 text-[13px]">
          <em class="dot" :class="termTaskStatus" />
          <span>{{ termTaskTitle }}</span>
          <span class="flex-1" />
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="sendCtrlC">^C</button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="closeTerminal">close</button>
        </header>
        <div ref="termHost" class="min-h-0 flex-1 overflow-hidden p-2"></div>
      </div>
    </div>

    <!-- ── Full-transcript dialog ───────────────────────────────────────── -->
    <div v-if="transcriptOpen" class="fixed inset-0 z-[110] flex items-center justify-center bg-black/[0.73]" @click.self="closeTranscript">
      <div class="flex h-[min(760px,86vh)] w-[min(860px,90vw)] flex-col overflow-hidden rounded-[14px] border border-border bg-panel shadow-[0_24px_64px_-16px_#000c] [-webkit-backdrop-filter:var(--blur-overlay,none)] [backdrop-filter:var(--blur-overlay,none)]">
        <header class="flex items-center gap-2 border-b border-border px-3.5 py-2.5 text-[13px] font-semibold">
          <PhArticle :size="15" weight="bold" class="shrink-0 text-accent" />
          <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ transcriptTitle }}</span>
          <span class="text-[11px] font-normal text-muted-foreground" v-if="transcriptEntries.length">{{ transcriptEntries.length }} entries</span>
          <span class="flex-1" />
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="reloadTranscript" title="reload from disk"><PhArrowClockwise :size="12" /></button>
          <button class="inline-flex items-center justify-center gap-1.5 rounded-lg border border-border bg-hover px-2 py-1 text-[11px] text-foreground hover:bg-[color-mix(in_srgb,var(--text-primary)_8%,var(--bg-hover))]" @click="closeTranscript">close</button>
        </header>
        <div class="flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto px-4 py-3.5" ref="trBodyEl">
          <div v-if="transcriptLoading" class="flex items-center justify-center gap-2 py-8 text-center text-[13px] text-muted-foreground"><PhArrowClockwise :size="14" class="spin" /> loading transcript…</div>
          <div v-else-if="!transcriptEntries.length" class="flex items-center justify-center gap-2 py-8 text-center text-[13px] text-muted-foreground">No transcript found on disk yet.</div>
          <template v-else>
            <div v-for="(e, i) in transcriptEntries" :key="i" class="tr-entry grid grid-cols-[20px_1fr] items-start gap-2" :class="[e.role, e.kind, { err: e.isError }]">
              <!-- text / thinking → bubble -->
              <template v-if="e.kind === 'text' || e.kind === 'thinking'">
                <div class="tr-gutter flex justify-center pt-0.5 text-muted-foreground">
                  <PhCaretRight v-if="e.role === 'user'" :size="13" weight="bold" />
                  <PhBrain v-else-if="e.kind === 'thinking'" :size="14" />
                  <PhRobot v-else :size="14" />
                </div>
                <div class="min-w-0">
                  <div class="mb-[3px] text-[10px] uppercase tracking-[.05em] text-muted-foreground">{{ e.kind === 'thinking' ? 'thinking' : e.role }}</div>
                  <div v-if="e.kind === 'thinking'" class="whitespace-pre-wrap text-[13px] italic leading-[1.55] text-secondary-foreground opacity-85">{{ e.text }}</div>
                  <!-- eslint-disable-next-line vue/no-v-html -->
                  <div v-else-if="e.role === 'assistant'" class="tr-text md-body whitespace-pre-wrap break-words text-[13px] leading-[1.55] text-foreground" v-html="renderMd(e.text)"></div>
                  <div v-else class="whitespace-pre-wrap break-words rounded-lg border border-border bg-hover px-2.5 py-2 text-[13px] leading-[1.55] text-foreground">{{ e.text }}</div>
                </div>
              </template>
              <!-- tool call → collapsible -->
              <template v-else-if="e.kind === 'tool_use'">
                <div class="flex justify-center pt-0.5 text-muted-foreground"><PhWrench :size="13" /></div>
                <details class="tr-tool overflow-hidden rounded-lg border border-border bg-[var(--bg-base,var(--bg-panel))]">
                  <summary class="flex list-none select-none items-baseline gap-2 px-2.5 py-1.5 text-xs [&::-webkit-details-marker]:hidden hover:bg-hover"><span class="shrink-0 font-mono font-semibold text-accent">{{ e.tool }}</span><span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-muted-foreground">tool call</span></summary>
                  <pre class="tr-pre m-0 max-h-[320px] overflow-y-auto whitespace-pre-wrap break-words border-t border-border px-2.5 py-2 font-mono text-[11.5px] leading-[1.5] text-secondary-foreground">{{ e.text }}</pre>
                </details>
              </template>
              <!-- tool result → collapsible -->
              <template v-else-if="e.kind === 'tool_result'">
                <div class="flex justify-center pt-0.5 text-muted-foreground"><PhArrowBendDownRight :size="13" /></div>
                <details class="tr-tool result overflow-hidden rounded-lg border border-border bg-[var(--bg-base,var(--bg-panel))]" :class="{ err: e.isError }">
                  <summary class="flex list-none select-none items-baseline gap-2 px-2.5 py-1.5 text-xs [&::-webkit-details-marker]:hidden hover:bg-hover"><span class="shrink-0 font-mono font-semibold text-secondary-foreground" :class="{ '!text-destructive': e.isError }">{{ e.isError ? 'error' : 'result' }}</span><span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-muted-foreground">{{ trFirstLine(e.text) }}</span></summary>
                  <pre class="tr-pre m-0 max-h-[320px] overflow-y-auto whitespace-pre-wrap break-words border-t border-border px-2.5 py-2 font-mono text-[11.5px] leading-[1.5] text-secondary-foreground" :class="{ '!text-destructive': e.isError }">{{ e.text }}</pre>
                </details>
              </template>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, inject, onMounted, onBeforeUnmount, nextTick } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import {
  PhCrosshair, PhPlus, PhFolder, PhGitBranch, PhRobot, PhTerminal,
  PhArrowRight, PhArrowLeft, PhArrowClockwise, PhTrash, PhImage, PhX, PhWarning,
  PhCaretRight, PhCaretDown, PhCheck, PhArrowSquareOut, PhPaperclip, PhFile,
  PhMagnifyingGlass, PhUserGear, PhStop, PhArrowUp,
  PhArticle, PhBrain, PhWrench, PhArrowBendDownRight,
} from "@phosphor-icons/vue";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useProfilesStore } from "@/stores/profiles";
import { useUIStore } from "@/stores/ui";
import { useNotificationsStore } from "@/stores/notifications";
import { playSound } from "@/lib/sounds";
import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import { marked } from "marked";
import DOMPurify from "dompurify";

function renderMd(text: string): string {
  return DOMPurify.sanitize(marked.parse(text) as string);
}

const wsStore = useWorkspaceStore();
const profilesStore = useProfilesStore();
const ui = useUIStore();
const notifStore = useNotificationsStore();
// App.vue provides the active workspace's Terminal component (for "send to tab").
const activeTerm = inject<() => {
  spawnAgent: (cmd: string) => void;
  adoptPty: (opts: { ptyId: number; cwd: string; title: string; sessionId?: string }) => void;
  focusLeaf: (ptyId: number) => void;
} | undefined>("activeTerm", () => undefined);

// Mirror the canonical terminal status set (terminalStatus.ts → TermStatus) so MC
// behaves exactly like a real terminal tab: `permission` is a distinct amber state
// (allow/deny decision), `review` is a finished-while-away green pulse that persists
// until the task is seen. The ONLY intentional MC-specific behavior is that a `done`
// turn also captures the transcript result (captureResult), which terminals don't.
type Status = "running" | "waiting" | "permission" | "done" | "review" | "error" | "idle";
type Role = "user" | "assistant";
type TurnKind = "text" | "thinking" | "tool_use" | "tool_result";
// `kind` absent ⇒ plain text (back-compat with turns persisted before live streaming).
// thinking/tool_use/tool_result are live-only activity entries — streamed from the
// transcript while a task runs, NOT persisted (saveTask strips them, see below).
interface Turn { role: Role; text: string; kind?: TurnKind; tool?: string | null; isError?: boolean }

interface Task {
  id: string;        // crypto.randomUUID() — also Claude's --session-id
  ptyId: number;     // headless PTY id (offset id-space, see PTY_BASE)
  workspaceId: number | null; // the Burrow workspace this task belongs to
  title: string;
  prompt: string;    // the first prompt (kept for the title/seed)
  cwd: string;
  model: string;
  profileId: string; // Claude profile (config dir / binary) used to launch — kept so resume reuses it
  status: Status;
  statusDetail?: string;   // when status==='error': the error type (rate_limit|overloaded|authentication_failed|billing_error|server_error…)
  sessionSource?: string;  // last SessionStart source (startup|resume|clear|compact) — informational
  turns: Turn[];     // full conversation: user prompts + assistant replies
  followup: string;  // draft text for the in-card follow-up input
  followupImages?: string[]; // temp image paths attached to the next follow-up
  expanded: boolean; // show all turns vs last 4
  alive: boolean;
  handedOff: boolean; // PTY adopted by a real terminal tab — that tab owns input,
                      // MC keeps tracking status read-only (no double-input).
  createdAt: number;
}

// Row shape exchanged with the Rust mission_tasks table (snake_case = serde).
interface TaskRow {
  id: string;
  workspace_id: number | null;
  pty_id: number | null;
  title: string | null;
  cwd: string | null;
  model: string | null;
  status: string | null;
  turns: string | null;       // JSON-encoded Turn[]
  created_at: number;
  handed_off: number | null;  // 1 = handed off to a terminal tab
  profile_id: string | null;  // Claude profile id (NULL = default)
}

const MODELS = [
  { value: "default", label: "Default" },
  { value: "fable", label: "Fable" },
  { value: "opus", label: "Opus" },
  { value: "sonnet", label: "Sonnet" },
  { value: "haiku", label: "Haiku" },
  { value: "opusplan", label: "Opus Plan" },
];

// Mission's PTY ids live in a high range so they never collide with the main
// window's per-window counter (both share the daemon's global PTY map). The
// counter is seeded each mount from the source of truth — the persisted tasks in
// SQLite (max stored pty_id) plus the daemon's live sessions — so it never reuses
// an id whose PTY is still running. Reusing one would attach a "new task" to an
// old, still-running claude (typing the launch command into its REPL instead of a
// fresh shell). No localStorage: the DB + daemon already know every taken id.
const PTY_BASE = 2_000_000;
let ptyCounter = 0;

// Capture geometry for headless MC PTYs. A headless task has no attached xterm at
// birth, so it must be CREATED at a fixed width — and every byte the agent emits is
// produced at THIS width until a modal attaches and resizes it. The replay xterm
// must therefore render dead history at exactly MC_PTY_COLS (mismatching the width
// is what scrambled the modal: dumping 120-col TUI frames into a ~95-col fitted grid
// landed every absolute-cursor redraw off-grid). 120 ≈ the modal's inner width at
// fontSize 13, so dead history fits without horizontal scroll.
const MC_PTY_COLS = 120;
const MC_PTY_ROWS = 34;

function allocPtyId(): number {
  ptyCounter++;
  return PTY_BASE + ptyCounter;
}

// Push the sequence past an id we know is taken (restored task or live daemon
// session), so the next allocation can't collide with it.
function bumpPtySeqPast(ptyId: number) {
  if (ptyId < PTY_BASE) return;
  const seq = ptyId - PTY_BASE;
  if (seq > ptyCounter) ptyCounter = seq;
}

const tasks = ref<Task[]>([]);
// Debounce `done`: a backgrounded sub-agent (e.g. Explore) makes the main session
// park + auto-resume repeatedly, firing INTERIM Stops whose `background_tasks` slice
// momentarily empties → a `done` that's reversed by a `running` a beat later. Hold
// each `done` briefly; a live state (running/waiting/permission/error) arriving in
// the window cancels it. Only a `done` that survives is a real turn end. (Terminal.vue
// gets this for free via settleDone; MC applies hook states raw, so it needs its own.)
const DONE_DEBOUNCE_MS = 1800;
const pendingDone = new Map<string, ReturnType<typeof setTimeout>>();
function cancelPendingDone(taskId: string) {
  const h = pendingDone.get(taskId);
  if (h !== undefined) { clearTimeout(h); pendingDone.delete(taskId); }
}
const queue = ref<{ qid: string; prompt: string; cwd: string; model: string; profileId: string; isolate: boolean; branch: string; images: string[]; files: string[]; skipPerms: boolean }[]>([]);
const maxConcurrent = ref(1);

const maxConcurrentClamped = computed(() => Math.max(1, maxConcurrent.value));
const selectedId = ref<string | null>(null);
const composerOpen = ref(false);
const modelMenuOpen = ref(false);
const profileMenuOpen = ref(false);
const wsMenuOpen = ref(false);
const convoEl = ref<HTMLElement | null>(null);
const modelLabel = computed(() => MODELS.find((m) => m.value === draft.model)?.label ?? "Default");
const profileLabel = computed(() => profilesStore.get(draft.profileId)?.name ?? "Default");
// path → data-URL thumbnail for attached images (kept only for the composer preview;
// the temp file path is what's actually passed to claude).
const imagePreviews = reactive<Record<string, string>>({});

// ── Workspace scope: the active workspace, chosen in Burrow's sidebar (no picker
// of our own — the sidebar already owns selection). Every new task targets it; with
// no active workspace the whole view falls back to a blank "pick a workspace" state.
const activeWs = computed(() => wsStore.active);
function wsIcon(id: number): string | undefined {
  return wsStore.icons[id];
}
// Switching workspace re-targets the composer at the new cwd AND drops a stale
// selection: if the selected task isn't in the new scope, re-point at the newest
// task there (or clear). Otherwise the previous workspace's conversation lingers
// in the detail pane after a switch.
watch(activeWs, (w) => {
  if (w) draft.cwd = w.path;
  const scope = scopeWsIds.value;
  const cur = tasks.value.find((t) => t.id === selectedId.value);
  if (!cur || cur.workspaceId == null || !scope.has(cur.workspaceId)) {
    const next = orderedTasks.value.find((t) => t.workspaceId != null && scope.has(t.workspaceId));
    selectedId.value = next?.id ?? null;
  }
});
// Switch the active workspace from MC itself (mirrors the sidebar). Also re-scopes
// the task list + detail feed via the activeWs watcher above.
function pickWorkspace(w: Workspace) {
  wsMenuOpen.value = false;
  if (w.id === activeWs.value?.id) return;
  wsStore.open(w);
}

const draft = reactive({
  prompt: "",
  cwd: "",
  model: "default",
  profileId: "default",      // Claude profile (config dir / binary) — see profiles store
  isolate: false,            // spawn in a fresh git worktree off the active repo
  branch: "",                // optional worktree branch name (else auto mission/m-XXXXXX)
  images: [] as string[],    // temp image paths attached to the first prompt
  files: [] as string[],     // file paths tagged with @file syntax
  skipPerms: false,          // launch claude with --dangerously-skip-permissions
});

// File search state for the @-trigger picker
const fileSearchQuery = ref("");
const fileSearchResults = ref<string[]>([]);
const showFilePicker = ref(false);
const filePickerAnchor = ref<{ top: number; left: number }>({ top: 0, left: 0 });
const promptTextarea = ref<HTMLTextAreaElement | null>(null);

const canRun = computed(() => draft.prompt.trim().length > 0 && draft.cwd.trim().length > 0);
const orderedTasks = computed(() => [...tasks.value].sort((a, b) => b.createdAt - a.createdAt));
const selected = computed(() => tasks.value.find((t) => t.id === selectedId.value) || null);
// Detail feed turns, gated by the activity toggle: off → text messages only.
const visibleTurns = computed<Turn[]>(() => {
  const ts = selected.value?.turns ?? [];
  return ui.missionShowActivity ? ts : ts.filter((x) => !x.kind || x.kind === "text");
});

// A project = a Burrow workspace. Tasks group under their workspace; the label
// is the workspace name, falling back to the cwd basename for tasks whose
// workspace was deleted. Newest task first within each group.
function projectLabel(wsId: number | null, cwd: string): string {
  const w = wsId != null ? wsStore.workspaces.find((x) => x.id === wsId) : null;
  if (w) return w.name;
  return cwd.split("/").filter(Boolean).pop() || cwd;
}
// Tasks are scoped to the active workspace: only its own tasks + tasks living in
// a worktree off it (isolate spawns own workspace rows whose parent is the repo).
// Climb to the repo root first so the scope is the same whether you're sitting on
// the repo or one of its worktrees.
const scopeWsIds = computed<Set<number>>(() => {
  const a = activeWs.value;
  if (!a) return new Set();
  const root = a.parent_id != null ? (wsStore.workspaces.find((w) => w.id === a.parent_id) ?? a) : a;
  const ids = new Set<number>([root.id]);
  for (const w of wsStore.workspaces) if (w.parent_id === root.id) ids.add(w.id);
  return ids;
});
const projects = computed(() => {
  const scope = scopeWsIds.value;
  const groups = new Map<string, { label: string; tasks: Task[] }>();
  for (const t of orderedTasks.value) {
    if (t.workspaceId == null || !scope.has(t.workspaceId)) continue; // other workspace → hide
    const key = `ws:${t.workspaceId}`;
    if (!groups.has(key)) groups.set(key, { label: projectLabel(t.workspaceId, t.cwd), tasks: [] });
    groups.get(key)!.tasks.push(t);
  }
  return [...groups.entries()].map(([key, g]) => ({ key, label: g.label, tasks: g.tasks }));
});

// Resolve the workspace a cwd belongs to (exact path match), else the active
// workspace — so a task spawned in the active project nests under it.
function workspaceIdForCwd(cwd: string): number | null {
  const exact = wsStore.workspaces.find((w) => w.path === cwd);
  if (exact) return exact.id;
  return wsStore.active?.id ?? null;
}

function openComposer() {
  if (!activeWs.value) return;  // no active workspace → blank state covers the view
  draft.cwd = activeWs.value.path;
  composerOpen.value = true;
}

// ── Per-PTY plumbing: raw byte buffer (for terminal replay) + listeners ──────
const buffers = new Map<number, string>();      // ptyId → accumulated decoded output
const unlisteners = new Map<number, UnlistenFn[]>();

function countBy(s: Status) {
  const scope = scopeWsIds.value;
  return tasks.value.filter((t) => t.status === s && t.workspaceId != null && scope.has(t.workspaceId)).length;
}

// Selecting a task = the user has now SEEN it (mirrors Terminal.vue's markTabSeen).
// A `review` (finished-while-away) badge clears to a settled `done` once read. An
// `error` is left flagged red — a failed turn isn't "fine once glanced at"; it stays
// until the user acts on it (resume / follow-up re-enters via `running`).
function selectTask(t: Task) {
  selectedId.value = t.id;
  if (t.status === "review") {
    t.status = "done";
    saveTask(t);
  }
}

function statusLabel(t: Task): string {
  switch (t.status) {
    case "running": return "working…";
    case "waiting": return "waiting for input";
    case "permission": return "needs permission";
    case "done": return "finished";
    case "review": return "finished — unread";
    case "error": return "error: " + (t.statusDetail || "failed");
    default: return t.alive ? "idle" : "stopped";
  }
}

function shortCwd(p: string) {
  const home = "/Users/";
  return p.replace(home + (p.split("/")[2] || ""), "~");
}

// ── Spawn ────────────────────────────────────────────────────────────────────
function shquote(s: string) {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}

// Build the launch prefix + binary for a profile: a CLAUDE_CONFIG_DIR env override
// (so a profile is a separate Claude config/account) + the binary + extra args.
function profileLaunch(profileId: string): { env: string; bin: string; args: string } {
  const p = profilesStore.get(profileId);
  const bin = (p?.command || "").trim() || "claude";
  const env = p?.configDir?.trim() ? `CLAUDE_CONFIG_DIR=${shquote(p.configDir.trim())} ` : "";
  const args = p?.args?.trim() ? ` ${p.args.trim()}` : "";
  return { env, bin, args };
}

async function spawnTask(prompt: string, cwd: string, model: string, images: string[] = [], skipPerms = false, files: string[] = [], profileId = "default"): Promise<Task> {
  const id = crypto.randomUUID();
  const ptyId = allocPtyId();
  // Prepend @file refs if the user tagged files via the picker (not already in prompt)
  const fileRefs = files.filter((f) => !prompt.includes(`@${f}`)).map((f) => `@${f}`).join("\n");
  const fullPrompt = fileRefs ? `${fileRefs}\n\n${prompt.trim()}` : prompt.trim();
  const task: Task = {
    id, ptyId,
    workspaceId: workspaceIdForCwd(cwd.trim()),
    title: prompt.trim().split("\n")[0].slice(0, 48) || "task",
    prompt: fullPrompt,
    cwd: cwd.trim(),
    model,
    profileId,
    status: "running",
    turns: [{ role: "user", text: prompt.trim() + (images.length ? `\n📎 ${images.length} image${images.length === 1 ? "" : "s"}` : "") + (files.length ? `\n📄 ${files.length} file${files.length === 1 ? "" : "s"} tagged` : "") }],
    followup: "",
    expanded: false,
    alive: true,
    handedOff: false,
    createdAt: Date.now(),
  };
  tasks.value.push(task);
  saveTask(task);

  buffers.set(ptyId, "");
  await wireTask(task);

  // Headless PTY = a shell; then we type the claude command into it.
  await invoke("create_pty", { id: ptyId, cwd: task.cwd, cols: MC_PTY_COLS, rows: MC_PTY_ROWS });

  const modelFlag = model && model !== "default" ? ` --model ${model}` : "";
  const permFlag = skipPerms ? " --dangerously-skip-permissions" : "";
  const imageFlags = images.map((p) => ` ${shquote(p)}`).join("");  // claude reads image paths as positional args
  const { env, bin, args: profileArgs } = profileLaunch(profileId);
  const cmd = `${env}${bin} --session-id ${id}${modelFlag}${permFlag}${profileArgs} ${shquote(fullPrompt)}${imageFlags}\n`;
  // Small delay so the shell rc has finished and won't swallow the line.
  setTimeout(() => {
    invoke("write_pty", { id: ptyId, data: Array.from(new TextEncoder().encode(cmd)) }).catch(() => {});
  }, 450);

  return task;
}

async function wireTask(task: Task) {
  const offs: UnlistenFn[] = [];

  // PTY output → replay buffer (+ live terminal if attached).
  offs.push(await listen<number[]>(`pty-data-${task.ptyId}`, (ev) => {
    const bytes = new Uint8Array(ev.payload);
    const text = new TextDecoder().decode(bytes);
    let buf = (buffers.get(task.ptyId) || "") + text;
    if (buf.length > 262_144) buf = buf.slice(-262_144); // cap replay scrollback
    buffers.set(task.ptyId, buf);
    if (termTaskId.value === task.id && termInstance) termInstance.write(bytes);
  }));

  // Status dot via the global hook server (same channel Burrow tabs use).
  // Payload is backward-compatible: a plain string for the simple states, or a
  // structured object `{ state, detail?, model?, source?, title? }` for the new
  // `error` (failed turn) + `session` (SessionStart metadata) events.
  offs.push(await listen<string | { state: string; detail?: string; model?: string; source?: string; title?: string }>(`pty-hook-${task.ptyId}`, (ev) => {
    const p = typeof ev.payload === "string" ? { state: ev.payload } : (ev.payload || { state: "" });
    const state = p.state;
    const t = tasks.value.find((x) => x.id === task.id);
    if (!t) return;
    const prev = t.status;
    // `session` carries SessionStart metadata, NOT a turn boundary — never touch status.
    // Populate model only if the user didn't pick one (keep their choice), and the
    // title only when the task is still on its auto-generated seed (don't clobber).
    if (state === "session") {
      if (p.model && (!t.model || t.model === "default")) t.model = p.model;
      if (p.title && (!t.title || t.title === "task")) t.title = p.title;
      if (p.source) t.sessionSource = p.source;
      saveTask(t);
      return;
    }
    // Any live signal cancels a still-pending `done` — the prior Stop was interim
    // (sub-agent park/resume), the turn is actually still in flight.
    if (state === "running" || state === "waiting" || state === "permission" || state === "error") {
      cancelPendingDone(t.id);
    }
    if (state === "running") {
      // A failed turn (`error`) is terminal + attention-needing: a stray `running`
      // ping must NOT silently resume it unless the turn genuinely restarts. Claude
      // re-enters a real new turn via UserPromptSubmit, which also fires `running`,
      // so we only clear the error when the user has actually sent a follow-up —
      // signalled by a trailing user turn after the error was recorded.
      if (prev === "error") {
        const lastTurn = t.turns[t.turns.length - 1];
        if (lastTurn?.role !== "user") return; // no resume yet → keep the error sticky
        t.statusDetail = undefined;
      }
      t.status = "running";
    }
    // `waiting` (a blocking question) and `permission` (an allow/deny request) are
    // DISTINCT states — exactly like a real terminal tab (waiting = blue, permission
    // = amber). Both only matter while a turn is in flight: after the turn ends Claude
    // can fire a late idle ping, which must NOT drag a finished task back out of
    // `done`/`review`/`error`. A genuine follow-up re-enters via UserPromptSubmit →
    // `running`, so we only honor them when coming from a live state.
    else if (state === "waiting") {
      if (prev === "running" || prev === "waiting" || prev === "permission") t.status = "waiting";
    }
    else if (state === "permission") {
      if (prev === "running" || prev === "waiting" || prev === "permission") t.status = "permission";
    }
    // `error` = the turn failed (rate_limit|overloaded|authentication_failed|
    // billing_error|server_error…). Terminal like `done`, but surfaced red so the
    // user sees WHY. `p.detail` carries the error type.
    else if (state === "error") {
      t.status = "error";
      t.statusDetail = p.detail || "failed";
      pumpQueue();   // turn is over → free the slot
    }
    else if (state === "done") {
      // Debounced: a backgrounded sub-agent's interim Stop also fires `done`, then
      // resumes (`running`) a beat later. Defer the commit; if a live state lands in
      // the window, cancelPendingDone above drops this. Only a surviving `done` is real.
      cancelPendingDone(t.id);
      pendingDone.set(t.id, setTimeout(() => {
        pendingDone.delete(t.id);
        const tt = tasks.value.find((x) => x.id === task.id);
        if (!tt) return;
        const before = tt.status;
        // Same done/review split as Terminal.vue's settleDone: watching → transient
        // `done`; away → `review` (green pulse persists until opened).
        tt.status = isWatching(tt) ? "done" : "review";
        setTimeout(() => captureResult(tt), 600);  // transcript flushes a beat after Stop
        pumpQueue();   // free slot → start the next queued prompt
        if (tt.status !== before && (tt.status === "done" || tt.status === "review")) notifyTask(tt, "done");
        saveTask(tt);
      }, DONE_DEBOUNCE_MS));
      return;  // commit happens in the timer; nothing to surface yet
    }
    // Notify only on a real transition INTO a needs-attention/finished state, and only
    // when the user isn't already looking at this task (Superset-style "finished while
    // away"). notifyTask itself no-ops when watching.
    if (t.status !== prev) {
      if (t.status === "done" || t.status === "review") notifyTask(t, "done");
      else if (t.status === "error") notifyTask(t, "error");
      else if (t.status === "waiting" || t.status === "permission") notifyTask(t, "waiting");
    }
    saveTask(t);
  }));

  unlisteners.set(task.ptyId, offs);
}

// Feature #9 — notifications. The user is "watching" a task only when the Mission
// Control view is up, that task is selected, and the window is focused. If so, the
// transition is already visible → stay quiet. Otherwise: toast + sound, plus a
// system notification when the window isn't focused (mirrors Terminal.vue).
function isWatching(t: Task): boolean {
  return ui.mode === "mission" && selectedId.value === t.id && document.hasFocus();
}

async function notifyTask(t: Task, kind: "done" | "waiting" | "error") {
  if (isWatching(t)) return;
  const title = kind === "done" ? "Task complete" : kind === "error" ? "Task error" : "Task needs input";
  const detail = kind === "error" ? ` — ${t.statusDetail || "failed"}` : "";
  const body = (t.title || (kind === "done" ? "Agent finished" : kind === "error" ? "Agent failed" : "Claude is waiting")) + detail;
  notifStore.push({ type: kind === "done" ? "done" : kind === "error" ? "error" : "info", title, body, workspaceId: t.workspaceId ?? undefined });
  // No `error` sound preset — reuse the attention chime.
  playSound(kind === "error" ? "waiting" : kind);
  if (!document.hasFocus()) {
    try {
      let granted = await isPermissionGranted();
      if (!granted) granted = (await requestPermission()) === "granted";
      const icon = kind === "done" ? "✓" : kind === "error" ? "⚠️" : "⏳";
      if (granted) sendNotification({ title: "Burrow", body: `${icon} ${body}` });
    } catch { /* notifications optional */ }
  }
}

// A task's transcript lives under its profile's CLAUDE_CONFIG_DIR (default = ~/.claude).
function taskConfigDir(t: Task): string {
  return profilesStore.get(t.profileId)?.configDir?.trim() || "";
}

async function captureResult(t: Task) {
  try {
    const out = await invoke<{ text: string; error: { status: number | null; message: string } | null }>(
      "read_claude_outcome", { cwd: t.cwd, sessionId: t.id, configDir: taskConfigDir(t) },
    );
    // Feature C — surface API errors (429/529/…) as a distinct `error` state
    // instead of a false `done`. tank does the same via isApiErrorMessage.
    if (out.error) {
      t.status = "error";
      const reason = apiErrorReason(out.error);
      const last = t.turns[t.turns.length - 1];
      if (!last || last.text !== reason) {
        t.turns.push({ role: "assistant", text: reason });
        if (t.id === selectedId.value) scrollConvo();
      }
      if (!isWatching(t)) {
        notifStore.push({ type: "error", title: "Task error", body: `${t.title} — ${reason.split("\n")[0]}`, workspaceId: t.workspaceId ?? undefined });
        playSound("waiting");
      }
      saveTask(t);
      return;
    }
    if (!out.text) return;
    // Append as an assistant turn — but only if it's new (each `done` reads the
    // transcript's *latest* assistant message; a follow-up produces a fresh one).
    const lastAssistant = [...t.turns].reverse().find((x) => x.role === "assistant");
    if (!lastAssistant || lastAssistant.text !== out.text) {
      t.turns.push({ role: "assistant", text: out.text });
      if (t.id === selectedId.value) scrollConvo();
      saveTask(t);
    }
  } catch { /* best-effort */ }
}

// Live streaming: while a task runs, its JSONL transcript grows one block at a time
// (per-message, not per-token — text, thinking, tool_use, tool_result). Rebuild the
// detail feed from the transcript each tick so it fills in as the turn progresses,
// instead of staying on "working…" until the final `captureResult`. The transcript
// is the source of truth (clean dedup for repeated tool inputs, reload-safe), with
// ONE exception: a just-submitted follow-up user turn is pushed optimistically by
// sendFollowup and lags the transcript by <1s — preserve it at the tail so it
// doesn't flicker out for a tick.
function turnsEqual(a: Turn[], b: Turn[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i].role !== b[i].role || a[i].kind !== b[i].kind || a[i].text !== b[i].text) return false;
  }
  return true;
}

async function syncLiveTurns(t: Task) {
  let rows: TranscriptEntry[];
  try {
    rows = await invoke<TranscriptEntry[]>("read_claude_transcript", {
      cwd: t.cwd, sessionId: t.id, configDir: taskConfigDir(t),
    });
  } catch { return; }
  if (!rows?.length) return;
  const derived: Turn[] = rows
    .filter((r) => (r.text && r.text.trim()) || r.kind === "tool_use" || r.kind === "tool_result")
    .map((r) => ({
      role: r.role === "assistant" ? "assistant" : "user",
      text: r.text || "",
      kind: (r.kind === "image" ? "text" : r.kind) as TurnKind,
      tool: r.tool,
      isError: r.isError,
    }));
  // Preserve an optimistic follow-up user message not yet flushed to the transcript.
  // Match by core text (strip our "📎 N images" annotation + collapse whitespace):
  // the transcript stores the raw prompt — newline-collapsed, image paths appended —
  // so an exact compare would miss it and duplicate the turn once it lands.
  const last = t.turns[t.turns.length - 1];
  if (last?.role === "user" && (!last.kind || last.kind === "text")) {
    const core = last.text.split("\n📎")[0].replace(/\s+/g, " ").trim();
    const present = core.length > 0 && derived.some(
      (d) => d.role === "user" && d.kind === "text" && d.text.replace(/\s+/g, " ").includes(core),
    );
    if (!present) derived.push({ role: "user", text: last.text, kind: "text" });
  }
  if (turnsEqual(derived, t.turns)) return;
  t.turns = derived;
  if (t.id === selectedId.value) scrollConvo();
  saveTask(t);
}

function apiErrorReason(e: { status: number | null; message: string }): string {
  const code = e.status ? ` (HTTP ${e.status})` : "";
  const hint = e.status === 429 ? " — rate limited"
    : e.status === 529 ? " — Anthropic overloaded"
    : "";
  const msg = (e.message || "").trim();
  return `⚠️ API error${code}${hint}${msg ? `\n${msg}` : ""}`;
}

// ── Follow-up: claude is still alive at its prompt in the same PTY, so we just
// type the next message into it (no --resume needed — that's tank's trick for a
// killed session; ours never dies). UserPromptSubmit hook flips status back to
// running automatically.
function sendFollowup(t: Task) {
  const text = t.followup.trim();
  const imgs = t.followupImages ?? [];
  if ((!text && !imgs.length) || t.status === "running" || !t.alive) return;
  const turnText = text + (imgs.length ? `${text ? "\n" : ""}📎 ${imgs.length} image${imgs.length === 1 ? "" : "s"}` : "");
  t.turns.push({ role: "user", text: turnText });
  t.followup = "";
  for (const p of imgs) delete imagePreviews[p];
  t.followupImages = [];
  t.status = "running";
  // Collapse newlines: claude's REPL submits on Enter, so a raw \n would split
  // the message. Image paths are appended bare (claude reads image file paths
  // referenced in the prompt).
  const imgPart = imgs.map((p) => shquote(p)).join(" ");
  const body = (text.replace(/\r?\n/g, " ") + (imgPart ? ` ${imgPart}` : "")).trim();
  // Two writes: claude's REPL treats a fast text+CR burst as a bracketed paste,
  // so a trailing \r lands as a literal newline (text applied, never submitted).
  // Send the body, then submit with a standalone Enter a beat later.
  const enc = (s: string) => Array.from(new TextEncoder().encode(s));
  invoke("write_pty", { id: t.ptyId, data: enc(body) })
    .then(() => new Promise((r) => setTimeout(r, 40)))
    .then(() => invoke("write_pty", { id: t.ptyId, data: enc("\r") }))
    .catch(() => {});
  scrollConvo();
  saveTask(t);
}

// Keep the conversation pinned to the newest turn when it's the open task.
function scrollConvo() {
  nextTick(() => {
    if (convoEl.value) convoEl.value.scrollTop = convoEl.value.scrollHeight;
  });
}

// ── Composer + queue ─────────────────────────────────────────────────────────
// Feature 5 — optionally spawn the task in a fresh git worktree off the workspace
// owning `cwd`, so parallel tasks never clobber each other's working tree. Mirrors
// the New-worktree dialog's path convention: <worktreesDir>/<repo>/<branch>.
async function resolveCwd(branchName: string, cwd: string, isolate: boolean): Promise<string> {
  if (!isolate) return cwd.trim();
  const wid = workspaceIdForCwd(cwd.trim());
  const repo = wsStore.workspaces.find((w) => w.id === wid) ?? wsStore.active;
  if (!repo || repo.parent_id) {  // need a top-level repo (no worktree-of-worktree)
    worktreeError.value = "Isolate needs a git-repo workspace (not a worktree).";
    return "";
  }
  const repoName = repo.path.split("/").filter(Boolean).pop() || "repo";
  // A typed name is slugified; otherwise a clean auto name — never the prompt text.
  const name = branchName.trim() ? slugify(branchName) : `m-${crypto.randomUUID().slice(0, 6)}`;
  const branch = `mission/${name}`;
  const path = `${ui.worktreesDir}/${repoName}/${branch}`;
  try {
    const wt = await wsStore.createWorktree(repo.id, branch, "HEAD", path);
    return wt.path;
  } catch (e) {
    worktreeError.value = `Worktree failed: ${e}`;
    return "";
  }
}

function slugify(s: string): string {
  return s.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 32) || "task";
}

const worktreeError = ref("");

async function runDraft() {
  if (!canRun.value) return;
  worktreeError.value = "";
  const cwd = await resolveCwd(draft.branch, draft.cwd, draft.isolate);
  if (!cwd) return;  // worktree creation failed — error shown, keep the modal open
  const images = [...draft.images];
  const files = [...draft.files];
  const t = await spawnTask(draft.prompt, cwd, draft.model, images, draft.skipPerms, files, draft.profileId);
  selectedId.value = t.id;        // jump straight into the new task's detail
  resetDraft();
  composerOpen.value = false;
}

function enqueueDraft() {
  if (!canRun.value) return;
  // Queued tasks resolve their cwd at spawn time (so worktrees aren't created
  // until they actually run); the isolate flag rides along.
  queue.value.push({ qid: crypto.randomUUID(), prompt: draft.prompt.trim(), cwd: draft.cwd.trim(), model: draft.model, profileId: draft.profileId, isolate: draft.isolate, branch: draft.branch.trim(), images: [...draft.images], files: [...draft.files], skipPerms: draft.skipPerms });
  resetDraft();
  composerOpen.value = false;
  pumpQueue();
}

function resetDraft() {
  draft.prompt = "";
  for (const p of draft.images) delete imagePreviews[p];
  draft.images = [];
  draft.files = [];
  draft.profileId = "default";
  draft.isolate = false;
  draft.branch = "";
  draft.skipPerms = false;
  worktreeError.value = "";
  showFilePicker.value = false;
}

// Feature 3 — image attachments. Paste or drop images into the composer; each is
// saved to a temp file (save_temp_image) and its path is passed to claude as a
// positional arg on spawn (claude reads image paths from argv).
async function addImageFiles(files: FileList | File[], sink: string[] = draft.images) {
  for (const f of Array.from(files)) {
    if (!f.type.startsWith("image/")) continue;
    try {
      const b64 = await fileToBase64(f);
      const ext = (f.type.split("/")[1] || "png").replace("jpeg", "jpg");
      const path = await invoke<string>("save_temp_image", { b64, ext });
      sink.push(path);
      imagePreviews[path] = `data:${f.type || "image/png"};base64,${b64}`; // thumbnail preview
    } catch { /* skip unreadable image */ }
  }
}

function fileToBase64(f: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(",")[1] || "");
    r.onerror = reject;
    r.readAsDataURL(f);
  });
}

function onComposerPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items;
  if (!items) return;
  const imgs = Array.from(items).filter((i) => i.type.startsWith("image/")).map((i) => i.getAsFile()).filter(Boolean) as File[];
  if (imgs.length) { e.preventDefault(); addImageFiles(imgs); }
}

function onComposerDrop(e: DragEvent) {
  const files = e.dataTransfer?.files;
  if (files?.length) { e.preventDefault(); addImageFiles(files); }
}

function removeImage(i: number) {
  const [p] = draft.images.splice(i, 1);
  if (p) delete imagePreviews[p];
}

// Follow-up image attachments — same temp-file pipeline as the composer, but the
// paths ride along with the next follow-up message instead of a fresh spawn.
function onFollowupPaste(e: ClipboardEvent, t: Task) {
  const items = e.clipboardData?.items;
  if (!items) return;
  const imgs = Array.from(items).filter((i) => i.type.startsWith("image/")).map((i) => i.getAsFile()).filter(Boolean) as File[];
  if (imgs.length) { e.preventDefault(); if (!t.followupImages) t.followupImages = []; addImageFiles(imgs, t.followupImages); }
}

function onFollowupDrop(e: DragEvent, t: Task) {
  const files = e.dataTransfer?.files;
  if (files?.length) { e.preventDefault(); if (!t.followupImages) t.followupImages = []; addImageFiles(files, t.followupImages); }
}

function removeFollowupImage(t: Task, i: number) {
  const [p] = (t.followupImages ?? []).splice(i, 1);
  if (p) delete imagePreviews[p];
}

// ── File tagging (@ references) ──────────────────────────────────────────────
// Files are included as @/absolute/path in the prompt — Claude Code reads these
// as file-content injections. Images stay separate (positional argv); plain files
// go into the prompt string so they're visible to the user in the conversation.

async function pickFiles() {
  try {
    const result = await openDialog({
      multiple: true,
      directory: false,
      defaultPath: draft.cwd || undefined,
    });
    if (!result) return;
    const paths = Array.isArray(result) ? result : [result];
    for (const p of paths) {
      if (!draft.files.includes(p)) draft.files.push(p);
    }
  } catch { /* dialog cancelled */ }
}

function removeFile(i: number) {
  draft.files.splice(i, 1);
}

function fileBasename(p: string) {
  return p.split("/").pop() || p;
}

// @-trigger in textarea: when user types @ we run a fuzzy search against the
// workspace dir and show a small inline picker.
async function onPromptInput(e: Event) {
  const ta = e.target as HTMLTextAreaElement;
  const val = ta.value;
  const pos = ta.selectionStart;
  // Find last @ before cursor that isn't preceded by a non-space char
  const before = val.slice(0, pos);
  const atIdx = before.lastIndexOf("@");
  if (atIdx === -1 || (atIdx > 0 && !/\s/.test(before[atIdx - 1]))) {
    showFilePicker.value = false;
    return;
  }
  const query = before.slice(atIdx + 1);
  if (query.includes(" ") || query.includes("\n")) {
    showFilePicker.value = false;
    return;
  }
  fileSearchQuery.value = query;
  // Search the workspace dir
  try {
    const entries = await invoke<{ name: string; is_dir: boolean }[]>(
      "read_dir_shallow", { path: draft.cwd }
    );
    const q = query.toLowerCase();
    const base = draft.cwd.replace(/\/$/, "");
    fileSearchResults.value = entries
      .filter((e) => !e.is_dir && e.name.toLowerCase().includes(q))
      .map((e) => `${base}/${e.name}`)
      .slice(0, 8);
    showFilePicker.value = fileSearchResults.value.length > 0;
    // Position the picker near the textarea cursor (best-effort)
    if (showFilePicker.value && ta) {
      const rect = ta.getBoundingClientRect();
      filePickerAnchor.value = { top: rect.bottom + 4, left: rect.left };
    }
  } catch {
    showFilePicker.value = false;
  }
}

function selectFileFromPicker(path: string) {
  // Replace the @<query> fragment in the textarea with @path
  const ta = promptTextarea.value;
  if (!ta) { draft.files.push(path); showFilePicker.value = false; return; }
  const val = ta.value;
  const pos = ta.selectionStart;
  const before = val.slice(0, pos);
  const atIdx = before.lastIndexOf("@");
  const after = val.slice(pos);
  draft.prompt = before.slice(0, atIdx) + `@${path}` + after;
  showFilePicker.value = false;
  nextTick(() => { ta.focus(); const np = atIdx + path.length + 1; ta.setSelectionRange(np, np); });
}

// Feature 4 — hand the task off to a real Burrow terminal tab WITHOUT killing it.
// The same daemon PTY is *adopted* by a terminal tab (create_pty reattaches the live
// session — no `--resume`, no new process). The terminal tab now owns input; Mission
// Control flips `handedOff` so its follow-up bar + embedded-terminal attach lock out
// (no two writers on one PTY), but keeps its status listeners — the dot stays live,
// since `pty-hook-{id}` broadcasts to every listener. Re-handing-off just focuses the
// existing tab. The flag persists (DB), so input stays locked across an app restart.
function handoffToTab(t: Task) {
  if (!t.alive) return;
  // Drop the in-MC embedded terminal if it's attached to this task — that's another
  // input path into the same PTY, and the tab is taking over.
  if (termTaskId.value === t.id) closeTerminal();
  t.handedOff = true;
  saveTask(t);
  ui.setMode("terminal");
  const wsRow = wsStore.workspaces.find((w) => w.id === t.workspaceId) || wsStore.active;
  if (wsRow) wsStore.open(wsRow);
  // Defer until the workspace's Terminal is mounted/active, then adopt the live PTY.
  setTimeout(() => activeTerm()?.adoptPty({ ptyId: t.ptyId, cwd: t.cwd, title: t.title, sessionId: t.id }), 80);
}

// Jump back to the terminal tab that owns a handed-off task's PTY.
function focusHandoff(t: Task) {
  ui.setMode("terminal");
  const wsRow = wsStore.workspaces.find((w) => w.id === t.workspaceId) || wsStore.active;
  if (wsRow) wsStore.open(wsRow);
  setTimeout(() => activeTerm()?.adoptPty({ ptyId: t.ptyId, cwd: t.cwd, title: t.title, sessionId: t.id }), 80);
}

// Feature A — resume a dead (read-only) task in place. Spawn a fresh mission PTY
// running `claude --resume <session-id>`: the conversation reloads from the
// transcript and the task goes live again (follow-up works), without leaving
// Mission Control. Complements reconcileLive (which only re-binds PTYs the daemon
// still holds); this revives a task whose PTY is truly gone.
async function resumeTask(t: Task) {
  if (t.alive) return;
  // Drop any listeners/buffer left under the OLD ptyId before we reassign — a task
  // that died mid-session was wired under its previous id; without this those
  // pty-data/pty-hook listeners orphan (and the stale replay buffer lingers).
  teardown(t.ptyId);
  buffers.delete(t.ptyId);
  const ptyId = allocPtyId();
  t.ptyId = ptyId;
  t.alive = true;
  t.status = "running";
  buffers.set(ptyId, "");
  await wireTask(t);
  await invoke("create_pty", { id: ptyId, cwd: t.cwd, cols: MC_PTY_COLS, rows: MC_PTY_ROWS });
  // Resume under the SAME profile's config dir — sessions are stored per
  // CLAUDE_CONFIG_DIR, so the default binary wouldn't find this session.
  const { env, bin, args: profileArgs } = profileLaunch(t.profileId);
  const cmd = `${env}${bin} --resume ${t.id}${profileArgs}\n`;
  setTimeout(() => {
    invoke("write_pty", { id: ptyId, data: Array.from(new TextEncoder().encode(cmd)) }).catch(() => {});
    // No turn runs on a bare resume — claude just reloads and waits at its prompt.
    setTimeout(() => { if (t.status === "running") t.status = "waiting"; }, 1500);
  }, 450);
  saveTask(t);
}

// Sequential runner: keep activeCount up to maxConcurrent.
function activeCount() {
  return tasks.value.filter((t) => t.alive && (t.status === "running" || t.status === "waiting")).length;
}
async function pumpQueue() {
  while (queue.value.length && activeCount() < maxConcurrentClamped.value) {
    const next = queue.value.shift()!;
    const cwd = await resolveCwd(next.branch, next.cwd, next.isolate);
    if (!cwd) continue;  // worktree failed — drop this item, keep draining
    spawnTask(next.prompt, cwd, next.model, next.images, next.skipPerms, next.files ?? [], next.profileId ?? "default");
  }
}

// ── Stop / cleanup ───────────────────────────────────────────────────────────
// Interrupt the current turn without killing the session. claude's REPL cancels
// the in-flight turn on ESC (0x1b) — Ctrl+C there only *arms exit* ("press again
// to exit") and a second one kills the whole session, breaking follow-up. ESC
// returns claude to its prompt with the session intact.
function stopGeneration(t: Task) {
  if (!t.alive || t.status !== "running") return;
  invoke("write_pty", { id: t.ptyId, data: [0x1b] }).catch(() => {});
  // Optimistic flip — the Stop hook confirms, but the dot shouldn't stay orange
  // if the interrupt lands before any hook fires.
  t.status = "waiting";
}

// Kill the PTY and drop the task entirely (header "Delete").
function deleteTask(t: Task) {
  invoke("kill_pty", { id: t.ptyId }).catch(() => {});
  cancelPendingDone(t.id);
  teardown(t.ptyId);
  buffers.delete(t.ptyId);
  tasks.value = tasks.value.filter((x) => x.id !== t.id);
  if (selectedId.value === t.id) selectedId.value = tasks.value[0]?.id ?? null;
  invoke("delete_mission_task", { id: t.id }).catch(() => {});
  pumpQueue();
}

function teardown(ptyId: number) {
  unlisteners.get(ptyId)?.forEach((u) => u());
  unlisteners.delete(ptyId);
}

function clearDead() {
  const removed = tasks.value.filter((t) => !t.alive);
  removed.forEach((t) => {
    teardown(t.ptyId);
    buffers.delete(t.ptyId);
    invoke("delete_mission_task", { id: t.id }).catch(() => {});
  });
  tasks.value = tasks.value.filter((t) => t.alive);
  if (selectedId.value && !tasks.value.some((t) => t.id === selectedId.value)) {
    selectedId.value = tasks.value[0]?.id ?? null;
  }
}

// ── Full-transcript dialog ────────────────────────────────────────────────────
interface TranscriptEntry {
  role: "user" | "assistant" | "system";
  kind: "text" | "thinking" | "tool_use" | "tool_result" | "image";
  text: string;
  tool: string | null;
  isError?: boolean;
  ts: string | null;
}
const transcriptOpen = ref(false);
const transcriptLoading = ref(false);
const transcriptEntries = ref<TranscriptEntry[]>([]);
const transcriptTaskId = ref<string | null>(null);
const trBodyEl = ref<HTMLElement | null>(null);
const transcriptTitle = computed(() => tasks.value.find((t) => t.id === transcriptTaskId.value)?.title ?? "Transcript");

function trFirstLine(s: string): string {
  const line = (s || "").split("\n").find((l) => l.trim()) ?? "";
  return line.length > 80 ? line.slice(0, 80) + "…" : line;
}

async function loadTranscript(t: Task) {
  transcriptLoading.value = true;
  try {
    const rows = await invoke<TranscriptEntry[]>("read_claude_transcript", {
      cwd: t.cwd, sessionId: t.id, configDir: taskConfigDir(t),
    });
    transcriptEntries.value = rows ?? [];
  } catch {
    transcriptEntries.value = [];
  } finally {
    transcriptLoading.value = false;
  }
  await nextTick();
  if (trBodyEl.value) trBodyEl.value.scrollTop = trBodyEl.value.scrollHeight;
}

function openTranscript(t: Task) {
  transcriptTaskId.value = t.id;
  transcriptEntries.value = [];
  transcriptOpen.value = true;
  loadTranscript(t);
}

function reloadTranscript() {
  const t = tasks.value.find((x) => x.id === transcriptTaskId.value);
  if (t) loadTranscript(t);
}

function closeTranscript() {
  transcriptOpen.value = false;
  transcriptTaskId.value = null;
  transcriptEntries.value = [];
}

// ── Terminal modal ───────────────────────────────────────────────────────────
const termHost = ref<HTMLElement | null>(null);
const termTaskId = ref<string | null>(null);
let termInstance: Terminal | null = null;
let termFit: FitAddon | null = null;
let termResizeObs: ResizeObserver | null = null;
let termInputOff: (() => void) | null = null;

const termTaskTitle = computed(() => tasks.value.find((t) => t.id === termTaskId.value)?.title ?? "");
const termTaskStatus = computed(() => tasks.value.find((t) => t.id === termTaskId.value)?.status ?? "idle");

async function openTerminal(t: Task) {
  termTaskId.value = t.id;
  await nextTick();
  const css = getComputedStyle(document.documentElement);
  const cssVar = (n: string, fb: string) => css.getPropertyValue(n).trim() || fb;
  // Width invariant: an xterm grid renders a byte stream FAITHFULLY only when its
  // column count equals the width that stream was produced at. Violating it is the
  // whole bug — dumping 120-col TUI frames (the capture geometry of a headless MC
  // PTY) into a ~95-col fitted grid lands every absolute-cursor redraw off-grid, so
  // historical frames overlap into the scramble seen in the modal. So we split by
  // liveness instead of blindly fitting + replaying:
  //   • ALIVE  → adopt the FITTED geometry: resize the PTY to it, wipe the grid
  //              (scrollback included), and SIGWINCH so the agent repaints its whole
  //              screen clean at the new width. The un-replayable 120-col history is
  //              dropped on purpose — a live agent always has a current screen to
  //              repaint, so there's nothing to lose and everything to unscramble.
  //   • DEAD   → can't repaint (PTY is gone), so render the captured buffer at its
  //              OWN capture width (MC_PTY_COLS) with NO fit. On-grid, faithful, and
  //              ≈ the modal's inner width so it fits without horizontal scroll.
  // Integer fontSize keeps cell metrics clean (same fix as XTerm.vue).
  termInstance = new Terminal({
    cursorBlink: true,
    fontFamily: cssVar("--font-mono", "ui-monospace, SFMono-Regular, Menlo, monospace"),
    fontSize: 13,
    theme: {
      background: cssVar("--terminal-bg", "#0a0a0a"),
      foreground: cssVar("--text-primary", "#e6edf3"),
    },
    scrollback: 5000,
  });
  termFit = new FitAddon();
  termInstance.loadAddon(termFit);
  termInstance.open(termHost.value!);

  const onData = termInstance.onData((data) => {
    invoke("write_pty", { id: t.ptyId, data: Array.from(new TextEncoder().encode(data)) }).catch(() => {});
  });
  termInputOff = () => onData.dispose();

  if (t.alive) {
    // Fit to the modal, push that geometry to the PTY, then force a clean repaint.
    fitMissionTerm(t.ptyId);
    // Re-fit after layout + web fonts settle (first fit can measure stale metrics).
    requestAnimationFrame(() => requestAnimationFrame(() => { fitMissionTerm(t.ptyId); forceRepaintMissionTerm(); }));
    document.fonts?.ready.then(() => { fitMissionTerm(t.ptyId); forceRepaintMissionTerm(); }).catch(() => {});
    repaintMissionTerm(t.ptyId);
    // Keep grid + PTY in lockstep with the modal size (live drag / window resize).
    termResizeObs = new ResizeObserver(() => fitMissionTerm(t.ptyId));
    termResizeObs.observe(termHost.value!);
  } else {
    // Dead: replay the static history at its OWN capture width (MC_PTY_COLS) so every
    // absolute-cursor frame lands on-grid — no re-wrap, no scramble. The catch: a
    // fixed 120-col grid is wider than the modal's inner width on smaller windows
    // (80vw < ~940px), and term-host is `overflow:hidden`, so the right columns get
    // CLIPPED — that's the "rozházený" cut-off screen. Fix: SCALE the font so exactly
    // MC_PTY_COLS fill the available width. Cols scale inversely with fontSize, so
    // measure how many cols a known fontSize fits, then pick the size that yields 120.
    const baseFont = 13;
    termInstance.options.fontSize = baseFont;
    let pickedFont = baseFont;
    if (termFit && termHost.value && termHost.value.offsetWidth > 0) {
      try {
        const dims = (termFit as unknown as { proposeDimensions?: () => { cols: number; rows: number } | undefined }).proposeDimensions?.();
        if (dims?.cols) {
          // baseFont fits dims.cols → to fit MC_PTY_COLS we need baseFont*dims.cols/120.
          // Cap at baseFont (never enlarge dead text), floor at 6 for legibility.
          pickedFont = Math.max(6, Math.min(baseFont, Math.floor((baseFont * dims.cols) / MC_PTY_COLS)));
        }
      } catch { /* keep baseFont */ }
    }
    termInstance.options.fontSize = pickedFont;
    // Rows: fill the host height at the picked font; fall back to the capture height.
    let rows = MC_PTY_ROWS;
    try {
      const dims2 = (termFit as unknown as { proposeDimensions?: () => { cols: number; rows: number } | undefined }).proposeDimensions?.();
      if (dims2?.rows) rows = dims2.rows;
    } catch { /* keep MC_PTY_ROWS */ }
    termInstance.resize(MC_PTY_COLS, Math.max(MC_PTY_ROWS, rows));
    termInstance.write(buffers.get(t.ptyId) || "", () => forceRepaintMissionTerm());
  }
}

// Drop stale/overlapping glyphs the renderer never cleared and force a full redraw
// from the (correct) buffer — same render-side un-scramble as XTerm.vue's forceRepaint.
function forceRepaintMissionTerm() {
  if (!termInstance) return;
  try { (termInstance as unknown as { _core?: { _renderService?: { clear?: () => void } } })._core?._renderService?.clear?.(); } catch { /* no-op */ }
  termInstance.refresh(0, termInstance.rows - 1);
}

let lastFitCols = 0;
let lastFitRows = 0;
// Fit the modal terminal and push the resulting geometry to the PTY (debounce via
// dedupe: only resize_pty when cols/rows actually changed, to avoid SIGWINCH spam).
// Alive-only — a dead task renders at a pinned width and is never fitted.
function fitMissionTerm(ptyId: number) {
  if (!termInstance || !termFit || !termHost.value) return;
  if (termHost.value.offsetWidth === 0 || termHost.value.offsetHeight === 0) return;
  try { termFit.fit(); } catch { return; }
  const { cols, rows } = termInstance;
  if (cols === lastFitCols && rows === lastFitRows) return;
  lastFitCols = cols;
  lastFitRows = rows;
  invoke("resize_pty", { id: ptyId, cols, rows }).catch(() => {});
}

// Wipe the grid (scrollback + viewport, local to xterm) and SIGWINCH the PTY so the
// agent repaints its whole alt-screen onto a clean grid at the current fitted size.
// `\x1b[3J` clears scrollback too (vs ED2's viewport-only), so no stale 120-col
// frames survive above the repaint. Toggling cols±1 guarantees SIGWINCH delivery (a
// same-size resize is a no-op). The PTY is owned solely by this modal → safe.
function repaintMissionTerm(ptyId: number) {
  if (!termInstance) return;
  termInstance.write("\x1b[3J\x1b[2J\x1b[H");
  const { cols, rows } = termInstance;
  invoke("resize_pty", { id: ptyId, cols: Math.max(1, cols - 1), rows }).catch(() => {});
  setTimeout(() => invoke("resize_pty", { id: ptyId, cols, rows }).catch(() => {}), 60);
}

function sendCtrlC() {
  const t = tasks.value.find((x) => x.id === termTaskId.value);
  if (t) invoke("write_pty", { id: t.ptyId, data: [0x03] }).catch(() => {});
}

function closeTerminal() {
  termResizeObs?.disconnect(); termResizeObs = null;
  termInputOff?.(); termInputOff = null;
  termInstance?.dispose(); termInstance = null;
  termFit = null;
  lastFitCols = 0; lastFitRows = 0;
  termTaskId.value = null;
}

// ── Persistence — shared workspaces.db (mission_tasks table), like every other
// Burrow feature. Metadata only; live PTYs aren't serializable, so a restored
// task is read-only (its daemon PTY died with the previous app session).
// thinking/tool entries are live-only — regenerated from the transcript while a task
// runs. Persist just the text conversation (user prompts + assistant replies): keeps
// the DB lean (tool_result text can be a whole file dump) and avoids restoring stale
// activity for a dead task whose transcript the next session re-reads anyway.
function persistTurns(turns: Turn[]): Turn[] {
  return turns.filter((x) => !x.kind || x.kind === "text").map((x) => ({ role: x.role, text: x.text }));
}

function saveTask(t: Task) {
  invoke("upsert_mission_task", {
    task: {
      id: t.id,
      workspace_id: t.workspaceId,
      pty_id: t.ptyId,
      title: t.title,
      cwd: t.cwd,
      model: t.model,
      status: t.status,
      turns: JSON.stringify(persistTurns(t.turns)),
      created_at: t.createdAt,
      handed_off: t.handedOff ? 1 : 0,
      profile_id: t.profileId,
    },
  }).catch(() => {});
}

async function loadTasks() {
  let rows: TaskRow[] = [];
  try { rows = await invoke<TaskRow[]>("list_mission_tasks"); } catch { return; }
  tasks.value = rows.map((r) => ({
    id: r.id,
    ptyId: r.pty_id ?? 0,
    workspaceId: r.workspace_id ?? null,
    title: r.title ?? "task",
    prompt: "",
    cwd: r.cwd ?? "",
    model: r.model ?? "default",
    profileId: r.profile_id ?? "default",
    status: (r.status as Status) ?? "done",
    turns: parseTurns(r.turns),
    followup: "",
    expanded: false,
    alive: false,   // restored: PTY gone → read-only
    handedOff: r.handed_off === 1,
    createdAt: r.created_at,
  }));
  // Keep new PTY ids above any restored one (they share the daemon's id map).
  for (const t of tasks.value) bumpPtySeqPast(t.ptyId);
}

// Feature 2 — live PTY reconciliation. The Burrow daemon keeps PTYs alive across
// app reloads, so a restored task whose PTY is still running can be re-attached:
// re-stream it (same as XTerm's restore path) and re-wire listeners, making live
// attach + follow-up work again. Also bumps the seq past every daemon PTY id so a
// new task never reuses a live one (the bug that typed `claude …` into a running
// claude's REPL). This is the authoritative guard — the DB max alone can miss a
// PTY whose task row was deleted but whose process lingers.
async function reconcileLive() {
  let sessions: { pty_id: number; alive: boolean }[] = [];
  try { sessions = await invoke("list_pty_sessions"); } catch { return; }
  const live = new Map(sessions.map((s) => [s.pty_id, s]));
  for (const s of sessions) bumpPtySeqPast(s.pty_id);
  for (const t of tasks.value) {
    if (t.alive || !live.get(t.ptyId)?.alive) continue;
    try {
      // Re-open the daemon stream for this existing session, then listen again.
      await invoke("create_pty", { id: t.ptyId, cwd: t.cwd, cols: MC_PTY_COLS, rows: MC_PTY_ROWS });
      buffers.set(t.ptyId, buffers.get(t.ptyId) || "");
      await wireTask(t);
      t.alive = true;
      // Keep a finished task's status as-is on reattach. Flipping done→waiting here
      // made every reloaded done task read "waiting for input" forever (the stuck-
      // waiting bug) — and a `done` task already accepts a follow-up (the composer is
      // only disabled while `running`), so the flip bought nothing.
      saveTask(t);
    } catch { /* leave it read-only */ }
  }
}

function parseTurns(s: string | null): Turn[] {
  if (!s) return [];
  try { const v = JSON.parse(s); return Array.isArray(v) ? v : []; } catch { return []; }
}

// Selecting a task jumps the conversation to its newest turn — and immediately
// pulls any assistant messages already on disk (don't wait for the 1.5s poll).
watch(selectedId, (id) => {
  scrollConvo();
  const t = tasks.value.find((x) => x.id === id);
  if (t?.alive && (t.status === "running" || t.status === "waiting")) syncLiveTurns(t);
});

// Keep the activity bar badge in sync with active task count.
watch(
  tasks,
  (ts) => { ui.missionActiveCount = ts.filter((t) => t.alive && (t.status === "running" || t.status === "waiting")).length; },
  { deep: true },
);

// ── Hook-independent status fallback ──────────────────────────────────────────
// The status dot is hook-driven (pty-hook-{id} → running/waiting/done). But a hook
// can be missed (server not up yet, an agent that doesn't emit Stop, a task that
// finished while the UI wasn't listening, or a stale state restored from the DB),
// leaving a task stuck on `running`. The JSONL transcript is the source of truth
// tank reads: a finished turn ends with an assistant text reply and then the file
// goes idle. So every couple seconds we check live `running` tasks and settle any
// whose transcript has clearly parked at the end of a turn. Conservative on purpose
// — `turn_ended` is false mid-tool-call, so a long-running tool won't false-settle.
const TURN_IDLE_MS = 2500;
let statusTimer: number | null = null;
type Activity = { exists: boolean; idle_ms: number; turn_ended: boolean; awaiting_input: boolean; is_error: boolean };

async function reconcileStatuses() {
  for (const t of tasks.value) {
    // Forward-settle from `running` OR `waiting` — never pull a settled `done`/`error`
    // back. Including `waiting` heals a task whose blocking question was resolved (or
    // a `done` hook dropped after a wait): only settles if the transcript shows the
    // turn truly ended (text reply, no pending tool), so a task genuinely parked on a
    // question has `turn_ended=false` and stays `waiting`.
    if (!t.alive || (t.status !== "running" && t.status !== "waiting")) continue;
    // Stream new assistant messages into the detail feed as the turn progresses.
    await syncLiveTurns(t);
    let act: Activity;
    try {
      act = await invoke("read_claude_activity", { cwd: t.cwd, sessionId: t.id, configDir: taskConfigDir(t) });
    } catch { continue; }
    if (!act.exists) continue;
    // Parked on a blocking tool (question / plan approval) → waiting for the user.
    // Guard on a real transition so a task already `waiting` doesn't re-notify/save
    // every tick (this block now also runs for `waiting` tasks, not just `running`).
    if (act.awaiting_input) {
      if (t.status !== "waiting") {
        t.status = "waiting";
        if (!isWatching(t)) notifyTask(t, "waiting");
        saveTask(t);
      }
      continue;
    }
    // Turn ended (text reply, no pending tool) + transcript idle, but no `done` hook
    // arrived — settle it. The idle gate avoids racing a long in-flight tool call.
    if (act.turn_ended && act.idle_ms >= TURN_IDLE_MS) {
      t.status = act.is_error ? "error" : "done";
      captureResult(t);
      if (!act.is_error && !isWatching(t)) notifyTask(t, "done");
      saveTask(t);
      pumpQueue();
    }
  }
}

// A restored task whose PTY the daemon no longer holds is read-only — it can't be
// "running" anymore (nothing is driving it). Settle any such ghost from its
// transcript so a crash/restart mid-turn doesn't leave a permanent orange dot.
async function settleRestoredGhosts() {
  for (const t of tasks.value) {
    if (t.alive || (t.status !== "running" && t.status !== "waiting")) continue;
    let act: { exists: boolean; turn_ended: boolean; is_error: boolean };
    try { act = await invoke("read_claude_activity", { cwd: t.cwd, sessionId: t.id, configDir: taskConfigDir(t) }); } catch { continue; }
    t.status = act.exists && act.is_error ? "error" : "done"; // PTY gone → no longer running
    captureResult(t);
    saveTask(t);
  }
}

onMounted(async () => {
  await loadTasks();
  await reconcileLive();   // re-attach still-running PTYs + never reuse a live id
  await settleRestoredGhosts();
  statusTimer = window.setInterval(reconcileStatuses, 1500);
  // Composer cwd defaults to the active workspace (the scope chosen in the sidebar).
  draft.cwd = activeWs.value?.path || tasks.value[0]?.cwd || "";
  selectedId.value = orderedTasks.value[0]?.id ?? null;
  if (!tasks.value.length) composerOpen.value = true;  // empty → invite a new task
});

onBeforeUnmount(() => {
  closeTerminal();
  if (statusTimer != null) clearInterval(statusTimer);
  for (const offs of unlisteners.values()) offs.forEach((u) => u());
});
</script>

<style scoped>
/* Markdown body inside assistant turns and transcript entries: content is injected via
   v-html, so scoped attribute-selectors can't reach it — :deep() is required here. */
.md-body :deep(p) { margin: 0 0 8px; }
.md-body :deep(p:last-child) { margin-bottom: 0; }
.md-body :deep(ul), .md-body :deep(ol) { margin: 4px 0 8px; padding-left: 20px; }
.md-body :deep(li) { margin: 2px 0; }
.md-body :deep(code) { font-family: var(--font-mono); font-size: 11px; background: color-mix(in srgb, var(--accent) 12%, transparent); padding: 1px 4px; border-radius: 3px; }
.md-body :deep(pre) { background: var(--bg-base); border: 1px solid var(--border); border-radius: 6px; padding: 10px 12px; overflow-x: auto; margin: 6px 0; }
.md-body :deep(pre code) { background: none; padding: 0; font-size: 11px; }
.md-body :deep(blockquote) { border-left: 3px solid var(--accent); margin: 6px 0; padding-left: 10px; color: var(--text-muted); }
.md-body :deep(h1), .md-body :deep(h2), .md-body :deep(h3) { font-weight: 700; margin: 10px 0 4px; color: var(--text-primary); }
.md-body :deep(h1) { font-size: 16px; }
.md-body :deep(h2) { font-size: 14px; }
.md-body :deep(h3) { font-size: 13px; }
.md-body :deep(a) { color: var(--accent); text-decoration: underline; }
.md-body :deep(hr) { border: none; border-top: 1px solid var(--border); margin: 8px 0; }
.md-body :deep(table) { border-collapse: collapse; font-size: 12px; margin: 6px 0; }
.md-body :deep(th), .md-body :deep(td) { border: 1px solid var(--border); padding: 4px 8px; }
.md-body :deep(th) { background: var(--bg-panel); font-weight: 600; }
.md-body :deep(strong) { color: var(--text-primary); font-weight: 700; }

/* ── Status dots: shared "dot" pattern with a pulse animation for the active states,
   keyed off status keyword classes toggled from the model — kept as CSS since the
   color + shadow + conditional animation per state reads cleaner as a class. ── */
.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; background: var(--text-muted); flex-shrink: 0; }
.dot.running { background: var(--yellow); box-shadow: 0 0 8px color-mix(in srgb, var(--yellow) 53%, transparent); animation: pulse 1.4s infinite; }
.dot.waiting { background: var(--accent); box-shadow: 0 0 8px color-mix(in srgb, var(--accent) 53%, transparent); }
/* permission = agent needs an allow/deny decision (amber pulse — distinct from blue waiting) */
.dot.permission { background: var(--status-permission, #f59e0b); box-shadow: 0 0 8px color-mix(in srgb, var(--status-permission, #f59e0b) 60%, transparent); animation: pulse 1.2s infinite; }
.dot.done { background: var(--green); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 53%, transparent); }
/* review = finished while you were away; green pulse persists until the task is opened */
.dot.review { background: var(--status-review, var(--green)); box-shadow: 0 0 8px color-mix(in srgb, var(--green) 53%, transparent); animation: pulse 1.8s infinite; }
.dot.error { background: var(--red); box-shadow: 0 0 8px color-mix(in srgb, var(--red) 60%, transparent); animation: pulse 1.4s infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

.spin { animation: tr-spin 1s linear infinite; }
@keyframes tr-spin { to { transform: rotate(360deg); } }
</style>
