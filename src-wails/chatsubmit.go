package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The submit door (ADR 0001): chat_submit is the one way a client sends a
// user message. Go decides send-vs-queue under the chat lock, so no client
// races its own stale copy of the phase (a check-then-act between chat_send
// and chat_queue_add). Steer stays a separate, explicit call.

// ChatSubmitResult: Sent = the message went to the agent now; else Item is
// its place in the queue. Text = the message as sent (what chat_answer_turn
// composed — the client's echo); empty on a deduped retry.
type ChatSubmitResult struct {
	Sent bool           `json:"sent"`
	Item *ChatQueueItem `json:"item,omitempty"`
	Text string         `json:"text,omitempty"`
}

// ChatSubmit sends text now if the chat is settled and nothing is queued
// ahead of it, else queues it (delivered by the settle kick / sweep). A
// clientID already accepted for the chat — by any door, also before a core
// restart (claimClientID) — is a no-op success with the earlier outcome.
func (a *App) ChatSubmit(chatID int64, text string, images []string, clientID string) (ChatSubmitResult, error) {
	return a.submit(chatID, images, clientID, func(Chat) (string, error) { return text, nil })
}

// submit is ChatSubmit with the text made from the chat row — after the
// clientID check, so a retry of a message whose source has since gone (an
// answered end_turn card) is still the earlier outcome, not an error.
func (a *App) submit(chatID int64, images []string, clientID string, compose func(Chat) (string, error)) (ChatSubmitResult, error) {
	c, err := a.chatByID(chatID)
	if err != nil {
		return ChatSubmitResult{}, err
	}
	prev, finish, dup := a.claimClientID(chatID, clientID)
	if dup {
		return ChatSubmitResult{Sent: prev.item == nil, Item: prev.item}, nil
	}
	text, err := compose(c)
	if err != nil {
		finish(false, nil)
		return ChatSubmitResult{}, err
	}
	id := strconv.FormatInt(chatID, 10)
	unlock := a.chatRT.lock(id)
	// A non-empty queue goes first: sending now would overtake it.
	if a.chatSettled(id) && a.chatQueueLen(chatID) == 0 {
		err := a.chatSendLocked(c, id, text, images, clientID)
		unlock()
		finish(err == nil, nil)
		if err != nil {
			return ChatSubmitResult{}, err
		}
		return ChatSubmitResult{Sent: true, Text: text}, nil
	}
	it, err := a.chatQueueInsert(chatID, text, images, clientID)
	unlock()
	if err != nil {
		finish(false, nil)
		return ChatSubmitResult{}, err
	}
	finish(true, &it)
	a.emitQueueChanged(chatID)
	a.kickChatDelivery()
	return ChatSubmitResult{Item: &it, Text: text}, nil
}

func (a *App) chatQueueLen(chatID int64) int {
	var n int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_queue WHERE chat_id = ?`, chatID).Scan(&n)
	return n
}

// claimClientID is clientIDs.claim plus the durable check: the in-memory map
// is empty after a core restart, so a miss looks for the id on the chat's
// queue, its recent chat-user lines and its recent transcript rows.
func (a *App) claimClientID(chatID int64, id string) (*clientIDEntry, func(bool, *ChatQueueItem), bool) {
	prev, finish, dup := a.clientIDs.claim(chatID, id)
	if dup || id == "" {
		return prev, finish, dup
	}
	if seen, item := a.clientIDStored(chatID, id); seen {
		finish(true, item)
		return &clientIDEntry{ok: true, item: item}, nil, true
	}
	return nil, finish, false
}

// clientIDStoredWindow bounds the durable lookup to the chat's latest rows.
const clientIDStoredWindow = 200

func (a *App) clientIDStored(chatID int64, id string) (bool, *ChatQueueItem) {
	if a.db == nil {
		return false, nil
	}
	if it, err := scanQueueItem(a.db.QueryRow(`SELECT `+chatQueueColumns+` FROM chat_queue WHERE chat_id = ? AND client_id = ? LIMIT 1`, chatID, id)); err == nil {
		return true, &it
	}
	needle, _ := json.Marshal(id) // the id as it appears inside a JSON payload
	match := func(q string, args ...any) bool {
		rows, err := a.db.Query(q, args...)
		if err != nil {
			return false
		}
		defer rows.Close()
		for rows.Next() {
			var payload string
			if rows.Scan(&payload) != nil {
				continue
			}
			var v struct {
				ClientID string `json:"clientId"`
			}
			if json.Unmarshal([]byte(payload), &v) == nil && v.ClientID == id {
				return true
			}
		}
		return false
	}
	return match(`SELECT line FROM (SELECT line FROM chat_stream WHERE chat_id = ? AND kind = ? ORDER BY ord DESC LIMIT ?) WHERE instr(line, ?) > 0`,
		chatID, chatUserKind, clientIDStoredWindow, string(needle)) ||
		match(`SELECT payload_json FROM (SELECT payload_json FROM chat_messages WHERE chat_id = ? ORDER BY ord DESC LIMIT ?) WHERE instr(payload_json, ?) > 0`,
			chatID, clientIDStoredWindow, string(needle)), nil
}

// userLine is a chat-user stream line. Older lines are the bare prompt text;
// decodeUserLine reads both.
type userLine struct {
	Text     string   `json:"text"`
	Images   []string `json:"images,omitempty"`
	ClientID string   `json:"clientId,omitempty"`
}

func encodeUserLine(text string, images []string, clientID string) string {
	b, _ := json.Marshal(userLine{Text: text, Images: images, ClientID: clientID})
	return string(b)
}

func decodeUserLine(line string) userLine {
	if strings.HasPrefix(line, "{") {
		var v struct {
			Text     *string  `json:"text"`
			Images   []string `json:"images"`
			ClientID string   `json:"clientId"`
		}
		// "text" must be present: a plain prompt that merely looks like JSON
		// stays the prompt.
		if json.Unmarshal([]byte(line), &v) == nil && v.Text != nil {
			return userLine{Text: *v.Text, Images: v.Images, ClientID: v.ClientID}
		}
	}
	return userLine{Text: line}
}

// emitUserLine records what the human sent (see chatUserKind).
func (a *App) emitUserLine(id, text string, images []string, clientID string) {
	// The CLI already has the images; the recorded line keeps refs only
	// (chatimages.go).
	a.emitChatLine(id, chatUserKind, encodeUserLine(text, a.storeChatImages(id, images), clientID))
}

// withSendingClientID makes clientID the id the transport's user line carries
// for the send fn performs. Callers hold the chat lock, so one send per chat
// at a time.
//
// ponytail: side channel instead of a clientID parameter through
// ClaudeSend / AcpSend / codexSteer (wire methods). Thread it as a parameter
// if a send ever runs outside the chat lock.
func (a *App) withSendingClientID(id, clientID string, fn func() error) error {
	if clientID == "" {
		return fn()
	}
	a.chatRT.sending.Store(id, clientID)
	defer a.chatRT.sending.Delete(id)
	return fn()
}

func (a *App) sendingClientID(id string) string {
	v, _ := a.chatRT.sending.Load(id)
	s, _ := v.(string)
	return s
}
