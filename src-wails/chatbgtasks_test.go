package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadChatTaskLinesSinceFiltersTaskLines(t *testing.T) {
	a := newTestApp(t)
	const chat = "7"
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"\", \"run_in_background\": true"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"pnpm dev","run_in_background":true}}]}}`,
		`{"type":"system","subtype":"task_started","task_id":"b1","tool_use_id":"toolu_1","is_backgrounded":true,"task_type":"local_bash"}`,
		`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1"}]}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"Command running in background with ID: b1."}]},"tool_use_result":{"backgroundTaskId":"b1"}}`,
		`{"type":"system","subtype":"status","status":null}`,
		`{"type":"system","subtype":"taskXstarted"}`,
		`{"type":"system","subtype":"task_notification","task_id":"b1","status":"stopped"}`,
	}
	for _, l := range lines {
		a.chatStream().append(chat, "claude-data", l)
	}
	drainChatStream(t, a, chat)

	got, err := a.LoadChatTaskLinesSince(chat, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("want 5 task lines (assistant, started, snapshot, result, notification), got %d: %+v", len(got), got)
	}
	for _, l := range got {
		if strings.Contains(l.Line, "stream_event") || strings.Contains(l.Line, "taskXstarted") {
			t.Fatalf("unexpected line %s", l.Line)
		}
	}
	rest, err := a.LoadChatTaskLinesSince(chat, got[len(got)-1].Ord+1)
	if err != nil || len(rest) != 0 {
		t.Fatalf("since past the last line: %v %+v", err, rest)
	}
}

func TestReadFileTail(t *testing.T) {
	a := &App{}
	p := filepath.Join(t.TempDir(), "x.output")
	if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s, err := a.ReadFileTail(p, 4); err != nil || s != "6789" {
		t.Fatalf("tail 4 = %q, %v", s, err)
	}
	if s, err := a.ReadFileTail(p, 100); err != nil || s != "0123456789" {
		t.Fatalf("tail 100 = %q, %v", s, err)
	}
	if _, err := a.ReadFileTail(p+".missing", 4); err == nil {
		t.Fatal("missing file should error")
	}
}

// fakeCodexServer answers every request Go writes to sess through the real
// pump, the way a live app-server's stdout would reach pumpCodexLine.
func fakeCodexServer(t *testing.T, a *App, chatID string, sess *acpSession, r io.Reader, answer func(req map[string]any) map[string]any) {
	t.Helper()
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			var req map[string]any
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				continue
			}
			reply := answer(req)
			reply["id"] = req["id"]
			a.pumpCodexLine(chatID, reply, sess)
		}
	}()
}

func TestChatListAndStopCodexBgTerminals(t *testing.T) {
	a := newTestApp(t)
	chat, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "codex", AgentKind: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(chat.ID, 10)
	pr, pw := io.Pipe()
	defer pw.Close()
	sess := &acpSession{stdin: pw, proto: protoCodexAppServer, sessionID: "thread-1", pendingTurn: 99}
	a.acpReg().put(id, sess)
	defer func() {
		sess.mu.Lock()
		if sess.turnWatchdog != nil {
			sess.turnWatchdog.Stop()
		}
		sess.mu.Unlock()
	}()
	var seen []string
	fakeCodexServer(t, a, id, sess, pr, func(req map[string]any) map[string]any {
		method, _ := req["method"].(string)
		seen = append(seen, method+" "+fmt.Sprint(mapOf(req["params"])["threadId"]))
		switch method {
		case "thread/backgroundTerminals/list":
			// Shape captured from codex-cli 0.164.0, plus a row.
			return map[string]any{"result": map[string]any{"data": []any{map[string]any{
				"itemId": "exec-1376590c", "processId": "83564", "command": "/bin/zsh -lc 'PORT=8801 .venv/bin/python webui.py'",
				"cwd": "/Users/x/suno-local", "osPid": nil, "cpuPercent": nil, "rssKb": nil,
			}}, "nextCursor": nil}}
		case "thread/backgroundTerminals/terminate":
			if mapOf(req["params"])["processId"] == "83564" {
				return map[string]any{"result": map[string]any{"terminated": true}}
			}
			return map[string]any{"result": map[string]any{"terminated": false}}
		}
		return map[string]any{"error": map[string]any{"code": -32601, "message": "nope"}}
	})

	got, err := a.ChatListBgTerminals(chat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ProcessID != "83564" || got[0].ItemID != "exec-1376590c" || got[0].Cwd != "/Users/x/suno-local" {
		t.Fatalf("list = %+v", got)
	}
	if err := a.ChatStopTask(chat.ID, "83564"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := a.ChatStopTask(chat.ID, "1"); err == nil {
		t.Fatal("stopping an exited terminal should report it")
	}
	if len(seen) != 3 || seen[0] != "thread/backgroundTerminals/list thread-1" {
		t.Fatalf("requests = %v", seen)
	}
	// Our replies are not the turn's: the pending turn is untouched.
	sess.mu.Lock()
	pending := sess.pendingTurn
	sess.mu.Unlock()
	if pending != 99 {
		t.Fatalf("a codexCall reply settled the turn (pendingTurn=%d)", pending)
	}
}

func TestChatListBgTerminalsWithoutCodexSession(t *testing.T) {
	a := newTestApp(t)
	got, err := a.ChatListBgTerminals(42)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no live codex session → empty list, got %+v, %v", got, err)
	}
}
