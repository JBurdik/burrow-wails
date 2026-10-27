package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
)

// Turn notes: two system-info rows Go writes into a chat's own transcript so a
// person scrolling the chat sees, at the point it happened, that the agent
// made a plan ("progress") and which files the turn changed ("outputs"). Both
// open their Right Panel surface when clicked (publishSurfaceNote).
//
// Derived from the provider-neutral events, so Claude, Codex and generic ACP
// all get them with no per-transport code beyond reading a tool's input.
//
// LIVE ONLY, by construction: the single caller is the tail of emitChatLine,
// and nothing that replays a stream (LoadChatEventsSince, the fold catch-up,
// a restart) goes through emitChatLine. The notes it writes are themselves
// chat_stream rows, so a replay renders them from the log like any other row
// instead of deriving them a second time.
//
// The state is in-memory and per chat: a turn interrupted by an app restart
// simply gets no notes, which is the honest outcome for a turn nobody watched
// finish.

// turnNoteState is one chat's view of its current turn.
type turnNoteState struct {
	open      bool                // a prompt was sent and no turn end seen since
	planNoted bool                // the turn's first plan already got its note
	pending   map[string][]string // tool call id → paths it will change if it succeeds
	changed   map[string]bool     // paths a successful tool call changed this turn
}

// Keyed by *App rather than a field on App so this feature stays in one file;
// tests build many Apps with the same chat ids, so the key must include the App.
var turnNotes = struct {
	mu    sync.Mutex
	byApp map[*App]map[string]*turnNoteState
}{byApp: map[*App]map[string]*turnNoteState{}}

type surfaceNote struct{ surface, text string }

// chatTurnNotes is called once per live line, after it has been persisted,
// emitted and folded — so a note it publishes lands AFTER the line that
// triggered it (the plan row, the turn's last assistant text).
//
// Re-entrancy: publishSurfaceNote goes back through emitChatLine, which calls
// this again with chatNoteKind — ignored on the first line below. The state
// lock is released before publishing, so the nested call never runs under it.
func (a *App) chatTurnNotes(chatID, kind string, events []ProviderRuntimeEvent) {
	if kind == chatNoteKind || len(events) == 0 {
		return
	}
	a.chatRecapHook(chatID, kind, events) // session recap (chatrecap.go)
	notes := a.turnNotesFor(chatID, kind, events)
	if len(notes) == 0 {
		return
	}
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}
	for _, n := range notes {
		a.publishSurfaceNote(id, n.surface, n.text)
	}
}

// turnNotesFor advances the chat's turn state over one line's events and
// returns the notes to publish. Pure apart from the state map.
func (a *App) turnNotesFor(chatID, kind string, events []ProviderRuntimeEvent) []surfaceNote {
	turnNotes.mu.Lock()
	defer turnNotes.mu.Unlock()
	chats := turnNotes.byApp[a]
	if chats == nil {
		chats = map[string]*turnNoteState{}
		turnNotes.byApp[a] = chats
	}
	st := chats[chatID]
	closeTurn := func() { delete(chats, chatID); st = nil }

	// The human's prompt opens a turn. A second prompt while one is open (sent
	// mid-turn) joins it rather than restarting the count.
	if kind == chatUserKind {
		if st == nil {
			chats[chatID] = &turnNoteState{open: true, pending: map[string][]string{}, changed: map[string]bool{}}
		}
		return nil
	}

	var out []surfaceNote
	for _, ev := range events {
		switch ev.Type {
		case EvtSessionReset, EvtSessionExited:
			// Restart / interrupt-by-restart / clear, or the process died: the
			// turn is over with no turn end to report it.
			closeTurn()
			continue
		}
		// Outside an open turn nothing counts — in particular an ACP
		// session/load history replay (EmitHistory), which IS a live
		// emitChatLine but describes turns that already happened.
		if st == nil {
			continue
		}
		switch ev.Type {
		case EvtToolStarted:
			if ev.Name == "TodoWrite" {
				if !st.planNoted {
					if text := planNoteText(ev.Input); text != "" {
						st.planNoted = true
						out = append(out, surfaceNote{"progress", text})
					}
				}
				continue
			}
			if paths := changedPaths(kind, ev); len(paths) > 0 && ev.ToolCallID != "" {
				st.pending[ev.ToolCallID] = paths
			}
		case EvtToolCompleted:
			paths, ok := st.pending[ev.ToolCallID]
			if !ok {
				continue
			}
			delete(st.pending, ev.ToolCallID)
			if ev.Failed {
				continue // errored edit, denied write, declined patch: nothing changed
			}
			for _, p := range paths {
				st.changed[p] = true
			}
		case EvtTurnCompleted, EvtTurnFailed:
			if text := changedNoteText(st.changed); text != "" {
				out = append(out, surfaceNote{"outputs", text})
			}
			closeTurn()
		}
	}
	return out
}

// planNoteText: "Plan: <done>/<total> — <current item>", the current item
// being the first in-progress one, else the first pending one.
func planNoteText(input map[string]any) string {
	todos, _ := input["todos"].([]any)
	total, done := 0, 0
	inProgress, pending := "", ""
	for _, raw := range todos {
		todo := mapField(raw)
		content := strField(todo["content"])
		if content == "" {
			continue
		}
		total++
		switch strField(todo["status"]) {
		case "completed":
			done++
		case "in_progress":
			if inProgress == "" {
				inProgress = content
			}
		default:
			if pending == "" {
				pending = content
			}
		}
	}
	if total == 0 {
		return ""
	}
	text := fmt.Sprintf("Plan: %d/%d", done, total)
	if inProgress == "" {
		inProgress = pending
	}
	if inProgress != "" {
		text += " — " + inProgress
	}
	return text
}

func changedNoteText(changed map[string]bool) string {
	switch len(changed) {
	case 0:
		return ""
	case 1:
		for p := range changed {
			return "Changed " + filepath.Base(p)
		}
	}
	return fmt.Sprintf("Changed %d files", len(changed))
}

// changedPaths: the files a tool call will have changed if it succeeds. Mirrors
// src/lib/chatOutputs.ts (opsOf) — keep the two in step.
func changedPaths(kind string, ev ProviderRuntimeEvent) []string {
	input := ev.Input
	if input == nil {
		return nil
	}
	if kind == "claude-data" {
		switch ev.Name {
		case "Write", "Edit", "MultiEdit", "NotebookEdit":
			p := strField(input["file_path"])
			if p == "" {
				p = strField(input["notebook_path"])
			}
			if p != "" {
				return []string{p}
			}
		}
		return nil
	}
	// Codex fileChange ("Apply file changes"): changes[{path, kind:{type, move_path}}].
	if changes, ok := input["changes"].([]any); ok {
		var out []string
		for _, raw := range changes {
			c := mapField(raw)
			p := strField(c["path"])
			if p == "" {
				continue
			}
			if moved := strField(mapField(c["kind"])["move_path"]); moved != "" {
				p = moved // a rename: the file now lives there
			}
			out = append(out, p)
		}
		return out
	}
	// Generic ACP: only an input shaped like an edit counts — a path alone is
	// also what a read carries.
	p := strField(input["file_path"])
	if p == "" {
		p = strField(input["path"])
	}
	if p == "" {
		return nil
	}
	_, oldS := input["old_string"].(string)
	_, newS := input["new_string"].(string)
	_, edits := input["edits"].([]any)
	_, content := input["content"].(string)
	if oldS || newS || edits || content {
		return []string{p}
	}
	return nil
}
