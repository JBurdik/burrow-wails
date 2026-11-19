import { describe, expect, it } from "vitest";
import { createActivityLog } from "./activityLog";

describe("activity log", () => {
  it("notes only while on, hangs always, ≤ 1 hang per 10 s", () => {
    let on = false;
    let t = 0;
    const sent: unknown[][] = [];
    const log = createActivityLog({
      invoke: async (_cmd, args) => { sent.push(args.entries as unknown[]); },
      enabled: () => on,
      route: () => "/ws/1",
      now: () => t,
    });
    log.note({ kind: "click", label: "Settings" });
    log.hang(500); // below 1 s
    log.hang(1500);
    t = 5000;
    log.hang(2000); // rate-limited
    log.flush();
    expect(sent).toEqual([[{ at: 0, kind: "hang", ms: 1500, route: "/ws/1", last: undefined }]]);
    on = true;
    log.note({ kind: "send", images: 2 });
    t = 20_000;
    log.hang(3000);
    log.flush();
    expect(sent[1]).toEqual([
      { kind: "send", images: 2, at: 5000 },
      { at: 20_000, kind: "hang", ms: 3000, route: "/ws/1", last: "send" },
    ]);
    log.flush();
    expect(sent.length).toBe(2);
  });
});
