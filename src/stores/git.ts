import { defineStore } from "pinia";
import { ref, computed } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "./ui";
import { useNotificationsStore } from "./notifications";

export interface GitFile {
  path: string;
  x: string;
  y: string;
}

export interface GitFileStat {
  path: string;
  insertions: number;
  deletions: number;
  binary: boolean;
}

export interface GitCommit {
  hash: string;
  shortHash: string;
  subject: string;
  author: string;
  relTime: string;
}

interface GitOutput {
  stdout: string;
  stderr: string;
  code: number;
}

async function runGit(cwd: string, args: string[]): Promise<string> {
  const out = await invoke<GitOutput>("run_git", { cwd, args });
  if (out.code !== 0) throw new Error(out.stderr || "git error");
  return out.stdout;
}

// ── Pull-request status (via gh CLI) ─────────────────────────────────────────
export type PrChecks = "pass" | "fail" | "pending" | "none";

export interface PrInfo {
  number: number;
  state: string; // OPEN | MERGED | CLOSED
  isDraft: boolean;
  checks: PrChecks;
  url: string;
}

// Collapse gh's statusCheckRollup array into a single CI verdict. Each entry is
// either a CheckRun (status/conclusion) or a StatusContext (state).
function rollupChecks(rollup: unknown): PrChecks {
  if (!Array.isArray(rollup) || rollup.length === 0) return "none";
  let pending = false;
  for (const c of rollup as Array<Record<string, string>>) {
    const conclusion = (c.conclusion || "").toUpperCase();
    const state = (c.state || "").toUpperCase();
    const status = (c.status || "").toUpperCase();
    if (["FAILURE", "TIMED_OUT", "CANCELLED", "ERROR", "ACTION_REQUIRED"].includes(conclusion)
      || ["FAILURE", "ERROR"].includes(state)) {
      return "fail";
    }
    if ((status && status !== "COMPLETED") || state === "PENDING" || state === "EXPECTED") {
      pending = true;
    }
  }
  return pending ? "pending" : "pass";
}

function parseStatus(raw: string): { staged: GitFile[]; unstaged: GitFile[]; untracked: GitFile[] } {
  const staged: GitFile[] = [];
  const unstaged: GitFile[] = [];
  const untracked: GitFile[] = [];

  for (const line of raw.split("\n")) {
    if (line.length < 3) continue;
    const x = line[0];
    const y = line[1];
    const rawPath = line.slice(3);
    const path = rawPath.includes(" -> ") ? rawPath.split(" -> ")[1] : rawPath;
    const file: GitFile = { path, x, y };

    if (x === "?" && y === "?") {
      untracked.push(file);
    } else {
      if (x !== " " && x !== "?") staged.push(file);
      if (y !== " " && y !== "?") unstaged.push(file);
    }
  }

  return { staged, unstaged, untracked };
}

export const useGitStore = defineStore("git", () => {
  const notif = useNotificationsStore();
  const cwd = ref("");
  const branch = ref("");
  const staged = ref<GitFile[]>([]);
  const unstaged = ref<GitFile[]>([]);
  const untracked = ref<GitFile[]>([]);
  const diff = ref("");
  const diffFile = ref<string | null>(null);
  const diffStaged = ref(false);
  const loading = ref(false);
  const error = ref<string | null>(null);
  const commitMsg = ref("");
  const ahead = ref(0);
  const behind = ref(0);
  const hasUpstream = ref(false);
  const pushing = ref(false);
  const pulling = ref(false);
  const generating = ref(false);
  const generateError = ref<string | null>(null);
  const committing = ref(false);
  const log = ref<GitCommit[]>([]);
  const logLoading = ref(false);
  const branches = ref<string[]>([]);
  const fetching = ref(false);

  // PR status cache, keyed by workspace id. null = checked, no open PR (or gh
  // missing/unauthed). undefined (absent key) = never checked.
  const prByWs = ref<Record<number, PrInfo | null>>({});

  // One store, one cwd, but a workspace switch can land in the middle of any
  // git op — every call here is async and `refresh` alone runs four of them.
  // So each op PINS the directory it started in and stamps this counter, which
  // setCwd bumps; a result that comes back for a directory the app has since
  // left is dropped instead of written over the new workspace's state. Without
  // it, workspace A's slow status landed after B's and the titlebar's
  // Commit & push button acted on the wrong project — armed with A's changes
  // in B, or (worse) finding "nothing staged" in A and silently doing nothing.
  let gen = 0;
  const stale = (g: number) => g !== gen;
  // Per-workspace in-flight guard so the 60s poll never stacks gh calls.
  const prInFlight = new Set<number>();
  // Current branch per workspace, filled by the same 60s sweep as the PR badges.
  // The single `branch` ref above only tracks the ACTIVE cwd; the sidebar lists
  // every workspace at once and needs one branch each.
  const branchByWs = ref<Record<number, string>>({});

  // Fetch PR status for one workspace via `gh pr view`. Never throws — any
  // failure (no gh, not authed, no PR, not a GitHub repo) caches null so the
  // Sidebar simply shows no badge. Cheap + non-blocking; safe to call on a poll.
  // The branch read used to live inside fetchPr, which meant a chip showing
  // `branchByWs` only ever got a value once the gh PR sweep ran — deferred
  // 2.5s off the startup path, then every 60s. Until then AgentChat's header
  // fell back to the literal "HEAD". Reading a ref is a local git call with
  // none of gh's cost, so it is its own thing and callers can ask for it eagerly.
  async function ensureBranch(wsId: number, cwd: string) {
    if (!cwd) return;
    const head = await invoke<GitOutput>("run_git", { cwd, args: ["branch", "--show-current"] }).catch(() => null);
    if (head?.code === 0) branchByWs.value[wsId] = head.stdout.trim();
  }

  async function fetchPr(wsId: number, cwd: string) {
    if (!cwd || prInFlight.has(wsId)) return;
    prInFlight.add(wsId);
    try {
      await ensureBranch(wsId, cwd);
      const out = await invoke<GitOutput>("run_gh", {
        cwd,
        args: ["pr", "view", "--json", "number,state,isDraft,statusCheckRollup,url"],
      });
      if (out.code !== 0) {
        prByWs.value[wsId] = null;
        return;
      }
      const j = JSON.parse(out.stdout) as {
        number: number; state: string; isDraft: boolean;
        statusCheckRollup?: unknown; url: string;
      };
      prByWs.value[wsId] = {
        number: j.number,
        state: j.state,
        isDraft: j.isDraft,
        checks: rollupChecks(j.statusCheckRollup),
        url: j.url,
      };
    } catch {
      prByWs.value[wsId] = null;
    } finally {
      prInFlight.delete(wsId);
    }
  }

  // Batch PR refresh with a small concurrency cap. prInFlight dedupes; this pool
  // bounds how many gh subprocesses run at once so a 10-workspace sweep can't fire
  // 10 blocking gh calls in parallel (the startup gray-screen saturation). 3 in
  // flight keeps the badges fresh without flooding the command workers.
  const PR_POOL = 3;
  async function fetchPrs(items: Array<{ wsId: number; cwd: string }>) {
    const queue = items.filter((it) => it.cwd && !prInFlight.has(it.wsId));
    let i = 0;
    const worker = async () => {
      while (i < queue.length) {
        const it = queue[i++];
        await fetchPr(it.wsId, it.cwd);
      }
    };
    await Promise.all(Array.from({ length: Math.min(PR_POOL, queue.length) }, worker));
  }

  async function refresh(silent = false) {
    const dir = cwd.value;
    const g = gen;
    if (!dir) return;
    if (!silent) loading.value = true;
    error.value = null;
    try {
      const [statusOut, branchOut] = await Promise.all([
        runGit(dir, ["status", "--porcelain"]),
        runGit(dir, ["branch", "--show-current"]),
      ]);
      if (stale(g)) return;
      const parsed = parseStatus(statusOut);
      staged.value = parsed.staged;
      unstaged.value = parsed.unstaged;
      untracked.value = parsed.untracked;
      branch.value = branchOut.trim();
      await refreshUpstream(dir, g);
      await refreshLog(dir, g);
      await fetchBranches(dir, g);
    } catch (e: unknown) {
      if (stale(g)) return;
      error.value = e instanceof Error ? e.message : "git error";
      staged.value = [];
      unstaged.value = [];
      untracked.value = [];
      branch.value = "";
      ahead.value = 0;
      behind.value = 0;
      hasUpstream.value = false;
      log.value = [];
    } finally {
      if (!silent && !stale(g)) loading.value = false;
    }
  }

  async function refreshUpstream(dir = cwd.value, g = gen) {
    try {
      // counts: "<behind>\t<ahead>" relative to upstream
      const out = await runGit(dir, [
        "rev-list", "--left-right", "--count", "@{upstream}...HEAD",
      ]);
      if (stale(g)) return;
      const [b, a] = out.trim().split(/\s+/);
      behind.value = parseInt(b, 10) || 0;
      ahead.value = parseInt(a, 10) || 0;
      hasUpstream.value = true;
    } catch {
      if (stale(g)) return;
      // no upstream configured
      ahead.value = 0;
      behind.value = 0;
      hasUpstream.value = false;
    }
  }

  async function refreshLog(dir = cwd.value, g = gen) {
    try {
      const out = await runGit(dir, [
        "log", "-30", "--pretty=format:%H%x1f%h%x1f%s%x1f%an%x1f%cr",
      ]);
      if (stale(g)) return;
      log.value = out
        .split("\n")
        .filter((l) => l.length > 0)
        .map((l) => {
          const [hash, shortHash, subject, author, relTime] = l.split("\x1f");
          return { hash, shortHash, subject, author, relTime };
        });
    } catch {
      if (!stale(g)) log.value = [];
    }
  }

  // `branch`/`hasUpstream` describe whatever cwd points at now, so a push for
  // some OTHER directory has to ask git about that one instead.
  async function branchStateOf(dir: string) {
    const branchName = await runGit(dir, ["branch", "--show-current"]).then((o) => o.trim()).catch(() => "");
    const upstream = await runGit(dir, ["rev-parse", "--abbrev-ref", "@{upstream}"]).then(() => true).catch(() => false);
    return { branchName, upstream };
  }

  // "Commit & push" pins the repo it started in: generating a commit message
  // can take up to 180 s, and a workspace switch during it used to leave the
  // commit unpushed and push the newly-selected project instead.
  async function push(dir = cwd.value) {
    if (!dir) return;
    pushing.value = true;
    error.value = null;
    const toastId = notif.push({ type: "pending", title: "Pushing…", source: "git" });
    try {
      const { branchName, upstream } = dir === cwd.value
        ? { branchName: branch.value, upstream: hasUpstream.value }
        : await branchStateOf(dir);
      const args = upstream ? ["push"] : ["push", "-u", "origin", branchName];
      await runGit(dir, args);
      await refresh();
      notif.resolve(toastId, { type: "done", title: "Pushed", source: "git" });
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "git push failed";
      notif.resolve(toastId, { type: "error", title: "Push failed", body: error.value ?? undefined, source: "git" });
    } finally {
      pushing.value = false;
    }
  }

  async function pull() {
    if (!cwd.value || !hasUpstream.value) return;
    const dir = cwd.value;
    pulling.value = true;
    error.value = null;
    const toastId = notif.push({ type: "pending", title: "Pulling…", source: "git" });
    try {
      await runGit(dir, ["pull", "--ff-only"]);
      await refresh();
      notif.resolve(toastId, { type: "done", title: "Pulled", source: "git" });
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "git pull failed";
      notif.resolve(toastId, { type: "error", title: "Pull failed", body: error.value ?? undefined, source: "git" });
    } finally {
      pulling.value = false;
    }
  }

  // Which model writes the app's generated text — and in whose house style — is
  // a preference, not something each call site should thread through as an
  // argument (they all forgot the policy the moment it existed).
  function textGenPrefs() {
    const ui = useUIStore();
    return { model: ui.textGenerationModel, policy: ui.textGenerationPolicy };
  }

  async function generateCommitMessage(dir = cwd.value) {
    if (!dir || !hasWorkingTreeChanges.value || generating.value) return;
    generating.value = true;
    generateError.value = null;
    const toastId = notif.push({ type: "pending", title: "Generating commit message…", source: "git" });
    try {
      await stageAllIfNeeded(dir);
      const out = await invoke<GitOutput>("generate_commit_message", { cwd: dir, ...textGenPrefs() });
      if (out.code !== 0) throw new Error(out.stderr || "commit message generation failed");
      commitMsg.value = out.stdout.trim();
      notif.resolve(toastId, { type: "done", title: "Commit message generated", source: "git" });
    } catch (e: unknown) {
      generateError.value = e instanceof Error ? e.message : "commit message generation failed";
      notif.resolve(toastId, { type: "error", title: "Commit message generation failed", body: generateError.value ?? undefined, source: "git" });
    } finally {
      generating.value = false;
    }
  }

  // Best-effort: Go returns "" on any failure and the caller keeps its fallback.
  async function generateBranchName(message: string): Promise<string> {
    if (!cwd.value || !message.trim()) return "";
    try {
      return await invoke<string>("generate_branch_name", { cwd: cwd.value, message, ...textGenPrefs() });
    } catch {
      return "";
    }
  }

  function setCwd(path: string) {
    if (path === cwd.value) return;
    gen++;
    cwd.value = path;
    diff.value = "";
    diffFile.value = null;
    commitMsg.value = "";
    refresh();
  }

  async function stageFile(path: string) {
    await runGit(cwd.value, ["add", "--", path]);
    await refresh();
  }

  async function unstageFile(path: string) {
    await runGit(cwd.value, ["reset", "HEAD", "--", path]);
    await refresh();
  }

  async function stageAll() {
    await runGit(cwd.value, ["add", "-A"]);
    await refresh();
  }

  // Like t3code (GitVcsDriverCore.prepareCommitContext): commit/generate act
  // on the whole working tree, not just what's manually staged — `git add -A`
  // first when nothing's staged yet but there are working-tree changes.
  const hasWorkingTreeChanges = computed(
    () => staged.value.length > 0 || unstaged.value.length > 0 || untracked.value.length > 0,
  );

  async function stageAllIfNeeded(dir = cwd.value) {
    if (staged.value.length > 0) return;
    if (unstaged.value.length === 0 && untracked.value.length === 0) return;
    await runGit(dir, ["add", "-A"]);
    await refresh(true);
  }

  // Like t3code (GitActionsControl.logic.ts: `canCommit = hasChanges`, no
  // message check): Commit doesn't require you to type anything first — an
  // empty box gets auto-generated right before the commit.
  // ponytail: numstat only, not full diff — the commit review dialog only needs
  // per-file +/- counts, not the patch text (showDiff already covers that).
  async function stagedFileStats(): Promise<GitFileStat[]> {
    if (!cwd.value) return [];
    const out = await runGit(cwd.value, ["diff", "--cached", "--numstat"]).catch(() => "");
    return out
      .split("\n")
      .filter((l) => l.trim().length > 0)
      .map((line) => {
        const [ins, del, path] = line.split("\t");
        const binary = ins === "-" || del === "-";
        return { path, insertions: binary ? 0 : parseInt(ins, 10) || 0, deletions: binary ? 0 : parseInt(del, 10) || 0, binary };
      });
  }

  async function commit() {
    if (committing.value) return;
    // The whole commit belongs to the repo it started in, and it decides WHAT
    // to do before it awaits anything: the refs below describe `dir` only until
    // the first await, after which a workspace switch may have refreshed them
    // for another repo. Re-reading them mid-commit is what made "Commit & push"
    // do nothing after switching workspaces — it asked the new workspace
    // whether the old one had anything staged, got "no", and returned.
    const dir = cwd.value;
    let msg = commitMsg.value.trim();
    const needsStaging = staged.value.length === 0
      && (unstaged.value.length > 0 || untracked.value.length > 0);
    if (!dir || (staged.value.length === 0 && !needsStaging)) return;
    committing.value = true;
    // The commit review dialog closes the moment it hands the work over, so the
    // toast is the only thing left reporting it.
    const toastId = notif.push({ type: "pending", title: "Committing…", source: "git" });
    try {
      if (needsStaging) await runGit(dir, ["add", "-A"]);
      if (!msg) {
        await generateCommitMessage(dir);
        msg = commitMsg.value.trim();
        if (!msg) {
          notif.resolve(toastId, { type: "error", title: "Commit failed", body: "no commit message", source: "git" });
          return;
        }
      }
      await runGit(dir, ["commit", "-m", msg]);
      commitMsg.value = "";
      diff.value = "";
      diffFile.value = null;
      await refresh();
      notif.resolve(toastId, { type: "done", title: "Committed", body: msg.split("\n")[0], source: "git" });
      return dir;
    } catch (e: unknown) {
      // Not caught before: a failed commit threw past every caller (GitPanel's
      // "Commit & Push" awaits this with no try/catch) and vanished as an
      // unhandled rejection, so the button just went quiet with no feedback.
      error.value = e instanceof Error ? e.message : "git commit failed";
      notif.resolve(toastId, { type: "error", title: "Commit failed", body: error.value, source: "git" });
      throw e;
    } finally {
      committing.value = false;
    }
  }

  async function showDiff(path: string, isStagedFile: boolean) {
    diffFile.value = path;
    diffStaged.value = isStagedFile;
    try {
      const args = isStagedFile
        ? ["diff", "--cached", "--", path]
        : ["diff", "--", path];
      diff.value = await runGit(cwd.value, args);
    } catch {
      diff.value = "";
    }
  }

  function clearDiff() {
    diff.value = "";
    diffFile.value = null;
  }

  async function fetchAllDiff(staged: boolean): Promise<string> {
    const args = staged ? ["diff", "--cached"] : ["diff"];
    try {
      return await runGit(cwd.value, args);
    } catch {
      return "";
    }
  }

  async function fetchBranches(dir = cwd.value, g = gen) {
    if (!dir) return;
    try {
      const out = await runGit(dir, ["branch", "--format=%(refname:short)"]);
      if (stale(g)) return;
      branches.value = out.split("\n").map((b) => b.trim()).filter(Boolean);
    } catch {
      if (!stale(g)) branches.value = [];
    }
  }

  async function switchBranch(name: string) {
    await runGit(cwd.value, ["checkout", name]);
    await refresh();
  }

  // Checkout a branch in a SPECIFIC workspace's directory, independent of the
  // store's single shared `cwd` (which tracks whichever workspace last called
  // setCwd, not necessarily the workspace this chat lives in).
  async function checkoutBranchForWorkspace(wsId: number, path: string, name: string) {
    await runGit(path, ["checkout", name]);
    branchByWs.value[wsId] = name;
    if (path === cwd.value) await refresh();
  }

  async function createBranch(name: string) {
    await runGit(cwd.value, ["checkout", "-b", name]);
    await refresh();
  }

  async function fetch() {
    if (!cwd.value) return;
    fetching.value = true;
    try {
      await runGit(cwd.value, ["fetch"]);
      await refreshUpstream();
    } catch {
      /* network errors are silent */
    } finally {
      fetching.value = false;
    }
  }

  async function discardFile(path: string) {
    await runGit(cwd.value, ["checkout", "--", path]);
    await refresh();
  }

  async function unstageAll() {
    await runGit(cwd.value, ["reset", "HEAD"]);
    await refresh();
  }

  async function gitInit() {
    if (!cwd.value) return;
    loading.value = true;
    error.value = null;
    try {
      await invoke<GitOutput>("run_git", { cwd: cwd.value, args: ["init"] });
      await refresh();
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "git init failed";
    } finally {
      loading.value = false;
    }
  }

  return {
    cwd, branch, staged, unstaged, untracked,
    diff, diffFile, diffStaged,
    loading, error, commitMsg,
    ahead, behind, hasUpstream, pushing, pulling, generating, generateError, committing, hasWorkingTreeChanges, log, logLoading,
    setCwd, refresh, stageFile, unstageFile, unstageAll, stageAll, stageAllIfNeeded, commit, showDiff, clearDiff, fetchAllDiff, gitInit,
    push, pull, refreshLog, generateCommitMessage, stagedFileStats, generateBranchName,
    branches, fetching, fetchBranches, switchBranch, checkoutBranchForWorkspace, createBranch, fetch, discardFile,
    prByWs, branchByWs, ensureBranch, fetchPr, fetchPrs,
  };
});
