package main

import (
	"regexp"
	"strings"
	"sync"
)

// chatUsage is a small, durable projection of context.usage events. The raw
// chat_stream remains the source of truth: scanned_ord makes the projection
// cheap to bring up to date without counting an already-recorded event twice.
func usageSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_usage (
			chat_id INTEGER PRIMARY KEY,
			scanned_ord INTEGER NOT NULL DEFAULT -1,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_creation_tokens INTEGER NOT NULL DEFAULT 0
		)`,
	}
}

// ChatUsage is the accumulated provider usage for one Burrow chat. Cost is an
// estimate: the provider stream does not carry cache TTL, so every cache write
// is charged at the documented five-minute rate.
type ChatUsage struct {
	ChatID              int64   `json:"chat_id"`
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	Priced              bool    `json:"priced"`
}

// ChatUsageReport is the one frontend-facing usage read. Its total deliberately
// contains only chats whose model has a known price, rather than assigning an
// invented rate to an unrecognised provider or future model id.
type ChatUsageReport struct {
	Chats            []ChatUsage `json:"chats"`
	EstimatedCostUSD float64     `json:"estimated_cost_usd"`
}

type modelPricing struct {
	input, output, cacheRead, cacheWrite float64 // USD per million tokens
}

// claudeModelPricing is the single source for every price used by chat usage.
// Cache writes in context.usage do not expose a TTL, so cacheWrite is the 5m
// rate. The stream does expose cache reads separately and they are billed at
// their own rate.
var claudeModelPricing = map[string]modelPricing{
	"claude-fable-5":    {10, 50, 1, 12.5},
	"claude-opus-5":     {5, 25, 0.5, 6.25},
	"claude-sonnet-5":   {2, 10, 0.2, 2.5},
	"claude-opus-4-8":   {5, 25, 0.5, 6.25},
	"claude-opus-4-7":   {5, 25, 0.5, 6.25},
	"claude-opus-4-6":   {5, 25, 0.5, 6.25},
	"claude-opus-4-5":   {5, 25, 0.5, 6.25},
	"claude-opus-4-1":   {15, 75, 1.5, 18.75},
	"claude-opus-4":     {15, 75, 1.5, 18.75},
	"claude-sonnet-4-6": {3, 15, 0.3, 3.75},
	"claude-sonnet-4-5": {3, 15, 0.3, 3.75},
	"claude-sonnet-4":   {3, 15, 0.3, 3.75},
	"claude-sonnet-3-7": {3, 15, 0.3, 3.75},
	"claude-sonnet-3-5": {3, 15, 0.3, 3.75},
	"claude-haiku-4-5":  {1, 5, 0.1, 1.25},
	"claude-haiku-3-5":  {0.8, 4, 0.08, 1},
	"claude-haiku-3":    {0.25, 1.25, 0.03, 0.3},
}

var legacyBaseOpus4 = regexp.MustCompile(`opus-4(?:$|-thinking$|-20\d{6}(?:-thinking)?$|@20\d{6}$)`)

// normalizeModelForPricing uses version boundaries, never prefixes. In
// particular, sonnet-50 is not Sonnet 5 and opus-4.9 is not Opus 4.1.
func normalizeModelForPricing(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	lower = strings.TrimPrefix(lower, "anthropic/")
	lower = strings.TrimPrefix(lower, "anthropic:")
	if lower == "" {
		return ""
	}
	normalized := strings.ReplaceAll(lower, ".", "-")
	boundary := func(name string) bool {
		return regexp.MustCompile(regexp.QuoteMeta(name)+`(?:$|[^0-9])`).FindStringIndex(normalized) != nil
	}

	// Check the most specific / highest-price families first. This preserves the
	// reference behaviour for names such as "claude-sonnet-5-opus-5".
	for _, name := range []string{"claude-fable-5", "claude-opus-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-opus-4-5", "claude-opus-4-1"} {
		if boundary(strings.TrimPrefix(name, "claude-")) {
			return name
		}
	}
	if legacyBaseOpus4.MatchString(normalized) {
		return "claude-opus-4"
	}
	// Unknown later Opus 4 point releases use the current Opus 4 rate, but only
	// hyphenated provider IDs reach this fallback; claude.opus.4.9 remains unknown.
	if strings.Contains(lower, "opus-4") {
		return "claude-opus-4-8"
	}
	if boundary("sonnet-5") {
		return "claude-sonnet-5"
	}
	for _, name := range []string{"claude-sonnet-4-6", "claude-sonnet-4-5"} {
		if boundary(strings.TrimPrefix(name, "claude-")) {
			return name
		}
	}
	if strings.Contains(lower, "sonnet-4") {
		return "claude-sonnet-4-6"
	}
	if strings.Contains(lower, "sonnet-3-7") || strings.Contains(lower, "sonnet-3.7") {
		return "claude-sonnet-3-7"
	}
	if strings.Contains(lower, "sonnet-3-5") || strings.Contains(lower, "sonnet-3.5") || strings.Contains(lower, "3-5-sonnet") || strings.Contains(lower, "3.5-sonnet") {
		return "claude-sonnet-3-5"
	}
	if strings.Contains(lower, "haiku-4-5") {
		return "claude-haiku-4-5"
	}
	if strings.Contains(lower, "haiku-3-5") || strings.Contains(lower, "haiku-3.5") || strings.Contains(lower, "3-5-haiku") || strings.Contains(lower, "3.5-haiku") {
		return "claude-haiku-3-5"
	}
	if strings.Contains(lower, "haiku-3") {
		return "claude-haiku-3"
	}
	return ""
}

func estimateCostUSD(model string, input, output, cacheRead, cacheCreation int64) (float64, bool) {
	price, ok := claudeModelPricing[normalizeModelForPricing(model)]
	if !ok {
		return 0, false
	}
	return (float64(input)*price.input + float64(output)*price.output + float64(cacheRead)*price.cacheRead + float64(cacheCreation)*price.cacheWrite) / 1_000_000, true
}

var usageMu sync.Mutex

type usageDelta struct {
	lastOrd                               int64
	input, output, cacheRead, cacheCreate int64
}

func (a *App) scanChatUsage() error {
	if a.db == nil {
		return nil
	}
	usageMu.Lock()
	defer usageMu.Unlock()

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT s.chat_id, s.ord, s.kind, s.line
		FROM chat_stream s LEFT JOIN chat_usage u ON u.chat_id = s.chat_id
		WHERE s.ord > COALESCE(u.scanned_ord, -1) ORDER BY s.chat_id, s.ord`)
	if err != nil {
		return err
	}
	deltas := map[string]*usageDelta{}
	for rows.Next() {
		var chatID, kind, line string
		var ord int64
		if err := rows.Scan(&chatID, &ord, &kind, &line); err != nil {
			rows.Close()
			return err
		}
		delta := deltas[chatID]
		if delta == nil {
			delta = &usageDelta{}
			deltas[chatID] = delta
		}
		delta.lastOrd = ord
		for _, event := range NormalizeChatLine(kind, line, ord) {
			if event.Type != EvtContextUsage {
				continue
			}
			delta.input += int64(event.InputTokens)
			delta.output += int64(event.OutputTokens)
			delta.cacheRead += int64(event.CacheReadTokens)
			delta.cacheCreate += int64(event.CacheCreationTokens)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for chatID, delta := range deltas {
		if _, err := tx.Exec(`INSERT INTO chat_usage (chat_id, scanned_ord, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(chat_id) DO UPDATE SET
				scanned_ord = excluded.scanned_ord,
				input_tokens = chat_usage.input_tokens + excluded.input_tokens,
				output_tokens = chat_usage.output_tokens + excluded.output_tokens,
				cache_read_tokens = chat_usage.cache_read_tokens + excluded.cache_read_tokens,
				cache_creation_tokens = chat_usage.cache_creation_tokens + excluded.cache_creation_tokens`,
			chatID, delta.lastOrd, delta.input, delta.output, delta.cacheRead, delta.cacheCreate); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetChatUsage catches the durable projection up on every read, so old stream
// rows are included after an upgrade and new rows survive an app restart.
func (a *App) GetChatUsage() (ChatUsageReport, error) {
	report := ChatUsageReport{Chats: []ChatUsage{}}
	if a.db == nil {
		return report, nil
	}
	if err := a.scanChatUsage(); err != nil {
		return report, err
	}
	rows, err := a.db.Query(`SELECT u.chat_id, c.model, u.input_tokens, u.output_tokens,
		u.cache_read_tokens, u.cache_creation_tokens
		FROM chat_usage u JOIN chats c ON u.chat_id = c.id ORDER BY u.chat_id`)
	if err != nil {
		return report, err
	}
	defer rows.Close()
	for rows.Next() {
		var usage ChatUsage
		if err := rows.Scan(&usage.ChatID, &usage.Model, &usage.InputTokens, &usage.OutputTokens, &usage.CacheReadTokens, &usage.CacheCreationTokens); err != nil {
			return report, err
		}
		usage.EstimatedCostUSD, usage.Priced = estimateCostUSD(usage.Model, usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreationTokens)
		if usage.Priced {
			report.EstimatedCostUSD += usage.EstimatedCostUSD
		}
		report.Chats = append(report.Chats, usage)
	}
	return report, rows.Err()
}
