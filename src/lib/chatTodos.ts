import type { ChatMessage } from "@/lib/chatTypes";

// The live plan of a chat: Claude's TodoWrite replaces the whole list on every
// call, so the latest call in the transcript IS the current state.

export type TodoStatus = "pending" | "in_progress" | "completed";

export interface TodoItem {
  content: string;
  status: TodoStatus;
  activeForm?: string;
}

function parseTodo(raw: unknown): TodoItem | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const content = typeof r.content === "string" ? r.content : "";
  if (!content) return null;
  const status: TodoStatus =
    r.status === "in_progress" || r.status === "completed" ? r.status : "pending";
  const activeForm = typeof r.activeForm === "string" && r.activeForm ? r.activeForm : undefined;
  return { content, status, ...(activeForm ? { activeForm } : {}) };
}

/** Todos of the most recent native `TodoWrite` call, or [] when there is none. */
export function latestTodos(messages: readonly ChatMessage[]): TodoItem[] {
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "tool" || !m.toolRawName || m.text !== "TodoWrite") continue;
    const todos = m.toolInput?.todos;
    if (!Array.isArray(todos)) return [];
    return todos.map(parseTodo).filter((t): t is TodoItem => t !== null);
  }
  return [];
}
