import { describe, expect, it } from "vitest";
import { applyBgTaskLine, deriveBgTasks, emptyBgTaskState, listBgTasks, mergeCodexTerminals, unwrapShellCommand } from "./backgroundTasks";

type Line = { ord: number; line: string };

// Real lines captured from chat_stream (claude 2.1.3xx), trimmed of
// uuid/session_id/usage noise — the shapes are untouched.

// Bash run_in_background, then the agent's TaskStop.
const STOPPED: Line[] = [
  { ord: 761, line: "{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_01G63Q74aFPtEEDYLFQKcLph\",\"name\":\"Bash\",\"input\":{\"command\":\"pnpm dev\",\"description\":\"Run Vite dev server\",\"run_in_background\":true},\"caller\":{\"type\":\"direct\"}}]},\"timestamp\":\"2026-10-23T07:44:07.043Z\"}" },
  { ord: 782, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[{\"task_id\":\"b14xkae6q\",\"run_id\":\"0mvknyygd-15b00d4f\",\"task_type\":\"local_bash\",\"description\":\"Run Vite dev server\"}]}" },
  { ord: 783, line: "{\"type\":\"system\",\"subtype\":\"task_started\",\"task_id\":\"b14xkae6q\",\"tool_use_id\":\"toolu_01G63Q74aFPtEEDYLFQKcLph\",\"description\":\"Run Vite dev server\",\"is_backgrounded\":true,\"task_type\":\"local_bash\"}" },
  { ord: 784, line: "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"tool_use_id\":\"toolu_01G63Q74aFPtEEDYLFQKcLph\",\"type\":\"tool_result\",\"content\":\"Command running in background with ID: b14xkae6q. Output is being written to: /private/tmp/claude-501/-Users-jirkab-code-eshop-Administration/7b112814-ead4-4d46-bfd3-a1d07d29bc3c/tasks/b14xkae6q.output. You will be notified when it completes. To check interim output, use Read on that file path.\",\"is_error\":false}]},\"timestamp\":\"2026-10-23T07:44:08.216Z\",\"tool_use_result\":{\"stdout\":\"\",\"stderr\":\"\",\"interrupted\":false,\"isImage\":false,\"noOutputExpected\":false,\"backgroundTaskId\":\"b14xkae6q\"}}" },
  { ord: 1144, line: "{\"type\":\"system\",\"subtype\":\"task_updated\",\"task_id\":\"b14xkae6q\",\"patch\":{\"status\":\"killed\",\"end_time\":1792741576106}}" },
  { ord: 1145, line: "{\"type\":\"system\",\"subtype\":\"task_notification\",\"task_id\":\"b14xkae6q\",\"tool_use_id\":\"toolu_01G63Q74aFPtEEDYLFQKcLph\",\"status\":\"stopped\",\"output_file\":\"/private/tmp/claude-501/-Users-jirkab-code-eshop-Administration/7b112814-ead4-4d46-bfd3-a1d07d29bc3c/tasks/b14xkae6q.output\",\"summary\":\"Run Vite dev server\"}" },
  { ord: 1146, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[]}" },
];

// A foreground Bash moved to the background by its 120 s timeout.
const MOVED: Line[] = [
  { ord: 14073, line: "{\"type\":\"system\",\"subtype\":\"task_started\",\"task_id\":\"bj4g2jief\",\"tool_use_id\":\"toolu_015b4kKNhJjATiPBTMdCNiHV\",\"description\":\"Register plugin in app config\",\"is_backgrounded\":false,\"task_type\":\"local_bash\"}" },
  { ord: 14078, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[{\"task_id\":\"bj4g2jief\",\"run_id\":\"0mvobgfnb-965c4870\",\"task_type\":\"local_bash\",\"description\":\"Register plugin in app config\"}]}" },
  { ord: 14079, line: "{\"type\":\"system\",\"subtype\":\"task_updated\",\"task_id\":\"bj4g2jief\",\"patch\":{\"is_backgrounded\":true}}" },
  { ord: 14080, line: "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"tool_use_id\":\"toolu_015b4kKNhJjATiPBTMdCNiHV\",\"type\":\"tool_result\",\"content\":\"Command did not complete within its 120s timeout and was moved to the background (ID: bj4g2jief). Output is being written to: /private/tmp/claude-501/-Users-jirkab-code-ak-monorepo/04d9c159-845a-491d-815a-f730205c9fd1/tasks/bj4g2jief.output. You will be notified when it completes. If it is still running after 30m in the background, it will be stopped and you will be notified. To check interim output, use Read on that file path.\",\"is_error\":false}]},\"timestamp\":\"2026-10-25T21:06:50.438Z\",\"tool_use_result\":{\"stdout\":\"\",\"stderr\":\"\",\"interrupted\":false,\"isImage\":false,\"noOutputExpected\":false,\"backgroundTaskId\":\"bj4g2jief\",\"timedOutAfterMs\":120000}}" },
  { ord: 14084, line: "{\"type\":\"system\",\"subtype\":\"task_updated\",\"task_id\":\"bj4g2jief\",\"patch\":{\"status\":\"completed\",\"end_time\":1792962412383}}" },
  { ord: 14085, line: "{\"type\":\"system\",\"subtype\":\"task_notification\",\"task_id\":\"bj4g2jief\",\"tool_use_id\":\"toolu_015b4kKNhJjATiPBTMdCNiHV\",\"status\":\"completed\",\"output_file\":\"/private/tmp/claude-501/-Users-jirkab-code-ak-monorepo/04d9c159-845a-491d-815a-f730205c9fd1/tasks/bj4g2jief.output\",\"summary\":\"Background command \\\"Register plugin in app config\\\" completed (exit code 0)\"}" },
  { ord: 14086, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[]}" },
];

// Bash run_in_background that ran to completion.
const COMPLETED: Line[] = [
  { ord: 791, line: "{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_01UMHFePCdB6PPS9VGsaSp5k\",\"name\":\"Bash\",\"input\":{\"command\":\"just release minor 2>&1 | tail -40\",\"description\":\"Build, sign, notarize and publish v0.6.0\",\"timeout\":3600000,\"run_in_background\":true},\"caller\":{\"type\":\"direct\"}}]},\"timestamp\":\"2026-10-22T14:43:55.727Z\"}" },
  { ord: 797, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[{\"task_id\":\"bxlfet4mx\",\"run_id\":\"0mvjnj2ch-e358a7e3\",\"task_type\":\"local_bash\",\"description\":\"Build, sign, notarize and publish v0.6.0\"}]}" },
  { ord: 798, line: "{\"type\":\"system\",\"subtype\":\"task_started\",\"task_id\":\"bxlfet4mx\",\"tool_use_id\":\"toolu_01UMHFePCdB6PPS9VGsaSp5k\",\"description\":\"Build, sign, notarize and publish v0.6.0\",\"is_backgrounded\":true,\"task_type\":\"local_bash\"}" },
  { ord: 799, line: "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"tool_use_id\":\"toolu_01UMHFePCdB6PPS9VGsaSp5k\",\"type\":\"tool_result\",\"content\":\"Command running in background with ID: bxlfet4mx. Output is being written to: /private/tmp/claude-501/-Users-jirkab-code-agentic-ide-go-wails/46b14cea-9865-47c9-9962-41b827cb8242/tasks/bxlfet4mx.output. You will be notified when it completes. To check interim output, use Read on that file path.\",\"is_error\":false}]},\"timestamp\":\"2026-10-22T14:44:00.605Z\",\"tool_use_result\":{\"stdout\":\"\",\"stderr\":\"\",\"interrupted\":false,\"isImage\":false,\"noOutputExpected\":false,\"backgroundTaskId\":\"bxlfet4mx\"}}" },
  { ord: 959, line: "{\"type\":\"system\",\"subtype\":\"task_updated\",\"task_id\":\"bxlfet4mx\",\"patch\":{\"status\":\"completed\",\"end_time\":1792680402695}}" },
  { ord: 960, line: "{\"type\":\"system\",\"subtype\":\"task_notification\",\"task_id\":\"bxlfet4mx\",\"tool_use_id\":\"toolu_01UMHFePCdB6PPS9VGsaSp5k\",\"status\":\"completed\",\"output_file\":\"/private/tmp/claude-501/-Users-jirkab-code-agentic-ide-go-wails/46b14cea-9865-47c9-9962-41b827cb8242/tasks/bxlfet4mx.output\",\"summary\":\"Background command \\\"Build, sign, notarize and publish v0.6.0\\\" completed (exit code 0)\"}" },
  { ord: 961, line: "{\"type\":\"system\",\"subtype\":\"background_tasks_changed\",\"tasks\":[]}" },
];

describe("background tasks", () => {
  it("tracks a run_in_background task through TaskStop", () => {
    const [t] = deriveBgTasks(STOPPED);
    expect(t).toMatchObject({
      id: "b14xkae6q",
      toolUseId: "toolu_01G63Q74aFPtEEDYLFQKcLph",
      description: "Run Vite dev server",
      command: "pnpm dev",
      status: "killed",
      endedAt: 1792741576106,
      outputFile: "/private/tmp/claude-501/-Users-jirkab-code-eshop-Administration/7b112814-ead4-4d46-bfd3-a1d07d29bc3c/tasks/b14xkae6q.output",
    });
    expect(t.startedAt).toBe(Date.parse("2026-10-23T07:44:07.043Z"));
  });

  it("is running while only the start has been seen", () => {
    const tasks = deriveBgTasks(STOPPED.slice(0, 4));
    expect(tasks).toHaveLength(1);
    expect(tasks[0].status).toBe("running");
    expect(tasks[0].endedAt).toBeNull();
  });

  it("includes a foreground Bash only once it is moved to the background", () => {
    expect(deriveBgTasks(MOVED.slice(0, 2))).toEqual([]);
    const [t] = deriveBgTasks(MOVED);
    expect(t).toMatchObject({ id: "bj4g2jief", description: "Register plugin in app config", status: "completed", exitCode: 0 });
    // started = the tool_result's timestamp minus timedOutAfterMs
    expect(t.startedAt).toBe(Date.parse("2026-10-25T21:06:50.438Z") - 120000);
    expect(t.outputFile).toMatch(/bj4g2jief\.output$/);
  });

  it("reads the exit code from the notification summary", () => {
    const [t] = deriveBgTasks(COMPLETED);
    expect(t).toMatchObject({ id: "bxlfet4mx", status: "completed", exitCode: 0, endedAt: 1792680402695 });
    expect(t.command).toBe("just release minor 2>&1 | tail -40");
  });

  it("marks a non-zero exit as failed", () => {
    const lines = COMPLETED.map((l) => ({ ...l, line: l.line.replace("exit code 0", "exit code 2") }));
    const [t] = deriveBgTasks(lines.filter((l) => !l.line.includes('"subtype":"task_updated"')));
    expect(t).toMatchObject({ status: "failed", exitCode: 2 });
  });

  it("ends a running task the live snapshot no longer lists", () => {
    const lines = [...STOPPED.slice(0, 4), { ord: 900, line: '{"type":"system","subtype":"background_tasks_changed","tasks":[]}' }];
    expect(deriveBgTasks(lines)[0].status).toBe("ended");
    // a later notification still gives the precise outcome
    const [t] = deriveBgTasks([...lines, ...STOPPED.slice(4)]);
    expect(t.status).toBe("killed");
  });

  it("folds incrementally and ignores junk", () => {
    const s = emptyBgTaskState();
    applyBgTaskLine(s, "not json", 1);
    applyBgTaskLine(s, '{"type":"system","subtype":"task_started","task_id":"x","is_backgrounded":true,"task_type":"local_agent"}', 2);
    expect(listBgTasks(s)).toEqual([]);
    for (const l of COMPLETED) applyBgTaskLine(s, l.line, l.ord);
    expect(listBgTasks(s).map((t) => t.id)).toEqual(["bxlfet4mx"]);
    expect(s.lastOrd).toBe(961);
  });
});

describe("Codex background terminals", () => {
  // Command / cwd / processId as Codex 0.164.0 reported them in chat_stream
  // (a unifiedExecStartup commandExecution of chat 229).
  const server = {
    itemId: "exec-1376590c-baad-428e-8eb6-e1575a3d4862",
    processId: "83564",
    command: "/bin/zsh -lc 'PORT=8801 .venv/bin/python webui.py'",
    cwd: "/Users/jirkab/code/fun/suno-local",
  };

  it("unwraps Codex's login-shell wrapper", () => {
    expect(unwrapShellCommand(server.command)).toBe("PORT=8801 .venv/bin/python webui.py");
    expect(unwrapShellCommand("/bin/zsh -lc 'echo '\\''hi'\\'''")).toBe("echo 'hi'");
    expect(unwrapShellCommand("pnpm dev")).toBe("pnpm dev");
  });

  it("lists live terminals as running, keyed by processId", () => {
    const rows = mergeCodexTerminals([], [server]);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      id: "83564", toolUseId: server.itemId, command: "PORT=8801 .venv/bin/python webui.py",
      cwd: server.cwd, status: "running", outputFile: "", startedAt: null,
    });
  });

  it("keeps a terminal that left the list as ended, in first-seen order", () => {
    const other = { ...server, processId: "59911", command: "/bin/zsh -lc 'pnpm dev'" };
    let rows = mergeCodexTerminals([], [server]);
    rows = mergeCodexTerminals(rows, [server, other]);
    expect(rows.map((r) => [r.id, r.status])).toEqual([["83564", "running"], ["59911", "running"]]);
    rows = mergeCodexTerminals(rows, [other]);
    expect(rows.map((r) => [r.id, r.status])).toEqual([["83564", "ended"], ["59911", "running"]]);
    rows = mergeCodexTerminals(rows, []);
    expect(rows.every((r) => r.status === "ended")).toBe(true);
  });
});
