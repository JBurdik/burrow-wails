import { describe, expect, it, vi, beforeEach } from "vitest";
import { createPinia, setActivePinia } from "pinia";

const invoke = vi.fn();
vi.mock("@tauri-apps/api/core", () => ({ invoke: (...a: unknown[]) => invoke(...a) }));

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
});
