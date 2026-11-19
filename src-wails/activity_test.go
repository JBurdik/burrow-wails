package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"burrow/internal/control"
)

func TestActivityAppendReadPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.Local)
	old := filepath.Join(dir, "2026-10-01.jsonl")
	if err := os.WriteFile(old, []byte(`{"kind":"click"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	activityPrunedOn = ""
	batch := []control.ActivityEntry{
		{Kind: "click", Label: "Settings", Surface: "sidebar"},
		{Kind: "click", Label: "Settings", Surface: "sidebar"},
		{Kind: "hang", Ms: 1500, Route: "/ws/1"},
		{Label: "no kind, dropped"},
	}
	if err := appendActivity(dir, batch, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("file older than 30 days was not pruned")
	}
	got := control.ReadActivity(dir, 1, now)
	if len(got) != 3 || got[0].At == 0 {
		t.Fatalf("entries = %+v", got)
	}
	sum := control.SummarizeActivity(got)
	if sum["longestHangMs"].(int64) != 1500 || sum["kinds"].(map[string]int)["click"] != 2 {
		t.Fatalf("summary = %+v", sum)
	}
	if err := appendActivity(dir, []control.ActivityEntry{{Kind: "click", Label: strings.Repeat("x", 500)}}, now); err != nil {
		t.Fatal(err)
	}
	if got := control.ReadActivity(dir, 1, now); len([]rune(got[3].Label)) != activityMaxString {
		t.Fatal("label not clipped")
	}
}
