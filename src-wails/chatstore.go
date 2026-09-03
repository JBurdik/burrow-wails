package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
// or order by. If message-level search ever lands, add an FTS index over
// payload_json's text — the rows are already there.

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
func (a *App) LoadChatMessages(chatID int) (string, error) {
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
func (a *App) SaveChatMessages(chatID int, messagesJSON string) error {
	if a.db == nil {
		return fmt.Errorf("db not open")
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(messagesJSON), &msgs); err != nil {
		return fmt.Errorf("decode messages: %w", err)
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
	return tx.Commit()
}

func (a *App) DeleteChatMessages(chatID int) error {
	if a.db == nil {
		return nil
	}
	_, err := a.db.Exec(`DELETE FROM chat_messages WHERE chat_id = ?`, chatID)
	return err
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
		if err := a.SaveChatMessages(id, string(msgs)); err != nil {
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
