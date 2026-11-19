package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"burrow/internal/control"
)

// Activity log + hang watch (src/lib/activityLog.ts): the desktop batches
// entries and flushes them here every few seconds. One JSON line each into
// <app-data>/activity/YYYY-MM-DD.jsonl; files older than 30 days are deleted.
// Never sent anywhere; read back by the local-only `activity` verb.

const (
	activityKeepDays  = 30
	activityMaxBatch  = 500
	activityMaxString = 200
)

var (
	activityMu       sync.Mutex
	activityPrunedOn string
)

func (a *App) ActivityAppend(entries json.RawMessage) error {
	var batch []control.ActivityEntry
	if err := json.Unmarshal(entries, &batch); err != nil {
		return err
	}
	if len(batch) > activityMaxBatch {
		return errors.New("activity: batch too large")
	}
	dataDir, err := appDataDir()
	if err != nil {
		return err
	}
	return appendActivity(filepath.Join(dataDir, "activity"), batch, time.Now())
}

func appendActivity(dir string, batch []control.ActivityEntry, now time.Time) error {
	if len(batch) == 0 {
		return nil
	}
	var b strings.Builder
	for _, e := range batch {
		// Re-encoding through the struct drops any field it doesn't name.
		if e.Kind == "" {
			continue
		}
		if e.At == 0 {
			e.At = now.UnixMilli()
		}
		e.Kind, e.Label, e.Surface = clipActivity(e.Kind), clipActivity(e.Label), clipActivity(e.Surface)
		e.Route, e.Mode, e.Last = clipActivity(e.Route), clipActivity(e.Mode), clipActivity(e.Last)
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	activityMu.Lock()
	defer activityMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	today := now.Format("2006-01-02")
	if activityPrunedOn != today {
		activityPrunedOn = today
		pruneActivity(dir, now)
	}
	f, err := os.OpenFile(filepath.Join(dir, today+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(b.String())
	return err
}

// pruneActivity deletes daily files dated before now − 30 days.
func pruneActivity(dir string, now time.Time) {
	cutoff := now.AddDate(0, 0, -activityKeepDays).Format("2006-01-02")
	names, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for _, p := range names {
		if day := strings.TrimSuffix(filepath.Base(p), ".jsonl"); len(day) == 10 && day < cutoff {
			os.Remove(p)
		}
	}
}

func clipActivity(s string) string {
	if r := []rune(s); len(r) > activityMaxString {
		return string(r[:activityMaxString])
	}
	return s
}
