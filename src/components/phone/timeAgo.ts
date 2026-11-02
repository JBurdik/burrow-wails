/** "now" / "5m" / "3h" / "2d" — row timestamps on the phone screens; "" for no stamp. */
export function timeAgo(ts: number, now = Date.now()): string {
  if (!ts) return "";
  const m = Math.floor((now - ts) / 60_000);
  if (m < 1) return "now";
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h` : `${Math.floor(h / 24)}d`;
}
