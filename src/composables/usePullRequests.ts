import { computed, ref, shallowRef } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";

export type PrScope = "assigned" | "created" | "all";
export type PrTab = "summary" | "timeline" | "code";

interface GhOutput { stdout: string; stderr: string; code: number }
export interface PullRequest {
  number: number; title: string; body: string; url: string; state: string; isDraft: boolean;
  headRefName: string; baseRefName: string; updatedAt: string; additions: number; deletions: number;
  author?: { login: string } | null;
  reviewDecision?: string | null; mergeStateStatus?: string | null;
  statusCheckRollup?: Array<{ name?: string; status?: string; conclusion?: string }>;
  comments?: Array<{ author?: { login: string }; body: string; createdAt: string }>;
  files?: Array<{ path: string; additions: number; deletions: number }>;
}

async function gh(cwd: string, args: string[]) {
  const out = await invoke<GhOutput>("run_gh", { cwd, args });
  if (out.code !== 0) throw new Error(out.stderr.trim() || "GitHub CLI request failed");
  return out.stdout;
}

// `gh pr list`/`gh pr view` don't expose a "repository" field (unlike `gh search prs`) —
// they're already scoped to the repo at cwd, so the list is implicitly single-repo.
// Fetch the richer branch/review/check data only after the user opens a row.
const listFields = "number,title,url,state,isDraft,author,headRefName,updatedAt";
const detailFields = `${listFields},body,baseRefName,additions,deletions,statusCheckRollup,comments,files`;

export function usePullRequests(cwd: () => string) {
  const scope = shallowRef<PrScope>("assigned");
  const query = shallowRef("");
  const selected = ref<PullRequest | null>(null);
  const items = ref<PullRequest[]>([]);
  const loading = shallowRef(false);
  const actionLoading = shallowRef(false);
  const error = shallowRef("");
  const activeTab = shallowRef<PrTab>("summary");

  const visibleItems = computed(() => {
    const needle = query.value.trim().toLowerCase();
    if (!needle) return items.value;
    return items.value.filter((pr) => `${pr.number} ${pr.title} ${pr.headRefName}`.toLowerCase().includes(needle));
  });

  async function refresh() {
    if (!cwd()) return;
    loading.value = true; error.value = "";
    try {
      // `gh pr list` (unlike `search prs`) is scoped to the repo at cwd.
      const args = ["pr", "list", "--state", "open", "--json", listFields, "--limit", "100"];
      if (scope.value === "assigned") args.push("--assignee", "@me");
      if (scope.value === "created") args.push("--author", "@me");
      items.value = JSON.parse(await gh(cwd(), args)) as PullRequest[];
    } catch (e) { error.value = e instanceof Error ? e.message : "Nelze načíst pull requesty."; items.value = []; }
    finally { loading.value = false; }
  }

  async function select(pr: PullRequest) {
    selected.value = pr; activeTab.value = "summary"; actionLoading.value = true; error.value = "";
    try { selected.value = JSON.parse(await gh(cwd(), ["pr", "view", pr.url, "--json", detailFields])) as PullRequest; }
    catch (e) { error.value = e instanceof Error ? e.message : "Nelze načíst detail PR."; }
    finally { actionLoading.value = false; }
  }

  async function act(args: string[]) {
    if (!selected.value) return;
    actionLoading.value = true; error.value = "";
    try { await gh(cwd(), args); await select(selected.value); await refresh(); }
    catch (e) { error.value = e instanceof Error ? e.message : "Akci se nepodařilo dokončit."; }
    finally { actionLoading.value = false; }
  }

  // The title and body a model writes from the branch's commits and diff, or
  // null so the caller falls back to `--fill` (which just reuses the last
  // commit message). Best-effort by design: a missing model, an offline CLI or
  // a slow answer must not stop the user from opening a PR.
  async function generatedContent(): Promise<{ title: string; body: string } | null> {
    const ui = useUIStore();
    try {
      const head = (await invoke<GhOutput>("run_git", { cwd: cwd(), args: ["branch", "--show-current"] })).stdout.trim();
      const base = JSON.parse(await gh(cwd(), ["repo", "view", "--json", "defaultBranchRef"])).defaultBranchRef?.name;
      if (!head || !base || head === base) return null;
      const out = await invoke<Record<string, string>>("generate_pr_content", {
        cwd: cwd(),
        model: ui.textGenerationModel,
        policy: ui.textGenerationPolicy,
        baseBranch: base,
        headBranch: head,
      });
      return out.title ? { title: out.title, body: out.body ?? "" } : null;
    } catch {
      return null;
    }
  }

  async function create() {
    actionLoading.value = true; error.value = "";
    try {
      const content = await generatedContent();
      const args = content
        ? ["pr", "create", "--title", content.title, "--body", content.body]
        : ["pr", "create", "--fill"];
      const url = (await gh(cwd(), args)).trim();
      await refresh();
      const created = items.value.find((pr) => pr.url === url);
      if (created) await select(created);
    } catch (e) { error.value = e instanceof Error ? e.message : "Nelze vytvořit PR."; }
    finally { actionLoading.value = false; }
  }

  return { scope, query, selected, items: visibleItems, loading, actionLoading, error, activeTab, refresh, select, act, create };
}
