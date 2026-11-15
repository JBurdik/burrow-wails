package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Images by reference (ADR 0001 phase B). A user message's images are stored
// once, here, when its line is recorded; the chat-user line, chat_messages
// and every splice / page carry only a reference. Content-addressed per chat,
// so a repeat attachment is one row and a fork copies rows, not data.
//
// Two reference shapes:
//   - stored:  "burrow-img:<sha256>"       — what lines and chat_messages hold
//   - on wire: "burrow-image://<index>/<n>" (chatImageStubPrefix) — what
//     clients see in splices and pages, fetched with load_chat_image
//
// The agent CLI always gets the full data: emitUserLine is handed the
// already-sent images, only the recorded copy is swapped for refs.

const chatImageRefPrefix = "burrow-img:"

func chatImagesSchema() []string {
	return []string{`CREATE TABLE IF NOT EXISTS chat_images (
		chat_id INTEGER NOT NULL,
		hash TEXT NOT NULL,
		data TEXT NOT NULL,
		PRIMARY KEY (chat_id, hash)
	)`}
}

// storeChatImages stores every data-URL image and returns the list with each
// replaced by its ref. A failed write keeps that image inline: a fat line beats
// a lost picture.
func (a *App) storeChatImages(chatID string, images []string) []string {
	if len(images) == 0 || a.db == nil {
		return images
	}
	id, err := strconv.Atoi(chatID)
	if err != nil {
		return images
	}
	out := make([]string, len(images))
	for i, img := range images {
		out[i] = img
		if !strings.HasPrefix(img, "data:") {
			continue
		}
		sum := sha256.Sum256([]byte(img))
		hash := hex.EncodeToString(sum[:])
		if _, err := a.db.Exec(`INSERT OR IGNORE INTO chat_images (chat_id, hash, data) VALUES (?, ?, ?)`, id, hash, img); err != nil {
			log.Printf("chat images: store %s: %v", chatID, err)
			continue
		}
		out[i] = chatImageRefPrefix + hash
	}
	return out
}

// resolveChatImage turns a stored ref back into its data URL; anything else
// is returned as is.
func (a *App) resolveChatImage(chatID int, img string) (string, error) {
	hash, ok := strings.CutPrefix(img, chatImageRefPrefix)
	if !ok {
		return img, nil
	}
	if a.db == nil {
		return "", fmt.Errorf("db not open")
	}
	var data string
	err := a.db.QueryRow(`SELECT data FROM chat_images WHERE hash = ? ORDER BY chat_id = ? DESC LIMIT 1`, hash, chatID).Scan(&data)
	if err != nil {
		return "", fmt.Errorf("image %s: %w", hash, err)
	}
	return data, nil
}

// noteImagesByRef swaps a chat-note's inline images for refs.
func (a *App) noteImagesByRef(chatID, note string) string {
	if !strings.Contains(note, `"data:`) {
		return note
	}
	var env chatNoteEnvelope
	if json.Unmarshal([]byte(note), &env) != nil || len(env.Images) == 0 {
		return note
	}
	env.Images = a.storeChatImages(chatID, env.Images)
	b, err := json.Marshal(env)
	if err != nil {
		return note
	}
	return string(b)
}

// lineImagesByRef swaps the inline data-URL images of a recorded chat-user /
// chat-note line for stored refs — what LoadChatStreamSince replays, so a line
// recorded before images went by reference doesn't ship its base64 again.
// Storing on read is idempotent (INSERT OR IGNORE, content-addressed).
//
// ponytail: provider raw lines (a Claude stream-json user record with an image
// block, a tool_result screenshot) still replay inline; ref them if a replay
// client shows up that is not the desktop's own bus.
func (a *App) lineImagesByRef(chatID, kind, line string) string {
	if !strings.Contains(line, `"data:`) {
		return line
	}
	switch kind {
	case chatNoteKind:
		return a.noteImagesByRef(chatID, line)
	case chatUserKind:
		u := decodeUserLine(line)
		if len(u.Images) == 0 {
			return line
		}
		return encodeUserLine(u.Text, a.storeChatImages(chatID, u.Images), u.ClientID)
	}
	return line
}

// stubRowImages replaces a row's stored refs — and, unless refsOnly, its large
// inline data URLs (old rows) — with "burrow-image://<index>/<n>". Never
// mutates the row's own slice: it belongs to the fold.
func stubRowImages(m ChatMessage, index int, refsOnly bool) ChatMessage {
	var imgs []string
	for n, img := range m.Images {
		stub := strings.HasPrefix(img, chatImageRefPrefix) ||
			(!refsOnly && len(img) >= chatImageStubMin && strings.HasPrefix(img, "data:"))
		if !stub {
			continue
		}
		if imgs == nil {
			imgs = append([]string(nil), m.Images...)
		}
		imgs[n] = fmt.Sprintf("%s%d/%d", chatImageStubPrefix, index, n)
	}
	if imgs != nil {
		m.Images = imgs
	}
	return m
}

// copyChatImages gives a fork its parent's stored images.
func (a *App) copyChatImages(from, to int64) {
	if a.db == nil {
		return
	}
	if _, err := a.db.Exec(`INSERT OR IGNORE INTO chat_images (chat_id, hash, data) SELECT ?, hash, data FROM chat_images WHERE chat_id = ?`, to, from); err != nil {
		log.Printf("chat images: copy %d → %d: %v", from, to, err)
	}
}

func (a *App) deleteChatImages(chatID int) {
	if a.db != nil {
		if _, err := a.db.Exec(`DELETE FROM chat_images WHERE chat_id = ?`, chatID); err != nil {
			log.Printf("chat images: delete %d: %v", chatID, err)
		}
	}
}
