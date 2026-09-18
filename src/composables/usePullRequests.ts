import { computed, ref, shallowRef } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";

export type PrScope = "assigned" | "created" | "all";
export type PrTab = "summary" | "timeline" | "code";

export interface PrCheck { name: string; status?: string; conclusion?: string }
export interface PrFile { path: string; additions: number; deletions: number }
export interface PrComment { author?: string; body: string; createdAt?: string }

// The shape the Go forge package normalizes every provider onto. Optional
// fields are the capability model: a provider that cannot supply checks leaves
// them out and the panel hides that section, with no flag to keep in sync.
export interface PullRequest {
  number: number; title: string; body?: string; url: string;
  state: string; isDraft: boolean;
  headRefName: string; baseRefName: string;
  author?: string; updatedAt?: string;
  additions?: number; deletions?: number;
  checks?: PrCheck[];
  reviewDecision?: string;
  files?: PrFile[];
  comments?: PrComment[];
}

export interface ForgeStatus {
  provider: string; installed: boolean; authed: boolean;
  bin?: string; installCmd?: string; authCmd?: string; docsUrl?: string;
}

function msg(e: unknown, fallback: string) {
  return e instanceof Error && e.message ? e.message : fallback;
}

export function usePullRequests(cwd: () => string) {
  const scope = shallowRef<PrScope>("assigned");
  const query = shallowRef("");
  const selected = ref<PullRequest | null>(null);
  const items = ref<PullRequest[]>([]);
  const loading = shallowRef(false);
  const actionLoading = shallowRef(false);
  const error = shallowRef("");
  const activeTab = shallowRef<PrTab>("summary");
  const forge = ref<ForgeStatus | null>(null);

  const visibleItems = computed(() => {
    const needle = query.value.trim().toLowerCase();
    if (!needle) return items.value;
    return items.value.filter((pr) => `${pr.number} ${pr.title} ${pr.headRefName}`.toLowerCase().includes(needle));
  });

  // One call tells the panel which provider this repo is on and whether its CLI
  // is installed and logged in — so the empty state names the right command
  // instead of always saying `gh auth login`.
  async function loadForge() {
    if (!cwd()) return;
    try { forge.value = await invoke<ForgeStatus>("forge_info", { cwd: cwd() }); }
    catch { forge.value = null; }
  }

  async function refresh() {
    if (!cwd()) return;
    loading.value = true; error.value = "";
    try {
      items.value = await invoke<PullRequest[]>("forge_pr_list", {
        cwd: cwd(),
        scope: scope.value === "all" ? "" : scope.value,
        state: "open",
      });
    } catch (e) { error.value = msg(e, "Nelze načíst pull requesty."); items.value = []; }
    finally { loading.value = false; }
  }

  async function select(pr: PullRequest) {
    selected.value = pr; activeTab.value = "summary"; actionLoading.value = true; error.value = "";
    try { selected.value = await invoke<PullRequest>("forge_pr_view", { cwd: cwd(), number: pr.number }); }
    catch (e) { error.value = msg(e, "Nelze načíst detail PR."); }
    finally { actionLoading.value = false; }
  }

  async function merge(squash: boolean) {
    if (!selected.value) return;
    actionLoading.value = true; error.value = "";
    try {
      await invoke("forge_pr_merge", { cwd: cwd(), number: selected.value.number, squash });
      await select(selected.value); await refresh();
    } catch (e) { error.value = msg(e, "Akci se nepodařilo dokončit."); }
    finally { actionLoading.value = false; }
  }

  // The title and body a model writes from the branch's commits and diff, or
  // null so the caller falls back to the last commit message. Best-effort by
  // design: a missing model or a slow answer must not stop the user from
  // opening a PR.
  async function generatedContent(): Promise<{ title: string; body: string } | null> {
    const ui = useUIStore();
    try {
      const head = (await invoke<{ stdout: string }>("run_git", { cwd: cwd(), args: ["branch", "--show-current"] })).stdout.trim();
      const base = (await invoke<{ stdout: string }>("run_git", {
        cwd: cwd(), args: ["symbolic-ref", "--short", "refs/remotes/origin/HEAD"],
      })).stdout.trim().replace(/^origin\//, "");
      if (!head || !base || head === base) return null;
      const out = await invoke<Record<string, string>>("generate_pr_content", {
        cwd: cwd(),
        model: ui.textGenerationModel,
        policy: ui.textGenerationPolicy,
        rules: ui.textGenerationRules,
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
      const created = await invoke<PullRequest>("forge_pr_create", {
        cwd: cwd(),
        title: content?.title ?? "",
        body: content?.body ?? "",
        base: "",
        head: "",
      });
      await refresh();
      if (created?.number) await select(created);
    } catch (e) { error.value = msg(e, "Nelze vytvořit PR."); }
    finally { actionLoading.value = false; }
  }

  return {
    scope, query, selected, items: visibleItems, loading, actionLoading, error, activeTab,
    forge, loadForge, refresh, select, merge, create,
  };
}
