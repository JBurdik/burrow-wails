package main

import (
	"testing"
	"time"
)

func TestHeuristicChatTitle(t *testing.T) {
	for in, want := range map[string]string{
		"can you fix the   status dots":                  "Fix the status dots",
		"od apple integrace foundation models. se stává": "Od apple integrace foundation models. se…",
		"čau": "Čau",
	} {
		if got := heuristicChatTitle(in); got != want {
			t.Errorf("heuristicChatTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutoTitleChat(t *testing.T) {
	a := newTestApp(t)
	done := make(chan struct{}, 1)
	prev := chatTitleGenerate
	chatTitleGenerate = func(*App, string, string, string, string) string { done <- struct{}{}; return "Model title" }
	t.Cleanup(func() { chatTitleGenerate = prev })

	title := func(id int64) string {
		c, err := a.chatByID(id)
		if err != nil {
			t.Fatal(err)
		}
		return c.Title
	}

	c, err := a.CreateChat(Chat{WorkspaceID: 1, Title: "Chat 13"})
	if err != nil {
		t.Fatal(err)
	}
	a.autoTitleChat(c, "fix the dots")
	if got := title(c.ID); got != "Fix the dots" {
		t.Fatalf("heuristic: got %q", got)
	}
	<-done
	// The goroutine writes right after the stub returns; SaveChats with the
	// stale default must not undo either write.
	for i := 0; i < 100 && title(c.ID) != "Model title"; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if got := title(c.ID); got != "Model title" {
		t.Fatalf("model: got %q", got)
	}
	c.Title = "Chat 13"
	if err := a.SaveChats([]Chat{c}); err != nil {
		t.Fatal(err)
	}
	if got := title(c.ID); got != "Model title" {
		t.Fatalf("stale save reverted the title to %q", got)
	}

	// A named thread is left alone.
	named, _ := a.CreateChat(Chat{WorkspaceID: 1, Title: "My name"})
	a.autoTitleChat(named, "fix the dots")
	if got := title(named.ID); got != "My name" {
		t.Fatalf("named: got %q", got)
	}
}
