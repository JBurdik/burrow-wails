/**
 * Snooze presets (ported in principle from t3code's Sidebar.snooze.ts):
 * 1 hour · This evening 18:00 (only while it is still ahead) · Tomorrow 9:00 ·
 * Next Monday 9:00 (a Monday never picks itself). Local time, `now`-injected.
 */
export interface SnoozePreset {
  label: string;
  until: number;
}

function at(base: Date, dayOffset: number, hour: number): number {
  const d = new Date(base.getFullYear(), base.getMonth(), base.getDate() + dayOffset, hour, 0, 0, 0);
  return d.getTime();
}

export function snoozePresets(now: number): SnoozePreset[] {
  const d = new Date(now);
  const presets: SnoozePreset[] = [{ label: "1 hour", until: now + 60 * 60 * 1000 }];
  const evening = at(d, 0, 18);
  if (evening > now) presets.push({ label: "This evening", until: evening });
  presets.push({ label: "Tomorrow 9:00", until: at(d, 1, 9) });
  // getDay(): 0 = Sunday … 1 = Monday. From a Monday that is 7, not 0.
  presets.push({ label: "Next Monday 9:00", until: at(d, ((8 - d.getDay()) % 7) || 7, 9) });
  return presets;
}

/** "18:00" today, "Tue 9:00" within a week, else "3 Nov 9:00". */
export function formatWakeTime(until: number, now: number): string {
  const d = new Date(until);
  const time = `${d.getHours()}:${String(d.getMinutes()).padStart(2, "0")}`;
  const n = new Date(now);
  if (d.toDateString() === n.toDateString()) return time;
  if (until - now < 6 * 24 * 60 * 60 * 1000) return `${d.toLocaleDateString("en", { weekday: "short" })} ${time}`;
  return `${d.getDate()} ${d.toLocaleDateString("en", { month: "short" })} ${time}`;
}
