<template>
  <aside class="flex w-[var(--sidebar-width,220px)] shrink-0 grow-0 basis-[var(--sidebar-width,220px)] flex-col overflow-hidden border-r border-border bg-panel [-webkit-backdrop-filter:var(--blur-panels,none)] [backdrop-filter:var(--blur-panels,none)]">
    <!-- Project filter: "All projects" or one repo, narrows the feed below -->
    <div class="flex shrink-0 items-center gap-1 p-1.5 pb-1" ref="filterEl">
      <button
        class="flex min-w-0 flex-1 items-center gap-[7px] rounded-[7px] px-1.5 py-1.5 text-left transition-colors hover:bg-hover"
        :class="filterOpen && 'bg-hover'"
        @click.stop="filterOpen = !filterOpen"
      >
        <PhFolder :size="13" weight="fill" class="shrink-0 text-accent" />
        <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-foreground">{{ filterProjectId == null ? "All projects" : (repoById(filterProjectId)?.name ?? "All projects") }}</span>
        <PhCaretDown :size="11" weight="bold" class="shrink-0 text-muted-foreground" />
      </button>
      <button class="flex shrink-0 items-center rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90" title="Open folder…" @click="pickFolder">
        <PhFolderPlus :size="14" />
      </button>
      <Teleport to="body">
        <div v-if="filterOpen" class="fixed z-[1000] max-h-[60vh] overflow-y-auto rounded-lg border border-border bg-panel p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="filterMenuStyle" @click.stop>
          <button
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="filterProjectId == null && 'bg-[color-mix(in_srgb,var(--accent)_10%,transparent)] text-foreground'"
            @click="filterProjectId = null; filterOpen = false"
          >
            All projects
          </button>
          <button
            v-for="repo in store.topLevel"
            :key="repo.id"
            class="flex w-full items-center gap-[7px] rounded-md border-0 bg-transparent px-2 py-1.5 text-left font-ui text-[11.5px] text-secondary-foreground hover:bg-hover hover:text-foreground"
            :class="filterProjectId === repo.id && 'bg-[color-mix(in_srgb,var(--accent)_10%,transparent)] text-foreground'"
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

    <!-- Every new thread starts on the composer; this is its only entry point. -->
    <div v-if="active" class="flex shrink-0 items-center gap-1 px-1.5 pb-1.5">
      <button
        class="flex min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md border border-border/65 bg-transparent px-2 py-1.5 text-[11px] font-medium text-secondary-foreground transition-colors hover:border-border hover:bg-hover hover:text-foreground active:scale-[0.985]"
        :class="ui.welcomeOpen && 'border-accent/45 bg-accent/[0.08] text-foreground'"
        @click="ui.openWelcome()"
      >
        <PhPlus :size="12" class="text-accent" />
        New thread
      </button>
      <button v-if="activeRepo" class="flex shrink-0 items-center rounded-md border border-border/65 p-1.5 text-muted-foreground transition-colors hover:border-border hover:bg-hover hover:text-foreground active:scale-90" title="New worktree" @click="openWtDialog(activeRepo)"><PhGitBranch :size="13" /></button>
    </div>

    <div class="flex-1 overflow-y-auto pb-2">
      <!-- Live feed: every open project's tabs, newest activity first -->
      <div
        v-for="row in feed.live"
        :key="rowKey(row)"
        class="group relative cursor-pointer border-b border-border/40 px-2.5 py-[7px] transition-colors hover:bg-hover"
        :class="[
          isActiveRow(row) && 'bg-[color-mix(in_srgb,var(--accent)_9%,transparent)]',
          row.tab.isAgent && `agent-state-${attentionState(row)}`,
        ]"
        @click="selectTab(row)"
        @contextmenu.prevent.stop="openRowMenu(row.ws, row.tab, $event)"
      >
        <span v-if="isActiveRow(row)" class="absolute inset-y-0 left-0 w-[2px] bg-accent" />

        <!-- line 1: project + when -->
        <div class="flex items-center gap-1.5">
          <img v-if="repoIcon(row.ws)" :src="repoIcon(row.ws)!" class="h-3 w-3 shrink-0 rounded-[3px] object-cover" />
          <PhFolder v-else :size="11" weight="fill" class="shrink-0 text-accent/80" />
          <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[10.5px] text-muted-foreground">{{ repoName(row.ws) }}</span>
          <span class="shrink-0 text-[10px] tabular-nums" :class="statusClass(row.tab.status)">{{ statusText(row) }}</span>
        </div>

        <!-- line 2: thread title -->
        <div class="mt-[3px] flex items-center gap-1.5">
          <PhChatCenteredText v-if="row.tab.isChat" :size="11" class="shrink-0 text-[#d97706]" />
          <PhRobot v-else-if="row.tab.isAgent" :size="11" class="ws-term-icon-agent shrink-0 text-accent" />
          <PhTerminal v-else :size="11" class="shrink-0 text-muted-foreground" />
          <input
            v-if="editingTab?.wsId === row.ws.id && editingTab?.tabId === row.tab.id"
            v-model="editingTabTitle"
            class="ws-term-rename-input m-0 w-full min-w-0 flex-1 border-0 border-b border-accent bg-transparent p-0 text-[12px] text-foreground outline-none"
            @blur="commitTabRename"
            @keydown.enter.prevent="commitTabRename"
            @keydown.esc.prevent="cancelTabRename"
            @click.stop
          />
          <span
            v-else
            class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-foreground"
            @dblclick.stop="startTabRename(row.ws.id, row.tab)"
          >{{ row.tab.title }}</span>
          <PhX
            :size="10"
            weight="bold"
            class="shrink-0 rounded-sm p-px text-muted-foreground opacity-0 transition-opacity group-hover:opacity-60 hover:!opacity-100 hover:!text-destructive"
            title="Close tab"
            @click.stop="termTabs.close(row.ws.id, row.tab.id)"
          />
        </div>

        <!-- line 3: branch + badges -->
        <div class="mt-[3px] flex items-center gap-1.5 text-[10px] text-muted-foreground">
          <PhGitBranch v-if="row.ws.parent_id" :size="9" class="shrink-0 text-[#a78bfa]" />
          <span class="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap font-mono">{{ branchOf(row.ws) }}</span>
          <span class="ml-auto flex shrink-0 items-center gap-1">
            <span v-if="(row.tab.leafCount ?? 1) > 1" class="rounded bg-white/[0.08] px-1 text-[9px] font-semibold leading-[1.5]" :title="`${row.tab.leafCount} panes`">{{ row.tab.leafCount }}</span>
            <span v-if="row.tab.isAgent && (row.tab.round ?? 0) > 1" class="text-[9px] font-semibold opacity-70" :title="`${row.tab.round} messages sent this session`">↺{{ row.tab.round }}</span>
            <span
              v-if="git.prByWs[row.ws.id]"
              class="flex items-center gap-[3px] rounded-[7px] bg-white/[0.06] px-[5px] py-px pl-1 font-mono text-[9px] font-semibold leading-none"
              :class="prClass(git.prByWs[row.ws.id]!)"
              :title="prTitle(git.prByWs[row.ws.id]!)"
            ><span class="h-1.5 w-1.5 shrink-0 rounded-full bg-current" />#{{ git.prByWs[row.ws.id]!.number }}</span>
            <PhBell v-if="row.tab.status === 'permission'" :size="10" weight="fill" class="text-[#f59e0b]" title="Permission required" />
            <span
              v-if="row.tab.status && row.tab.status !== 'idle'"
              class="status-dot"
              :class="`status-${row.tab.status}`"
              :title="attentionLabel(attentionState(row))"
              role="status"
            >{{ row.tab.status === 'running' ? spinnerFrame : '' }}</span>
          </span>
        </div>
      </div>

      <div v-if="!feed.live.length" class="m-2 rounded-lg border border-dashed border-border/60 px-5 py-7 text-center text-[11.5px] leading-[1.7] text-muted-foreground">
        <template v-if="store.workspaces.length === 0">No projects.<br />Open a folder to start.</template>
        <template v-else-if="active">No threads here yet.<br />Start one above.</template>
        <template v-else>Nothing open.<br />Pick a project up top.</template>
      </div>

      <!-- Settled: tabs of archived projects / worktrees -->
      <template v-if="feed.settled.length">
        <button class="section-header" @click="toggleSection('settled')">
          <PhCaretDown :size="9" weight="bold" class="shrink-0 transition-transform" :class="collapsed.includes('settled') && '-rotate-90'" />
          Settled
          <span class="opacity-60">{{ feed.settled.length }}</span>
        </button>
        <template v-if="!collapsed.includes('settled')">
          <div
            v-for="row in settledVisible"
            :key="rowKey(row)"
            class="flex cursor-pointer items-center gap-1.5 px-2.5 py-[5px] text-muted-foreground opacity-60 transition-opacity hover:bg-hover hover:opacity-100"
            @click="selectTab(row)"
            @contextmenu.prevent.stop="openRowMenu(row.ws, row.tab, $event)"
          >
            <PhChatCenteredText v-if="row.tab.isChat" :size="10" class="shrink-0" />
            <PhRobot v-else-if="row.tab.isAgent" :size="10" class="shrink-0" />
            <PhTerminal v-else :size="10" class="shrink-0" />
            <span class="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px]">{{ row.tab.title }}</span>
            <span v-if="git.prByWs[row.ws.id]" class="shrink-0 font-mono text-[9px]">#{{ git.prByWs[row.ws.id]!.number }}</span>
            <PhMoon v-if="isSnoozed(row.ws.id, row.tab.id)" :size="9" weight="fill" class="shrink-0" title="Snoozed — wakes when the agent moves" />
            <span class="shrink-0 text-[10px] tabular-nums">{{ ago(row.ts) }}</span>
          </div>
          <button
            v-if="feed.settled.length > settledLimit"
            class="flex w-full items-center gap-1.5 px-2.5 py-1.5 text-[11px] text-muted-foreground hover:bg-hover hover:text-foreground"
            @click="settledLimit += 25"
          >
            <PhPlus :size="10" />
            Show {{ feed.settled.length - settledLimit }} more
          </button>
        </template>
      </template>

    </div>

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
      <div class="relative" ref="scriptsEl">
        <button
          class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90"
          :class="[scriptsOpen && 'bg-hover text-foreground', !active && 'opacity-35']"
          :disabled="!active"
          title="Run a script"
          @click.stop="scriptsOpen = !scriptsOpen"
        ><PhPlayCircle :size="14" /></button>
      </div>
      <button
        class="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground active:scale-90"
        :disabled="!active"
        :class="!active && 'opacity-35'"
        title="Open browser tab"
        @click="emit('open-browser')"
      ><PhGlobe :size="14" /></button>
    </div>
  </aside>

  <!-- Dialogs teleported to body to escape backdrop-filter stacking context -->
  <Teleport to="body">
    <!-- Scripts popover -->
    <div
      v-if="scriptsOpen"
      class="fixed z-[1000] min-w-[240px] max-w-[420px] rounded-lg border border-border bg-panel p-1.5 shadow-[0_14px_36px_rgba(0,0,0,0.55)]"
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
    <!-- Row context menu -->
    <div
      v-if="rowMenu"
      class="fixed z-[1000] w-[196px] rounded-lg border border-border bg-panel p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]"
      :style="rowMenuStyle"
      @click.stop
    >
      <template v-if="rowMenu.tab">
        <button class="menu-item" @click="withMenu((ws, tab) => startTabRename(ws.id, tab!))">Rename tab…</button>
        <button class="menu-item" @click="withMenu((ws, tab) => toggleSnooze(ws.id, tab!.id))">
          {{ isSnoozed(rowMenu.ws.id, rowMenu.tab?.id ?? -1) ? "Wake" : "Snooze" }}
        </button>
        <button class="menu-item" @click="withMenu((ws, tab) => termTabs.close(ws.id, tab!.id))">Close tab</button>
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
        {{ isArchived(rowMenu.ws.id) ? "Unarchive" : "Archive" }}
      </button>
      <button v-if="isOpened(rowMenu.ws.id)" class="menu-item" @click="withMenu((ws) => store.closeWorkspace(ws.id))">Close project</button>
      <div class="menu-sep" />
      <button class="menu-item !text-destructive hover:!bg-destructive/10" @click="withMenu((ws) => (deleteTarget = ws))">Delete…</button>
    </div>

    <!-- Delete confirm — destructive: a worktree delete removes files from disk -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="deleteTarget" @click.self="deleteTarget = null">
      <div class="flex w-[420px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
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
          <button class="flex items-center gap-[5px] rounded-md border-0 bg-destructive px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:cursor-default disabled:opacity-50" @click="confirmDelete" :disabled="deleteBusy">
            {{ deleteBusy ? "Deleting…" : "Delete" }}
          </button>
        </div>
      </div>
    </div>

    <!-- Rename dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="renameId !== null" @click.self="renameId = null">
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
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
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
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
      <div class="flex w-[400px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
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
        <input
          v-model="wtBase"
          class="w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="defaults to current HEAD"
          list="wt-base-branches"
          spellcheck="false"
          @keydown.enter="confirmWorktree"
          @keydown.esc="closeWtDialog"
        />
        <datalist id="wt-base-branches">
          <option v-for="b in wtBaseList" :key="b" :value="b" />
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
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted, watch } from "vue";
import {
  PhFolder,
  PhFolderPlus,
  PhCaretDown,
  PhX,
  PhTerminal,
  PhRobot,
  PhBell,
  PhPlus,
  PhGitBranch,
  PhChatCenteredText,
  PhMoon,
  PhGear,
  PhSquaresFour,
  PhPlayCircle,
  PhGlobe,
} from "@phosphor-icons/vue";
import { open as openDialog } from "@tauri-apps/plugin-dialog";
import { invoke } from "@tauri-apps/api/core";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useTerminalTabsStore, type TabSummary } from "@/stores/terminalTabs";
import { useUIStore } from "@/stores/ui";
import { spinnerFrame } from "@/lib/spinner";
import {
  getAgentAttentionState,
  type AgentAttentionState,
  type TermStatus,
} from "@/lib/terminalStatus";
import { useGitStore, type PrInfo } from "@/stores/git";
import { isPinned, togglePin, unpin } from "@/lib/pinnedWorkspaces";
import { archivedIds, isArchived, toggleArchived, forgetArchived } from "@/lib/archivedWorkspaces";
import { buildActivityRows, type ActivityRow } from "@/lib/sidebarGroups";
import { snoozedKeys, isSnoozed, toggleSnooze } from "@/lib/snoozedTabs";
import { getProjectSettings } from "@/lib/projectSettings";

import { useScriptsStore, type Script } from "@/stores/scripts";

const emit = defineEmits<{ (e: "open-browser"): void }>();

const store = useWorkspaceStore();
const termTabs = useTerminalTabsStore();
const ui = useUIStore();
const git = useGitStore();
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

const feed = computed(() =>
  buildActivityRows({
    openedWorkspaces: store.opened,
    tabsByWs: termTabs.tabsByWs,
    activityAt: termTabs.activityAt,
    archivedIds: archivedIds.value,
    snoozedKeys: snoozedKeys.value,
    filterProjectId: filterProjectId.value,
  }),
);

const settledLimit = ref(12);
const settledVisible = computed(() => feed.value.settled.slice(0, settledLimit.value));

type SectionKey = "settled";
const collapsed = ref<SectionKey[]>(["settled"]);
function toggleSection(key: SectionKey) {
  collapsed.value = collapsed.value.includes(key)
    ? collapsed.value.filter((k) => k !== key)
    : [...collapsed.value, key];
}

function rowKey(row: ActivityRow): string {
  return `${row.ws.id}:${row.tab.id}`;
}
function isActiveRow(row: ActivityRow): boolean {
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
    case "permission": return "text-[#f59e0b]";
    case "waiting": return "text-[#60a5fa]";
    case "error": return "text-destructive";
    case "review": return "text-[#4ade80]";
    default: return "text-muted-foreground";
  }
}

function attentionState(row: ActivityRow): AgentAttentionState {
  return getAgentAttentionState(row.tab.status, termTabs.isCompletionUnseen(row.ws.id, row.tab.id));
}

function attentionLabel(state: AgentAttentionState): string {
  switch (state) {
    case "error": return "Error";
    case "needs-input": return "Needs input";
    case "done-unread": return "Done unread";
    case "working": return "Working";
    default: return "Idle";
  }
}

function prClass(info: PrInfo): string {
  if (info.checks === "fail") return "text-[#f87171] bg-[color-mix(in_srgb,#f87171_14%,transparent)]";
  if (info.checks === "pending") return "text-[#fbbf24] bg-[color-mix(in_srgb,#fbbf24_14%,transparent)]";
  if (info.state === "MERGED") return "text-[#a78bfa] bg-[color-mix(in_srgb,#a78bfa_14%,transparent)]";
  if (info.state === "CLOSED") return "text-[#f87171] bg-[color-mix(in_srgb,#f87171_12%,transparent)]";
  if (info.isDraft) return "text-[#9ca3af]";
  return "text-[#4ade80] bg-[color-mix(in_srgb,#4ade80_12%,transparent)]";
}
function prTitle(info: PrInfo): string {
  const state = info.isDraft && info.state === "OPEN" ? "draft" : info.state.toLowerCase();
  const checks = info.checks === "none" ? "" : ` · checks ${info.checks}`;
  return `PR #${info.number} (${state})${checks}`;
}

// ── selection ────────────────────────────────────────────────────────────────
/**
 * Picking a project in the filter both narrows the feed and opens that repo on
 * the composer — the sidebar no longer lists projects, so this is how a repo
 * with no threads gets mounted at all.
 */
function pickProject(repo: Workspace) {
  filterProjectId.value = repo.id;
  filterOpen.value = false;
  selectWs(repo);
  ui.openWelcome();
}

function selectWs(ws: Workspace) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  store.open(ws);
  // Keep the repo's worktrees mounted so their rows stay in the feed.
  const parent = ws.parent_id ?? ws.id;
  for (const wt of store.worktreesByParent[parent] || []) store.ensureOpen(wt);
}

function selectTab(row: ActivityRow) {
  if (ui.mode !== "terminal") ui.setMode("terminal");
  ui.closeWelcome();
  if (store.active?.id !== row.ws.id) store.open(row.ws);
  nextTick(() => termTabs.activate(row.ws.id, row.tab.id));
}

// "New chat" opens the project picker (⌘⇧O's Spotlight mode) rather than a
// chat tab directly — same command-palette → Welcome composer flow either way.
function newChatSession(_ws: Workspace) {
  ui.pickProjectThenWelcome();
}

function openGitTab(ws: Workspace) {
  const wasOpen = isOpened(ws.id);
  selectWs(ws);
  const open = () => termTabs.openGit(ws.id);
  wasOpen ? open() : nextTick(open);
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
function openRowMenu(ws: Workspace, tab: TabSummary | null, e: MouseEvent) {
  rowMenu.value = { ws, tab, x: e.clientX, y: e.clientY };
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

function onDocClick() { filterOpen.value = false; rowMenu.value = null; scriptsOpen.value = false; }

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

onMounted(() => {
  document.addEventListener("click", onDocClick);
  mountSections();
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
  document.removeEventListener("click", onDocClick);
});

// Watch only the STRUCTURE of the workspace set (its id list), not every nested
// property — a deep watch re-ran the mount sweep on any tab/PR mutation.
watch(() => store.workspaces.map((ws) => ws.id).join(","), () => { mountSections(); refreshAllPrs(); });
watch(() => active.value?.id, mountSections);

// ── branch helpers (worktree dialog) ─────────────────────────────────────────
interface GitOutput { stdout: string; stderr: string; code: number; }

async function listBranches(path: string): Promise<string[]> {
  if (git.cwd === path && git.branches.length > 0) return git.branches;
  try {
    const out = await invoke<GitOutput>("run_git", { cwd: path, args: ["branch", "--list"] });
    if (out.code === 0) {
      return out.stdout.split("\n").map((l) => l.replace(/^\*?\s+/, "").trim()).filter(Boolean);
    }
  } catch {}
  return [];
}

// ── new worktree dialog ──────────────────────────────────────────────────────
const wtParent = ref<Workspace | null>(null);
const wtBranch = ref("");
const wtBase = ref("");
const wtBaseList = ref<string[]>([]);
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
  await nextTick();
  wtBranchEl.value?.focus();
  wtBaseList.value = await listBranches(parent.path);
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
  const selected = await openDialog({ directory: true, multiple: false });
  if (!selected || typeof selected !== "string") return;
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
</style>
