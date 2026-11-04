import { describe, expect, it } from "vitest";
import { feedGroups, feedRows } from "./threadFeed";
import type { ShellSnapshotData } from "@/runtime/shellSnapshot";
import type { ClaudeSession } from "@/stores/claudeChats";

const now = 1_000_000_000_000;
const chat = (id: number, workspaceId: number, extra: Partial<ClaudeSession> = {}) =>
  ({ id, workspaceId, title: `c${id}`, lastActivityAt: now - id, ...extra }) as unknown as ClaudeSession;
const phase = (state: string, extra = {}) => ({ state, updated_at: 0, ...extra }) as never;

const snap: ShellSnapshotData = {
  seq: 1, environment_id: "", chats: [],
  workspaces: [{ id: 1, name: "repo", path: "/r" }, { id: 2, name: "wt", path: "/r-wt" }],
  tabs: { 1: [{ pty_id: 9, title: "claude" }, { pty_id: 10, title: "zsh" }] },
  phases: {
    "chat:1": phase("running"),
    "chat:2": phase("waiting_approval", { detail: "Bash" }),
    "chat:3": phase("done", { turn_ended_at: now - 5 }),
    "pty:9": phase("running", { is_agent: true }),
    "pty:10": phase("idle"),
  },
};

describe("threadFeed", () => {
  const rows = feedRows(snap, [
    chat(1, 1), chat(2, 2), chat(3, 1), chat(4, 1),
    chat(5, 1, { parentChatId: 1 } as Partial<ClaudeSession>), // a sub-agent: its parent's business
    chat(6, 1, { archivedAt: now } as Partial<ClaudeSession>),
  ], {}, now, { parentOf: { 2: 1 } });

  it("lists active top-level chats and agent tabs of every project", () => {
    expect(rows.map((r) => r.key).sort()).toEqual(["chat:1", "chat:2", "chat:3", "chat:4", "pty:9"]);
  });

  it("files a worktree's thread under its repo", () => {
    expect(rows.find((r) => r.key === "chat:2")).toMatchObject({ workspaceId: 2, repoId: 1, workspace: "wt", detail: "Bash" });
  });

  it("groups by what the thread needs from you", () => {
    const g = Object.fromEntries(feedGroups(rows).map((x) => [x.key, x.rows.map((r) => r.key)]));
    expect(g.needs).toEqual(["chat:2"]);
    expect(g.working.sort()).toEqual(["chat:1", "pty:9"]);
    expect(g.unread).toEqual(["chat:3"]);
    expect(g.recent).toEqual(["chat:4"]);
    expect(feedGroups(rows, 0).find((x) => x.key === "recent")!.rows).toEqual([]);
  });
});
