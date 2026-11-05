import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

/**
 * One pending agent secret request, mirrored from Go (src-wails/secretrequests.go).
 * Never carries a value — not the typed one, not a saved one (`hasSaved` only).
 */
export interface SecretRequest {
  id: string;
  owner: string; // "chat:<id>" | "pty:<id>"
  name: string;
  reason: string;
  at: number;
  repo_id: number; // 0 = no project: nothing can be remembered
  hasSaved: boolean;
}

/** Saved project secret: name + dates, never the value. */
export interface SavedSecret {
  name: string;
  created: number;
  updated: number;
}

/**
 * Pending secret requests. The `secret-requests` event carries the WHOLE list
 * (state, not a delta). The typed value goes straight from the card's input to
 * answer_secret_request and nowhere else — this store never holds it.
 */
export const useSecretRequestsStore = defineStore("secretRequests", () => {
  const list = ref<SecretRequest[]>([]);
  const byOwner = computed(() => {
    const m: Record<string, SecretRequest[]> = {};
    for (const r of list.value) (m[r.owner] ??= []).push(r);
    return m;
  });

  async function refresh() {
    try {
      list.value = (await invoke<SecretRequest[]>("list_secret_requests")) ?? [];
    } catch {
      // Not connected yet; the next event or resync repopulates.
    }
  }

  let started = false;
  function start() {
    if (started) return;
    started = true;
    void listen<{ requests: SecretRequest[] }>("secret-requests", (e) => {
      list.value = e.payload.requests ?? [];
    });
    void refresh();
  }

  /** Throws the backend's reason (e.g. already answered) for the card to show. */
  async function answer(r: SecretRequest, value: string, opts: { useSaved?: boolean; remember?: boolean } = {}) {
    await invoke("answer_secret_request", { id: r.id, value, useSaved: !!opts.useSaved, remember: !!opts.remember });
  }

  async function decline(r: SecretRequest) {
    await invoke("decline_secret_request", { id: r.id });
  }

  return { list, byOwner, start, refresh, answer, decline };
});

export async function listSavedSecrets(repoId: number): Promise<SavedSecret[]> {
  return (await invoke<SavedSecret[]>("list_saved_secrets", { repoId })) ?? [];
}

export async function deleteSavedSecret(repoId: number, name: string): Promise<void> {
  await invoke("delete_saved_secret", { repoId, name });
}
