// The category table out of Claude Code's `/context` answer.
//
// The report is markdown and mostly noise: after the table comes one row per
// MCP tool, which on a machine with a few servers is over a thousand lines.
// Only the category table is worth showing, so only it is parsed.
export interface CtxReportRow {
  label: string;
  tokens: string;
  pct: string;
}

export function parseContextReport(text: string): CtxReportRow[] | null {
  const start = text.indexOf("Estimated usage by category");
  if (start < 0) return null;
  const rows: CtxReportRow[] = [];
  for (const line of text.slice(start).split("\n")) {
    if (line.startsWith("###") && rows.length) break;
    const cells = line.split("|").map((c) => c.trim());
    // A markdown row is ["", label, tokens, pct, ""]. The header row and the
    // |---| separator fall out on the numeric test of the token cell.
    if (cells.length < 4 || !/^[\d.]+k?$/.test(cells[2] ?? "")) continue;
    rows.push({ label: cells[1], tokens: cells[2], pct: cells[3] });
  }
  return rows.length ? rows : null;
}
