package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSanitizeChatRecap(t *testing.T) {
	if got := sanitizeChatRecap("  \"Recap: Fixed the race; PR open\"\nsecond line"); got != "Fixed the race; PR open" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeChatRecap(""); got != "" {
		t.Fatalf("empty -> %q", got)
	}
	long := sanitizeChatRecap(strings.Repeat("é", 300))
	if r := []rune(long); len(r) != chatRecapMaxChars || !strings.HasSuffix(long, "...") {
		t.Fatalf("long: %d runes %q", len(r), long)
	}
}

func TestRecapTranscript(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Text: "a"}, {Role: "thinking", Text: "hmm"},
		{Role: "tool", Text: "Bash"}, {Role: "assistant", Text: "done"},
	}
	got := recapTranscript(msgs, 2)
	if got != "[tool: Bash]\nAssistant: done" {
		t.Fatalf("got %q", got)
	}
}

func TestChatRecapObserve(t *testing.T) {
	a := &App{}
	on := func() bool { return true }
	off := func() bool { return false }
	now := time.Unix(10_000, 0)
	text := []ProviderRuntimeEvent{{Type: EvtTextDelta}}
	done := []ProviderRuntimeEvent{{Type: EvtTurnCompleted}}
	turn := func(id string, at time.Time, en func() bool, withText bool) bool {
		a.chatRecapObserve(id, chatUserKind, []ProviderRuntimeEvent{{Type: EvtUserDelta}}, at, en)
		if withText {
			a.chatRecapObserve(id, "claude-data", text, at, en)
		}
		return a.chatRecapObserve(id, "claude-data", done, at, en)
	}
	release := func(id string) { chatRecaps.mu.Lock(); delete(chatRecaps.inFlight[a], id); chatRecaps.mu.Unlock() }

	if !turn("1", now, on, true) {
		t.Fatal("first completed turn with text must recap")
	}
	release("1")
	if turn("1", now.Add(time.Minute), on, true) {
		t.Fatal("inside the debounce window")
	}
	if !turn("1", now.Add(3*time.Minute), on, true) {
		t.Fatal("after the window")
	}
	release("1")
	if turn("2", now, on, false) {
		t.Fatal("turn without assistant text")
	}
	if turn("3", now, off, true) {
		t.Fatal("setting off")
	}
	// Replay: no live user prompt opened a turn, so a turn end alone is ignored.
	a.chatRecapObserve("4", "claude-data", text, now, on)
	if a.chatRecapObserve("4", "claude-data", done, now, on) {
		t.Fatal("replay must not recap")
	}
	// A failed turn doesn't recap.
	a.chatRecapObserve("5", chatUserKind, []ProviderRuntimeEvent{{Type: EvtUserDelta}}, now, on)
	a.chatRecapObserve("5", "claude-data", text, now, on)
	if a.chatRecapObserve("5", "claude-data", []ProviderRuntimeEvent{{Type: EvtTurnFailed}}, now, on) {
		t.Fatal("failed turn")
	}
}

func TestChatRecapsEnabledSetting(t *testing.T) {
	a, _, _, _ := runtimeApp(t)
	writeConfig(t, map[string]any{})
	if !a.chatRecapsEnabled() {
		t.Fatal("default must be on")
	}
	writeConfig(t, map[string]any{"uiPrefs": map[string]any{"chatRecaps": false}})
	if a.chatRecapsEnabled() {
		t.Fatal("off")
	}
}

func TestRefreshChatRecapStoresAndKeepsOnEmpty(t *testing.T) {
	a, _, wsID, _ := runtimeApp(t)
	writeConfig(t, map[string]any{})
	res, err := a.db.Exec(`INSERT INTO chats (workspace_id, title, recap) VALUES (?, 't', 'old')`, wsID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if err := a.SaveChatMessages(int(id), `[{"id":0,"role":"user","text":"hi"},{"id":1,"role":"assistant","text":"done"}]`, 0); err != nil {
		t.Fatal(err)
	}
	orig := recapGenerate
	defer func() { recapGenerate = orig }()
	answer := "Fixed it; tests pass"
	recapGenerate = func(_ *App, _, _, transcript string) string {
		if !strings.Contains(transcript, "Assistant: done") {
			t.Errorf("transcript %q", transcript)
		}
		return answer
	}
	read := func() string { c, _ := a.chatByID(id); return c.Recap }
	chatID := strconv.FormatInt(id, 10)
	a.refreshChatRecap(chatID)
	if got := read(); got != answer {
		t.Fatalf("recap = %q", got)
	}
	answer = ""
	a.refreshChatRecap(chatID)
	if got := read(); got != "Fixed it; tests pass" {
		t.Fatalf("empty answer overwrote: %q", got)
	}
}
