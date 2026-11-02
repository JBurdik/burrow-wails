package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

// Background shell tasks a Claude chat launched (Bash run_in_background, or a
// foreground Bash the CLI moved to the background). The Right Panel's
// Background surface derives the list itself (src/lib/backgroundTasks.ts);
// Go only serves the few stream lines it needs and the one control request
// that stops a task.

// LoadChatTaskLinesSince returns the chat_stream lines that describe
// background tasks — `system` task_started / task_updated / task_notification /
// background_tasks_changed, and the Bash tool_result carrying
// backgroundTaskId (the output file path), and the assistant tool_use with
// run_in_background (the command) — from ord `since` on. A chat's
// stream is megabytes of token deltas; shipping all of it to rebuild a
// handful of rows on every poll is what this filter avoids. The LIKE is a
// prefilter: the client still parses and ignores anything that isn't one.
func (a *App) LoadChatTaskLinesSince(chatID string, since int64) ([]ChatStreamLine, error) {
	out := []ChatStreamLine{}
	if a.db == nil {
		return out, nil
	}
	rows, err := a.db.Query(
		`SELECT ord, kind, line FROM chat_stream
		 WHERE chat_id = ? AND ord >= ? AND kind = 'claude-data'
		   AND (line LIKE '%"subtype":"task!_%' ESCAPE '!'
		     OR line LIKE '%"subtype":"background!_tasks!_changed"%' ESCAPE '!'
		     OR line LIKE '%"backgroundTaskId"%'
		     OR line LIKE '%"run_in_background":true%')
		 ORDER BY ord`,
		chatID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l ChatStreamLine
		if err := rows.Scan(&l.Ord, &l.Kind, &l.Line); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ChatStopTask stops one background task of a live Claude chat through the
// CLI's own `stop_task` control request — the same path as the agent's
// TaskStop tool, so the CLI reports it back as task_updated (killed) +
// task_notification (stopped). A chat with no live process has no running
// tasks to stop: the CLI stops them when it exits.
func (a *App) ChatStopTask(chatID int64, taskID string) error {
	if taskID == "" {
		return errors.New("no task id")
	}
	if _, err := a.chatByID(chatID); err != nil {
		return err
	}
	// A Codex chat's "task" is a background terminal; its id is the
	// app-server's processId (ChatListBgTerminals).
	if sess, ok := a.liveCodexSession(chatID); ok {
		return a.stopCodexBgTerminal(sess, taskID)
	}
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()
	if !a.claudeMgr().Alive(id) {
		return errors.New("chat is not running")
	}
	return a.claudeControl(id, map[string]any{"subtype": "stop_task", "task_id": taskID})
}

// ReadFileTail returns at most the last maxBytes of path (64 KiB when
// maxBytes <= 0) — a background task's output file can grow without bound
// and is polled while the surface is visible. Same reach as ReadTextFile.
func (a *App) ReadFileTail(path string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 64 << 10
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() > maxBytes {
		if _, err := f.Seek(st.Size()-maxBytes, io.SeekStart); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes))
	return string(b), err
}

// --- Codex background terminals -------------------------------------------
//
// Codex's "unified exec" keeps a command's process alive after the tool call
// returns (a dev server, a watcher): the process outlives the turn and an
// interrupt, and only dies on `thread/backgroundTerminals/clean`, its own exit,
// the 64-process LRU cap, or the app-server shutting down (Codex core:
// unified_exec/process_manager.rs, session/handlers.rs). The app-server lists
// them and stops one through the experimental `thread/backgroundTerminals/*`
// requests (Burrow already initializes with experimentalApi). There is no
// stream event that marks a process as backgrounded, so the list is polled —
// and only a live app-server has any: when the chat's process is gone, so are
// its terminals.

// CodexBgTerminal is one row of `thread/backgroundTerminals/list`.
type CodexBgTerminal struct {
	ItemID    string `json:"itemId"`
	ProcessID string `json:"processId"`
	Command   string `json:"command"`
	Cwd       string `json:"cwd"`
}

const codexCallTimeout = 5 * time.Second

// codexReplies routes the response to a request Go itself issued on a live
// Codex session (pumpCodexLine hands every method-less line to
// deliverCodexReply first). Keyed by session, so a restarted chat's new
// app-server can never answer the old one's waiter with a reused rpc id.
var codexReplies = struct {
	sync.Mutex
	m map[*acpSession]map[int64]chan map[string]any
}{m: map[*acpSession]map[int64]chan map[string]any{}}

// deliverCodexReply reports whether msg answered a pending codexCall.
func deliverCodexReply(sess *acpSession, msg map[string]any) bool {
	rid, ok := msg["id"].(float64)
	if !ok {
		return false
	}
	codexReplies.Lock()
	ch, ok := codexReplies.m[sess][int64(rid)]
	if ok {
		delete(codexReplies.m[sess], int64(rid))
		if len(codexReplies.m[sess]) == 0 {
			delete(codexReplies.m, sess)
		}
	}
	codexReplies.Unlock()
	if ok {
		ch <- msg
	}
	return ok
}

// codexCall sends one request on a live Codex session and waits for its
// response's result.
func codexCall(sess *acpSession, method string, params map[string]any) (map[string]any, error) {
	rpc := sess.rpcID()
	ch := make(chan map[string]any, 1)
	codexReplies.Lock()
	if codexReplies.m[sess] == nil {
		codexReplies.m[sess] = map[int64]chan map[string]any{}
	}
	codexReplies.m[sess][rpc] = ch
	codexReplies.Unlock()
	forget := func() {
		codexReplies.Lock()
		delete(codexReplies.m[sess], rpc)
		if len(codexReplies.m[sess]) == 0 {
			delete(codexReplies.m, sess)
		}
		codexReplies.Unlock()
	}
	if err := sess.write(map[string]any{"jsonrpc": "2.0", "id": rpc, "method": method, "params": params}); err != nil {
		forget()
		return nil, err
	}
	select {
	case msg := <-ch:
		if failure := codexRPCErrorMessage(msg); failure != "" {
			return nil, errors.New(failure)
		}
		return mapOf(msg["result"]), nil
	case <-time.After(codexCallTimeout):
		forget()
		return nil, fmt.Errorf("codex did not answer %s", method)
	}
}

// liveCodexSession is the chat's running app-server with a loaded thread.
func (a *App) liveCodexSession(chatID int64) (*acpSession, bool) {
	sess, ok := a.acpReg().get(strconv.FormatInt(chatID, 10))
	if !ok || sess.proto != protoCodexAppServer || sess.sessionID == "" || sess.stdin == nil {
		return nil, false
	}
	return sess, true
}

// ChatListBgTerminals returns the background terminals of a Codex chat's
// thread — empty for any other chat, and for a Codex chat with no live
// app-server (its terminals died with it).
func (a *App) ChatListBgTerminals(chatID int64) ([]CodexBgTerminal, error) {
	out := []CodexBgTerminal{}
	sess, ok := a.liveCodexSession(chatID)
	if !ok {
		return out, nil
	}
	cursor := ""
	for page := 0; page < 10; page++ {
		params := map[string]any{"threadId": sess.sessionID}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := codexCall(sess, "thread/backgroundTerminals/list", params)
		if err != nil {
			return out, err
		}
		data, _ := result["data"].([]any)
		for _, raw := range data {
			t := mapOf(raw)
			row := CodexBgTerminal{}
			row.ItemID, _ = t["itemId"].(string)
			row.ProcessID, _ = t["processId"].(string)
			row.Command, _ = t["command"].(string)
			row.Cwd, _ = t["cwd"].(string)
			if row.ProcessID != "" {
				out = append(out, row)
			}
		}
		next, _ := result["nextCursor"].(string)
		if next == "" || next == cursor {
			break
		}
		cursor = next
	}
	return out, nil
}

// stopCodexBgTerminal terminates one background terminal of a Codex chat.
func (a *App) stopCodexBgTerminal(sess *acpSession, processID string) error {
	result, err := codexCall(sess, "thread/backgroundTerminals/terminate",
		map[string]any{"threadId": sess.sessionID, "processId": processID})
	if err != nil {
		return err
	}
	if ok, _ := result["terminated"].(bool); !ok {
		return errors.New("the process had already exited")
	}
	return nil
}
