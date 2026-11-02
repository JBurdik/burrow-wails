import { describe, it, expect } from "vitest";
import { subagentArchivable, SUBAGENT_AUTOARCHIVE_MS } from "./chatTree";
import type { ClaudeSession } from "./claudeChats";
import type { Phase } from "@/runtime/displayStatus";

const ENDED = 1_000_000;
const SEEN = ENDED + 5_000;
const LATER = SEEN + SUBAGENT_AUTOARCHIVE_MS;

const child = (over: Partial<ClaudeSession> = {}): ClaudeSession =>
  ({ id: 7, workspaceId: 1, title: "sub", claudeSessionId: "", busy: false, messageCount: 1, parentChatId: 3, collectedAt: SEEN, ...over }) as ClaudeSession;
const phase = (over: Partial<Phase> = {}): Phase =>
  ({ state: "done", is_agent: true, turn_ended_at: ENDED, updated_at: ENDED, ...over });

describe("subagentArchivable", () => {
  it("archives a settled, seen, collected sub-agent once the delay has passed", () => {
    expect(subagentArchivable(child(), phase(), SEEN, false, LATER)).toBe(true);
    expect(subagentArchivable(child(), phase({ state: "failed" }), SEEN, false, LATER)).toBe(true);
    expect(subagentArchivable(child(), phase({ state: "idle" }), SEEN, false, LATER)).toBe(true);
  });

  it("waits the full delay from the receipt", () => {
    expect(subagentArchivable(child(), phase(), SEEN, false, LATER - 1)).toBe(false);
  });

  it("restarts the delay on a later phase change", () => {
    expect(subagentArchivable(child(), phase({ state: "idle", updated_at: SEEN + 60_000 }), SEEN, false, LATER)).toBe(false);
  });

  it("never archives a busy or blocked one", () => {
    for (const state of ["running", "waiting_input", "waiting_approval"] as const) {
      expect(subagentArchivable(child(), phase({ state }), SEEN, false, LATER)).toBe(false);
    }
    expect(subagentArchivable(child({ busy: true }), phase(), SEEN, false, LATER)).toBe(false);
  });

  it("keeps an unseen turn", () => {
    expect(subagentArchivable(child(), phase(), 0, false, LATER)).toBe(false);
    expect(subagentArchivable(child(), phase({ turn_ended_at: SEEN + 1 }), SEEN, false, LATER + 10)).toBe(false);
  });

  it("keeps an uncollected result", () => {
    expect(subagentArchivable(child({ collectedAt: undefined }), phase(), SEEN, false, LATER)).toBe(false);
  });

  it("keeps one on screen, a thread, an archived one, or one with no known phase", () => {
    expect(subagentArchivable(child(), phase(), SEEN, true, LATER)).toBe(false);
    expect(subagentArchivable(child({ parentChatId: undefined }), phase(), SEEN, false, LATER)).toBe(false);
    expect(subagentArchivable(child({ archivedAt: 1 }), phase(), SEEN, false, LATER)).toBe(false);
    expect(subagentArchivable(child(), undefined, SEEN, false, LATER)).toBe(false);
  });
});
