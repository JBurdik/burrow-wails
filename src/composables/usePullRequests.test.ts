import { describe, expect, it, vi, beforeEach } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const invoke = vi.fn();
vi.mock("@tauri-apps/api/core", () => ({ invoke: (...a: unknown[]) => invoke(...a) }));
// generatedContent() only reads these three fields; the real ui store's theme
// setup isn't wired up outside the running app, so a fresh Pinia instance
// crashes on it — irrelevant to what's under test here.
vi.mock("@/stores/ui", () => ({ useUIStore: () => ({ textGenerationModel: "", textGenerationPolicy: "default", textGenerationRules: "" }) }));

import { usePullRequests } from "./usePullRequests";

describe("usePullRequests", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    invoke.mockReset();
  });

  it("lists through forge_pr_list and never through run_gh", async () => {
    invoke.mockResolvedValue([
      { number: 7, title: "t", url: "u", state: "open", isDraft: false, headRefName: "b", baseRefName: "main" },
    ]);
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(invoke).toHaveBeenCalledWith("forge_pr_list", { cwd: "/repo", scope: "assigned", state: "open" });
    expect(pr.items.value).toHaveLength(1);
    expect(pr.items.value[0].number).toBe(7);
  });

  it("surfaces the backend's own error text", async () => {
    invoke.mockRejectedValue(new Error("glab: not logged in"));
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(pr.error.value).toContain("not logged in");
    expect(pr.items.value).toHaveLength(0);
  });

  it("does not parse JSON — the backend already did", async () => {
    invoke.mockResolvedValue([{ number: 1, title: "t", url: "u", state: "open", isDraft: false, headRefName: "b", baseRefName: "m" }]);
    const pr = usePullRequests(() => "/repo");
    await pr.refresh();
    expect(pr.items.value[0].title).toBe("t");
  });

  it("falls back to the head commit when no text-generation model is configured", async () => {
    invoke.mockImplementation((cmd: string, args?: Record<string, unknown>) => {
      if (cmd === "run_git") {
        const gitArgs = args?.args as string[];
        if (gitArgs.includes("--show-current")) return Promise.resolve({ stdout: "feat/x\n" });
        if (gitArgs.includes("symbolic-ref")) return Promise.resolve({ stdout: "origin/main\n" });
        if (gitArgs.includes("--format=%s")) return Promise.resolve({ stdout: "fix: the thing\n" });
        if (gitArgs.includes("--format=%b")) return Promise.resolve({ stdout: "\n" });
      }
      if (cmd === "generate_pr_content") return Promise.resolve({}); // no model configured -> no title
      if (cmd === "forge_pr_create") return Promise.resolve({ number: 9, title: "fix: the thing" });
      return Promise.resolve([]);
    });
    const pr = usePullRequests(() => "/repo");
    await pr.create();
    expect(invoke).toHaveBeenCalledWith("forge_pr_create", { cwd: "/repo", title: "fix: the thing", body: "", base: "", head: "" });
    expect(pr.error.value).toBe("");
  });

  it("surfaces an error instead of sending an empty title when both the model and git fail", async () => {
    invoke.mockImplementation((cmd: string) => {
      if (cmd === "run_git") return Promise.reject(new Error("not a git repo"));
      return Promise.resolve([]);
    });
    const pr = usePullRequests(() => "/repo");
    await pr.create();
    expect(invoke).not.toHaveBeenCalledWith("forge_pr_create", expect.anything());
    expect(pr.error.value).not.toBe("");
  });
});
