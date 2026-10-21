package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Chat transcript storage. Modelled on t3code's `projection_thread_messages`:
// one row per message, so saving a turn writes that turn instead of rewriting
// every chat's history. Burrow used to keep all of it in config.json, which had
// grown to 5.6 MB — and since lib/config.ts serialises the whole cache on every
// setConfig, one saved message rewrote 5.6 MB (non-atomically: a crash mid-write
// took the prefs with it).
//
// ponytail: the message body stays an opaque payload_json blob rather than
// typed columns. ChatMessage is a frontend type that grows a field per agent
// feature; mirroring it in SQL would mean a migration each time, and nothing
// server-side queries inside a message. Columns exist only for what we filter
// or order by. Message-level search reads `$.text` out of payload_json by
// trigger into an FTS5 index (chatsearch.go).

func chatMessagesSchema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS chat_messages (
			chat_id INTEGER NOT NULL,
			ord INTEGER NOT NULL,
			payload_json TEXT NOT NULL,
			PRIMARY KEY (chat_id, ord)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_chat ON chat_messages(chat_id, ord)`,
	}
}

// LoadChatMessages returns the chat's transcript as the same JSON array the
// frontend used to read out of config.json.
//
// The in-memory fold wins when this process has one (chattranscript.go): it is
// authoritative while the app runs, and the table lags it by up to one
// coalescing window. Reading the table instead would show a client a
// transcript a second old for no reason.
func (a *App) LoadChatMessages(chatID int) (string, error) {
	if w := a.chatStream(); w != nil {
		if msgs := w.chatTailMessages(strconv.Itoa(chatID)); msgs != nil {
			out, err := json.Marshal(msgs)
			if err != nil {
				return "", err
			}
			return string(out), nil
		}
	}
	return a.loadStoredChatMessages(chatID)
}

// ChatMessagesPage is one window of a transcript: Messages are the rows
// [Start, Start+len), Total is the whole transcript's length. A client that
// holds only a window must treat Start as "this many earlier messages exist".
type ChatMessagesPage struct {
	Messages []json.RawMessage `json:"messages"`
	Start    int               `json:"start"`
	Total    int               `json:"total"`
}

// LoadChatMessagesPage returns up to `limit` messages ending just before index
// `before` (before < 0 = the newest). It exists so a client opening a long chat
// — an iPad over a tailnet, where the images inlined in old user messages
// alone were megabytes — reads the tail first and fetches older ones on demand.
// Index here is the message's position in the transcript, which is also its
// chat_messages.ord (the fold and SaveChatMessages both write ord = index).
//
// Inline images are NOT sent: each becomes a stub (see stubChatImages) the
// client resolves with LoadChatImage when the user asks for it.
func (a *App) LoadChatMessagesPage(chatID int, before int, limit int) (ChatMessagesPage, error) {
	if limit <= 0 {
		limit = 60
	}
	msgs, start, total, err := a.chatMessagesWindow(chatID, func(total int) (int, int) {
		end := total
		if before >= 0 && before < total {
			end = before
		}
		return max(0, end-limit), end
	})
	if err != nil {
		return ChatMessagesPage{}, err
	}
	for i, m := range msgs {
		msgs[i] = stubChatImages(m, start+i)
	}
	return ChatMessagesPage{Messages: msgs, Start: start, Total: total}, nil
}

// chatImageStubPrefix marks an image the page left out: "<prefix><index>/<n>"
// names image n of the message at transcript index `index`.
const chatImageStubPrefix = "burrow-image://"

// chatImageStubMin: a data URL shorter than this is cheaper to send than to
// ask for (and a stub is itself ~25 bytes).
const chatImageStubMin = 4096

// stubChatImages replaces the large inline images of one message with stubs.
// Messages without images — nearly all of them — are returned untouched
// without being parsed.
func stubChatImages(raw json.RawMessage, index int) json.RawMessage {
	if !bytes.Contains(raw, []byte(`"images"`)) {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	var images []string
	if json.Unmarshal(m["images"], &images) != nil || len(images) == 0 {
		return raw
	}
	changed := false
	for i, img := range images {
		if len(img) >= chatImageStubMin && strings.HasPrefix(img, "data:") {
			images[i] = fmt.Sprintf("%s%d/%d", chatImageStubPrefix, index, i)
			changed = true
		}
	}
	if !changed {
		return raw
	}
	m["images"], _ = json.Marshal(images)
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// LoadChatImage returns image `image` of the message at transcript `index` as
// the data URL it was stored with.
func (a *App) LoadChatImage(chatID int, index int, image int) (string, error) {
	msgs, _, _, err := a.chatMessagesWindow(chatID, func(total int) (int, int) {
		if index < 0 || index >= total {
			return 0, 0
		}
		return index, index + 1
	})
	if err != nil {
		return "", err
	}
	if len(msgs) != 1 {
		return "", fmt.Errorf("no message %d in chat %d", index, chatID)
	}
	var m struct {
		Images []string `json:"images"`
	}
	if err := json.Unmarshal(msgs[0], &m); err != nil {
		return "", err
	}
	if image < 0 || image >= len(m.Images) {
		return "", fmt.Errorf("message %d has no image %d", index, image)
	}
	return m.Images[image], nil
}

// chatMessagesWindow reads rows [start, end) as `pick` chooses them from the
// transcript's length — from the live fold when this process has one (it is
// ahead of the table, same precedence as LoadChatMessages), else the table.
func (a *App) chatMessagesWindow(chatID int, pick func(total int) (start, end int)) (msgs []json.RawMessage, start, total int, err error) {
	if w := a.chatStream(); w != nil {
		if live := w.chatTailMessages(strconv.Itoa(chatID)); live != nil {
			start, end := pick(len(live))
			out := make([]json.RawMessage, 0, end-start)
			for _, m := range live[start:end] {
				raw, err := json.Marshal(m)
				if err != nil {
					return nil, 0, 0, err
				}
				out = append(out, raw)
			}
			return out, start, len(live), nil
		}
	}
	if a.db == nil {
		return []json.RawMessage{}, 0, 0, nil
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE chat_id = ?`, chatID).Scan(&total); err != nil {
		return nil, 0, 0, err
	}
	start, end := pick(total)
	rows, err := a.db.Query(
		`SELECT payload_json FROM chat_messages WHERE chat_id = ? AND ord >= ? AND ord < ? ORDER BY ord`,
		chatID, start, end)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	out := make([]json.RawMessage, 0, end-start)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, 0, err
		}
		out = append(out, json.RawMessage(payload))
	}
	return out, start, total, rows.Err()
}

// loadStoredChatMessages reads the table, ignoring any in-memory fold. This is
// what adoption reads (loadChatTail), so it must never consult the tails map:
// that is the state being built.
func (a *App) loadStoredChatMessages(chatID int) (string, error) {
	if a.db == nil {
		return "[]", nil
	}
	rows, err := a.db.Query(`SELECT payload_json FROM chat_messages WHERE chat_id = ? ORDER BY ord`, chatID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	out := []byte{'['}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return "", err
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(out, payload...)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return string(append(out, ']')), nil
}

// SaveChatMessages replaces the chat's transcript. Scoped to one chat and run in
// a transaction, so a crash can't leave a half-written history and a busy chat
// never rewrites its neighbours.
//
// ponytail: replace-all per chat, not a per-message diff. The write is already
// bounded by one chat's length; tracking dirty rows in the component would be
// real bookkeeping for a save that is now sub-millisecond. If a very long chat
// ever shows up in a profile, upsert by ord and delete the tail instead.
//
// foldedOrd says how far into chat_stream this transcript accounts for (the
// first ord NOT folded in). It is written in the same transaction as the
// messages, so the stream trim can never be told that lines are safe to drop
// before the rendered copy of them is actually committed. Pass -1 when the
// caller does not track ords (mobile clients, the config.json migration) and
// the existing mark is left alone.
//
// Since Go owns the fold (chattranscript.go), this whole-transcript
// DELETE+reinsert is NOT the live path — the fold upserts only its changed
// tail. What is left here is the import/migration path, which has no stream
// behind it, plus the clients that have not moved onto the fold yet. A
// successful save therefore FORGETS the in-memory tail, so the next folded
// line re-adopts what the caller just wrote instead of folding onto state the
// table no longer agrees with.
func (a *App) SaveChatMessages(chatID int, messagesJSON string, foldedOrd int64) error {
	if a.db == nil {
		return fmt.Errorf("db not open")
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(messagesJSON), &msgs); err != nil {
		return fmt.Errorf("decode messages: %w", err)
	}
	// Hold the chat's fold lock across the whole write, so this and Go's own
	// coalesced persist cannot interleave into a transcript with a hole in it
	// (see lockChatTail). A stopgap, and cheap: it goes away with this write
	// path.
	var tail *chatTail
	if w := a.chatStream(); w != nil {
		if tail = w.lockChatTail(strconv.Itoa(chatID)); tail != nil {
			defer tail.mu.Unlock()
		}
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_messages WHERE chat_id = ?`, chatID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO chat_messages (chat_id, ord, payload_json) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, m := range msgs {
		if _, err := stmt.Exec(chatID, i, string(m)); err != nil {
			return err
		}
	}
	if foldedOrd >= 0 {
		if _, err := tx.Exec(
			`INSERT INTO chat_stream_state (chat_id, folded_ord) VALUES (?, ?)
			 ON CONFLICT(chat_id) DO UPDATE SET folded_ord = MAX(folded_ord, excluded.folded_ord)`,
			strconv.Itoa(chatID), foldedOrd,
		); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Still inside the fold lock: the tail this forgets cannot be mid-persist,
	// and the next folded line re-adopts what was just written.
	if w := a.chatStream(); w != nil {
		w.forgetChatTail(strconv.Itoa(chatID))
	}
	return nil
}

func (a *App) DeleteChatMessages(chatID int) error {
	if a.db == nil {
		return nil
	}
	if _, err := a.db.Exec(`DELETE FROM openai_sessions WHERE chat_id = ?`, chatID); err != nil {
		return err
	}
	if _, err := a.db.Exec(`DELETE FROM chat_messages WHERE chat_id = ?`, chatID); err != nil {
		return err
	}
	return a.deleteChatStream(strconv.Itoa(chatID))
}

// migrateChatHistoryToSQLite moves an existing config.json `chatMessageHistory`
// map into chat_messages, then drops the key so config.json shrinks back to
// actual settings. Idempotent by construction: once the key is gone it no-ops.
// A .bak copy is kept because this rewrites the file holding every preference.
func (a *App) migrateChatHistoryToSQLite() {
	path, err := configFilePath()
	if err != nil {
		return
	}
	a.migrateChatHistoryFrom(path)
}

// migrateChatHistoryFrom is the testable half — it takes the config path so a
// test can never point it at the real config.json.
func (a *App) migrateChatHistoryFrom(path string) {
	if a.db == nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return // no config yet — nothing to move
	}
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Printf("chat history migration: config.json unparseable: %v", err)
		return
	}
	historyRaw, ok := cfg["chatMessageHistory"]
	if !ok {
		return
	}
	var history map[string]json.RawMessage
	if err := json.Unmarshal(historyRaw, &history); err != nil {
		log.Printf("chat history migration: chatMessageHistory unparseable: %v", err)
		return
	}

	if err := os.WriteFile(path+".bak", raw, 0o644); err != nil {
		log.Printf("chat history migration: backup failed, aborting: %v", err)
		return
	}

	imported := 0
	for chatID, msgs := range history {
		var id int
		if _, err := fmt.Sscanf(chatID, "%d", &id); err != nil {
			continue
		}
		if err := a.SaveChatMessages(id, string(msgs), -1); err != nil {
			// Leave the config key in place so the next launch retries rather
			// than silently dropping this chat's transcript.
			log.Printf("chat history migration: chat %d failed, keeping config.json: %v", id, err)
			return
		}
		imported++
	}

	delete(cfg, "chatMessageHistory")
	out, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("chat history migration: re-encode failed: %v", err)
		return
	}
	if err := writeFileAtomic(path, out); err != nil {
		log.Printf("chat history migration: rewrite failed: %v", err)
		return
	}
	log.Printf("chat history migration: moved %d chat transcript(s) into SQLite (backup at %s.bak)", imported, path)
}

// writeFileAtomic writes via a temp file in the same directory and renames, so
// an interrupted write leaves the previous file intact instead of a truncated
// one. config.json holds every preference; losing it to a crash mid-save is
// worse than the write being slightly slower.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
