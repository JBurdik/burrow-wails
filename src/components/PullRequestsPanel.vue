<script setup lang="ts">
import { computed, watch } from "vue";
import { PhArrowClockwise, PhArrowSquareOut, PhCheck, PhGitPullRequest, PhGitMerge, PhMagnifyingGlass, PhPlus, PhSealWarning, PhX } from "@phosphor-icons/vue";
import { usePullRequests, type PrScope } from "@/composables/usePullRequests";
import { cn } from "@/lib/utils";

const props = defineProps<{ cwd: string }>();
const pr = usePullRequests(() => props.cwd);
const scopes: Array<{ id: PrScope; label: string }> = [{ id: "assigned", label: "Assigned" }, { id: "created", label: "Created" }, { id: "all", label: "All" }];
const stateLabel = computed(() => pr.selected.value?.isDraft ? "Draft" : pr.selected.value?.state === "MERGED" ? "Merged" : "Open");
const checkCount = computed(() => pr.selected.value?.statusCheckRollup?.length ?? 0);
const comment = defineModel<string>("comment", { default: "" });

watch(() => props.cwd, () => { pr.selected.value = null; pr.refresh(); }, { immediate: true });
watch(pr.scope, () => pr.refresh());
function postComment() { const body = comment.value.trim(); if (!body || !pr.selected.value) return; comment.value = ""; pr.act(["pr", "comment", pr.selected.value.url, "--body", body]); }
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <template v-if="!pr.selected.value">
      <header class="flex items-center justify-between border-b border-border p-2">
        <span class="flex items-center gap-1.5 font-semibold text-foreground"><PhGitPullRequest :size="14" /> Pull requests</span>
        <span class="flex items-center gap-1.5">
          <button class="inline-flex p-0.5 text-muted-foreground hover:rounded hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="pr.loading.value" title="Refresh" @click="pr.refresh">
            <PhArrowClockwise :size="13" :class="{ 'animate-spin': pr.loading.value }" />
          </button>
          <button class="inline-flex p-0.5 text-muted-foreground hover:rounded hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="pr.actionLoading.value" title="Create pull request" @click="pr.create">
            <PhPlus :size="15" />
          </button>
        </span>
      </header>
      <div class="flex gap-1 border-b border-border p-1.5">
        <button
          v-for="item in scopes"
          :key="item.id"
          :class="cn('rounded px-1.5 py-1 text-muted-foreground', pr.scope.value === item.id && 'bg-accent/18 text-foreground')"
          @click="pr.scope.value = item.id"
        >{{ item.label }}</button>
      </div>
      <label class="flex items-center gap-1 border-b border-border px-2 py-1.5 text-muted-foreground">
        <PhMagnifyingGlass :size="12" />
        <input v-model="pr.query.value" placeholder="Search pull requests" class="min-w-0 flex-1 border-0 bg-transparent text-foreground outline-none" />
      </label>
      <p v-if="pr.error.value" class="flex gap-1.5 p-4 text-center text-destructive"><PhSealWarning :size="13" />{{ pr.error.value }}</p>
      <div v-if="pr.loading.value" class="p-4 text-center leading-relaxed text-muted-foreground">Načítám pull requesty…</div>
      <div v-else-if="!props.cwd" class="p-4 text-center leading-relaxed text-muted-foreground">Otevři Git workspace.</div>
      <div v-else-if="pr.items.value.length === 0" class="p-4 text-center leading-relaxed text-muted-foreground">Žádné pull requesty. Přihlas se přes <code>gh auth login</code>.</div>
      <button
        v-for="item in pr.items.value"
        v-else
        :key="item.url"
        class="flex flex-col gap-0.5 border-b border-border/65 p-2 text-left hover:bg-hover"
        @click="pr.select(item)"
      >
        <span class="flex items-center justify-between">
          <span class="font-mono text-accent">#{{ item.number }}</span>
          <span class="text-[10px]" :class="item.isDraft ? 'text-warning' : 'text-success'">{{ item.isDraft ? 'Draft' : item.reviewDecision || 'Open' }}</span>
        </span>
        <span class="font-semibold leading-snug text-foreground">{{ item.title }}</span>
        <span v-if="item.headRefName" class="truncate text-muted-foreground">{{ item.headRefName }}</span>
      </button>
    </template>

    <template v-else>
      <header class="flex items-center justify-between border-b border-border p-2">
        <button class="inline-flex p-0.5 text-muted-foreground hover:rounded hover:bg-hover hover:text-foreground" @click="pr.selected.value = null">← Pull requests</button>
        <button class="inline-flex p-0.5 text-muted-foreground hover:rounded hover:bg-hover hover:text-foreground" title="Open on GitHub" @click="pr.act(['pr', 'view', pr.selected.value.url, '--web'])"><PhArrowSquareOut :size="13" /></button>
      </header>
      <div class="border-b border-border p-2.5">
        <div class="flex justify-between">
          <span class="font-mono text-accent">#{{ pr.selected.value.number }}</span>
          <span class="text-[10px]" :class="stateLabel === 'Draft' ? 'text-warning' : 'text-success'">{{ stateLabel }}</span>
        </div>
        <h2 class="my-1.5 text-[13px] leading-snug text-foreground">{{ pr.selected.value.title }}</h2>
        <p class="truncate text-muted-foreground">{{ pr.selected.value.author?.login || 'Unknown' }} · {{ pr.selected.value.headRefName }} → {{ pr.selected.value.baseRefName }}</p>
      </div>
      <div class="flex border-b border-border">
        <button
          v-for="tab in [{ id: 'summary', label: 'Summary' }, { id: 'timeline', label: 'Timeline' }, { id: 'code', label: 'Code' }]"
          :key="tab.id"
          :class="cn('flex-1 border-b-2 border-transparent px-1.5 py-1 text-muted-foreground', pr.activeTab.value === tab.id && 'border-accent text-foreground')"
          @click="pr.activeTab.value = tab.id as any"
        >{{ tab.label }}</button>
      </div>
      <p v-if="pr.error.value" class="flex gap-1.5 p-4 text-center text-destructive"><PhSealWarning :size="13" />{{ pr.error.value }}</p>
      <div class="flex-1 overflow-auto">
        <template v-if="pr.activeTab.value === 'summary'">
          <div class="grid grid-cols-3 gap-1 p-2 text-muted-foreground">
            <span class="flex flex-col gap-0.5">Reviewers <b class="font-medium text-secondary-foreground">{{ pr.selected.value.reviewDecision || 'None' }}</b></span>
            <span class="flex flex-col gap-0.5">Comments <b class="font-medium text-secondary-foreground">{{ pr.selected.value.comments?.length || 0 }}</b></span>
            <span class="flex flex-col gap-0.5">Checks <b class="font-medium text-secondary-foreground">{{ checkCount }}</b></span>
          </div>
          <section class="p-2.5">
            <h3 class="mb-2 text-foreground">Description</h3>
            <p class="whitespace-pre-wrap leading-relaxed">{{ pr.selected.value.body || 'No description provided.' }}</p>
          </section>
        </template>
        <template v-else-if="pr.activeTab.value === 'timeline'">
          <section class="p-2.5">
            <h3 class="mb-2 text-foreground">Comments</h3>
            <p v-if="!pr.selected.value.comments?.length" class="text-muted-foreground">No comments yet.</p>
            <article v-for="entry in pr.selected.value.comments" :key="entry.createdAt + entry.body" class="border-t border-border/70 py-1.5">
              <b>{{ entry.author?.login || 'Unknown' }}</b>
              <p class="leading-relaxed">{{ entry.body }}</p>
            </article>
          </section>
        </template>
        <template v-else>
          <section class="p-2.5">
            <h3 class="mb-2 text-foreground">Files · {{ pr.selected.value.files?.length || 0 }}</h3>
            <div v-for="file in pr.selected.value.files" :key="file.path" class="flex items-center justify-between gap-2 py-1 font-mono">
              <span class="truncate">{{ file.path }}</span>
              <span><i class="not-italic text-success">+{{ file.additions }}</i> <em class="not-italic text-destructive">-{{ file.deletions }}</em></span>
            </div>
          </section>
        </template>
        <section class="p-2.5">
          <h3 class="mb-2 text-foreground">Checks · {{ checkCount }}</h3>
          <p v-if="!checkCount" class="text-muted-foreground">No checks reported.</p>
          <div v-for="check in pr.selected.value.statusCheckRollup" :key="check.name" class="flex items-center gap-1.5 py-0.5">
            <PhCheck v-if="check.conclusion === 'SUCCESS'" :size="12" />
            <PhX v-else :size="12" />
            <span>{{ check.name || 'Check' }}</span>
            <small class="ml-auto text-muted-foreground">{{ check.conclusion || check.status }}</small>
          </div>
        </section>
        <section class="p-2.5">
          <h3 class="mb-2 text-foreground">Leave a comment</h3>
          <textarea v-model="comment" placeholder="Leave a comment" rows="4" class="box-border w-full resize-y rounded border border-border bg-panel p-1.5 text-foreground outline-none" />
          <button class="mt-1.5 rounded border border-border px-1.5 py-1 text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="!comment.trim() || pr.actionLoading.value" @click="postComment">Comment</button>
        </section>
      </div>
      <footer class="flex gap-1.5 border-t border-border p-1.5">
        <button class="rounded border border-border px-1.5 py-1 text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="pr.actionLoading.value" @click="pr.act(['pr', 'review', pr.selected.value.url, '--approve'])">Approve</button>
        <button class="rounded border border-border px-1.5 py-1 text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-40" :disabled="pr.actionLoading.value" @click="pr.act(['pr', 'review', pr.selected.value.url, '--request-changes'])">Request changes</button>
        <button
          class="ml-auto flex items-center gap-1 rounded border border-border px-1.5 py-1 text-success hover:bg-hover disabled:opacity-40"
          :disabled="pr.actionLoading.value || pr.selected.value.isDraft"
          @click="pr.act(['pr', 'merge', pr.selected.value.url, '--merge', '--delete-branch'])"
        ><PhGitMerge :size="13" /> Merge</button>
      </footer>
    </template>
  </section>
</template>
