import { describe, it, expect, vi } from "vitest";
import { fetchReviewDiff } from "./reviewDiff";

describe("review comparisons", () => {
  it("uses the selected thread's frozen last turn, never falls back to workspace changes", async () => {
    const invoke = vi.fn().mockResolvedValue({ id: 5, diff: "frozen", label: "fix", cwd: "/repo", subjectId: "chat:42" });
    const result = await fetchReviewDiff({ cwd: "/repo", scope: { kind: "last-turn" }, subjectId: "chat:42" }, invoke);
    expect(result.diff).toBe("frozen");
    expect(invoke).toHaveBeenCalledWith("last_turn_audit", { cwd: "/repo", subjectId: "chat:42" });
    invoke.mockClear();
    const empty = await fetchReviewDiff({ cwd: "/repo", scope: { kind: "last-turn" }, subjectId: "" }, invoke);
    expect(empty.diff).toBe("");
    expect(invoke).not.toHaveBeenCalled();
  });

  it("reports branch errors rather than claiming there are no changes", async () => {
    const invoke = vi.fn().mockResolvedValue({ stdout: "", stderr: "unknown revision", code: 128 });
    await expect(fetchReviewDiff({ cwd: "/repo", scope: { kind: "branch" }, subjectId: "", manualBase: "missing" }, invoke)).rejects.toThrow("unknown revision");
  });
});
