/**
 * The git store has ONE cwd for the whole app, so every long-running git op
 * races a workspace switch. Both halves of that race are covered here:
 *
 *  - a refresh started for workspace A must not write A's status over B's
 *    after the user switched (the titlebar's Commit & push button reads
 *    hasWorkingTreeChanges/ahead, so a stale write left it acting on the
 *    wrong project — disabled with changes pending, or armed with none);
 *  - a commit/push must run every one of its git calls against the directory
 *    it started in, not whatever cwd holds by the time each await resolves.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";

type Call = { cwd: string; args: string[] };
const calls: Call[] = [];
// Per-cwd delay so a slow workspace's refresh can be made to land last.
const delay: Record<string, number> = {};
// Porcelain status per cwd.
const status: Record<string, string> = {};

vi.mock("@tauri-apps/api/core", () => ({
  invoke: async (cmd: string, a: { cwd: string; args: string[] }) => {
    if (cmd !== "run_git") return { stdout: "", stderr: "", code: 0 };
    calls.push({ cwd: a.cwd, args: a.args });
    const ms = delay[a.cwd] ?? 0;
    if (ms) await new Promise((r) => setTimeout(r, ms));
    const sub = a.args[0];
    const out =
      sub === "status" ? (status[a.cwd] ?? "")
      : sub === "branch" ? "main"
      : sub === "rev-list" ? "0\t0"
      : "";
    return { stdout: out, stderr: "", code: 0 };
  },
}));

const settle = () => new Promise((r) => setTimeout(r, 50));

describe("git store follows the workspace it is pointed at", () => {
  beforeEach(async () => {
    calls.length = 0;
    for (const k of Object.keys(delay)) delete delay[k];
    for (const k of Object.keys(status)) delete status[k];
    setActivePinia(createPinia());
  });

  it("drops a refresh whose workspace is no longer the current one", async () => {
    const { useGitStore } = await import("./git");
    const git = useGitStore();

    status["/a"] = " M dirty.txt";
    status["/b"] = "";
    delay["/a"] = 30; // A answers after B

    git.setCwd("/a");
    git.setCwd("/b");
    await settle();

    expect(git.cwd).toBe("/b");
    expect(git.unstaged).toHaveLength(0);
    expect(git.hasWorkingTreeChanges).toBe(false);
  });

  it("runs every git call of one commit against the same directory", async () => {
    const { useGitStore } = await import("./git");
    const git = useGitStore();

    status["/a"] = " M dirty.txt";
    git.setCwd("/a");
    await settle();
    expect(git.hasWorkingTreeChanges).toBe(true);

    git.commitMsg = "fix: something";
    const done = git.commit();
    git.setCwd("/b"); // user switches mid-commit
    await done;
    await settle();

    const commits = calls.filter((c) => c.args[0] === "commit");
    expect(commits).toHaveLength(1);
    expect(commits[0].cwd).toBe("/a");
    expect(calls.filter((c) => c.args[0] === "add").every((c) => c.cwd === "/a")).toBe(true);
  });

  it("pushes the repo the commit landed in", async () => {
    const { useGitStore } = await import("./git");
    const git = useGitStore();

    status["/a"] = " M dirty.txt";
    git.setCwd("/a");
    await settle();

    git.commitMsg = "fix: something";
    const dir = await git.commit();
    git.setCwd("/b"); // switch after the commit, before the push
    await git.push(dir);
    await settle();

    const pushes = calls.filter((c) => c.args[0] === "push");
    expect(pushes).toHaveLength(1);
    expect(pushes[0].cwd).toBe("/a");
  });
});
