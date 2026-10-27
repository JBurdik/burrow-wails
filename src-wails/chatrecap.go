package main

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Session recap: one line saying where a chat stands, shown under its title in
// the Sidebar. Generated in Go after a live turn settles, through the same
// one-shot textgen machinery (and model preference) as chat titles, so every
// provider gets it with no per-transport code.
//
// Trigger (chatRecapObserve), all of it in-memory per chat:
//   - the turn was opened by a live user prompt (chatUserKind) — same rule as
//     chatturnnotes.go, which is what keeps an ACP history replay, a restart
//     and every LoadChatEventsSince read from recapping turns nobody just ran;
//   - the turn produced at least one assistant text delta;
//   - it ended in turn.completed (not failed / reset / exited);
//   - the chat setting is on, and at least chatRecapMinGap since the last
//     recap attempt for that chat, and none is in flight.
//
// The result lands in chats.recap, read-only on the wire like collected_at.

const (
	chatRecapMinGap   = 2 * time.Minute
	chatRecapMaxChars = 100
	chatRecapWindow   = 12 // last N transcript messages fed to the model
)

type recapState struct {
	open      bool
	assistant bool
}

var chatRecaps = struct {
	mu       sync.Mutex
	turns    map[*App]map[string]*recapState
	last     map[*App]map[string]time.Time
	inFlight map[*App]map[string]bool
}{
	turns:    map[*App]map[string]*recapState{},
	last:     map[*App]map[string]time.Time{},
	inFlight: map[*App]map[string]bool{},
}

// chatRecapsEnabled reads uiPrefs.chatRecaps fresh; absent = on, unreadable
// config = off (a background model call is never worth guessing about).
func (a *App) chatRecapsEnabled() bool {
	raw, err := a.ReadConfig()
	if err != nil {
		return false
	}
	var cfg struct {
		UI struct {
			Recaps *bool `json:"chatRecaps"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return false
	}
	return cfg.UI.Recaps == nil || *cfg.UI.Recaps
}

// chatRecapHook is the tail call from chatTurnNotes: advances the turn state
// and, when a recap is due, generates it off this goroutine.
func (a *App) chatRecapHook(chatID, kind string, events []ProviderRuntimeEvent) {
	if !a.chatRecapObserve(chatID, kind, events, time.Now(), a.chatRecapsEnabled) {
		return
	}
	go a.refreshChatRecap(chatID)
}

// chatRecapObserve advances one chat's turn over a line and reports whether a
// recap should be generated now (already marked in flight and stamped).
// enabled is only consulted on a completed turn, so the config isn't read per
// streamed token.
func (a *App) chatRecapObserve(chatID, kind string, events []ProviderRuntimeEvent, now time.Time, enabled func() bool) bool {
	chatRecaps.mu.Lock()
	turns := chatRecaps.turns[a]
	if turns == nil {
		turns = map[string]*recapState{}
		chatRecaps.turns[a] = turns
	}
	st := turns[chatID]
	if kind == chatUserKind {
		if st == nil {
			turns[chatID] = &recapState{open: true}
		}
		chatRecaps.mu.Unlock()
		return false
	}
	due := false
	for _, ev := range events {
		switch ev.Type {
		case EvtSessionReset, EvtSessionExited, EvtTurnFailed:
			delete(turns, chatID)
			st = nil
		case EvtTextDelta:
			if st != nil {
				st.assistant = true
			}
		case EvtTurnCompleted:
			if st != nil && st.assistant {
				due = true
			}
			delete(turns, chatID)
			st = nil
		}
	}
	chatRecaps.mu.Unlock()
	if !due || !enabled() {
		return false
	}

	chatRecaps.mu.Lock()
	defer chatRecaps.mu.Unlock()
	if chatRecaps.inFlight[a][chatID] {
		return false
	}
	if last, ok := chatRecaps.last[a][chatID]; ok && now.Sub(last) < chatRecapMinGap {
		return false
	}
	if chatRecaps.last[a] == nil {
		chatRecaps.last[a] = map[string]time.Time{}
	}
	if chatRecaps.inFlight[a] == nil {
		chatRecaps.inFlight[a] = map[string]bool{}
	}
	chatRecaps.last[a][chatID] = now
	chatRecaps.inFlight[a][chatID] = true
	return true
}

// recapGenerate is the model call, swapped by tests.
var recapGenerate = func(a *App, cwd, selection, transcript string) string {
	return a.generateChatRecap(cwd, selection, transcript)
}

// refreshChatRecap generates and stores one chat's recap. A "" answer keeps
// the old recap. Takes seconds: always run on its own goroutine.
func (a *App) refreshChatRecap(chatID string) {
	defer func() {
		chatRecaps.mu.Lock()
		delete(chatRecaps.inFlight[a], chatID)
		chatRecaps.mu.Unlock()
	}()
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}
	row, err := a.chatByID(id)
	if err != nil {
		return
	}
	raw, err := a.LoadChatMessages(int(id))
	if err != nil {
		return
	}
	var msgs []ChatMessage
	if json.Unmarshal([]byte(raw), &msgs) != nil {
		return
	}
	transcript := recapTranscript(msgs, chatRecapWindow)
	if transcript == "" {
		return
	}
	recap := recapGenerate(a, a.chatCwd(row), a.textGenSelection(), transcript)
	if recap == "" || recap == row.Recap {
		return
	}
	if _, err := a.db.Exec(`UPDATE chats SET recap = ? WHERE id = ?`, recap, id); err != nil {
		log.Printf("store recap for chat %d: %v", id, err)
		return
	}
	busEmit("chats-changed", nil)
}

// textGenSelection is the persisted text-generation model preference.
func (a *App) textGenSelection() string {
	raw, err := a.ReadConfig()
	if err != nil {
		return ""
	}
	var cfg struct {
		UI struct {
			Model string `json:"textGenerationModel"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return ""
	}
	return cfg.UI.Model
}

// recapTranscript renders the last n messages as plain lines: user and
// assistant text (clipped), tools by name only. Thinking rows are skipped.
func recapTranscript(msgs []ChatMessage, n int) string {
	var lines []string
	for _, m := range msgs {
		switch m.Role {
		case "user":
			if t := strings.TrimSpace(m.Text); t != "" {
				lines = append(lines, "User: "+limitSection(t, 600))
			}
		case "assistant":
			if t := strings.TrimSpace(m.Text); t != "" {
				lines = append(lines, "Assistant: "+limitSection(t, 900))
			}
		case "tool":
			if t := strings.TrimSpace(m.Text); t != "" {
				lines = append(lines, "[tool: "+t+"]")
			}
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// sanitizeChatRecap trims the model's answer to one short line. Empty in,
// empty out: an empty recap must never overwrite the one already shown.
func sanitizeChatRecap(raw string) string {
	line := strings.Join(strings.Fields(firstLine(raw)), " ")
	line = strings.TrimSpace(strings.Trim(line, `"'`+"`"))
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "Recap:"), "recap:"))
	r := []rune(line)
	if len(r) > chatRecapMaxChars {
		line = strings.TrimSpace(string(r[:chatRecapMaxChars-3])) + "..."
	}
	return line
}

const chatRecapSchema = `{"type":"object","properties":{"recap":{"type":"string"}},"required":["recap"],"additionalProperties":false}`

// generateChatRecap writes the one-line recap of a conversation's current
// state. Best-effort: any failure returns "".
func (a *App) generateChatRecap(cwd, selection, transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return ""
	}
	prompt := strings.Join([]string{
		"You write a one-line recap of where a coding conversation stands right now.",
		"Return a JSON object with key: recap.",
		"Rules:",
		"- one sentence, at most 100 characters, past tense for what is done",
		"- say what was accomplished and the current state (tests, PR, blocker, next step) when known",
		"- example: Fixed the auth race; tests pass, PR #42 open",
		"- no quotes, no prefix like \"Recap:\", no trailing period needed",
		"",
		"Recent conversation:",
		limitSection(transcript, 12000),
	}, "\n")
	out, err := generateTextJSON(cwd, selection, prompt, chatRecapSchema)
	if err != nil {
		log.Printf("chat recap generation failed (%s): %v", selection, err)
		return ""
	}
	if extracted, err := extractGeneratedJSON(out); err == nil {
		out = extracted
	}
	var result struct {
		Recap string `json:"recap"`
	}
	if err := json.Unmarshal(out, &result); err == nil {
		return sanitizeChatRecap(result.Recap)
	}
	return sanitizeChatRecap(string(out))
}
