import { describe, expect, it } from "vitest";
import { createHost, defineExtension, getContext, requireWorkspace } from "../src/index";

describe("Burrow SDK", () => {
  it("validates an extension and reads its workspace context", () => {
    const extension = defineExtension({
      manifest: { apiVersion: 1, id: "sftp-sync", name: "SFTP Sync", version: "0.1.0" },
    });
    expect(extension.manifest.id).toBe("sftp-sync");
    expect(requireWorkspace(getContext({
      BURROW_EXTENSION_ID: "sftp-sync",
      BURROW_EXTENSION_DIR: "/extensions/sftp-sync",
      BURROW_EXTENSION_CWD: "/project",
    }))).toBe("/project");
  });

  it("rejects command paths before packaging", () => {
    expect(() => defineExtension({
      manifest: {
        apiVersion: 1, id: "unsafe", name: "Unsafe", version: "0.1.0",
        commands: [{ id: "run", title: "Run", command: "./run.sh" }],
      },
    })).toThrow("PATH");
  });

  it("sends capability requests through the one-run host bridge", async () => {
    const originalFetch = globalThis.fetch;
    const requests: Request[] = [];
    globalThis.fetch = async (input, init) => {
      requests.push(new Request(input, init));
      return new Response(JSON.stringify({ cwd: "/project" }), { status: 200 });
    };
    try {
      await expect(createHost({
        BURROW_EXTENSION_BRIDGE_URL: "http://127.0.0.1:40000",
        BURROW_EXTENSION_BRIDGE_TOKEN: "token",
      }).workspace()).resolves.toEqual({ cwd: "/project" });
      expect(requests[0]?.headers.get("Authorization")).toBe("Bearer token");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
