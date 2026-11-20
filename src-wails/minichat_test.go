package main

import "testing"

func TestMiniChatReusesLatestUntilFresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // scratch dir lives under appDataDir
	a := newTestApp(t)
	first, err := a.MiniChat(false)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := a.MiniChat(false)
	if again["chatId"] != first["chatId"] || again["workspaceId"] != first["workspaceId"] {
		t.Fatalf("reopen should reuse: %v vs %v", first, again)
	}
	fresh, _ := a.MiniChat(true)
	if fresh["chatId"] == first["chatId"] || fresh["workspaceId"] != first["workspaceId"] {
		t.Fatalf("fresh should be a new chat in the same scratch workspace: %v vs %v", first, fresh)
	}
}
