import { describe, it, expect } from "vitest";
import { applyShellEvent, emptySnapshot } from "./shellSnapshot";
import type { Phase } from "./displayStatus";

const phase = (state: Phase["state"]): Phase =>
  ({ state, detail: "", model: "", title: "", is_agent: false, turn_ended_at: 0, updated_at: 0 }) as Phase;

describe("applyShellEvent", () => {
  it("stores a phase under the server's own key", () => {
    // The key must stay `pty:7` / `chat:12` exactly as the server writes it —
    // a client that re-derives it has a second naming scheme to keep in step.
    const s = applyShellEvent(emptySnapshot(), { seq: 1, name: "phase-pty:7", payload: phase("running") });
    expect(s.phases["pty:7"].state).toBe("running");
  });

  it("moves seq forward and never backward", () => {
    let s = applyShellEvent(emptySnapshot(), { seq: 9, name: "phase-pty:7", payload: phase("running") });
    s = applyShellEvent(s, { seq: 4, name: "phase-pty:7", payload: phase("done") });
    // A rewind would make the client resume from a gap it already holds.
    expect(s.seq).toBe(9);
  });

  it("ignores an event it does not model without disturbing the rest", () => {
    const base = applyShellEvent(emptySnapshot(), { seq: 1, name: "phase-pty:7", payload: phase("running") });
    const s = applyShellEvent(base, { seq: 2, name: "control:result", payload: { token: "x" } });
    expect(s.phases).toEqual(base.phases);
    expect(s.seq).toBe(2);
  });

  it("does not mutate the state it was given", () => {
    const base = emptySnapshot();
    applyShellEvent(base, { seq: 1, name: "phase-pty:7", payload: phase("running") });
    expect(base.phases).toEqual({});
    expect(base.seq).toBe(0);
  });
});
