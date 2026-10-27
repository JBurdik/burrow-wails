package main

// Subscription usage limits (session / weekly %) for the Claude and Codex
// accounts this machine is logged into.
//
// Claude: GET api.anthropic.com/api/oauth/usage with the Claude Code OAuth
// access token (what Claude Desktop does; works on Team). The token is read
// fresh from the Keychain on every fetch and is NEVER refreshed here —
// rotating the refresh token would log the CLI out. An expired token or a 401
// is reported as unavailable; the CLI refreshes it on its next run. The token
// is never logged, returned or emitted.
//
// Codex: a short-lived `codex app-server` probe (same handshake as
// CodexListModels) asking `account/rateLimits/read`.
//
// Both are cached (Claude 60 s / 20 s after failure, Codex 120 s / 60 s) and
// fetched in parallel; a provider's mutex is held across its fetch so
// concurrent callers coalesce onto one request.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type PlanUsageRow struct {
	Label    string  `json:"label"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resets_at,omitempty"` // RFC 3339
}

type PlanUsageProvider struct {
	Kind   string         `json:"kind"`   // claude | codex
	Plan   string         `json:"plan"`   // e.g. "team", "plus"
	Status string         `json:"status"` // ok | unavailable | not_applicable
	Reason string         `json:"reason,omitempty"`
	Rows   []PlanUsageRow `json:"rows"`
	Extra  string         `json:"extra,omitempty"`
}

type PlanUsage struct {
	Providers []PlanUsageProvider `json:"providers"`
}

type planUsageCacheEntry struct {
	mu      sync.Mutex
	val     PlanUsageProvider
	expires time.Time
}

func (c *planUsageCacheEntry) get(okTTL, failTTL time.Duration, fetch func() PlanUsageProvider) PlanUsageProvider {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.val
	}
	v := fetch()
	if v.Rows == nil {
		v.Rows = []PlanUsageRow{}
	}
	ttl := okTTL
	if v.Status == "unavailable" {
		ttl = failTTL
	}
	c.val, c.expires = v, time.Now().Add(ttl)
	return v
}

var (
	planUsageClaude planUsageCacheEntry
	planUsageCodex  planUsageCacheEntry
)

// PlanUsage returns the subscription usage of every known provider.
func (a *App) PlanUsage() PlanUsage {
	out := make([]PlanUsageProvider, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		out[0] = planUsageClaude.get(60*time.Second, 20*time.Second, fetchClaudePlanUsage)
	}()
	go func() {
		defer wg.Done()
		out[1] = planUsageCodex.get(120*time.Second, 60*time.Second, fetchCodexPlanUsage)
	}()
	wg.Wait()
	return PlanUsage{Providers: out}
}

// --- Claude ----------------------------------------------------------------

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

type claudeOAuthCreds struct {
	AccessToken      string `json:"accessToken"`
	ExpiresAt        int64  `json:"expiresAt"` // ms
	SubscriptionType string `json:"subscriptionType"`
}

func fetchClaudePlanUsage() PlanUsageProvider {
	p := PlanUsageProvider{Kind: "claude"}
	if why := claudeRoutedElsewhere(); why != "" {
		p.Status, p.Reason = "not_applicable", why
		return p
	}
	creds, err := readClaudeOAuthCreds()
	if err != nil || creds.AccessToken == "" {
		p.Status, p.Reason = "not_applicable", "No Claude subscription login found"
		return p
	}
	p.Plan = creds.SubscriptionType
	if creds.ExpiresAt > 0 && time.Now().UnixMilli() >= creds.ExpiresAt {
		p.Status, p.Reason = "unavailable", "Login token expired — it refreshes the next time Claude runs"
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, body, err := claudeUsageRequest(ctx, creds.AccessToken)
	if err != nil {
		p.Status, p.Reason = "unavailable", "Usage request failed"
		return p
	}
	switch {
	case code == http.StatusUnauthorized:
		p.Status, p.Reason = "unavailable", "Login token rejected — it refreshes the next time Claude runs"
		return p
	case code != http.StatusOK:
		p.Status, p.Reason = "unavailable", fmt.Sprintf("Usage endpoint returned HTTP %d", code)
		return p
	}
	rows, extra, err := parseClaudeUsage(body)
	if err != nil {
		p.Status, p.Reason = "unavailable", "Unexpected usage response"
		return p
	}
	p.Status, p.Rows, p.Extra = "ok", rows, extra
	return p
}

// claudeUsageRequest is a var so tests never hit the network.
var claudeUsageRequest = func(ctx context.Context, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeUsageURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, err
}

// readClaudeOAuthCreds reads the default Claude Code login: the Keychain entry
// first, then ~/.claude/.credentials.json (Linux / older installs).
var readClaudeOAuthCreds = func() (claudeOAuthCreds, error) {
	var raw []byte
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output(); err == nil {
		raw = out
	} else if home, herr := os.UserHomeDir(); herr == nil {
		raw, _ = os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
	}
	if len(raw) == 0 {
		return claudeOAuthCreds{}, errors.New("no credentials")
	}
	var doc struct {
		ClaudeAiOauth claudeOAuthCreds `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return claudeOAuthCreds{}, err
	}
	return doc.ClaudeAiOauth, nil
}

// claudeRoutedElsewhere reports why the subscription usage does not apply
// when the user's global Claude settings route it via Bedrock/Vertex/an API key.
func claudeRoutedElsewhere() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return ""
	}
	var s struct {
		Env          map[string]any `json:"env"`
		APIKeyHelper string         `json:"apiKeyHelper"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return claudeRoutingReason(s.Env, s.APIKeyHelper)
}

func claudeRoutingReason(env map[string]any, apiKeyHelper string) string {
	set := func(k string) bool {
		v, ok := env[k]
		if !ok {
			return false
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		return s != "" && s != "0" && !strings.EqualFold(s, "false")
	}
	switch {
	case set("CLAUDE_CODE_USE_BEDROCK"):
		return "Claude runs via Amazon Bedrock"
	case set("CLAUDE_CODE_USE_VERTEX"):
		return "Claude runs via Google Vertex AI"
	case set("CLAUDE_CODE_USE_FOUNDRY"):
		return "Claude runs via Microsoft Foundry"
	case set("ANTHROPIC_API_KEY"), set("ANTHROPIC_AUTH_TOKEN"), apiKeyHelper != "":
		return "Claude runs on an API key"
	}
	return ""
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// parseClaudeUsage maps the response the way Claude Desktop does. Nulls are
// normal (Team returns most keys null).
func parseClaudeUsage(body []byte) ([]PlanUsageRow, string, error) {
	var r struct {
		FiveHour       *claudeUsageWindow `json:"five_hour"`
		SevenDay       *claudeUsageWindow `json:"seven_day"`
		SevenDaySonnet *claudeUsageWindow `json:"seven_day_sonnet"`
		ExtraUsage     *struct {
			IsEnabled   bool     `json:"is_enabled"`
			Utilization *float64 `json:"utilization"`
		} `json:"extra_usage"`
		Limits []struct {
			Kind     string   `json:"kind"`
			Percent  *float64 `json:"percent"`
			ResetsAt string   `json:"resets_at"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, "", err
	}
	rows := []PlanUsageRow{}
	add := func(label string, w *claudeUsageWindow) {
		if w != nil && w.Utilization != nil {
			rows = append(rows, PlanUsageRow{Label: label, Percent: clampPct(*w.Utilization), ResetsAt: w.ResetsAt})
		}
	}
	add("5-hour limit", r.FiveHour)
	add("Weekly · all models", r.SevenDay)
	add("Weekly · Sonnet", r.SevenDaySonnet)
	for _, l := range r.Limits {
		if l.Kind != "weekly_scoped" || l.Percent == nil || l.Scope == nil || l.Scope.Model == nil || l.Scope.Model.DisplayName == "" {
			continue
		}
		rows = append(rows, PlanUsageRow{Label: "Weekly · " + l.Scope.Model.DisplayName, Percent: clampPct(*l.Percent), ResetsAt: l.ResetsAt})
	}
	extra := ""
	if e := r.ExtraUsage; e != nil && e.IsEnabled {
		if e.Utilization != nil {
			rows = append(rows, PlanUsageRow{Label: "Extra usage", Percent: clampPct(*e.Utilization)})
		} else {
			extra = "Extra usage enabled"
		}
	}
	return rows, extra, nil
}

func clampPct(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*10) / 10
}

// --- Codex -----------------------------------------------------------------

func fetchCodexPlanUsage() PlanUsageProvider {
	p := PlanUsageProvider{Kind: "codex"}
	home, _ := os.UserHomeDir()
	bin := resolveAgentBin("codex", home)
	if bin == "" {
		p.Status, p.Reason = "not_applicable", "Codex is not installed"
		return p
	}
	result, err := codexRateLimitsProbe(bin, home)
	if err != nil {
		p.Status, p.Reason = "unavailable", err.Error()
		return p
	}
	plan, rows, err := parseCodexRateLimits(result)
	if err != nil {
		p.Status, p.Reason = "unavailable", err.Error()
		return p
	}
	p.Status, p.Plan, p.Rows = "ok", plan, rows
	return p
}

// codexRateLimitsProbe spawns `codex app-server`, initializes, asks
// `account/rateLimits/read` and kills it. Returns the raw `result`.
var codexRateLimitsProbe = func(bin, cwd string) (json.RawMessage, error) {
	cmd, stdin, reader, _, err := spawnStdio(bin, []string{"app-server"}, cwd, map[string]string{"PATH": augmentedPath(cwd)})
	if err != nil {
		return nil, fmt.Errorf("failed to start codex app-server")
	}
	stop := time.AfterFunc(15*time.Second, func() { _ = killAgentProcessTree(cmd) })
	defer func() {
		stop.Stop()
		_ = stdin.Close()
		_ = killAgentProcessTree(cmd)
	}()
	sess := &acpSession{stdin: stdin, proto: protoCodexAppServer}
	if err := sess.write(map[string]any{
		"jsonrpc": "2.0", "id": 0, "method": "initialize",
		"params": map[string]any{
			"clientInfo":   map[string]any{"name": "burrow", "title": "Burrow", "version": appVersion},
			"capabilities": map[string]any{"experimentalApi": true},
		},
	}); err != nil {
		return nil, fmt.Errorf("codex handshake failed")
	}
	if _, err := reader.await(0, nil); err != nil {
		return nil, fmt.Errorf("codex handshake failed")
	}
	if err := sess.write(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "account/rateLimits/read"}); err != nil {
		return nil, fmt.Errorf("codex request failed")
	}
	resp, err := reader.await(1, nil)
	if err != nil {
		return nil, fmt.Errorf("codex did not answer")
	}
	if e := mapOf(resp["error"]); e != nil {
		msg, _ := e["message"].(string)
		if msg == "" {
			msg = "codex refused the request"
		}
		return nil, errors.New(msg)
	}
	return json.Marshal(resp["result"])
}

type codexRateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"` // unix seconds
}

type codexRateSnapshot struct {
	LimitID   *string          `json:"limitId"`
	LimitName *string          `json:"limitName"`
	Primary   *codexRateWindow `json:"primary"`
	Secondary *codexRateWindow `json:"secondary"`
	PlanType  *string          `json:"planType"`
}

func parseCodexRateLimits(raw []byte) (string, []PlanUsageRow, error) {
	var r struct {
		RateLimits          *codexRateSnapshot            `json:"rateLimits"`
		RateLimitsByLimitID map[string]*codexRateSnapshot `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", nil, fmt.Errorf("unexpected codex response")
	}
	snaps := []*codexRateSnapshot{}
	if len(r.RateLimitsByLimitID) > 0 {
		// The `codex` bucket first, the rest in key order.
		keys := make([]string, 0, len(r.RateLimitsByLimitID))
		for k := range r.RateLimitsByLimitID {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if s := r.RateLimitsByLimitID["codex"]; s != nil {
			snaps = append(snaps, s)
		}
		for _, k := range keys {
			if s := r.RateLimitsByLimitID[k]; s != nil && k != "codex" {
				snaps = append(snaps, s)
			}
		}
	} else if r.RateLimits != nil {
		snaps = append(snaps, r.RateLimits)
	}
	if len(snaps) == 0 {
		return "", nil, fmt.Errorf("no rate limits reported")
	}
	plan := ""
	rows := []PlanUsageRow{}
	for i, s := range snaps {
		if plan == "" && s.PlanType != nil {
			plan = *s.PlanType
		}
		prefix := ""
		if i > 0 {
			name := ""
			if s.LimitName != nil {
				name = *s.LimitName
			} else if s.LimitID != nil {
				name = *s.LimitID
			}
			if name != "" {
				prefix = name + " · "
			}
		}
		for _, w := range []*codexRateWindow{s.Primary, s.Secondary} {
			if w == nil {
				continue
			}
			row := PlanUsageRow{Label: prefix + codexWindowLabel(w.WindowDurationMins), Percent: clampPct(w.UsedPercent)}
			if w.ResetsAt != nil && *w.ResetsAt > 0 {
				row.ResetsAt = time.Unix(*w.ResetsAt, 0).UTC().Format(time.RFC3339)
			}
			rows = append(rows, row)
		}
	}
	return plan, rows, nil
}

func codexWindowLabel(mins *int64) string {
	if mins == nil || *mins <= 0 {
		return "Usage limit"
	}
	m := *mins
	switch {
	case m == 10080:
		return "Weekly"
	case m%1440 == 0:
		return fmt.Sprintf("%d-day limit", m/1440)
	case m%60 == 0:
		return fmt.Sprintf("%d-hour limit", m/60)
	}
	return fmt.Sprintf("%d-minute limit", m)
}
