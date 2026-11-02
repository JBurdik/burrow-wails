// Subscription usage limits (Go: src-wails/planusage.go, command `plan_usage`).

export interface PlanUsageRow {
  label: string;
  percent: number;
  resets_at?: string;
}

export interface PlanUsageProvider {
  kind: "claude" | "codex";
  plan: string;
  status: "ok" | "unavailable" | "not_applicable";
  reason?: string;
  rows: PlanUsageRow[];
  extra?: string;
}

export interface PlanUsage {
  providers: PlanUsageProvider[];
}

/** The binding constraint: the highest percent of any row of any working provider. */
export function peakPercent(u: PlanUsage | null): number | null {
  let peak: number | null = null;
  for (const p of u?.providers ?? []) {
    if (p.status !== "ok") continue;
    for (const r of p.rows) peak = Math.max(peak ?? 0, r.percent);
  }
  return peak;
}

export function usageLevel(pct: number): "ok" | "warning" | "exceeded" {
  return pct >= 90 ? "exceeded" : pct >= 70 ? "warning" : "ok";
}

/** "in 2h 14m" under a day, else "Mon 15:00". */
export function formatReset(iso: string | undefined, now = Date.now()): string {
  if (!iso) return "";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const ms = t - now;
  if (ms <= 0) return "now";
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `in ${mins}m`;
  if (mins < 24 * 60) return `in ${Math.floor(mins / 60)}h ${mins % 60}m`;
  const d = new Date(t);
  const day = d.toLocaleDateString(undefined, { weekday: "short" });
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${day} ${time}`;
}

export function providerName(kind: string): string {
  return kind === "codex" ? "Codex" : kind === "claude" ? "Claude" : kind;
}
