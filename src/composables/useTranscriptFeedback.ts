import { computed, shallowRef, toValue, watch, type MaybeRefOrGetter } from "vue";
import { composeTranscriptFeedback, type TranscriptNote, type TranscriptQuote, type FeedbackDelivery } from "@/lib/transcriptFeedback";

export function useTranscriptFeedback(chatId: MaybeRefOrGetter<number>) {
  const notes = shallowRef<TranscriptNote[]>([]);
  const deliveries = shallowRef<Record<string, FeedbackDelivery>>({});
  const error = shallowRef("");
  const pending = computed(() => notes.value.filter((note) => !note.delivery && !note.resolvedAt));
  const key = computed(() => `burrow.transcriptNotes.v1:${toValue(chatId)}`);

  watch(key, (value) => {
    error.value = "";
    notes.value = [];
    deliveries.value = {};
    try {
      const saved = JSON.parse(localStorage.getItem(value) ?? "{}");
      if (Array.isArray(saved.notes)) notes.value = saved.notes.filter((note: TranscriptNote) =>
        typeof note.id === "string" && Number.isInteger(note.messageId) && typeof note.body === "string" && typeof note.source === "string" && typeof note.quote === "string" && typeof note.before === "string" && typeof note.after === "string");
      if (saved.deliveries && typeof saved.deliveries === "object") deliveries.value = saved.deliveries;
    } catch { error.value = "Saved comments could not be loaded."; }
  }, { immediate: true });

  function persist() {
    try {
      localStorage.setItem(key.value, JSON.stringify({ notes: notes.value, deliveries: deliveries.value }));
      error.value = "";
    } catch { error.value = "Comments could not be saved on this device. Keep this chat open until you send them."; }
  }

  function add(messageId: number, source: string, quote: TranscriptQuote, body: string) {
    if (!quote.quote.trim() || !body.trim()) return;
    notes.value = [...notes.value, { id: crypto.randomUUID(), messageId, source, ...quote, body: body.trim() }];
    persist();
  }

  function remove(id: string) {
    notes.value = notes.value.filter((note) => note.id !== id || note.delivery);
    persist();
  }

  function markSent(batch: TranscriptNote[], text: string) {
    const delivery = crypto.randomUUID();
    const ids = new Set(batch.map((note) => note.id));
    notes.value = notes.value.map((note) => ids.has(note.id) && !note.delivery ? { ...note, delivery } : note);
    deliveries.value = { ...deliveries.value, [delivery]: { text, sentAt: Date.now() } };
    persist();
  }

  function setResolved(id: string, resolved: boolean) {
    notes.value = notes.value.map((note) => note.id === id ? { ...note, resolvedAt: resolved ? Date.now() : 0 } : note);
    persist();
  }

  return { notes, deliveries, error, pending, add, remove, markSent, setResolved, compose: composeTranscriptFeedback };
}
