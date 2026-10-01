export type ReviewDiffScope =
  | { kind: "workspace" }
  | { kind: "branch" }
  | { kind: "last-turn" }
  | { kind: "turn"; checkpointId: number };

export interface TurnReview {
  id: number;
  subjectId: string;
  cwd: string;
  label: string;
  diff: string;
  settledAt: number;
}

type Invoke = <T>(command: string, args?: Record<string, unknown>) => Promise<T>;

export async function fetchReviewDiff(
  options: { cwd: string; scope: ReviewDiffScope; subjectId: string; checkpointCommit?: string; manualBase?: string },
  invoke: Invoke,
): Promise<{ diff: string; base: string; turn: TurnReview | null }> {
  const { cwd, scope, subjectId, checkpointCommit, manualBase } = options;
  const result = { diff: "", base: "", turn: null as TurnReview | null };
  if (!cwd) return result;
  if (scope.kind === "workspace") {
    result.diff = await invoke<string>("working_tree_diff", { cwd });
  } else if (scope.kind === "last-turn") {
    if (subjectId) {
      result.turn = await invoke<TurnReview | null>("last_turn_audit", { cwd, subjectId });
      result.diff = result.turn?.diff ?? "";
    }
  } else if (scope.kind === "branch") {
    result.base = manualBase || await invoke<string>("branch_diff_base", { cwd });
    if (result.base) {
      const out = await invoke<{ stdout: string; stderr?: string; code: number }>("run_git", {
        cwd, args: ["diff", "--no-ext-diff", "--no-color", `${result.base}...HEAD`, "--"],
      });
      if (out.code !== 0) throw new Error(out.stderr || "Could not compare this branch.");
      result.diff = out.stdout;
    }
  } else if (checkpointCommit) {
    result.diff = await invoke<string>("checkpoint_diff", { cwd, commit: checkpointCommit });
  }
  return result;
}
