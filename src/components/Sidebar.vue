<template>
  <aside class="surface-glow relative isolate [--glow-base:var(--sidebar-bg)] flex w-[var(--sidebar-width,220px)] shrink-0 grow-0 basis-[var(--sidebar-width,220px)] flex-col overflow-hidden border-r border-border bg-[var(--sidebar-bg)] [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)]">
    <StageBackdrop />
    <!-- Top row: new thread + quiet icon actions. The project picker is a
         menu now: the feed below is already grouped by project. -->
    <div class="flex shrink-0 items-center gap-1 px-2 pb-1 pt-2" ref="filterEl">
      <button
        v-if="active"
        class="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2 py-1.5 text-[13px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
        :class="ui.welcomeVisible && 'bg-hover text-foreground'"
        @click="ui.openWelcome()"
      >
        <PhPlus :size="14" class="shrink-0" />
        <span class="truncate whitespace-nowrap">New thread</span>
      </button>
      <span v-else class="flex-1" />
      <button class="sb-icon" title="Projects" :class="filterOpen && 'text-foreground'" @click.stop="filterOpen = !filterOpen"><PhFolders :size="15" /></button>
      <button class="sb-icon" title="Open folder…" @click="pickFolder"><PhFolderPlus :size="15" /></button>
      <Teleport to="body">
        <div v-if="filterOpen" class="fixed z-[1000] max-h-[60vh] overflow-y-auto rounded-lg border border-border bg-[var(--bg-dropdown)] p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="filterMenuStyle" @click.stop>
          <button
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="filterProjectId == null && 'bg-selected text-foreground'"
            @click="filterProjectId = null; filterOpen = false"
          >
            All projects
          </button>
          <button
            v-for="repo in filterRepos"
            :key="repo.id"
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="[
              filterProjectId === repo.id && 'bg-selected text-foreground',
              isArchived(repo.id) && 'opacity-50',
            ]"
            @click="pickProject(repo)"
            @contextmenu.prevent.stop="openRowMenu(repo, null, $event)"
          >
            <img v-if="store.icons[repo.id]" :src="store.icons[repo.id]" class="h-3.5 w-3.5 shrink-0 rounded-sm object-cover" />
            <PhFolder v-else :size="12" weight="fill" class="shrink-0 text-accent" />
            {{ repo.name }}
          </button>
        </div>
      </Teleport>
    </div>
    <div v-if="filterProjectId != null" class="mx-2 mb-1 flex items-center gap-1.5 rounded-lg bg-hover px-2.5 py-1 text-[12px] text-secondary-foreground">
      Only <span class="truncate font-medium text-foreground">{{ repoById(filterProjectId)?.name }}</span>
      <button class="ml-auto text-muted-foreground hover:text-foreground" title="Show all projects" @click="filterProjectId = null"><PhX :size="11" weight="bold" /></button>
    </div>

    <div class="flex-1 overflow-y-auto pb-2" @scroll="cancelHoverCard">
      <!-- Live feed, grouped by project (Ghostex-style): a header per repo with
           its uncommitted diffstat, one compact line per thread, a thread's
           sub-agents indented under it. Branch/model/PR live in the hover card. -->
      <div v-for="group in topGroups" :key="group.repoId" class="px-2 pt-1.5">
        <div v-if="group.repoId === PINNED" class="px-1.5 pb-0.5 pt-1 text-[11.5px] font-medium text-muted-foreground">Pinned</div>
        <div v-else class="group/hdr flex items-center gap-2 rounded-lg px-1.5 py-1.5 hover:bg-hover" @contextmenu.prevent.stop="repoById(group.repoId) && openRowMenu(repoById(group.repoId)!, null, $event)">
          <button class="flex min-w-0 flex-1 items-center gap-2 text-left" @click="toggleGroup(group.repoId)">
            <PhCaretDown :size="11" weight="bold" class="shrink-0 text-muted-foreground transition-transform" :class="collapsedGroups.has(group.repoId) && '-rotate-90'" />
            <img v-if="store.icons[group.repoId]" :src="store.icons[group.repoId]" class="h-[18px] w-[18px] shrink-0 rounded-[5px] object-cover" />
            <span v-else class="sb-repo-tile" :style="{ background: repoColor(group.repoId) }">{{ (repoById(group.repoId)?.name ?? '?').slice(0, 1).toUpperCase() }}</span>
            <span class="min-w-0 flex-1 truncate text-[13.5px] text-foreground">{{ repoById(group.repoId)?.name ?? '…' }}</span>
          </button>
          <span v-if="repoDiff[group.repoId] && (repoDiff[group.repoId]!.added || repoDiff[group.repoId]!.removed)" class="shrink-0 text-[11.5px] tabular-nums group-hover/hdr:hidden" :title="'Uncommitted changes'">
            <span class="text-[var(--green)]">+{{ repoDiff[group.repoId]!.added }}</span>
            <span class="ml-1 text-[var(--red)]">-{{ repoDiff[group.repoId]!.removed }}</span>
          </span>
          <button class="hidden shrink-0 rounded p-0.5 text-muted-foreground hover:text-foreground group-hover/hdr:block" title="New thread in this project" @click.stop="repoById(group.repoId) && pickProject(repoById(group.repoId)!, false)"><PhPlus :size="13" /></button>
        </div>
        <div v-if="!collapsedGroups.has(group.repoId)">
          <div v-for="row in group.rows" :key="rowKey(row)">
            <div
              class="sb-row group relative mt-px flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-[7px]"
              :class="[isActiveRow(row) && 'sb-row-active', row.tab.isAgent && `agent-state-${attentionState(row)}`]"
              @click="onRowClick(row)"
              @contextmenu.prevent.stop="openRowMenu(row.ws, row.tab, $event)"
              @pointerdown="startLongPress(row, $event)"
              @pointermove="moveLongPress"
              @pointerup="cancelLongPress"
              @pointercancel="cancelLongPress"
              @mouseenter="scheduleHoverCard(hoverInfoFromRow(row), $event)"
              @mouseleave="cancelHoverCard"
            >
              <PhCrown v-if="row.tab.chatId != null && childrenOfChat(row.tab.chatId).length" :size="14" weight="fill" class="shrink-0 text-[var(--yellow)]" />
              <PhGitBranch v-else-if="row.ws.parent_id" :size="14" class="shrink-0 text-muted-foreground" />
              <component v-else-if="row.tab.agentIcon" :is="agentIconComp(row.tab.agentIcon)" :size="14" class="shrink-0 text-[#E0876A]" />
              <PhChatCenteredText v-else-if="row.tab.isChat" :size="14" class="shrink-0 text-muted-foreground" />
              <PhTerminal v-else :size="14" class="shrink-0 text-muted-foreground" />
              <PhPushPin v-if="isChatPinned(row.tab.chatId)" :size="11" weight="fill" class="shrink-0 text-muted-foreground" title="Pinned" />
              <PhMoon v-if="chatIsAsleep(row.tab.chatId)" :size="11" weight="fill" class="shrink-0 text-muted-foreground opacity-70" title="Sleeping — sending a message wakes it" />
              <input
                v-if="editingTab?.wsId === row.ws.id && editingTab?.tabId === row.tab.id"
                v-model="editingTabTitle"
                class="ws-term-rename-input m-0 w-full min-w-0 flex-1 border-0 border-b border-accent bg-transparent p-0 text-[13.5px] text-foreground outline-none"
                @blur="commitTabRename"
                @keydown.enter.prevent="commitTabRename"
                @keydown.esc.prevent="cancelTabRename"
                @click.stop
              />
              <span v-else class="min-w-0 flex-1 truncate text-[13.5px]" :class="isActiveRow(row) ? 'font-medium text-foreground' : 'text-foreground/90'" @dblclick.stop="startTabRename(row.ws.id, row.tab)">{{ row.tab.title }}</span>
              <span v-if="row.tab.chatId != null && childrenOfChat(row.tab.chatId).length" class="flex shrink-0 items-center gap-0.5 text-[11.5px] text-secondary-foreground group-hover:hidden"><PhUsersThree :size="12" />{{ childrenOfChat(row.tab.chatId).length }}</span>
              <span class="flex shrink-0 items-center gap-1 text-[11.5px] tabular-nums group-hover:hidden" :class="statusClass(row.tab.status)">
                <PhWarningCircle v-if="attentionState(row) === 'needs-input'" :size="11" weight="fill" class="text-[var(--yellow)]" />
                <span v-else-if="attentionState(row) === 'done-unread'" class="h-[7px] w-[7px] rounded-full bg-[var(--blue)]" title="Unread completion" />
                <span v-else-if="row.tab.status === 'running'" class="sb-spark">{{ spinnerFrame }}</span>
                <span v-if="row.tab.status !== 'running' && attentionState(row) !== 'done-unread'">{{ statusText(row) }}</span>
              </span>
              <span class="sb-hover-acts hidden shrink-0 items-center gap-0.5 group-hover:flex">
                <button class="sb-row-act" title="Settle" @click.stop="toggleSettled(row.tab, row.ws.id)"><PhCheck :size="12" weight="bold" /></button>
                <button class="sb-row-act hover:!text-destructive" title="Delete thread" @click.stop="deleteThread(row.ws.id, row.tab)"><PhX :size="11" weight="bold" /></button>
              </span>
              <!-- Touch has no hover and no right click: this opens the same menu. -->
              <button class="sb-row-more sb-row-act" title="More" aria-label="Thread actions" @click.stop="openRowMenu(row.ws, row.tab, $event)"><PhDotsThree :size="16" weight="bold" /></button>
            </div>
            <div v-if="chatRecapOf(row.tab.chatId)" class="ml-[22px] truncate px-2.5 pb-1 text-[11px] leading-tight text-muted-foreground" :title="chatRecapOf(row.tab.chatId)">{{ chatRecapOf(row.tab.chatId) }}</div>
            <div v-if="row.tab.chatId != null && childrenOfChat(row.tab.chatId).length" class="ml-[22px]">
              <button
                v-for="c in childrenOfChat(row.tab.chatId)"
                :key="c.id"
                class="sb-row flex w-full items-center gap-2 rounded-lg px-2.5 py-[6px] text-left"
                :title="c.title"
                @click="openSubagent(c.id, row.ws.id)"
              >
                <PhAsterisk :size="13" weight="bold" class="shrink-0" :class="c.status === 'idle' || !c.status ? 'text-muted-foreground' : 'text-[#E0876A]'" />
                <span class="min-w-0 flex-1 truncate text-[13px] text-foreground/85">{{ c.title }}</span>
                <span v-if="c.status && c.status !== 'idle'" class="status-dot" :class="`status-${c.status}`" />
                <span v-else class="text-[11px] text-muted-foreground">{{ ago(c.lastActivityAt ?? 0) }}</span>
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="!allRows.length" class="m-2 rounded-lg border border-dashed border-border/60 px-5 py-7 text-center text-[11.5px] leading-[1.7] text-muted-foreground">
        <template v-if="store.workspaces.length === 0">No projects.<br />Open a folder to start.</template>
        <template v-else-if="active">No threads here yet.<br />Start one above.</template>
        <template v-else>Nothing open.<br />Pick a project up top.</template>
      </div>

      <!-- Archived: the current workspace's chats closed by the user — no open
           tab, process stopped, reversible by clicking to reopen. -->
      <template v-if="active && archivedChats.length">
        <button class="section-header" @click="toggleSection('archived')">
          <PhCaretDown :size="9" weight="bold" class="shrink-0 transition-transform" :class="collapsed.includes('archived') && '-rotate-90'" />
          Archived
          <span class="opacity-60">{{ archivedChats.length }}</span>
        </button>
        <template v-if="!collapsed.includes('archived')">
          <div
            v-for="s in archivedChats"
            :key="s.id"
            class="group cursor-pointer px-2.5 py-[5px] text-muted-foreground opacity-60 transition-opacity hover:rounded-lg hover:bg-hover hover:opacity-100"
            @click="unarchiveAndOpen(s.id)"
            @mouseenter="active && scheduleHoverCard(hoverInfoFromArchived(s, active), $event)"
            @mouseleave="cancelHoverCard"
          >
            <div class="flex items-center gap-1.5">
              <PhChatCenteredText :size="10" class="shrink-0" />
              <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px]">{{ s.title }}</span>
              <span class="shrink-0 text-[10px] tabular-nums">{{ ago(s.archivedAt!) }}</span>
              <PhTrash
                :size="10"
                class="shrink-0 rounded-sm p-px opacity-0 transition-opacity group-hover:opacity-60 hover:!opacity-100 hover:!text-destructive"
                title="Delete permanently"
                @click.stop="deletePermanently(s.id)"
              />
            </div>
            <div v-if="active" class="mt-[2px] flex items-center gap-1.5 pl-[15px]">
              <img v-if="repoIcon(active)" :src="repoIcon(active)!" class="h-2.5 w-2.5 shrink-0 rounded-[2px] object-cover" />
              <PhFolder v-else :size="9" weight="fill" class="shrink-0 text-accent/80" />
              <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[9.5px] opacity-70" :title="active.name">{{ repoName(active) }}</span>
            </div>
          </div>
        </template>
      </template>

    </div>

    <!-- Docked shelf (t3code): Working, then Snoozed and Settled. Working is
         open by default; collapsed it still shows the thread on screen. -->
    <div v-if="shelfSections.length" class="flex max-h-[45%] shrink-0 flex-col overflow-y-auto border-t border-border px-2 pb-1.5 pt-0.5" @scroll="cancelHoverCard">
      <div
        v-for="sec in shelfSections"
        :key="sec.key"
        @pointerenter="sec.key === 'working' && onShelfHover($event, true)"
        @pointerleave="sec.key === 'working' && onShelfHover($event, false)"
      >
        <button class="section-header !mx-0 !w-full" @click="toggleShelf(sec.key)">
          {{ sec.label }}
          <span class="opacity-60">{{ sec.count }}</span>
          <PhCaretRight :size="9" weight="bold" class="shrink-0 transition-transform" :class="shelfOpen(sec.key) && 'rotate-90'" />
          <span
            v-if="sec.key === 'settled'"
            role="button"
            class="ml-auto rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground"
            title="Delete all settled threads (pinned ones are kept)"
            @click.stop="deleteAllSettled"
          >
            <PhTrash :size="10" />
          </span>
        </button>
        <div>
          <div
            v-for="row in sec.rows"
            :key="rowKey(row)"
            class="sb-row group cursor-pointer rounded-lg px-2.5 py-[5px]"
            :class="[isActiveRow(row) && 'sb-row-active', sec.key !== 'working' && 'text-muted-foreground opacity-60 transition-opacity hover:opacity-100']"
            @click="onRowClick(row)"
            @contextmenu.prevent.stop="openRowMenu(row.ws, row.tab, $event)"
            @pointerdown="startLongPress(row, $event)"
            @pointermove="moveLongPress"
            @pointerup="cancelLongPress"
            @pointercancel="cancelLongPress"
            @mouseenter="scheduleHoverCard(hoverInfoFromRow(row), $event)"
            @mouseleave="cancelHoverCard"
          >
            <div class="flex items-center gap-1.5">
              <component :is="row.tab.agentIcon ? agentIconComp(row.tab.agentIcon) : row.tab.isChat ? PhChatCenteredText : PhTerminal" :size="10" class="shrink-0" />
              <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px]" :class="sec.key === 'working' && 'text-foreground/90'">{{ row.tab.title }}</span>
              <component :is="prGlyphOf(row.tab.chatId)!.icon" v-if="prGlyphOf(row.tab.chatId)" :size="10" class="shrink-0" :class="prGlyphOf(row.tab.chatId)!.cls" :title="prGlyphOf(row.tab.chatId)!.title" />
              <template v-if="sec.key === 'working'">
                <span v-if="row.tab.status === 'running'" class="sb-spark shrink-0 text-[11px]">{{ spinnerFrame }}</span>
                <span class="shrink-0 text-[10px] tabular-nums text-muted-foreground">{{ ago(shelf.startedAt.get(rowKey(row)) ?? row.ts) }}</span>
              </template>
              <template v-else>
                <span v-if="snoozedUntilOf(row.tab.chatId)" class="flex shrink-0 items-center gap-0.5 text-[10px] tabular-nums" :title="`Snoozed until ${new Date(snoozedUntilOf(row.tab.chatId)).toLocaleString()}`">
                  <PhAlarm :size="10" />{{ formatWakeTime(snoozedUntilOf(row.tab.chatId), Date.now()) }}
                </span>
                <!-- Same visibility-in-one-grid-cell trick as the live rows: no hover jump. -->
                <span class="grid shrink-0 justify-items-end">
                  <span v-if="sec.key === 'settled'" class="sb-ago [grid-area:1/1] text-[10px] tabular-nums group-hover:invisible">{{ ago(row.ts) }}</span>
                  <button
                    class="sb-unsettle invisible flex items-center self-center rounded p-0.5 text-muted-foreground transition-colors [grid-area:1/1] hover:bg-hover hover:text-foreground group-hover:visible"
                    :title="sec.key === 'snoozed' ? 'Unsnooze' : 'Move back to active'"
                    @click.stop="sec.key === 'snoozed' ? chats.snooze(row.tab.chatId!, 0) : toggleSettled(row.tab, row.ws.id)"
                  >
                    <PhArrowCounterClockwise :size="11" weight="bold" />
                  </button>
                </span>
              </template>
            </div>
            <div class="mt-[2px] flex items-center gap-1.5 pl-[15px]">
              <img v-if="repoIcon(row.ws)" :src="repoIcon(row.ws)!" class="h-2.5 w-2.5 shrink-0 rounded-[2px] object-cover" />
              <PhFolder v-else :size="9" weight="fill" class="shrink-0 text-accent/80" />
              <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[9.5px] opacity-70" :title="row.ws.name">{{ repoName(row.ws) }}</span>
              <span v-if="git.prByWs[row.ws.id]" class="shrink-0 text-[9px] tabular-nums">#{{ git.prByWs[row.ws.id]!.number }}</span>
            </div>
          </div>
        </div>
        <button
          v-if="sec.key === 'settled' && shelfOpen('settled') && filteredSettled.length > settledChatsLimit"
          class="flex w-full items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] text-muted-foreground hover:bg-hover hover:text-foreground"
          @click="settledChatsLimit += 25"
        >
          <PhPlus :size="10" />
          Show {{ filteredSettled.length - settledChatsLimit }} more
        </button>
      </div>
    </div>

    <!-- App + provider update banners, docked above the toasts (Ghostex keeps
         its notices in the sidebar, not floating over the work). -->
    <div class="flex shrink-0 flex-col gap-1 px-2 pb-1.5 empty:hidden">
      <UpdateBanner docked />
      <ProviderUpdateBanner docked />
    </div>
    <!-- Toasts docked above the bottom bar (toastPosition "sidebar"), t3code-style. -->
    <!-- Same quiet row as the update notices above, so a running op reads as one more notice. -->
    <TransitionGroup v-if="ui.toastPosition === 'sidebar'" tag="div" name="sb-toast" class="flex shrink-0 flex-col gap-1 px-2 empty:hidden [&:not(:empty)]:pb-1.5">
      <div
        v-for="t in notif.toasts"
        :key="t.id"
        class="sb-notice"
        role="button"
        :title="t.body ? `${t.title} — ${t.body}` : t.title"
        @click="notif.dismiss(t.id)"
      >
        <PhCircleNotch v-if="t.type === 'pending'" :size="15" class="shrink-0 animate-spin text-muted-foreground" />
        <PhWarningCircle v-else-if="t.type === 'error'" :size="15" weight="fill" class="shrink-0 text-destructive" />
        <PhCheckCircle v-else-if="t.type === 'done'" :size="15" weight="fill" class="shrink-0 text-success" />
        <PhInfo v-else :size="15" weight="fill" class="shrink-0 text-accent" />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[12.5px] text-foreground">{{ t.title }}</span>
          <span v-if="t.body" class="block truncate text-[11px] text-muted-foreground">{{ t.body }}</span>
        </span>
        <button v-if="t.action" class="shrink-0 rounded px-1.5 text-[12px] font-semibold text-accent hover:bg-hover" @click.stop="notif.runAction(t.id)">{{ t.action.label }}</button>
        <button v-if="t.type !== 'pending'" class="sb-notice-x" title="Dismiss" @click.stop="notif.dismiss(t.id)"><PhX :size="11" /></button>
      </div>
    </TransitionGroup>

    <!-- Bottom bar: what the vertical ActivityBar used to hold -->
    <div class="flex shrink-0 items-center gap-0.5 border-t border-border px-1.5 py-1">
      <button class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="Settings (⌘,)" @click="ui.openSettings()"><PhGear :size="14" /></button>
      <button
        class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90"
        :disabled="!active"
        :class="!active && 'opacity-35'"
        title="Git manager"
        @click="active && openGitTab(active)"
      ><PhGitBranch :size="14" /></button>
      <button
        class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90"
        :class="ui.mode === 'dashboard' && 'bg-accent/12 text-accent'"
        title="Dashboard"
        @click="ui.toggleDashboard()"
      ><PhSquaresFour :size="14" /></button>
      <span class="flex-1" />
      <PlanUsageIndicator />
      <div class="relative" ref="scriptsEl">
        <button
          class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90"
          :class="[scriptsOpen && 'bg-hover text-foreground', !active && 'opacity-35']"
          :disabled="!active"
          title="Run a script"
          @click.stop="scriptsOpen = !scriptsOpen"
        ><PhPlayCircle :size="14" /></button>
      </div>
    </div>
  </aside>

  <!-- Dialogs teleported to body to escape backdrop-filter stacking context -->
  <Teleport to="body">
    <!-- Scripts popover -->
    <div
      v-if="scriptsOpen"
      class="fixed z-[1000] min-w-[240px] max-w-[420px] rounded-lg border border-border bg-[var(--bg-dropdown)] p-1.5 shadow-[0_14px_36px_rgba(0,0,0,0.55)]"
      :style="scriptsMenuStyle"
      @click.stop
    >
      <div class="px-2 pb-1.5 pt-1 text-[10px] font-semibold tracking-wide text-muted-foreground">Run script</div>
      <button
        v-for="sc in scripts"
        :key="sc.id"
        class="flex w-full items-center gap-2 rounded-[5px] bg-transparent px-2 py-1.5 text-left text-foreground hover:bg-accent/12 disabled:cursor-default disabled:opacity-40"
        :disabled="!scriptsStore.commandLine(sc)"
        :title="scriptsStore.commandLine(sc) || 'No steps'"
        @click="runScript(sc)"
      >
        <span class="h-2 w-2 shrink-0 rounded-full" :style="{ background: sc.color || '#60a5fa' }" />
        <span class="shrink-0 text-xs font-medium">{{ sc.name }}</span>
        <code class="ml-auto truncate font-mono text-[10.5px] text-muted-foreground">{{ scriptsStore.commandLine(sc) || "—" }}</code>
      </button>
      <div v-if="scripts.length === 0" class="px-2 py-1.5 text-[11px] text-muted-foreground">
        No scripts. Add some in Settings → Scripts.
      </div>
    </div>
    <!-- Row hover card: full thread details, shown after a short hover delay.
         Shared by live/settled/archived rows via hoverInfoFrom{Row,Archived}. -->
    <div
      v-if="hoverInfo"
      class="pointer-events-none fixed z-[900] w-64 max-w-[calc(100vw-16px)] rounded-lg border border-border bg-[var(--bg-dropdown)] p-2.5 shadow-[0_14px_36px_rgba(0,0,0,0.55)]"
      :style="hoverStyle"
    >
      <div class="mb-1.5 wrap-break-word text-[11px] font-semibold leading-[1.4] text-foreground">{{ hoverInfo.title }}</div>
      <div class="grid gap-1 text-[10.5px] text-muted-foreground">
        <div class="flex min-w-0 items-center gap-1.5">
          <img v-if="repoIcon(hoverInfo.ws)" :src="repoIcon(hoverInfo.ws)!" class="h-3 w-3 shrink-0 rounded-[3px] object-cover" />
          <PhFolder v-else :size="11" weight="fill" class="shrink-0 text-accent/80" />
          <span class="min-w-0 truncate text-foreground/80">{{ repoName(hoverInfo.ws) }}</span>
        </div>
        <div class="min-w-0 truncate font-mono text-[9.5px] text-muted-foreground" :title="hoverInfo.ws.path">{{ hoverInfo.ws.path }}</div>
        <div v-if="branchOf(hoverInfo.ws) || hoverInfo.branch" class="flex min-w-0 items-center gap-1.5">
          <PhGitBranch :size="11" class="shrink-0" />
          <span class="min-w-0 truncate font-mono text-foreground/80">{{ branchOf(hoverInfo.ws) || hoverInfo.branch }}</span>
        </div>
        <!-- Only worth saying when the checkout moved under the thread. -->
        <div v-if="hoverInfo.branch && branchOf(hoverInfo.ws) && hoverInfo.branch !== branchOf(hoverInfo.ws)" class="flex min-w-0 items-center gap-1.5">
          <PhGitBranch :size="11" class="shrink-0 opacity-50" />
          <span class="min-w-0 truncate font-mono text-[9.5px] opacity-70">started on {{ hoverInfo.branch }}</span>
        </div>
        <div v-if="hoverInfo.model" class="flex min-w-0 items-center gap-1.5">
          <PhRobot :size="11" class="shrink-0" />
          <span class="min-w-0 truncate text-foreground/80">{{ hoverInfo.model }}</span>
        </div>
        <div v-if="(hoverInfo.leafCount ?? 1) > 1" class="flex min-w-0 items-center gap-1.5">
          <PhTerminal :size="11" class="shrink-0" />
          <span class="truncate text-foreground/80">{{ hoverInfo.leafCount }} panes running</span>
        </div>
        <div v-if="(hoverInfo.round ?? 0) > 0" class="flex min-w-0 items-center gap-1.5">
          <PhArrowCounterClockwise :size="11" class="shrink-0" />
          <span class="truncate text-foreground/80">{{ hoverInfo.round }} {{ hoverInfo.round === 1 ? "turn" : "turns" }} sent</span>
        </div>
        <div class="flex min-w-0 items-center gap-1.5">
          <span class="truncate">Last activity {{ ago(hoverInfo.ts) }}</span>
        </div>
        <div v-if="hoverInfo.sessionId" class="min-w-0 truncate font-mono text-[9.5px] text-muted-foreground" :title="hoverInfo.sessionId">{{ hoverInfo.sessionId }}</div>
        <div v-if="ui.debugIds && hoverInfo.debugId" class="min-w-0 truncate font-mono text-[9.5px] text-muted-foreground">{{ hoverInfo.debugId }}</div>
        <div
          v-if="git.prByWs[hoverInfo.ws.id]"
          class="flex min-w-0 items-center gap-1.5"
          :class="prClass(git.prByWs[hoverInfo.ws.id]!)"
        >
          <span class="h-1.5 w-1.5 shrink-0 rounded-full bg-current" />
          <span class="truncate">{{ prTitle(git.prByWs[hoverInfo.ws.id]!) }}</span>
        </div>
        <div
          v-if="hoverInfo.status && hoverInfo.status !== 'idle'"
          class="flex min-w-0 items-center gap-1.5"
          :class="statusClass(hoverInfo.status)"
        >
          <span class="status-dot" :class="`status-${hoverInfo.status}`" />
          <span class="truncate">{{ statusLabel(hoverInfo.status) }}</span>
        </div>
      </div>
    </div>

    <!-- Row context menu -->
    <div
      v-if="rowMenu"
      class="fixed z-[1000] w-[196px] rounded-lg border border-border bg-[var(--bg-dropdown)] p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]"
      :style="rowMenuStyle"
      @click.stop
    >
      <template v-if="rowMenu.tab">
        <button class="menu-item" @click="withMenu((ws, tab) => startTabRename(ws.id, tab!))">Rename tab…</button>
        <button v-if="rowMenu.tab.isChat" class="menu-item" @click="withMenu((ws, tab) => regenerateTitle(ws, tab!))">Regenerate title</button>
        <button v-if="rowMenu.tab.chatId != null" class="menu-item" @click="withMenu((_ws, tab) => toggleChatPin(tab!.chatId!))">
          <PhPushPin :size="15" class="mr-1.5 shrink-0" :weight="isChatPinned(rowMenu.tab.chatId) ? 'fill' : 'regular'" />
          {{ isChatPinned(rowMenu.tab.chatId) ? "Unpin thread" : "Pin thread to top" }}
        </button>
        <button class="menu-item" @click="withMenu((ws, tab) => toggleSettled(tab!, ws.id))">
          {{ rowMenu.tab.settled ? "Mark active" : "Settle now" }}
        </button>
        <template v-if="rowMenu.tab.isChat && rowMenu.tab.chatId != null">
          <button v-if="snoozedUntilOf(rowMenu.tab.chatId)" class="menu-item" @click="withMenu((_ws, tab) => chats.snooze(tab!.chatId!, 0))">
            <PhAlarm :size="15" class="mr-1.5 shrink-0" />Unsnooze
          </button>
          <button class="menu-item" @click="snoozeMenuOpen = !snoozeMenuOpen">
            <PhAlarm :size="15" class="mr-1.5 shrink-0" />Snooze
            <PhCaretDown :size="9" weight="bold" class="ml-auto shrink-0 transition-transform" :class="!snoozeMenuOpen && '-rotate-90'" />
          </button>
          <template v-if="snoozeMenuOpen">
            <button
              v-for="preset in snoozePresets(Date.now())"
              :key="preset.label"
              class="menu-item !pl-7"
              @click="withMenu((_ws, tab) => chats.snooze(tab!.chatId!, preset.until))"
            >
              {{ preset.label }}
            </button>
          </template>
        </template>
        <button class="menu-item" @click="withMenu((ws, tab) => closeTab(ws.id, tab!))">Close tab</button>
        <button class="menu-item !text-destructive hover:!bg-destructive/10" @click="withMenu((ws, tab) => deleteThread(ws.id, tab!))">{{ rowMenu.tab.isChat ? "Delete thread" : "Delete thread…" }}</button>
        <div class="menu-sep" />
      </template>
      <button class="menu-item" @click="withMenu(newChatSession)">New chat</button>
      <button class="menu-item" @click="withMenu(newTerminalTab)">New terminal</button>
      <button v-if="!rowMenu.ws.parent_id" class="menu-item" @click="withMenu(openWtDialog)">New worktree…</button>
      <div class="menu-sep" />
      <button v-if="!rowMenu.ws.parent_id" class="menu-item" @click="withMenu(startRename)">Rename project…</button>
      <button v-if="!rowMenu.ws.parent_id" class="menu-item" @click="withMenu((ws) => togglePin(ws.id))">
        {{ isPinned(rowMenu.ws.id) ? "Unpin" : "Pin to top" }}
      </button>
      <button class="menu-item" @click="withMenu((ws) => toggleArchived(ws.id))">
        <PhArchive :size="15" class="mr-1.5 shrink-0" />
        {{ isArchived(rowMenu.ws.id) ? "Unarchive" : "Archive" }}
      </button>
      <button v-if="isOpened(rowMenu.ws.id)" class="menu-item" @click="withMenu((ws) => store.closeWorkspace(ws.id))">Close project</button>
      <div class="menu-sep" />
      <button class="menu-item !text-destructive hover:!bg-destructive/10" @click="withMenu((ws) => (deleteTarget = ws))">Delete…</button>
    </div>

    <!-- Generic destructive confirm — window.confirm() is a silent no-op in the
         Wails webview (no native dialog delegate wired up), so any delete that
         needs a confirmation must go through this instead. -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="confirmDialog" @click.self="confirmDialog = null" @keydown.esc="confirmDialog = null">
      <div class="flex w-[380px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-5">
        <p class="text-[12.5px] leading-[1.6] text-foreground">{{ confirmDialog.message }}</p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="confirmDialog = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-destructive px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90" v-focus @click="runConfirm">Delete</button>
        </div>
      </div>
    </div>

    <!-- Delete confirm — destructive: a worktree delete removes files from disk -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="deleteTarget" @click.self="deleteTarget = null" @keydown.esc="deleteTarget = null">
      <div class="flex w-[420px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">
          Delete {{ deleteTarget!.parent_id ? "worktree" : "project" }} “{{ deleteTarget!.worktree_branch || deleteTarget!.name }}”?
        </h3>
        <p class="text-[11.5px] leading-[1.7] text-secondary-foreground">
          <template v-if="deleteTarget!.parent_id">
            Runs <code class="font-mono text-[11px]">git worktree remove</code> — <strong class="text-destructive">the directory on disk is deleted.</strong>
          </template>
          <template v-else>
            Removes it from Burrow{{ childCount(deleteTarget!.id)
              ? ` and deletes its ${childCount(deleteTarget!.id)} worktree director${childCount(deleteTarget!.id) === 1 ? "y" : "ies"} from disk`
              : "" }}. Your project folder itself stays untouched.
          </template>
        </p>
        <label v-if="deleteTarget!.parent_id" class="flex items-center gap-2 text-[11.5px] text-secondary-foreground">
          <input type="checkbox" v-model="deleteForce" /> Discard uncommitted changes (--force)
        </label>
        <p v-if="deleteError" class="whitespace-pre-wrap break-words text-[11px] text-destructive">{{ deleteError }}</p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="deleteTarget = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-destructive px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:cursor-default disabled:opacity-50" v-focus @click="confirmDelete" :disabled="deleteBusy">
            {{ deleteBusy ? "Deleting…" : "Delete" }}
          </button>
        </div>
      </div>
    </div>

    <!-- Rename dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="renameId !== null" @click.self="renameId = null">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">Rename workspace</h3>
        <input
          v-model="renameName"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="Workspace name"
          @keydown.enter="confirmRename"
          @keydown.esc="renameId = null"
          ref="renameInputEl"
        />
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="renameId = null">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmRename" :disabled="!renameName.trim()">Rename</button>
        </div>
      </div>
    </div>

    <!-- Name dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="pendingPath" @click.self="pendingPath = ''">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">Name this workspace</h3>
        <p class="overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-base px-2 py-1.5 font-mono text-[11px] text-secondary-foreground">{{ pendingPath }}</p>
        <input
          v-model="pendingName"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="Workspace name"
          @keydown.enter="confirmCreate"
          @keydown.esc="pendingPath = ''"
          ref="nameInputEl"
        />
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="pendingPath = ''">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmCreate" :disabled="!pendingName.trim()">Create</button>
        </div>
      </div>
    </div>

    <!-- New worktree dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="wtParent" @click.self="closeWtDialog">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-6">
        <h3 class="text-sm font-semibold text-foreground">New worktree — {{ wtParent?.name }}</h3>
        <label class="-mb-1.5 text-[11px] font-semibold text-secondary-foreground">Branch</label>
        <input
          v-model="wtBranch"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="feature/my-branch"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
          ref="wtBranchEl"
        />
        <label class="-mb-1.5 text-[11px] font-semibold text-secondary-foreground">Base branch <span class="font-normal text-muted-foreground">(only for a new branch)</span></label>
        <div class="relative">
          <input
            v-model="wtBase"
            class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
            placeholder="Search branches — empty = current HEAD"
            spellcheck="false"
            @focus="wtBaseOpen = true"
            @blur="wtBaseOpen = false"
            @input="wtBaseOpen = true"
            @keydown.enter="confirmWorktree"
            @keydown.esc.stop="wtBaseOpen ? (wtBaseOpen = false) : closeWtDialog()"
          />
          <div
            v-if="wtBaseOpen && wtBaseMatches.length"
            class="absolute left-0 right-0 top-full z-10 mt-1 max-h-[200px] overflow-y-auto rounded-md border border-border bg-[var(--bg-dropdown)] py-1"
          >
            <button
              v-for="b in wtBaseMatches"
              :key="b"
              class="block w-full truncate px-2.5 py-1 text-left text-[12px] hover:bg-hover"
              :class="b === wtBase ? 'text-accent' : 'text-foreground'"
              @mousedown.prevent="wtBase = b; wtBaseOpen = false"
            >{{ b }}</button>
          </div>
        </div>
        <datalist id="wt-base-branches">
          <option v-for="b in wtLocalBranches" :key="b" :value="b" />
        </datalist>
        <p class="overflow-hidden text-ellipsis whitespace-nowrap rounded border border-border bg-base px-2 py-1.5 font-mono text-[11px] text-secondary-foreground">{{ wtTargetPath }}</p>
        <p v-if="wtError" class="whitespace-pre-wrap break-words text-[11px] text-destructive">{{ wtError }}</p>
        <div class="flex justify-end gap-2">
          <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="closeWtDialog">Cancel</button>
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" @click="confirmWorktree" :disabled="!wtBranch.trim() || wtBusy">
            {{ wtBusy ? "Creating…" : "Create" }}
          </button>
        </div>
      </div>
    </div>

    <!-- Git manager — a full-screen surface like Settings, not a dialog: at panel
         scale the diff and file lists were unreadable. -->
    <div class="fixed inset-0 z-[1000] flex flex-col overflow-hidden bg-base [backdrop-filter:var(--blur-overlay,none)] [-webkit-backdrop-filter:var(--blur-overlay,none)]" v-if="gitDialogOpen">
      <div class="relative flex h-[52px] shrink-0 items-center justify-end border-b border-border bg-panel px-6">
        <div class="absolute left-1/2 flex -translate-x-1/2 items-center gap-2.5">
          <PhGitBranch :size="15" class="text-muted-foreground" />
          <span class="text-sm font-semibold text-foreground">Git</span>
        </div>
        <button class="flex rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="gitDialogOpen = false">
          <PhX :size="15" />
        </button>
      </div>
      <GitPanel size="lg" class="min-h-0 flex-1" />
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, reactive, computed, shallowRef, nextTick, onMounted, onUnmounted, watch, inject } from "vue";
import {
  PhFolder,
  PhFolderPlus,
  PhCaretDown,
  PhArrowCounterClockwise,
  PhX,
  PhTerminal,
  PhRobot,
  PhPlus,
  PhGitBranch,
  PhGitPullRequest,
  PhGitMerge,
  PhChatCenteredText,
  PhGear,
  PhCircleNotch,
  PhCheckCircle,
  PhInfo,
  PhSquaresFour,
  PhPlayCircle,
  PhTrash,
  PhCheck,
  PhArchive,
  PhPushPin,
  PhWarningCircle,
  PhFolders,
  PhCrown,
  PhUsersThree,
  PhAsterisk,
  PhMoon,
  PhDotsThree,
  PhAlarm,
  PhCaretRight,
} from "@phosphor-icons/vue";
import GitPanel from "./GitPanel.vue";
import UpdateBanner from "./UpdateBanner.vue";
import StageBackdrop from "./StageBackdrop.vue";
import ProviderUpdateBanner from "./ProviderUpdateBanner.vue";
import PlanUsageIndicator from "./PlanUsageIndicator.vue";
import { pickDir } from "@/lib/pickPath";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore, type TabSummary } from "@/stores/terminalTabs";
import { chatAsleep, useClaudeChatsStore, type ClaudeSession } from "@/stores/claudeChats";
import { useUIStore } from "@/stores/ui";
import { router } from "@/router";
import { spinnerFrame } from "@/lib/spinner";
import { agentIconComp } from "@/lib/agentIcons";
import {
  getAgentAttentionState,
  type AgentAttentionState,
  type TermStatus,
} from "@/lib/terminalStatus";
import { useGitStore, type PrInfo } from "@/stores/git";
import { useNotificationsStore } from "@/stores/notifications";
import { isPinned, togglePin, unpin } from "@/lib/pinnedWorkspaces";
import { isChatPinned, toggleChatPin, unpinChat } from "@/lib/pinnedChats";
import { createPendingDeletes } from "@/lib/pendingDeletes";
import { isArchived, toggleArchived, forgetArchived } from "@/lib/archivedWorkspaces";
import {
  buildActivityRows,
  groupByRepo,
  parseShortstat,
  splitSections,
  stepWorkingShelf,
  routeRowOnly,
  EMPTY_WORKING_SHELF,
  type ActivityRow,
} from "@/lib/sidebarGroups";
import { childrenOf } from "@/stores/chatTree";
import { toggleTabSettled } from "@/lib/settledTabs";
import { formatWakeTime, snoozePresets } from "@/lib/snoozePresets";
import { getProjectSettings } from "@/lib/projectSettings";


import { useScriptsStore, type Script } from "@/stores/scripts";


const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const chats = useClaudeChatsStore();
const snoozedUntilOf = (chatId: number | null | undefined) =>
  (chatId != null && chats.sessions.find((s) => s.id === chatId)?.snoozedUntil) || 0;
// Linked PR glyph (chatpr.go): state as last refreshed, no polling.
const PR_GLYPH = {
  open: { icon: PhGitPullRequest, cls: "text-success" },
  draft: { icon: PhGitPullRequest, cls: "text-muted-foreground" },
  merged: { icon: PhGitMerge, cls: "text-accent" },
  closed: { icon: PhGitPullRequest, cls: "text-destructive" },
} as const;
const prGlyphOf = (chatId: number | null | undefined) => {
  const s = chatId != null ? chats.sessions.find((c) => c.id === chatId) : undefined;
  return s?.prNumber && s.prState ? { ...PR_GLYPH[s.prState], title: `PR #${s.prNumber} ${s.prState}` } : null;
};
const chatIsAsleep = (chatId: number | null | undefined) => chatId != null && chatAsleep(chats.sessions.find((s) => s.id === chatId));
const chatRecapOf = (chatId: number | null | undefined) => (chatId != null && ui.chatRecaps ? chats.sessions.find((s) => s.id === chatId)?.recap : undefined);
const ui = useUIStore();
const git = useGitStore();
const notif = useNotificationsStore();
const scriptsStore = useScriptsStore();

const active = computed(() => store.active);
const activeRepo = computed(() =>
  active.value ? repoById(active.value.parent_id ?? active.value.id) ?? null : null,
);

function repoById(id: number): Workspace | undefined {
  return store.topLevel.find((w) => w.id === id);
}
function repoName(ws: Workspace): string {
  return repoById(ws.parent_id ?? ws.id)?.name ?? ws.name;
}
function repoIcon(ws: Workspace): string | undefined {
  return store.icons[ws.parent_id ?? ws.id];
}
/** A worktree shows its own branch; a repo shows whatever the 60s sweep saw. */
function branchOf(ws: Workspace): string {
  return ws.worktree_branch || git.branchByWs[ws.id] || "";
}
function isOpened(id: number): boolean {
  return store.opened.some((w) => w.id === id);
}
function childCount(id: number): number {
  return (store.worktreesByParent[id] || []).length;
}

// ── the feed ─────────────────────────────────────────────────────────────────
const filterProjectId = ref<number | null>(null);
const filterOpen = ref(false);
const filterEl = ref<HTMLElement>();
const filterMenuStyle = ref<Record<string, string>>({});

watch(filterOpen, (open) => {
  if (open && filterEl.value) {
    const r = filterEl.value.getBoundingClientRect();
    filterMenuStyle.value = { left: `${r.left + 6}px`, top: `${r.bottom - 4}px`, width: `${r.width - 12}px` };
  }
});

const filterRepos = computed(() =>
  [...store.topLevel].sort((a, b) => Number(isArchived(a.id)) - Number(isArchived(b.id))),
);

// Chats deleted inside their undo window are hidden here, not gone yet.
const pendingDeletes = createPendingDeletes();
const visibleTabsByWs = computed(() => {
  if (!pendingDeletes.hidden.size) return termTabs.tabsByWs;
  const out: Record<number, TabSummary[]> = {};
  for (const [ws, tabs] of Object.entries(termTabs.tabsByWs)) out[Number(ws)] = tabs.filter((t) => !pendingDeletes.isHidden(t.chatId));
  return out;
});

const feed = computed(() =>
  buildActivityRows({
    openedWorkspaces: store.opened,
    tabsByWs: visibleTabsByWs.value,
    // A chat's recency is the DB-backed `last_activity_at` on its row, not the
    // localStorage stamp: the stamps are pruned for any key a setTabs list
    // doesn't mention, so a restart (chat tabs restore after the pty ones) wiped
    // them and every thread came back reading "now". Plain pty tabs have no
    // durable row of their own, so they still use the stamp.
    activityAt: (wsId, tab) =>
      (tab.chatId != null
        ? chats.sessions.find((s) => s.id === tab.chatId)?.lastActivityAt
        : undefined) ?? termTabs.activityAt(wsId, tab),
    filterProjectId: filterProjectId.value,
    isPinned: (tab) => isChatPinned(tab.chatId),
  }),
);

// ── sections (t3code-style): Pinned + Active scroll on top; Working, Snoozed
// and Settled are docked at the bottom. Search lives in Spotlight (⌘P).
const allRows = computed(() => [...feed.value.live, ...feed.value.settledChats]);

// The Working shelf's own state: when each turn started, and the exit grace
// that keeps a row that just stopped in place for a beat (stepWorkingShelf).
const shelf = shallowRef(EMPTY_WORKING_SHELF);
const workingHover = ref(false);
let shelfTimer: number | undefined;
const runningKeys = computed(() =>
  allRows.value.filter((r) => r.tab.status === "running").map(rowKey).sort().join(","),
);
function stepShelf() {
  window.clearTimeout(shelfTimer);
  const { state, wakeAt } = stepWorkingShelf(shelf.value, new Set(runningKeys.value.split(",").filter(Boolean)), Date.now(), workingHover.value);
  shelf.value = state;
  if (wakeAt != null) shelfTimer = window.setTimeout(stepShelf, Math.max(0, wakeAt - Date.now()));
}
watch([runningKeys, workingHover], stepShelf, { immediate: true });
// Touch has no hover: a finger lifting would otherwise "leave" and drop rows mid-read.
function onShelfHover(e: PointerEvent, inside: boolean) {
  if (e.pointerType === "mouse") workingHover.value = inside;
}

const sections = computed(() =>
  splitSections(allRows.value, {
    isPinned: (r) => isChatPinned(r.tab.chatId),
    isSnoozed: (r) => snoozedUntilOf(r.tab.chatId) > 0,
    inWorking: (r) => shelf.value.shown.has(rowKey(r)),
    unseen: (r) => termTabs.isCompletionUnseen(r.ws.id, r.tab.id),
    startedAt: (r) => shelf.value.startedAt.get(rowKey(r)) ?? r.ts,
  }),
);
const filteredSettled = computed(() => sections.value.settled);

const WORKING_EXPANDED_KEY = "burrow.sidebar.workingExpanded";
const workingExpanded = ref(localStorage.getItem(WORKING_EXPANDED_KEY) !== "0");
function shelfOpen(key: "working" | "snoozed" | "settled"): boolean {
  return key === "working" ? workingExpanded.value : !collapsed.value.includes(key);
}
function toggleShelf(key: "working" | "snoozed" | "settled") {
  if (key !== "working") return toggleSection(key);
  workingExpanded.value = !workingExpanded.value;
  try { localStorage.setItem(WORKING_EXPANDED_KEY, workingExpanded.value ? "1" : "0"); } catch { /* private mode */ }
}
const shelfSections = computed(() => {
  const s = sections.value;
  return [
    // Collapsed Working still shows the thread on screen (t3code's routeThreadOnly).
    { key: "working" as const, label: "Working", count: s.working.length, rows: workingExpanded.value ? s.working : routeRowOnly(s.working, isActiveRow) },
    { key: "snoozed" as const, label: "Snoozed", count: s.snoozed.length, rows: shelfOpen("snoozed") ? s.snoozed : [] },
    { key: "settled" as const, label: "Settled", count: s.settled.length, rows: shelfOpen("settled") ? settledChatsVisible.value : [] },
  ].filter((sec) => sec.count);
});

// ── project groups (Ghostex-style feed) ─────────────────────────────────────
const liveGroups = computed(() =>
  groupByRepo(sections.value.active, activeRepo.value ? [activeRepo.value.id] : []),
);
/** Pinned rows sit above every repo group, as one pseudo-group under this id. */
const PINNED = -1;
const topGroups = computed(() =>
  sections.value.pinned.length ? [{ repoId: PINNED, rows: sections.value.pinned }, ...liveGroups.value] : liveGroups.value,
);

const COLLAPSED_KEY = "burrow.sidebar.collapsedRepos";
function loadCollapsed(): Set<number> {
  try { return new Set(JSON.parse(localStorage.getItem(COLLAPSED_KEY) ?? "[]")); } catch { return new Set(); }
}
const collapsedGroups = reactive(loadCollapsed());
function toggleGroup(repoId: number) {
  if (collapsedGroups.has(repoId)) collapsedGroups.delete(repoId);
  else collapsedGroups.add(repoId);
  try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...collapsedGroups])); } catch { /* private mode */ }
}

const REPO_TILE_COLORS = ["#7C6FF0", "#4C8B6A", "#C26A4A", "#3D7FB8", "#A0558C", "#8A7A3A"];
function repoColor(repoId: number): string {
  return REPO_TILE_COLORS[repoId % REPO_TILE_COLORS.length];
}

// Uncommitted +/− per repo header (`git diff --shortstat HEAD`: staged and
// unstaged tracked changes; untracked files don't count).
// ponytail: polled every 15 s + on window focus for the repos on screen; a
// file watcher in Go is the upgrade if this ever shows up in a profile.
const repoDiff = reactive<Record<number, { added: number; removed: number } | undefined>>({});
async function refreshRepoDiffs() {
  for (const g of liveGroups.value) {
    const repo = repoById(g.repoId);
    if (!repo) continue;
    // Not gated on `is_git`: a stale row can say false for a real repo; a
    // non-repo simply fails here and keeps no stat.
    const out = await invoke<{ stdout: string; success: boolean }>("run_git", { cwd: repo.path, args: ["diff", "--shortstat", "HEAD"] }).catch(() => null);
    if (out?.success) repoDiff[g.repoId] = parseShortstat(out.stdout);
  }
}
watch(() => liveGroups.value.map((g) => g.repoId).join(","), () => void refreshRepoDiffs(), { immediate: true });
let diffTimer: number | undefined;
onMounted(() => {
  diffTimer = window.setInterval(() => void refreshRepoDiffs(), 15_000);
  window.addEventListener("focus", refreshRepoDiffs);
});
onUnmounted(() => {
  window.clearInterval(diffTimer);
  window.removeEventListener("focus", refreshRepoDiffs);
});

function childrenOfChat(chatId: number): ClaudeSession[] {
  return childrenOf(chats.sessions, chatId);
}
const openRightPanelSubagent = inject<(chatId: number, workspaceId: number) => void>("openRightPanelSubagent", () => {});
function openSubagent(chatId: number, wsId: number) {
  openRightPanelSubagent(chatId, wsId);
}

const settledChatsLimit = ref(12);
const settledChatsVisible = computed(() => filteredSettled.value.slice(0, settledChatsLimit.value));
const archivedChats = computed(() => {
  const all = active.value ? chats.archivedSessionsForWs(active.value.id) : [];
  return all.filter((s) => !pendingDeletes.isHidden(s.id));
});

function unarchiveAndOpen(chatId: number) {
  if (!active.value) return;
  termTabs.openChat(active.value.id, chatId);
}
// Confirm dialogs focus their action, so Enter confirms and Esc (bubbling to
// the overlay) cancels without reaching for the mouse.
const vFocus = { mounted: (el: HTMLElement) => el.focus() };
const confirmDialog = ref<{ message: string; onConfirm: () => void } | null>(null);
function runConfirm() {
  confirmDialog.value?.onConfirm();
  confirmDialog.value = null;
}
// Deleting a chat asks nothing: its rows hide at once and an Undo toast holds
// the real delete for 8 s (t3code's undo notice). A plain terminal tab keeps
// its confirm — closing it kills the process, which no undo can bring back.
function deleteChatsWithUndo(items: { wsId: number; tabId?: number; chatId: number }[], title: string) {
  // The thread on screen is about to vanish from the list — send the view
  // home, same as settling it does.
  if (items.some((i) => i.tabId != null && store.active?.id === i.wsId && termTabs.activeByWs[i.wsId] === i.tabId)) ui.openWelcome();
  const undo = pendingDeletes.schedule(items.map((i) => i.chatId), async () => {
    for (const i of items) {
      // close() is a single request slot — let Terminal take each one. Sequential
      // remove(): it stops each chat's process before deleting its row.
      if (i.tabId != null) { termTabs.close(i.wsId, i.tabId); await nextTick(); }
      unpinChat(i.chatId);
      await chats.remove(i.chatId);
    }
  });
  notif.push({ type: "info", title, duration: 8000, action: { label: "Undo", run: undo } });
}
function deletePermanently(chatId: number) {
  const title = chats.sessions.find((s) => s.id === chatId)?.title ?? "chat";
  deleteChatsWithUndo([{ wsId: active.value?.id ?? 0, chatId }], `Deleted “${title}”`);
}
function deleteAllSettled() {
  const rows = filteredSettled.value.filter((r) => !(r.tab.isChat && isChatPinned(r.tab.chatId)));
  const chatRows = rows.filter((r) => r.tab.isChat && r.tab.chatId != null);
  const ptyRows = rows.filter((r) => !chatRows.includes(r));
  const run = () => {
    for (const r of ptyRows) termTabs.close(r.ws.id, r.tab.id);
    if (chatRows.length) {
      deleteChatsWithUndo(
        chatRows.map((r) => ({ wsId: r.ws.id, tabId: r.tab.id, chatId: r.tab.chatId! })),
        chatRows.length === 1 ? `Deleted “${chatRows[0].tab.title}”` : `Deleted ${chatRows.length} threads`,
      );
    }
  };
  if (!ptyRows.length) return run();
  confirmDialog.value = {
    message: `Close ${ptyRows.length} settled terminal tab${ptyRows.length === 1 ? "" : "s"} and kill their processes? Pinned threads are kept; deleted chats can be undone.`,
    onConfirm: run,
  };
}
// Last step of a thread's life: settled → archived → gone. A chat row is a
// stored session, so it needs the store's hard delete; a plain terminal tab
// only exists as long as its PTY, so closing it *is* the delete.
function deleteThread(wsId: number, tab: TabSummary) {
  if (tab.isChat && tab.chatId != null) {
    deleteChatsWithUndo([{ wsId, tabId: tab.id, chatId: tab.chatId }], `Deleted “${tab.title}”`);
    return;
  }
  confirmDialog.value = {
    message: `Delete “${tab.title}” permanently? This cannot be undone.`,
    onConfirm: () => termTabs.close(wsId, tab.id),
  };
}
// Closing a chat tab archives it (Terminal's stopChatSession); reopening the
// chat is the existing unarchive path, so that is the Undo.
// ponytail: an Undo landing before archive()'s process stop resolves gets
// re-archived by it; 8 s window vs a stop of a few ms, not worth a lock.
function closeTab(wsId: number, tab: TabSummary) {
  termTabs.close(wsId, tab.id);
  if (!tab.isChat || tab.chatId == null) return;
  const chatId = tab.chatId;
  notif.push({
    type: "info",
    title: `Archived “${tab.title}”`,
    duration: 8000,
    action: { label: "Undo", run: () => termTabs.openChat(wsId, chatId) },
  });
}
function toggleSettled(tab: TabSummary, wsId: number) {
  // Settling the tab the user is currently looking at leaves nothing to show
  // in its place — send them back to the composer. Settling any other row is
  // a background action: the view must not move.
  const wasFocused = store.active?.id === wsId && termTabs.activeByWs[wsId] === tab.id;
  const wasSettled = tab.settled;
  if (tab.isChat) {
    if (tab.chatId == null) return;
    if (wasSettled) chats.unsettle(tab.chatId);
    else chats.settle(tab.chatId);
  } else {
    toggleTabSettled(wsId, tab.id);
  }
  if (!wasSettled && wasFocused) ui.openWelcome();
}

type SectionKey = "snoozed" | "settled" | "archived";
const collapsed = ref<SectionKey[]>(["snoozed", "settled", "archived"]);
function toggleSection(key: SectionKey) {
  collapsed.value = collapsed.value.includes(key)
    ? collapsed.value.filter((k) => k !== key)
    : [...collapsed.value, key];
}

// ── row hover card ───────────────────────────────────────────────────────────
// Stolen from pingdotgg/t3code's SidebarThreadTooltip: a short delay before
// showing (so a fast mouse pass doesn't flash it) but instant on leave.
// Generic shape so live/settled rows (ActivityRow) and archived sessions
// (ClaudeSession, no live tab) can share one card.
interface HoverInfo {
  title: string;
  ws: Workspace;
  branch?: string;
  model?: string;
  leafCount?: number;
  round?: number;
  sessionId?: string;
  status?: TermStatus;
  /** Settings → Developer → Show internal IDs. */
  debugId?: string;
  ts: number;
}

const HOVER_DELAY_MS = 150;
const hoverInfo = ref<HoverInfo | null>(null);
const hoverStyle = ref<Record<string, string>>({});
let hoverTimer: number | undefined;

function hoverInfoFromRow(row: ActivityRow): HoverInfo {
  return {
    title: row.tab.title,
    ws: row.ws,
    branch: row.tab.branch,
    model: row.tab.model,
    leafCount: row.tab.leafCount,
    round: row.tab.isAgent ? row.tab.round : undefined,
    sessionId: row.tab.sessionId,
    status: row.tab.status,
    debugId: row.tab.chatId != null ? `chat ${row.tab.chatId}` : `tab ${row.tab.id}`,
    ts: row.ts,
  };
}

function hoverInfoFromArchived(s: ClaudeSession, ws: Workspace): HoverInfo {
  return {
    title: s.title,
    ws,
    model: s.model,
    round: s.messageCount,
    sessionId: s.claudeSessionId,
    debugId: `chat ${s.id}`,
    ts: s.archivedAt ?? Date.now(),
  };
}

function statusLabel(status: TermStatus): string {
  switch (status) {
    case "running": return "Working";
    case "permission": return "Needs input";
    case "waiting": return "Waiting";
    case "error": return "Error";
    case "review": return "Done";
    default: return "Idle";
  }
}

function scheduleHoverCard(info: HoverInfo, e: MouseEvent) {
  cancelHoverCard();
  const el = e.currentTarget as HTMLElement;
  hoverTimer = window.setTimeout(() => {
    const r = el.getBoundingClientRect();
    hoverStyle.value = {
      left: `${r.right + 6}px`,
      top: `${Math.min(r.top, window.innerHeight - 160)}px`,
    };
    hoverInfo.value = info;
  }, HOVER_DELAY_MS);
}
function cancelHoverCard() {
  if (hoverTimer) clearTimeout(hoverTimer);
  hoverTimer = undefined;
  hoverInfo.value = null;
}

function rowKey(row: ActivityRow): string {
  return `${row.ws.id}:${row.tab.id}`;
}
function isActiveRow(row: ActivityRow): boolean {
  // termTabs.activeByWs keeps pointing at the last focused tab even once the
  // welcome composer is up — gate on viewingTabs so "new thread" doesn't leave
  // the thread it came from looking focused with nothing behind it.
  if (!ui.viewingTabs) return false;
  return store.active?.id === row.ws.id && termTabs.activeByWs[row.ws.id] === row.tab.id;
}

// ── relative time ────────────────────────────────────────────────────────────
// One shared clock; every row's label recomputes off it instead of its own timer.
const now = ref(Date.now());
let clock: number | undefined;

function ago(ts: number): string {
  if (!ts) return "";
  const s = Math.max(0, Math.round((now.value - ts) / 1000));
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

function statusText(row: ActivityRow): string {
  switch (row.tab.status) {
    case "running": return `Working ${ago(row.ts)}`;
    case "permission": return "Needs input";
    case "waiting": return "Waiting";
    case "error": return "Error";
    case "review": return "Done";
    default: return ago(row.ts);
  }
}

function statusClass(status: TermStatus): string {
  switch (status) {
    case "running": return "text-accent";
    case "permission": return "text-[var(--yellow)]";
    case "waiting": return "text-[var(--yellow)]";
    case "error": return "text-destructive";
    case "review": return "text-[var(--blue)]";
    default: return "text-muted-foreground";
  }
}

function attentionState(row: ActivityRow): AgentAttentionState {
  return getAgentAttentionState(row.tab.status, termTabs.isCompletionUnseen(row.ws.id, row.tab.id));
}



function prClass(info: PrInfo): string {
  if (info.checks === "fail") return "text-destructive bg-[color-mix(in_srgb,var(--red)_14%,transparent)]";
  if (info.checks === "pending") return "text-[var(--yellow)] bg-[color-mix(in_srgb,var(--yellow)_14%,transparent)]";
  if (info.state === "merged") return "text-accent bg-[color-mix(in_srgb,var(--accent)_14%,transparent)]";
  if (info.state === "closed") return "text-destructive bg-[color-mix(in_srgb,var(--red)_12%,transparent)]";
  if (info.isDraft) return "text-muted-foreground";
  return "text-[var(--green)] bg-[color-mix(in_srgb,var(--green)_12%,transparent)]";
}
function prTitle(info: PrInfo): string {
  const state = info.isDraft && info.state === "open" ? "draft" : info.state.toLowerCase();
  const checks = info.checks === "none" ? "" : ` · checks ${info.checks}`;
  return `PR #${info.number} (${state})${checks}`;
}

// ── selection ────────────────────────────────────────────────────────────────
/**
 * Picking a project in the filter both narrows the feed and opens that repo on
 * the composer — the sidebar no longer lists projects, so this is how a repo
 * with no threads gets mounted at all. A group header's "+" passes
 * `narrow = false`: it starts a thread, it doesn't pick a filter.
 */
function pickProject(repo: Workspace, narrow = true) {
  if (narrow) filterProjectId.value = repo.id;
  filterOpen.value = false;
  // Navigate to welcome BEFORE store.open(): App.vue's watcher on
  // ws.active?.id force-navigates to /ws/:id whenever the active workspace
  // changes while a workspace/tab route is showing, so opening the workspace
  // first raced that watcher's router.replace against this push and could
  // land back on the workspace's last (old) tab instead of welcome.
  void ui.openWelcome().then(() => { // picking a project is an explicit "compose something new"
    store.open(repo);
    const parent = repo.parent_id ?? repo.id;
    for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
  });
}

function selectWs(ws: Workspace) {
  store.open(ws);
  // Keep the repo's worktrees mounted so their rows stay in the feed.
  const parent = ws.parent_id ?? ws.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
  // Navigate last: showTabs() reads the now-active workspace and lands on the
  // composer instead when that workspace has nothing live to show.
  ui.showTabs();
}

function selectTab(row: ActivityRow) {
  // Navigate only. Opening the workspace here first would set `ws.active`
  // synchronously while the route still named the OLD workspace, and App.vue's
  // `ws.active` watcher reads that as a stale URL and replaces it with a bare
  // `/ws/<id>` — dropping the tab. That is why clicking a thread in another
  // project used to take two clicks: the first one only switched projects.
  // The route watcher opens the workspace itself, in the right order.
  void router.push(`/ws/${row.ws.id}/tab/${row.tab.id}`);
}

// "New chat" opens the project picker (⌘⇧O's Spotlight mode) rather than a
// chat tab directly — same command-palette → Welcome composer flow either way.
function newChatSession(_ws: Workspace) {
  ui.pickProjectThenWelcome();
}

const gitDialogOpen = ref(false);

function openGitTab(ws: Workspace) {
  selectWs(ws);
  gitDialogOpen.value = true;
}

function newTerminalTab(ws: Workspace) {
  const wasOpen = isOpened(ws.id);
  selectWs(ws);
  const add = () => termTabs.add(ws.id);
  wasOpen ? add() : nextTick(add);
}

// ── row context menu ─────────────────────────────────────────────────────────
const rowMenu = ref<{ ws: Workspace; tab: TabSummary | null; x: number; y: number } | null>(null);
const rowMenuStyle = computed(() =>
  rowMenu.value
    ? {
        left: `${Math.min(rowMenu.value.x, window.innerWidth - 206)}px`,
        top: `${Math.min(rowMenu.value.y, window.innerHeight - 300)}px`,
      }
    : {},
);
const snoozeMenuOpen = ref(false);
function openRowMenu(ws: Workspace, tab: TabSummary | null, e: { clientX: number; clientY: number }) {
  rowMenu.value = { ws, tab, x: e.clientX, y: e.clientY };
  snoozeMenuOpen.value = false;
}

// Long press = right click on touch (iOS Safari never fires `contextmenu` for it).
// Mouse and pen keep right click; a finger that moves is a scroll, not a press.
const LONG_PRESS_MS = 500;
const LONG_PRESS_SLOP = 10;
let longPress: { timer: ReturnType<typeof setTimeout>; x: number; y: number } | null = null;
let swallowClick = false;
function startLongPress(row: ActivityRow, e: PointerEvent) {
  cancelLongPress();
  if (e.pointerType !== "touch") return;
  const { clientX: x, clientY: y } = e;
  longPress = {
    x, y,
    timer: setTimeout(() => {
      longPress = null;
      swallowClick = true; // the finger lifting after this must not open the row
      navigator.vibrate?.(10);
      openRowMenu(row.ws, row.tab, { clientX: x, clientY: y });
    }, LONG_PRESS_MS),
  };
}
function moveLongPress(e: PointerEvent) {
  if (longPress && Math.hypot(e.clientX - longPress.x, e.clientY - longPress.y) > LONG_PRESS_SLOP) cancelLongPress();
}
function cancelLongPress() {
  if (longPress) clearTimeout(longPress.timer);
  longPress = null;
}
function onRowClick(row: ActivityRow) {
  if (swallowClick) { swallowClick = false; return; }
  selectTab(row);
}
/** Run an action against the menu's row, then close the menu. */
function withMenu(fn: (ws: Workspace, tab: TabSummary | null) => void) {
  const entry = rowMenu.value;
  rowMenu.value = null;
  if (entry) fn(entry.ws, entry.tab);
}

// ── delete ───────────────────────────────────────────────────────────────────
const deleteTarget = ref<Workspace | null>(null);
const deleteForce = ref(false);
const deleteBusy = ref(false);
const deleteError = ref("");

watch(deleteTarget, () => { deleteForce.value = false; deleteError.value = ""; });

async function confirmDelete() {
  const ws = deleteTarget.value;
  if (!ws || deleteBusy.value) return;
  deleteBusy.value = true;
  deleteError.value = "";
  try {
    // store.remove cascades into the repo's worktrees; removeWorktree handles one.
    if (ws.parent_id) await store.removeWorktree(ws.id, deleteForce.value);
    else await store.remove(ws.id);
    forgetArchived(ws.id);
    unpin(ws.id);
    termTabs.clear(ws.id);
    deleteTarget.value = null;
  } catch (err) {
    deleteError.value = err instanceof Error ? err.message : String(err);
  } finally {
    deleteBusy.value = false;
  }
}

// ── tab inline rename ────────────────────────────────────────────────────────
const editingTab = ref<{ wsId: number; tabId: number } | null>(null);
const editingTabTitle = ref("");
let renameReadyAt = 0;

// Rebuild a chat's title from its full transcript, not just the first prompt
// (`refineTitle` in AgentChat.vue only sees that). Best-effort: falls back to
// the transcript-less current title as the seed if the load fails, and is a
// no-op if generation comes back empty.
async function regenerateTitle(ws: Workspace, tab: TabSummary) {
  if (!tab.isChat || tab.chatId == null) return;
  const chatId = tab.chatId;
  const toastId = notif.push({ type: "pending", title: "Regenerating title…", source: "agent" });
  let text = tab.title;
  try {
    const raw = await invoke<string>("load_chat_messages", { chatId });
    const messages = JSON.parse(raw) as { role: string; text?: string }[];
    const transcript = messages
      .filter((m) => m.text?.trim())
      .map((m) => `${m.role}: ${m.text}`)
      .join("\n");
    if (transcript) text = transcript;
  } catch {
    // fall back to current title as the seed
  }
  let title = "";
  try {
    title = await invoke<string>("generate_chat_title", {
      cwd: ws.path,
      model: ui.textGenerationModel,
      policy: ui.textGenerationPolicy,
      text,
    });
  } catch {
    notif.resolve(toastId, { type: "error", title: "Title regeneration failed", source: "agent" });
    return;
  }
  if (title) {
    chats.sync(chatId, { title });
    notif.resolve(toastId, { type: "done", title: "Title regenerated", source: "agent" });
  } else {
    notif.resolve(toastId, { type: "error", title: "Title regeneration failed", source: "agent" });
  }
}

function startTabRename(wsId: number, tab: TabSummary) {
  editingTab.value = { wsId, tabId: tab.id };
  editingTabTitle.value = tab.title;
  renameReadyAt = Date.now() + 200; // blur within 200ms of focus = noise, ignore
  nextTick(() => {
    const el = document.querySelector<HTMLInputElement>(".ws-term-rename-input");
    el?.focus();
    el?.select();
  });
}

function commitTabRename() {
  if (Date.now() < renameReadyAt) return; // spurious blur before user could type
  if (!editingTab.value) return;
  const title = editingTabTitle.value.trim();
  if (title) termTabs.rename(editingTab.value.wsId, editingTab.value.tabId, title);
  editingTab.value = null;
}

function cancelTabRename() {
  editingTab.value = null;
}

// ── PR + branch polling ──────────────────────────────────────────────────────
// gh/git run out of process and failures cache null, so this never blocks the UI.
let prTimer: number | undefined;
// Branches only. Cheap local git, so unlike the gh sweep below it runs on
// mount rather than behind requestIdleCallback — a branch chip that says
// "HEAD" for the first 2.5s is a wrong answer, not a pending one.
function refreshAllBranches() {
  for (const ws of store.workspaces) if (ws.path) git.ensureBranch(ws.id, ws.path);
}

function refreshAllPrs() {
  // Concurrency-capped pool (max 3 in flight) so a many-workspace sweep can't
  // spawn N blocking gh subprocesses at once.
  git.fetchPrs(store.workspaces.filter((ws) => ws.path).map((ws) => ({ wsId: ws.id, cwd: ws.path })));
}

// Mount the active workspace + its worktree siblings, so each Terminal restores
// its sessions into tabsByWs. NOT every workspace: mounting them all had their
// Terminals race to adopt the daemon's sessions, so a freshly-activated
// workspace showed another one's tabs.
function mountSections() {
  const a = active.value;
  if (!a) return;
  store.ensureOpen(a);
  const parent = a.parent_id ?? a.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
}

function onDocClick() { filterOpen.value = false; rowMenu.value = null; scriptsOpen.value = false; cancelHoverCard(); }

// ── scripts (bottom bar) ─────────────────────────────────────────────────────
const scriptsOpen = ref(false);
const scriptsEl = ref<HTMLElement>();
const scriptsMenuStyle = ref<Record<string, string>>({});
const scripts = computed(() => scriptsStore.scriptsFor(active.value?.path));

watch(scriptsOpen, (open) => {
  if (!open || !scriptsEl.value) return;
  const r = scriptsEl.value.getBoundingClientRect();
  scriptsMenuStyle.value = { left: `${r.left}px`, bottom: `${window.innerHeight - r.top + 6}px` };
});

// The scripts of a workspace are read from its config file on demand.
watch(() => active.value?.path, (path) => { if (path) scriptsStore.loadForPath(path); }, { immediate: true });

function runScript(sc: Script) {
  scriptsOpen.value = false;
  const cmd = scriptsStore.commandLine(sc);
  const ws = active.value;
  if (!cmd || !ws) return;
  ui.closeWelcome();
  termTabs.add(ws.id, cmd);
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape" && gitDialogOpen.value) gitDialogOpen.value = false;
}

onMounted(() => {
  document.addEventListener("click", onDocClick);
  document.addEventListener("keydown", onKeydown);
  mountSections();
  refreshAllBranches();
  clock = window.setInterval(() => { now.value = Date.now(); }, 15_000);
  // Defer the first PR sweep off the critical startup path. Firing gh for every
  // workspace synchronously here saturated the command workers and stalled the
  // real startup invokes → the window painted gray for seconds.
  const startPrs = () => { refreshAllPrs(); prTimer = window.setInterval(refreshAllPrs, 60_000); };
  if (typeof window.requestIdleCallback === "function") {
    window.requestIdleCallback(startPrs, { timeout: 2500 });
  } else {
    window.setTimeout(startPrs, 2500);
  }
});
onUnmounted(() => {
  if (prTimer) clearInterval(prTimer);
  if (clock) clearInterval(clock);
  if (hoverTimer) clearTimeout(hoverTimer);
  window.clearTimeout(shelfTimer);
  document.removeEventListener("click", onDocClick);
  document.removeEventListener("keydown", onKeydown);
});

// Watch only the STRUCTURE of the workspace set (its id list), not every nested
// property — a deep watch re-ran the mount sweep on any tab/PR mutation.
watch(() => store.workspaces.map((ws) => ws.id).join(","), () => { mountSections(); refreshAllBranches(); refreshAllPrs(); });
watch(() => active.value?.id, mountSections);

// ── branch helpers (worktree dialog) ─────────────────────────────────────────
interface GitOutput { stdout: string; stderr: string; code: number; }

// ── new worktree dialog ──────────────────────────────────────────────────────
const wtParent = ref<Workspace | null>(null);
const wtBranch = ref("");
const wtBase = ref("");
const wtBaseList = ref<string[]>([]);
const wtBaseOpen = ref(false);
// The Branch field suggests existing local branches to check out; the base
// picker lists remote-tracking ones too (origin/x).
const wtLocalBranches = ref<string[]>([]);
const wtBaseMatches = computed(() => {
  const q = wtBase.value.trim().toLowerCase();
  // The prefilled default must not hide the rest of the list.
  if (!q || wtBaseList.value.includes(wtBase.value)) return wtBaseList.value;
  return wtBaseList.value.filter((b) => b.toLowerCase().includes(q));
});
const wtBusy = ref(false);
const wtError = ref("");
const wtBranchEl = ref<HTMLInputElement>();

const wtTargetPath = computed(() => {
  if (!wtParent.value) return "";
  const repo = wtParent.value.path.split("/").filter(Boolean).pop() || "repo";
  const branch = wtBranch.value.trim() || "<branch>";
  return `${getProjectSettings(wtParent.value?.id ?? -1).worktreesDir || ui.worktreesDir}/${repo}/${branch}`;
});

async function openWtDialog(parent: Workspace) {
  wtParent.value = parent;
  wtBranch.value = "";
  wtBase.value = "";
  wtError.value = "";
  wtBaseList.value = [];
  wtLocalBranches.value = [];
  await nextTick();
  wtBranchEl.value?.focus();
  const [refs, def] = await Promise.all([
    gitLines(parent.path, ["for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes"]),
    gitLines(parent.path, ["symbolic-ref", "--short", "refs/remotes/origin/HEAD"]),
  ]);
  if (wtParent.value !== parent) return;
  wtLocalBranches.value = refs.filter((r) => r.startsWith("refs/heads/")).map((r) => r.slice("refs/heads/".length));
  const remote = refs
    .filter((r) => r.startsWith("refs/remotes/") && !r.endsWith("/HEAD"))
    .map((r) => r.slice("refs/remotes/".length));
  wtBaseList.value = [...wtLocalBranches.value, ...remote];
  // Default to the repo's default branch (origin/HEAD, else main/master), else leave HEAD.
  const defBranch = def[0]?.replace(/^origin\//, "") ?? ["main", "master"].find((b) => wtLocalBranches.value.includes(b));
  if (!wtBase.value && defBranch && wtBaseList.value.includes(defBranch)) wtBase.value = defBranch;
}

async function gitLines(cwd: string, args: string[]): Promise<string[]> {
  try {
    const out = await invoke<GitOutput>("run_git", { cwd, args });
    if (out.code === 0) return out.stdout.split("\n").map((l) => l.trim()).filter(Boolean);
  } catch {}
  return [];
}

function closeWtDialog() {
  wtParent.value = null;
}

async function confirmWorktree() {
  const branch = wtBranch.value.trim();
  if (!wtParent.value || !branch || wtBusy.value) return;
  wtBusy.value = true;
  wtError.value = "";
  try {
    const ws = await store.createWorktree(wtParent.value.id, branch, wtBase.value.trim() || null, wtTargetPath.value);
    wtParent.value = null;
    store.open(ws);
  } catch (err) {
    wtError.value = err instanceof Error ? err.message : String(err);
  } finally {
    wtBusy.value = false;
  }
}

// ── rename dialog ────────────────────────────────────────────────────────────
const renameId = ref<number | null>(null);
const renameName = ref("");
const renameInputEl = ref<HTMLInputElement>();

async function startRename(w: Workspace) {
  renameId.value = w.id;
  renameName.value = w.name;
  await nextTick();
  renameInputEl.value?.focus();
  renameInputEl.value?.select();
}
async function confirmRename() {
  const name = renameName.value.trim();
  if (renameId.value === null || !name) return;
  await store.rename(renameId.value, name);
  renameId.value = null;
}

// ── open folder ──────────────────────────────────────────────────────────────
const pendingPath = ref("");
const pendingName = ref("");
const nameInputEl = ref<HTMLInputElement>();

async function pickFolder() {
  // In-app picker (PathPicker.vue) instead of the native panel — same browse UI
  // everywhere a folder is chosen, and it can create the folder too.
  const selected = await pickDir({ title: "Add project", start: ui.defaultProjectDir || "~/" });
  if (!selected) return;
  pendingPath.value = selected;
  pendingName.value = selected.split("/").pop() || selected;
  await nextTick();
  nameInputEl.value?.focus();
  nameInputEl.value?.select();
}

async function confirmCreate() {
  if (!pendingName.value.trim()) return;
  const ws = await store.create(pendingName.value.trim(), pendingPath.value);
  pendingPath.value = "";
  pendingName.value = "";
  store.open(ws);
}
</script>

<style scoped>
.sb-toast-enter-active, .sb-toast-leave-active { transition: opacity 0.18s ease, transform 0.18s ease; }
.sb-toast-enter-from, .sb-toast-leave-to { opacity: 0; transform: translateY(6px); }
.section-header {
  display: flex;
  width: calc(100% - 8px);
  align-items: center;
  gap: 4px;
  margin: 8px 4px 2px;
  border-radius: 6px;
  padding: 4px 8px;
  text-align: left;
  font-size: 9.5px;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--text-muted);
  transition: color .12s, background .12s;
}
.section-header:hover { background: var(--bg-hover); color: var(--text-secondary); }

.menu-item {
  display: flex;
  width: 100%;
  align-items: center;
  border: 0;
  border-radius: 6px;
  background: transparent;
  padding: 6px 8px;
  text-align: left;
  font-size: 11.5px;
  color: var(--text-secondary);
  cursor: pointer;
}
.menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.menu-sep { margin: 4px 0; height: 1px; background: var(--border); }

/* Agent state tints the whole feed row, so it stays scannable at a glance. */
.agent-state-needs-input { background: color-mix(in srgb, var(--status-permission) 9%, transparent); }
.agent-state-error       { background: color-mix(in srgb, var(--red) 10%, transparent); }
.agent-state-done-unread { background: color-mix(in srgb, var(--green) 8%, transparent); }
.agent-state-needs-input .ws-term-icon-agent { color: var(--status-permission); }
.agent-state-error .ws-term-icon-agent { color: var(--red); }
.agent-state-done-unread .ws-term-icon-agent { color: var(--green); }
.agent-state-idle .ws-term-icon-agent { color: var(--text-muted); }

/* Ghostex-style feed */
.sb-icon { display: flex; align-items: center; justify-content: center; width: 28px; height: 28px; flex-shrink: 0; border-radius: 7px; color: var(--text-muted); transition: background-color .12s, color .12s; }
.sb-icon:hover { background: var(--bg-hover); color: var(--text-primary); }
.sb-repo-tile { display: flex; align-items: center; justify-content: center; width: 18px; height: 18px; flex-shrink: 0; border-radius: 5px; font-size: 10px; font-weight: 700; color: #fff; }
.sb-row { transition: background-color .12s; border: 1px solid transparent; -webkit-touch-callout: none; }
.sb-row-more { display: none; }
/* Finger, no hover: the ⋮ replaces the hover-only buttons, and the settled
   row's restore button replaces its age. */
@media (pointer: coarse) {
  .sb-row-more { display: flex; padding: 6px; }
  .sb-hover-acts { display: none !important; }
  .sb-unsettle { visibility: visible !important; padding: 6px; }
  .sb-ago { visibility: hidden; }
  .sb-row { padding-top: 10px; padding-bottom: 10px; }
}
.sb-row:hover { background: var(--bg-hover); }
.sb-row-active { background: var(--bg-selected); border-color: color-mix(in srgb, var(--text-primary) 10%, transparent); }
.sb-row-act { display: flex; padding: 3px; border-radius: 5px; color: var(--text-muted); }
.sb-row-act:hover { color: var(--text-primary); background: color-mix(in srgb, var(--text-primary) 8%, transparent); }
.sb-spark { color: #E0876A; font-size: 12px; }
</style>
