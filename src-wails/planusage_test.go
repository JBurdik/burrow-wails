package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Real response from a Team account (trimmed to the keys that were non-null
// plus a few nulls).
const claudeTeamUsageFixture = `{"five_hour":{"utilization":4.0,"resets_at":"2026-10-27T12:20:00.227824+00:00"},"seven_day":{"utilization":16.0,"resets_at":"2026-10-31T08:00:00.227842+00:00"},"seven_day_oauth_apps":null,"seven_day_sonnet":null,"seven_day_opus":null,"iguana_necktie":null,"extra_usage":{"is_enabled":true,"monthly_limit":0,"used_credits":0.0,"utilization":null,"currency":"EUR"},"limits":[{"kind":"session","percent":4,"resets_at":"2026-10-27T12:20:00.227824+00:00"},{"kind":"weekly_all","percent":16,"resets_at":"2026-10-31T08:00:00.227842+00:00"},{"kind":"weekly_scoped","percent":0,"resets_at":"2026-10-31T08:00:00.227842+00:00","scope":{"model":{"display_name":"Fable"}}}]}`

func TestParseClaudeUsageTeam(t *testing.T) {
	rows, extra, err := parseClaudeUsage([]byte(claudeTeamUsageFixture))
	if err != nil {
		t.Fatal(err)
	}
	want := []PlanUsageRow{
		{Label: "5-hour limit", Percent: 4, ResetsAt: "2026-10-27T12:20:00.227824+00:00"},
		{Label: "Weekly · all models", Percent: 16, ResetsAt: "2026-10-31T08:00:00.227842+00:00"},
		{Label: "Weekly · Fable", Percent: 0, ResetsAt: "2026-10-31T08:00:00.227842+00:00"},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, rows[i], want[i])
		}
	}
	if extra != "Extra usage enabled" {
		t.Errorf("extra = %q", extra)
	}
}

func TestParseClaudeUsageAllNull(t *testing.T) {
	rows, extra, err := parseClaudeUsage([]byte(`{"five_hour":null,"seven_day":null,"extra_usage":null,"limits":null}`))
	if err != nil || len(rows) != 0 || extra != "" {
		t.Fatalf("rows=%v extra=%q err=%v", rows, extra, err)
	}
}

func TestFetchClaudePlanUsageStatuses(t *testing.T) {
	origCreds, origReq := readClaudeOAuthCreds, claudeUsageRequest
	t.Cleanup(func() { readClaudeOAuthCreds, claudeUsageRequest = origCreds, origReq })
	home := t.TempDir() // no ~/.claude/settings.json → not routed elsewhere
	t.Setenv("HOME", home)

	future := time.Now().Add(time.Hour).UnixMilli()
	readClaudeOAuthCreds = func() (claudeOAuthCreds, error) {
		return claudeOAuthCreds{AccessToken: "SECRETTOKEN", ExpiresAt: future, SubscriptionType: "team"}, nil
	}
	var gotToken string
	claudeUsageRequest = func(_ context.Context, token string) (int, []byte, error) {
		gotToken = token
		return http.StatusOK, []byte(claudeTeamUsageFixture), nil
	}
	p := fetchClaudePlanUsage()
	if p.Status != "ok" || p.Plan != "team" || len(p.Rows) != 3 || gotToken != "SECRETTOKEN" {
		t.Fatalf("ok case: %+v", p)
	}

	claudeUsageRequest = func(context.Context, string) (int, []byte, error) { return 401, nil, nil }
	if p := fetchClaudePlanUsage(); p.Status != "unavailable" || strings.Contains(p.Reason, "SECRETTOKEN") {
		t.Fatalf("401 case: %+v", p)
	}

	readClaudeOAuthCreds = func() (claudeOAuthCreds, error) {
		return claudeOAuthCreds{AccessToken: "SECRETTOKEN", ExpiresAt: time.Now().Add(-time.Minute).UnixMilli()}, nil
	}
	claudeUsageRequest = func(context.Context, string) (int, []byte, error) {
		t.Fatal("expired token must not be sent")
		return 0, nil, nil
	}
	if p := fetchClaudePlanUsage(); p.Status != "unavailable" {
		t.Fatalf("expired case: %+v", p)
	}

	readClaudeOAuthCreds = func() (claudeOAuthCreds, error) { return claudeOAuthCreds{}, os.ErrNotExist }
	if p := fetchClaudePlanUsage(); p.Status != "not_applicable" {
		t.Fatalf("no login case: %+v", p)
	}
}

func TestClaudeRoutingReason(t *testing.T) {
	if r := claudeRoutingReason(map[string]any{"CLAUDE_CODE_USE_BEDROCK": "1"}, ""); !strings.Contains(r, "Bedrock") {
		t.Errorf("bedrock: %q", r)
	}
	if r := claudeRoutingReason(map[string]any{"CLAUDE_CODE_USE_VERTEX": "0"}, ""); r != "" {
		t.Errorf("vertex=0 should not count: %q", r)
	}
	if r := claudeRoutingReason(nil, "/bin/helper"); r == "" {
		t.Error("apiKeyHelper should count")
	}
	if r := claudeRoutingReason(map[string]any{"FOO": "1"}, ""); r != "" {
		t.Errorf("unrelated env: %q", r)
	}
}

// Shape from `codex app-server generate-ts` (GetAccountRateLimitsResponse).
const codexRateLimitsFixture = `{"ordinaryUsageAllowed":true,"rateLimits":{"limitId":"codex","limitName":null,"normalModelSlug":null,"primary":{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1793100000},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":1793500000},"credits":null,"individualLimit":null,"spendControlReached":null,"planType":"plus","rateLimitReachedType":null},"rateLimitsByLimitId":{"codex":{"limitId":"codex","limitName":null,"primary":{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1793100000},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":1793500000},"planType":"plus"},"codex_other":{"limitId":"codex_other","limitName":"GPT-5.5-Codex-Spark","primary":{"usedPercent":0,"windowDurationMins":300,"resetsAt":null},"secondary":null,"planType":"plus"}},"rateLimitResetCredits":null,"accountId":null,"rateLimitUpsell":null}`

func TestParseCodexRateLimits(t *testing.T) {
	plan, rows, err := parseCodexRateLimits([]byte(codexRateLimitsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if plan != "plus" {
		t.Errorf("plan = %q", plan)
	}
	labels := []string{}
	for _, r := range rows {
		labels = append(labels, r.Label)
	}
	if got := strings.Join(labels, "|"); got != "5-hour limit|Weekly|GPT-5.5-Codex-Spark · 5-hour limit" {
		t.Fatalf("labels = %s", got)
	}
	if rows[0].Percent != 12.5 || rows[0].ResetsAt != time.Unix(1793100000, 0).UTC().Format(time.RFC3339) {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[2].ResetsAt != "" {
		t.Errorf("null resetsAt should stay empty: %+v", rows[2])
	}
}

func TestParseCodexRateLimitsSingleBucket(t *testing.T) {
	plan, rows, err := parseCodexRateLimits([]byte(`{"rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":300,"resetsAt":null},"secondary":null,"planType":"pro"},"rateLimitsByLimitId":null}`))
	if err != nil || plan != "pro" || len(rows) != 1 || rows[0].Label != "5-hour limit" {
		t.Fatalf("plan=%q rows=%+v err=%v", plan, rows, err)
	}
}

func TestCodexWindowLabel(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	for in, want := range map[int64]string{300: "5-hour limit", 10080: "Weekly", 1440: "1-day limit", 45: "45-minute limit"} {
		if got := codexWindowLabel(n(in)); got != want {
			t.Errorf("%d → %q, want %q", in, got, want)
		}
	}
	if codexWindowLabel(nil) != "Usage limit" {
		t.Error("nil window")
	}
}

// Live check against the real endpoints. Opt-in:
//
//	BURROW_LIVE_PLANUSAGE=1 go test -run TestPlanUsageLive -v
//
// Prints statuses and row labels only — never a token.
func TestPlanUsageLive(t *testing.T) {
	if os.Getenv("BURROW_LIVE_PLANUSAGE") == "" {
		t.Skip("set BURROW_LIVE_PLANUSAGE=1")
	}
	u := (&App{}).PlanUsage()
	out, _ := json.MarshalIndent(u, "", "  ")
	t.Logf("%s", out)
}
