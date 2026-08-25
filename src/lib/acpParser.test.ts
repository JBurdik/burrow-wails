import { describe, expect, it } from "vitest";
import { parseAcpPermRequest } from "./acpParser";

describe("Codex app-server approval bridge", () => {
  it("maps command approval to the existing allow/deny UI contract", () => {
    expect(parseAcpPermRequest({
      id: 42,
      method: "item/commandExecution/requestApproval",
      params: { threadId: "thread-1", itemId: "item-1", command: "git status" },
    })).toMatchObject({
      rpcId: 42,
      title: "Run: git status",
      options: [
        { optionId: "codex:accept", kind: "allow_once" },
        { optionId: "codex:acceptForSession", kind: "allow_always" },
        { optionId: "codex:decline", kind: "reject_once" },
      ],
    });
  });

  it.each([
    "item/fileChange/requestApproval",
    "item/permissions/requestApproval",
  ])("supports Codex's %s request", (method) => {
    expect(parseAcpPermRequest({ id: 3, method, params: { threadId: "thread" } })?.kind).toBe("codex-approval");
  });
});
