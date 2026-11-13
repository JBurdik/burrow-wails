// What a finished turn needs from the user: the agent's end_turn call
// (src-wails/internal/control/verbs_turn.go), stored on chats.turn_needs.

export interface TurnAsk {
  question: string;
  options?: string[];
  /** details[i]: one line on what picking options[i] leads to. */
  details?: string[];
  /** Index into options the agent would pick; -1 = none. */
  suggested: number;
  /** A command that is the user's to run (Run on the card); state is Go's. */
  run?: { command: string; state?: "running" | "done"; exit?: number; interrupted?: boolean };
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

/**
 * One message answering the asks: each answered question quoted with its
 * answer, unanswered ones left out, then anything typed in the composer.
 * A single ask answered alone is sent as just the answer.
 */
export function composeAnswers(asks: TurnAsk[], answers: (string | undefined)[], extra = ""): string {
  const answered = asks
    .map((a, i) => ({ q: a.question, a: answers[i]?.trim() }))
    .filter((x): x is { q: string; a: string } => !!x.a);
  const tail = extra.trim();
  if (asks.length === 1 && answered.length === 1) return [answered[0].a, tail].filter(Boolean).join("\n\n");
  const body = answered.map((x) => `> ${x.q.replace(/\n/g, "\n> ")}\n${x.a}`).join("\n\n");
  return [body, tail].filter(Boolean).join("\n\n");
}

/** The follow-ups the user ticked on the Done card, as one message. */
export function composeNext(picked: string[], extra = ""): string {
  const tail = extra.trim();
  const body = picked.length === 1 ? picked[0] : picked.map((p) => `- ${p}`).join("\n");
  return [body, tail].filter(Boolean).join("\n\n");
}
