package control

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ActivityEntry is one line of <app-data>/activity/YYYY-MM-DD.jsonl: what the
// user did in the desktop app (kind click / route / shortcut / send / answer)
// or a UI stall (kind hang). Never typed text.
type ActivityEntry struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Surface string `json:"surface,omitempty"`
	Route   string `json:"route,omitempty"`
	Ms      int64  `json:"ms,omitempty"`
	Images  int    `json:"images,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Last    string `json:"last,omitempty"`
}

const activityTail = 500

func activityVerbs(c *Core) []Verb {
	return []Verb{{
		Name:    "activity",
		Summary: "Read the user's activity log (opt-in, Settings → General: clicks, routes, shortcuts, sends; plus UI hangs). --summary = counts of what is used most",
		Args: []Arg{
			{Name: "summary", Type: "bool", Desc: "Counts by kind + label instead of entries"},
			{Name: "days", Type: "int", Desc: "How many days back, today included (default 1, ≤ 30)"},
		},
		Scope: ScopeLocal,
		Fn: func(_ context.Context, p Params) (any, error) {
			if c.deps.ActivityDir == "" {
				return nil, fmt.Errorf("activity: not available")
			}
			entries := ReadActivity(c.deps.ActivityDir, int(p.Int("days")), time.Now())
			if p.Bool("summary") {
				return SummarizeActivity(entries), nil
			}
			if len(entries) > activityTail {
				entries = entries[len(entries)-activityTail:]
			}
			return map[string]any{"entries": entries, "count": len(entries)}, nil
		},
	}}
}

// ReadActivity returns the entries of the last `days` daily files, oldest
// first. Unreadable lines are skipped.
func ReadActivity(dir string, days int, now time.Time) []ActivityEntry {
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	out := []ActivityEntry{}
	for d := days - 1; d >= 0; d-- {
		f, err := os.Open(filepath.Join(dir, now.AddDate(0, 0, -d).Format("2006-01-02")+".jsonl"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			var e ActivityEntry
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.Kind != "" {
				out = append(out, e)
			}
		}
		f.Close()
	}
	return out
}

type activityCount struct {
	Kind    string `json:"kind"`
	Label   string `json:"label,omitempty"`
	Surface string `json:"surface,omitempty"`
	Count   int    `json:"count"`
}

// SummarizeActivity counts entries by kind + label + surface, most used first
// (top 50), plus per-kind totals and the longest hang.
func SummarizeActivity(entries []ActivityEntry) map[string]any {
	byKey := map[activityCount]int{}
	kinds := map[string]int{}
	var maxHang int64
	for _, e := range entries {
		kinds[e.Kind]++
		if e.Kind == "hang" {
			maxHang = max(maxHang, e.Ms)
			continue
		}
		byKey[activityCount{Kind: e.Kind, Label: e.Label, Surface: e.Surface}]++
	}
	top := make([]activityCount, 0, len(byKey))
	for k, n := range byKey {
		k.Count = n
		top = append(top, k)
	}
	sort.Slice(top, func(i, j int) bool {
		if top[i].Count != top[j].Count {
			return top[i].Count > top[j].Count
		}
		return top[i].Kind+top[i].Label < top[j].Kind+top[j].Label
	})
	if len(top) > 50 {
		top = top[:50]
	}
	return map[string]any{"total": len(entries), "kinds": kinds, "top": top, "longestHangMs": maxHang}
}
