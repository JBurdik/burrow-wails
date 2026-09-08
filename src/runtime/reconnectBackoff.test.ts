import { describe, it, expect } from "vitest";
import { createBackoff } from "./reconnectBackoff";

describe("createBackoff", () => {
  it("grows and then caps", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => 0 });
    expect(b.next()).toBe(100);
    expect(b.next()).toBe(200);
    expect(b.next()).toBe(400);
    expect(b.next()).toBe(800);
    expect(b.next()).toBe(800);
  });

  it("reset returns to the base delay", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => 0 });
    b.next();
    b.next();
    b.reset();
    expect(b.next()).toBe(100);
    expect(b.attempts()).toBe(1);
  });

  it("applies jitter within the current step", () => {
    // jitter() = 1 must never push the delay past the cap.
    const b = createBackoff({ baseMs: 100, maxMs: 150, jitter: () => 1 });
    for (let i = 0; i < 5; i++) expect(b.next()).toBeLessThanOrEqual(150);
  });

  it("never returns a negative or zero delay", () => {
    const b = createBackoff({ baseMs: 100, maxMs: 800, jitter: () => -1 });
    for (let i = 0; i < 5; i++) expect(b.next()).toBeGreaterThan(0);
  });

  it("counts attempts", () => {
    const b = createBackoff({ jitter: () => 0 });
    expect(b.attempts()).toBe(0);
    b.next();
    b.next();
    expect(b.attempts()).toBe(2);
  });
});
