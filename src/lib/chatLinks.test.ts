import { describe, expect, it } from "vitest";
import { classifyChatLink, localPort, localUrl, prRef, repoKey } from "./chatLinks";

describe("repoKey", () => {
  it("normalizes https, scp-style and ssh remotes to one key", () => {
    expect(repoKey("https://github.com/JBurdik/burrow-wails.git")).toBe("github.com/jburdik/burrow-wails");
    expect(repoKey("git@github.com:JBurdik/burrow-wails.git")).toBe("github.com/jburdik/burrow-wails");
    expect(repoKey("ssh://git@github.com/JBurdik/burrow-wails")).toBe("github.com/jburdik/burrow-wails");
    expect(repoKey("git@gitlab.com:group/sub/repo.git")).toBe("gitlab.com/group/sub/repo");
  });
  it("rejects garbage", () => {
    expect(repoKey("")).toBeNull();
    expect(repoKey("not a url")).toBeNull();
  });
});

describe("prRef", () => {
  it("parses GitHub pull and GitLab merge request links", () => {
    expect(prRef("https://github.com/JBurdik/burrow-wails/pull/42")).toEqual({ repo: "github.com/jburdik/burrow-wails", number: 42 });
    expect(prRef("https://github.com/o/r/pull/7/files#diff-1")).toEqual({ repo: "github.com/o/r", number: 7 });
    expect(prRef("https://gitlab.com/group/sub/repo/-/merge_requests/3")).toEqual({ repo: "gitlab.com/group/sub/repo", number: 3 });
  });
  it("ignores other GitHub pages", () => {
    expect(prRef("https://github.com/o/r/issues/7")).toBeNull();
    expect(prRef("https://github.com/o/r/pulls")).toBeNull();
  });
});

describe("local links", () => {
  it("frames localhost-style hosts, mapping 0.0.0.0 to localhost", () => {
    expect(localUrl("http://localhost:5173/app")).toBe("http://localhost:5173/app");
    expect(localUrl("http://0.0.0.0:3000/")).toBe("http://localhost:3000/");
    expect(localUrl("http://127.0.0.1:8080")).toBe("http://127.0.0.1:8080/");
    expect(localUrl("https://example.com")).toBeNull();
  });
  it("defaults the port from the scheme", () => {
    expect(localPort("http://localhost:5173")).toBe(5173);
    expect(localPort("http://localhost/")).toBe(80);
    expect(localPort("https://127.0.0.1/")).toBe(443);
    expect(localPort("https://example.com:5173")).toBeNull();
  });
});

describe("classifyChatLink", () => {
  const repos = ["github.com/jburdik/burrow-wails"];
  it("routes the workspace's own PR to the PRs surface", () => {
    expect(classifyChatLink("https://github.com/JBurdik/burrow-wails/pull/12", repos)).toEqual({ kind: "pr", number: 12, url: "https://github.com/JBurdik/burrow-wails/pull/12" });
  });
  it("leaves another repo's PR and ordinary sites external", () => {
    expect(classifyChatLink("https://github.com/other/repo/pull/12", repos)).toBeNull();
    expect(classifyChatLink("https://example.com/docs", repos)).toBeNull();
    expect(classifyChatLink("mailto:a@b.c", repos)).toBeNull();
  });
  it("routes a dev server to the Browser surface", () => {
    expect(classifyChatLink("http://localhost:5173/", repos)).toEqual({ kind: "browser", url: "http://localhost:5173/" });
  });
});
