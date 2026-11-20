package main

import (
	"encoding/json"
	"log"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Thread titles are Go's: a top-level thread's first prompt renames a row
// still at its default ("Chat N" / "Chat N (phone)") to a heuristic at once,
// then — off the send path — to a model-written title. In Go so every door
// (desktop, tray panel, phone, chat_send from the CLI) gets it; the client
// copies only ever ran for a send made from a mounted AgentChat.
//
// Both writes are conditional UPDATEs, so a rename, a pin or Claude's own
// session title landing in between is never overwritten.

var defaultChatTitleRe = regexp.MustCompile(`^Chat(\s+\d+)?(\s+\(phone\))?$`)

// isDefaultChatTitle: "Chat N" (desktop) or "Chat N (phone)" — never a chosen name.
func isDefaultChatTitle(title string) bool {
	return defaultChatTitleRe.MatchString(strings.TrimSpace(title))
}

var titleFillerRe = regexp.MustCompile(`(?i)^(can you |please |i want (you )?to |how (do i|to) |what (is|are) (the |a )?|could you |would you |help me |i need (you )?to )`)

// heuristicChatTitle: the first six words,
// filler prefix dropped, capitalised, "…" when cut.
func heuristicChatTitle(text string) string {
	clean := strings.Join(strings.Fields(titleFillerRe.ReplaceAllString(text, "")), " ")
	words := strings.Split(clean, " ")
	if len(words) > 6 {
		words = words[:6]
	}
	title := strings.Join(words, " ")
	if r, n := utf8.DecodeRuneInString(title); n > 0 {
		title = string(unicode.ToUpper(r)) + title[n:]
	}
	if len(title) < len(clean) {
		title += "…"
	}
	return title
}

// chatTitleGenerate is the model call, swapped by tests.
var chatTitleGenerate = func(a *App, cwd, selection, policy, text string) string {
	return a.GenerateChatTitle(cwd, selection, policy, text)
}

// autoTitleChat runs on a top-level thread's first prompt (chatSendBody).
// Synchronous part is one UPDATE; the model call runs on its own goroutine.
func (a *App) autoTitleChat(c Chat, text string) {
	text = strings.TrimSpace(text)
	if c.PinnedTitle || !isDefaultChatTitle(c.Title) || text == "" {
		return
	}
	heuristic := heuristicChatTitle(text)
	if !a.swapChatTitle(c.ID, c.Title, heuristic) {
		return
	}
	go func() {
		selection, policy := a.textGenPrefs()
		if title := chatTitleGenerate(a, a.chatCwd(c), selection, policy, text); title != "" {
			a.swapChatTitle(c.ID, heuristic, title)
		}
	}()
}

// swapChatTitle sets the title only while it still reads `from` and is not
// pinned; reports whether it did.
func (a *App) swapChatTitle(id int64, from, to string) bool {
	if from == to {
		return false
	}
	res, err := a.db.Exec(`UPDATE chats SET title = ? WHERE id = ? AND title = ? AND pinned_title = 0`, to, id, from)
	if err != nil {
		log.Printf("title chat %d: %v", id, err)
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	busEmit("chats-changed", nil)
	return true
}

// textGenPrefs is the persisted text-generation model + house policy.
func (a *App) textGenPrefs() (selection, policy string) {
	raw, err := a.ReadConfig()
	if err != nil {
		return "", ""
	}
	var cfg struct {
		UI struct {
			Model  string `json:"textGenerationModel"`
			Policy string `json:"textGenerationPolicy"`
		} `json:"uiPrefs"`
	}
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return "", ""
	}
	return cfg.UI.Model, cfg.UI.Policy
}
