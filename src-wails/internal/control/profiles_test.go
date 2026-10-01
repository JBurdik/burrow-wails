package control

import (
	"context"
	"testing"
)

func TestSpawnProfileForwardedWithOverrides(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{ChatID: 9, Target: "chat"}}
	c := newTestCore(t, Deps{UI: ui})
	_, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{"task": "Review the diff", "profile": "reviewer", "agent": "codex", "model": "chosen-model", "parent_chat_id": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if ui.args["profile"] != "reviewer" || ui.args["model"] != "chosen-model" || ui.args["agent"] != "codex" || ui.args["target"] != "chat" {
		t.Fatalf("forwarded args: %+v", ui.args)
	}
}

func TestListSubagentProfilesUsesSharedUIStore(t *testing.T) {
	ui := &fakeUI{result: []map[string]string{{"id": "scout"}}}
	c := newTestCore(t, Deps{UI: ui})
	_, err := c.Call(context.Background(), ScopeLocal, "list_subagent_profiles", nil)
	if err != nil || ui.action != "list_subagent_profiles" {
		t.Fatalf("action=%s err=%v", ui.action, err)
	}
}
