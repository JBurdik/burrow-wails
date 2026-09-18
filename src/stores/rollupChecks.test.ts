import { describe, expect, it } from "vitest";
import { rollupChecks } from "./git";

describe("rollupChecks", () => {
  it("returns none for an empty or non-array rollup", () => {
    expect(rollupChecks(undefined)).toBe("none");
    expect(rollupChecks([])).toBe("none");
  });

  it("fails on a GitHub-shaped failure", () => {
    expect(rollupChecks([{ conclusion: "FAILURE", status: "COMPLETED" }])).toBe("fail");
  });

  it("fails on a GitLab-shaped failure", () => {
    expect(rollupChecks([{ status: "failed", conclusion: "failed" }])).toBe("fail");
  });

  it("passes on a GitLab-shaped success", () => {
    expect(rollupChecks([{ status: "success", conclusion: "success" }])).toBe("pass");
  });

  it("is pending on a GitHub-shaped in-progress check", () => {
    expect(rollupChecks([{ status: "IN_PROGRESS" }])).toBe("pending");
  });
});
