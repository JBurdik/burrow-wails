package main

import (
	"strconv"
	"strings"
	"testing"
)

func TestPendingRequestsDescribeQuestionsAndApprovals(t *testing.T) {
	lines := []ChatStreamLine{
		{Kind: "claude-data", Line: `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which DB?","header":"DB","multiSelect":false,"options":[{"label":"Postgres"},{"label":"SQLite"}]}]}}}`},
		{Kind: "claude-data", Line: `{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm -rf build"}}}`},
		{Kind: "acp-req", Line: `{"jsonrpc":"2.0","id":7,"method":"item/tool/requestUserInput","params":{"questions":[{"id":"q1","header":"Key","question":"API key?","isSecret":true,"options":[]}]}}`},
	}
	got := pendingRequests(lines)
	if len(got) != 3 {
		t.Fatalf("got %d", len(got))
	}
	q := got[0]
	if q.Kind != "question" || q.ID != "r1" || q.Questions[0].ID != "Which DB?" || len(q.Questions[0].Options) != 2 {
		t.Errorf("claude question = %+v", q)
	}
	if a := got[1]; a.Kind != "approval" || a.Tool != "Bash" || a.Detail != "rm -rf build" {
		t.Errorf("approval = %+v", a)
	}
	if c := got[2]; c.Kind != "question" || c.ID != "7" || c.Questions[0].ID != "q1" || !c.Questions[0].Secret {
		t.Errorf("codex question = %+v", c)
	}
}

func TestStreamItemsFoldsTimeline(t *testing.T) {
	lines := []ChatStreamLine{
		{Ord: 1, Kind: chatUserKind, Line: "fix the bug"},
		{Ord: 2, Kind: "claude-data", Line: `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Looking."}]}}`},
		{Ord: 3, Kind: "claude-data", Line: `{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}`},
		{Ord: 4, Kind: "claude-data", Line: `{"type":"result","subtype":"success","is_error":false,"result":"done"}`},
	}
	msgs := streamItems(lines, false)
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[0].Text != "fix the bug" || msgs[1].Role != "assistant" || msgs[1].Text != "Looking." {
		t.Fatalf("messages view = %+v", msgs)
	}
	act := streamItems(lines, true)
	if len(act) != 4 || act[2].Role != "tool" || act[2].Text != "Bash: go test ./..." || act[3].Role != "turn" {
		t.Fatalf("activity view = %+v", act)
	}
}

func TestReadChatAndPendingFromTheLog(t *testing.T) {
	a, ws := noticeApp(t)
	c, err := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	a.emitChatLine(id, chatUserKind, "pick a db")
	a.emitChatLine(id, "claude-data", `{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Let me ask."}]}}`)
	a.emitChatLine(id, "claude-data", `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which DB?","options":[{"label":"Postgres"}]}]}}}`)

	got, err := a.readChat(c.ID, 0, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "waiting_input" || len(got.Items) != 2 || got.Items[1].Text != "Let me ask." || len(got.Pending) != 1 || got.Pending[0].Kind != "question" {
		t.Fatalf("first read = %+v", got)
	}
	again, err := a.readChat(c.ID, got.Next, 30, false)
	if err != nil || len(again.Items) != 0 || again.Next != got.Next {
		t.Fatalf("incremental read = %+v %v", again, err)
	}
}

func TestAnswerChatQuestionWritesTheToolAnswer(t *testing.T) {
	a, fake, ws, _ := runtimeApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	a.phases = store
	c, _ := a.CreateChat(Chat{WorkspaceID: ws, Transport: "claude-cli"})
	if err := a.ChatSend(c.ID, "go", nil); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	a.emitChatLine(id, "claude-data", `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which DB?"}]}}}`)
	a.emitChatLine(id, "claude-data", `{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`)

	if err := a.answerChatQuestion(c.ID, "r2", map[string][]string{"x": {"y"}}); err == nil {
		t.Fatal("answered an approval")
	}
	if err := a.answerChatQuestion(c.ID, "", map[string][]string{"Which DB?": {"Postgres"}}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	last := fake.sent[len(fake.sent)-1]
	fake.mu.Unlock()
	if !strings.Contains(last, `"request_id":"r1"`) || !strings.Contains(last, `"answers":{"Which DB?":"Postgres"}`) {
		t.Fatalf("wrote %s", last)
	}
}

func TestTranscriptItemsToolRow(t *testing.T) {
	a := newTestApp(t)
	if err := a.SaveChatMessages(9, `[{"id":1,"role":"user","text":"hi"},{"id":2,"role":"tool","text":"Bash","toolInput":{"command":"ls -la"}},{"id":3,"role":"assistant","text":"done"}]`, 0); err != nil {
		t.Fatal(err)
	}
	got, err := a.transcriptItems(9, true)
	if err != nil || len(got) != 3 || got[1].Role != "tool" || got[1].Text != "Bash: ls -la" {
		t.Fatalf("got %+v %v", got, err)
	}
	if msgs, _ := a.transcriptItems(9, false); len(msgs) != 2 {
		t.Fatalf("messages view = %+v", msgs)
	}
}
