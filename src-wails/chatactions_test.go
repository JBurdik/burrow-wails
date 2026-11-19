package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseReplyActions(t *testing.T) {
	ok := "```action\n{\"label\":\"Go\",\"kind\":\"new_thread\",\"brief\":\"do it\"}\n```"
	send := "```action\n{\"label\":\"Ask\",\"kind\":\"send\",\"brief\":\"hi\",\"chat_id\":7}\n```"
	cases := []struct {
		name string
		text string
		want []string // labels; "" = a block that takes an index but is no button
	}{
		{"none", "plain reply", nil},
		{"one", "Intro\n" + ok + "\nafter", []string{"Go"}},
		{"two in order", ok + "\ntext\n" + send, []string{"Go", "Ask"}},
		{"bad json keeps its index", "```action\n{nope\n```\n" + send, []string{"", "Ask"}},
		{"send without chat_id", "```action\n{\"label\":\"x\",\"kind\":\"send\",\"brief\":\"b\"}\n```", []string{""}},
		{"unknown kind", "```action\n{\"label\":\"x\",\"kind\":\"rm\",\"brief\":\"b\"}\n```", []string{""}},
		{"unclosed = streaming code", "```action\n{\"label\":\"Go\",\"kind\":\"new_thread\",\"brief\":\"x\"}", nil},
		{"nested in a longer fence is content", "````md\n" + ok + "\n````\n" + send, []string{"Ask"}},
		{"other langs skipped", "```bash\nls\n```\n" + ok, []string{"Go"}},
		{"indented (list item)", "- item\n  ```action\n  {\"label\":\"Go\",\"kind\":\"new_thread\",\"brief\":\"x\"}\n  ```", []string{"Go"}},
	}
	for _, c := range cases {
		got := parseReplyActions(c.text)
		var labels []string
		for _, a := range got {
			if a == nil {
				labels = append(labels, "")
			} else {
				labels = append(labels, a.Label)
			}
		}
		if strings.Join(labels, "|") != strings.Join(c.want, "|") || len(labels) != len(c.want) {
			t.Errorf("%s: got %q, want %q", c.name, labels, c.want)
		}
	}
}

func TestPressedNote(t *testing.T) {
	if got := pressedNote([]string{"A", "B"}); got != "\n[Pressed: A]\n[Pressed: B]" {
		t.Fatalf("note = %q", got)
	}
}

// A "send" button: runs once, the target gets the stored brief, the next user
// message to the writer carries [Pressed: …] once; a writer below full access
// can't drive a freer chat.
func TestChatPressActionSendOnce(t *testing.T) {
	a, fake, wsID, _ := runtimeApp(t)
	withPhases(t, a)
	writeConfig(t, map[string]any{"providers": []any{claudeAt("claude", "")}})
	writer, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	target, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli"})
	free, _ := a.CreateChat(Chat{WorkspaceID: wsID, Transport: "claude-cli", PermissionMode: "full-access"})

	reply := "Do this:\n```action\n" + `{"label":"Ask it","kind":"send","brief":"stored brief","chat_id":` + itoa(target.ID) + "}\n```\n" +
		"```action\n" + `{"label":"Free","kind":"send","brief":"x","chat_id":` + itoa(free.ID) + "}\n```"
	msgs, _ := json.Marshal([]ChatMessage{{ID: 0, Role: "user", Text: "hi"}, {ID: 1, Role: "assistant", Text: reply}})
	if err := a.SaveChatMessages(int(writer.ID), string(msgs), 0); err != nil {
		t.Fatal(err)
	}

	p, err := a.ChatPressAction(writer.ID, 1, 0)
	if err != nil || p.State != "done" || p.ResultChatID != target.ID {
		t.Fatalf("press = %+v, %v", p, err)
	}
	if n := countSent(fake.sent, "stored brief"); n != 1 {
		t.Fatalf("brief sent %d times", n)
	}
	if p, err := a.ChatPressAction(writer.ID, 1, 0); err != nil || p.State != "done" {
		t.Fatalf("second press = %+v, %v", p, err)
	}
	if n := countSent(fake.sent, "stored brief"); n != 1 {
		t.Fatalf("second press re-ran: %d", n)
	}

	// Writer in default mode → may not send into a full-access chat.
	if p, err := a.ChatPressAction(writer.ID, 1, 1); err == nil || p.State != "failed" {
		t.Fatalf("freer target = %+v, %v; want failed", p, err)
	}
	if _, err := a.ChatPressAction(writer.ID, 1, 2); err == nil {
		t.Fatal("missing button must fail")
	}

	list, _ := a.ChatActionPresses(writer.ID)
	if len(list) != 2 {
		t.Fatalf("presses = %+v", list)
	}

	res, err := a.ChatSubmit(writer.ID, "go ahead", nil, "u1")
	if err != nil || res.Text != "go ahead\n[Pressed: Ask it]" {
		t.Fatalf("submit = %+v, %v", res, err)
	}
	if note, _ := a.takePressedNote(writer.ID); note != "" {
		t.Fatalf("reported twice: %q", note)
	}

	a.dropActionPresses(writer.ID, 0)
	if list, _ := a.ChatActionPresses(writer.ID); len(list) != 0 {
		t.Fatalf("after clear = %+v", list)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
