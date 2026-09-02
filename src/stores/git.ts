import { defineStore } from "pinia";
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

export interface GitFile {
  path: string;
  x: string;
  y: string;
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
  const log = ref<GitCommit[]>([]);
  const logLoading = ref(false);
  const branches = ref<string[]>([]);
  const fetching = ref(false);

  // PR status cache, keyed by workspace id. null = checked, no open PR (or gh
  // missing/unauthed). undefined (absent key) = never checked.
  const prByWs = ref<Record<number, PrInfo | null>>({});
  // Per-workspace in-flight guard so the 60s poll never stacks gh calls.
  const prInFlight = new Set<number>();
  // Current branch per workspace, filled by the same 60s sweep as the PR badges.
  // The single `branch` ref above only tracks the ACTIVE cwd; the sidebar lists
  // every workspace at once and needs one branch each.
  const branchByWs = ref<Record<number, string>>({});

  // Fetch PR status for one workspace via `gh pr view`. Never throws — any
  // failure (no gh, not authed, no PR, not a GitHub repo) caches null so the
  // Sidebar simply shows no badge. Cheap + non-blocking; safe to call on a poll.
  async function fetchPr(wsId: number, cwd: string) {
    if (!cwd || prInFlight.has(wsId)) return;
    prInFlight.add(wsId);
    try {
      const head = await invoke<GitOutput>("run_git", { cwd, args: ["branch", "--show-current"] }).catch(() => null);
      if (head?.code === 0) branchByWs.value[wsId] = head.stdout.trim();
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
    if (!cwd.value) return;
    if (!silent) loading.value = true;
    error.value = null;
    try {
      const [statusOut, branchOut] = await Promise.all([
        runGit(cwd.value, ["status", "--porcelain"]),
        runGit(cwd.value, ["branch", "--show-current"]),
      ]);
      const parsed = parseStatus(statusOut);
      staged.value = parsed.staged;
      unstaged.value = parsed.unstaged;
      untracked.value = parsed.untracked;
      branch.value = branchOut.trim();
      await refreshUpstream();
      await refreshLog();
      await fetchBranches();
    } catch (e: unknown) {
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
      if (!silent) loading.value = false;
    }
  }

  async function refreshUpstream() {
    try {
      // counts: "<behind>\t<ahead>" relative to upstream
      const out = await runGit(cwd.value, [
        "rev-list", "--left-right", "--count", "@{upstream}...HEAD",
      ]);
      const [b, a] = out.trim().split(/\s+/);
      behind.value = parseInt(b, 10) || 0;
      ahead.value = parseInt(a, 10) || 0;
      hasUpstream.value = true;
    } catch {
      // no upstream configured
      ahead.value = 0;
      behind.value = 0;
      hasUpstream.value = false;
    }
  }

  async function refreshLog() {
    try {
      const out = await runGit(cwd.value, [
        "log", "-30", "--pretty=format:%H%x1f%h%x1f%s%x1f%an%x1f%cr",
      ]);
      log.value = out
        .split("\n")
        .filter((l) => l.length > 0)
        .map((l) => {
          const [hash, shortHash, subject, author, relTime] = l.split("\x1f");
          return { hash, shortHash, subject, author, relTime };
        });
    } catch {
      log.value = [];
    }
  }

  async function push() {
    if (!cwd.value) return;
    pushing.value = true;
    error.value = null;
    try {
      const args = hasUpstream.value
        ? ["push"]
        : ["push", "-u", "origin", branch.value];
      await runGit(cwd.value, args);
      await refresh();
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "git push failed";
    } finally {
      pushing.value = false;
    }
  }

  async function pull() {
    if (!cwd.value || !hasUpstream.value) return;
    pulling.value = true;
    error.value = null;
    try {
      await runGit(cwd.value, ["pull", "--ff-only"]);
      await refresh();
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : "git pull failed";
    } finally {
      pulling.value = false;
    }
  }

  async function generateCommitMessage() {
    if (!cwd.value || staged.value.length === 0 || generating.value) return;
    generating.value = true;
    generateError.value = null;
    try {
      const out = await invoke<GitOutput>("generate_commit_message", { cwd: cwd.value });
      if (out.code !== 0) throw new Error(out.stderr || "commit message generation failed");
      commitMsg.value = out.stdout.trim();
    } catch (e: unknown) {
      generateError.value = e instanceof Error ? e.message : "commit message generation failed";
    } finally {
      generating.value = false;
    }
  }

  function setCwd(path: string) {
    if (path === cwd.value) return;
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

  async function commit() {
    if (!commitMsg.value.trim()) return;
    await runGit(cwd.value, ["commit", "-m", commitMsg.value.trim()]);
    commitMsg.value = "";
    diff.value = "";
    diffFile.value = null;
    await refresh();
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

  async function fetchBranches() {
    if (!cwd.value) return;
    try {
      const out = await runGit(cwd.value, ["branch", "--format=%(refname:short)"]);
      branches.value = out.split("\n").map((b) => b.trim()).filter(Boolean);
    } catch {
      branches.value = [];
    }
  }

  async function switchBranch(name: string) {
    await runGit(cwd.value, ["checkout", name]);
    await refresh();
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
    ahead, behind, hasUpstream, pushing, pulling, generating, generateError, log, logLoading,
    setCwd, refresh, stageFile, unstageFile, unstageAll, stageAll, commit, showDiff, clearDiff, fetchAllDiff, gitInit,
    push, pull, refreshLog, generateCommitMessage,
    branches, fetching, fetchBranches, switchBranch, createBranch, fetch, discardFile,
    prByWs, branchByWs, fetchPr, fetchPrs,
  };
});
