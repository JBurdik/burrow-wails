package main

import "testing"

// chatIsControl backs the IMPORTANT 6 exemption (verbs_delegate.go's spawn):
// derived from the DB, same shape as chatIsSubagent, so the server decides
// this rather than trusting the request.
func TestChatIsControl(t *testing.T) {
	a := newTestApp(t)

	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, control) VALUES (1, 1, 'Manager', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, control) VALUES (2, 1, 'Chat 1', 0)`,
	); err != nil {
		t.Fatal(err)
	}

	if !a.chatIsControl(1) {
		t.Error("chat 1 is control:true, want chatIsControl true")
	}
	if a.chatIsControl(2) {
		t.Error("chat 2 is control:false, want chatIsControl false")
	}
	if a.chatIsControl(999) {
		t.Error("an unknown chat id must not read as control")
	}
	if a.chatIsControl(0) {
		t.Error("id 0 (no parent) must not read as control")
	}
}
