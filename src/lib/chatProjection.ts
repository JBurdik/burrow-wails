import type { ChatMessage } from "@/lib/chatTypes";

// The provider-neutral events on `chat-event-{chatId}` (parsed in Go,
// src-wails/providerruntime.go). The client no longer folds them into a
// transcript — Go does, and clients render its splices (transcriptSplice.ts,
// ADR 0001). Events drive live state only: busy, notifications, usage.

/** One provider-neutral event, as emitted on `chat-event-{chatId}`. */
export interface ChatEvent {
  type: string;
  messageId?: string;
  text?: string;
  /** Claude stream-json record uuid (text/thinking/tool events). */
  uuid?: string;
  toolCallId?: string;
  name?: string;
  input?: Record<string, unknown>;
  output?: string;
  failed?: boolean;
  inputTokens?: number;
  outputTokens?: number;
  contextTokens?: number;
  contextWindow?: number;
  cacheReadTokens?: number;
  cacheCreationTokens?: number;
  costUsd?: number;
  message?: string;
  title?: string;
  sessionId?: string;
  /** session.reset: the transcript was wiped too (chat_clear). */
  cleared?: boolean;
  /** message.note: a row Burrow authored (e.g. "spawned a sub-agent"). */
  role?: ChatMessage["role"];
  images?: string[];
  /** user.delta: the client id the submitting client gave the message (chat_submit). */
  clientId?: string;
  subagentChatId?: number;
  subagentAgent?: string;
  surface?: string;
}

export interface ChatEventBatch {
  ord: number;
  events: ChatEvent[];
}
