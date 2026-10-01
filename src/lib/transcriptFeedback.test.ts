import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { effectScope, nextTick, shallowRef } from "vue";
import { useTranscriptFeedback } from "@/composables/useTranscriptFeedback";

describe("transcript feedback", () => {
  beforeEach(() => {
    const storage = new Map<string, string>();
    vi.stubGlobal("localStorage", {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("preserves quotes and drafts until a send succeeds; only consumes that batch", () => {
    const scope = effectScope();
    const feedback = scope.run(() => useTranscriptFeedback(42))!;
    feedback.add(7, "Use the cache", { quote: "cache", before: "Use the ", after: "" }, "Invalidate it first");
    const batch = [...feedback.pending.value];
    const prompt = feedback.compose(batch, "Also add a regression test");
    expect(prompt).toContain("> cache");
    expect(prompt).toContain("Context before: Use the ");
    expect(prompt).toContain("Invalidate it first");
    expect(prompt).toContain("Also add a regression test");
    // No acceptance receipt: the same comments remain pending after reconnect.
    const restoredScope = effectScope();
    const restored = restoredScope.run(() => useTranscriptFeedback(42))!;
    expect(restored.pending.value).toHaveLength(1);
    feedback.add(8, "Run tests", { quote: "tests", before: "Run ", after: "" }, "Include the integration test");
    feedback.markSent(batch, prompt);
    expect(feedback.pending.value).toHaveLength(1);
    const sent = feedback.notes.value.find((note) => note.id === batch[0].id)!;
    expect(feedback.deliveries.value[sent.delivery!].text).toBe(prompt);
    scope.stop();
    restoredScope.stop();
  });

  it("isolates drafts between chats and allows removing unsent notes", async () => {
    const scope = effectScope();
    const id = shallowRef(1);
    const feedback = scope.run(() => useTranscriptFeedback(id))!;
    feedback.add(1, "one", { quote: "one", before: "", after: "" }, "Fix this");
    id.value = 2;
    await nextTick();
    expect(feedback.pending.value).toHaveLength(0);
    id.value = 1;
    await nextTick();
    expect(feedback.pending.value).toHaveLength(1);
    feedback.remove(feedback.pending.value[0].id);
    expect(feedback.pending.value).toHaveLength(0);
    scope.stop();
  });
  it("persists resolve state and reopens without duplicating an accepted delivery", () => {
    const scope = effectScope();
    const feedback = scope.run(() => useTranscriptFeedback(42))!;
    feedback.add(7, "Original", { quote: "Original", before: "", after: "" }, "Fix this");
    const note = feedback.pending.value[0];
    feedback.setResolved(note.id, true);
    expect(feedback.pending.value).toHaveLength(0);
    feedback.setResolved(note.id, false);
    expect(feedback.pending.value).toHaveLength(1);
    feedback.markSent([...feedback.pending.value], "Sent review");
    feedback.setResolved(note.id, true);
    const restoredScope = effectScope();
    const restored = restoredScope.run(() => useTranscriptFeedback(42))!;
    expect(restored.notes.value[0].resolvedAt).toBeGreaterThan(0);
    restored.setResolved(note.id, false);
    expect(restored.pending.value).toHaveLength(0);
    expect(restored.notes.value[0].quote).toBe("Original");
    expect(restored.deliveries.value[restored.notes.value[0].delivery!].text).toBe("Sent review");
    scope.stop(); restoredScope.stop();
  });

});
