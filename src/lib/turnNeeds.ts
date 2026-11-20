// What a finished turn needs from the user: the agent's end_turn call
// (src-wails/internal/control/verbs_turn.go), stored on chats.turn_needs.

export interface TurnAsk {
  question: string;
  options?: string[];
  /** details[i]: one line on what picking options[i] leads to. */
  details?: string[];
  /** Index into options the agent would pick; -1 = none. */
  suggested: number;
  /** Several options may be picked (checkboxes); the answer joins them. */
  multi?: boolean;
  /** A command that is the user's to run (Run on the card); state is Go's. */
  /** reply: the result kept on a multi-ask card, sent with the other answers. */
  run?: { command: string; state?: "running" | "done"; exit?: number; interrupted?: boolean; reply?: string };
}

export interface TurnNeeds {
  needs: "you" | "nothing";
  asks?: TurnAsk[];
  next?: string[];
}

/** Still open on a finished chat, checked by Go (chat_loose_ends). */
export interface LooseEnd {
  kind: "uncommitted" | "unpushed" | "pr" | "subagent" | "schedule";
  text: string;
  chat_id?: number;
}

export function parseTurnNeeds(raw: string | undefined | null): TurnNeeds | undefined {
  if (!raw) return undefined;
  try {
    const n = JSON.parse(raw) as TurnNeeds;
    return n && (n.needs === "you" || n.needs === "nothing") ? n : undefined;
  } catch {
    return undefined;
  }
}

/** One ask's answer for chat_answer_turn: picked option labels, else free text. */
export interface TurnPick {
  index: number;
  options?: string[];
  text?: string;
  /** Done card: these follow-ups are asked about, not ticked. */
  ask?: boolean;
}

/**
 * What the card sends to chat_answer_turn (Go composes the message):
 * answers[i] is the picked option ("A, B" for a multi ask) or, with
 * typed[i], the user's own words. Unanswered asks are left out.
 */
export function turnPicks(asks: TurnAsk[], answers: (string | undefined)[], typed: boolean[]): TurnPick[] {
  return asks.flatMap((a, i): TurnPick[] => {
    const v = answers[i]?.trim();
    if (!v) return [];
    if (typed[i] || !a.options?.length) return [{ index: i, text: v }];
    return [{ index: i, options: a.multi ? a.options.filter((o) => v.split(", ").includes(o)) : [v] }];
  });
}

/**
 * What the echo / a held answer shows until Go's composed text is known
 * (chat_answer_turn's reply). Only a preview: Go's composeTurnAnswer is the
 * message (questions quoted, kept run replies added).
 */
export function answerPreview(picks: TurnPick[], extra: string): string {
  return [...picks.map((p) => (p.ask ? (p.options ?? []).map((o) => `What do you mean by: ${o}?`).join("\n") : p.options?.length ? p.options.join(", ") : (p.text ?? "").trim())), extra.trim()]
    .filter(Boolean)
    .join("\n\n");
}
