package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Transcript notes for a Claude chat's background shell tasks: one row when a
// task goes to the background, one when it ends, each opening the Right
// Panel's Background surface. Written through publishSurfaceNote, so they are
// ordinary chat-note lines: persisted, replayed and folded like any other.
//
// Fed ONLY from the live Claude reader (claudechat.go's onLine, after the line
// itself was emitted) — never from emitChatLine, which also carries notes and
// would re-enter, and never from a replay (replayChatStream /
// LoadChatEventsSince read chat_stream and do not go through the reader). A
// restart therefore re-reads the notes already written instead of writing
// them again.
//
// Codex is deliberately not covered: its background terminals have no stream
// signal at all (only the polled `thread/backgroundTerminals/list`), so a
// note would have to come from a poll diff — not reliable enough to be worth a
// transcript row.

// bgNoteState is the per-chat dedupe and the bits of earlier lines a note
// needs. Process-lifetime only: a finish for a task backgrounded before a
// restart is recognised through chat_stream (bgTaskFromStream).
type bgNoteState struct {
	commands map[string]string      // Bash tool_use id → command
	tasks    map[string]*bgNoteTask // task id → what we know
}

type bgNoteTask struct {
	toolUseID   string
	description string
	background  bool
	startNoted  bool
	finishNoted bool
}

var bgNotes = struct {
	sync.Mutex
	chats map[string]*bgNoteState
}{chats: map[string]*bgNoteState{}}

// bgNoteMaxCommands bounds the tool_use → command map: every Bash call lands
// in it, only backgrounded ones are ever read back.
const bgNoteMaxCommands = 256

var bgExitCodeRE = regexp.MustCompile(`exit code (-?\d+)`)

// bgNoteLine is the live stream line as far as these notes care.
type bgNoteLine struct {
	Type         string `json:"type"`
	Subtype      string `json:"subtype"`
	TaskID       string `json:"task_id"`
	ToolUseID    string `json:"tool_use_id"`
	Description  string `json:"description"`
	TaskType     string `json:"task_type"`
	Backgrounded *bool  `json:"is_backgrounded"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	Patch        struct {
		Backgrounded *bool `json:"is_backgrounded"`
	} `json:"patch"`
	Message struct {
		Content []struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			Name  string `json:"name"`
			Input struct {
				Command string `json:"command"`
			} `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// noteBgTaskLine reads one live claude-data line and publishes the
// background-task note it implies, if any. Must not be called with a lock
// held that emitChatLine takes.
func (a *App) noteBgTaskLine(chatID, line string) {
	if !strings.Contains(line, `"task_`) && !strings.Contains(line, `"Bash"`) {
		return // the overwhelming majority: token deltas
	}
	var m bgNoteLine
	if json.Unmarshal([]byte(line), &m) != nil {
		return
	}
	if text := bgNoteFor(chatID, m, a.bgTaskFromStream); text != "" {
		if id := chatIDOf(chatID); id > 0 {
			a.publishSurfaceNote(id, "background", text)
		}
	}
}

// bgNoteFor advances the chat's state with m and returns the note text to
// publish ("" = none). lookup recovers a task this process never saw start.
func bgNoteFor(chatID string, m bgNoteLine, lookup func(chatID, taskID string) *bgNoteTask) string {
	bgNotes.Lock()
	st := bgNotes.chats[chatID]
	if st == nil {
		st = &bgNoteState{commands: map[string]string{}, tasks: map[string]*bgNoteTask{}}
		bgNotes.chats[chatID] = st
	}
	defer bgNotes.Unlock()

	switch {
	case m.Type == "assistant":
		for _, c := range m.Message.Content {
			if c.Type == "tool_use" && c.Name == "Bash" && c.ID != "" && c.Input.Command != "" {
				if len(st.commands) >= bgNoteMaxCommands {
					st.commands = map[string]string{}
				}
				st.commands[c.ID] = c.Input.Command
			}
		}
		return ""
	case m.Type != "system" || m.TaskID == "":
		return ""
	}

	t := st.tasks[m.TaskID]
	switch m.Subtype {
	case "task_started":
		if m.TaskType != "" && m.TaskType != "local_bash" {
			return ""
		}
		if t == nil {
			t = &bgNoteTask{}
			st.tasks[m.TaskID] = t
		}
		t.toolUseID, t.description = m.ToolUseID, m.Description
		if m.Backgrounded != nil && *m.Backgrounded {
			t.background = true
		}
	case "task_updated":
		if t == nil || m.Patch.Backgrounded == nil || !*m.Patch.Backgrounded {
			return ""
		}
		t.background = true
	case "task_notification":
		if t == nil && lookup != nil {
			bgNotes.Unlock() // lookup reads SQLite; never hold the map across it
			found := lookup(chatID, m.TaskID)
			bgNotes.Lock()
			if t = st.tasks[m.TaskID]; t == nil && found != nil {
				t = found
				t.startNoted = true // that note belongs to the previous run
				st.tasks[m.TaskID] = t
			}
		}
		if t == nil || !t.background || t.finishNoted {
			return ""
		}
		t.finishNoted = true
		label := bgTaskLabel(st, t)
		delete(st.commands, t.toolUseID)
		return bgFinishText(m.Status, m.Summary, label)
	default:
		return ""
	}

	if !t.background || t.startNoted {
		return ""
	}
	t.startNoted = true
	return "Started in background: " + bgTaskLabel(st, t)
}

// bgTaskLabel is the command's first line, else the CLI's description,
// truncated for a one-line row.
func bgTaskLabel(st *bgNoteState, t *bgNoteTask) string {
	s := st.commands[t.toolUseID]
	if strings.TrimSpace(s) == "" {
		s = t.description
	}
	s = strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if s == "" {
		return "shell command"
	}
	if r := []rune(s); len(r) > 80 {
		s = string(r[:79]) + "…"
	}
	return s
}

// bgFinishText words the end of a task the way the Background surface
// classifies it (backgroundTasks.ts): "completed" with a non-zero exit code
// is a failure.
func bgFinishText(status, summary, label string) string {
	code := ""
	if mm := bgExitCodeRE.FindStringSubmatch(summary); mm != nil {
		code = mm[1]
	}
	head := "Background task finished"
	switch status {
	case "completed":
		if code != "" && code != "0" {
			head = fmt.Sprintf("Background task failed (exit %s)", code)
		}
	case "failed":
		head = "Background task failed"
		if code != "" {
			head = fmt.Sprintf("Background task failed (exit %s)", code)
		}
	case "stopped", "killed":
		head = "Background task stopped"
	}
	return head + ": " + label
}

// bgTaskFromStream rebuilds a task this process never saw start (it started
// before an app restart) from the chat's recorded stream: backgrounded or
// not, its description, and the Bash command its tool_use carried. nil when
// the stream has no task_started for it.
func (a *App) bgTaskFromStream(chatID, taskID string) *bgNoteTask {
	if a.db == nil || taskID == "" {
		return nil
	}
	idJSON, _ := json.Marshal(taskID)
	rows, err := a.db.Query(
		`SELECT line FROM chat_stream WHERE chat_id = ? AND kind = 'claude-data'
		   AND line LIKE '%"subtype":"task!_%' ESCAPE '!' AND instr(line, ?) > 0
		 ORDER BY ord`,
		chatID, `"task_id":`+string(idJSON),
	)
	if err != nil {
		return nil
	}
	var t *bgNoteTask
	for rows.Next() {
		var line string
		var m bgNoteLine
		if rows.Scan(&line) != nil || json.Unmarshal([]byte(line), &m) != nil || m.TaskID != taskID {
			continue
		}
		switch m.Subtype {
		case "task_started":
			if m.TaskType != "" && m.TaskType != "local_bash" {
				rows.Close()
				return nil
			}
			t = &bgNoteTask{toolUseID: m.ToolUseID, description: m.Description,
				background: m.Backgrounded != nil && *m.Backgrounded}
		case "task_updated":
			if t != nil && m.Patch.Backgrounded != nil && *m.Patch.Backgrounded {
				t.background = true
			}
		}
	}
	rows.Close()
	if t == nil || t.toolUseID == "" {
		return t
	}
	// The command, for the label: the assistant line holding that tool_use.
	useJSON, _ := json.Marshal(t.toolUseID)
	var line string
	if a.db.QueryRow(
		`SELECT line FROM chat_stream WHERE chat_id = ? AND kind = 'claude-data'
		   AND instr(line, ?) > 0 AND line LIKE '%"type":"assistant"%' ORDER BY ord LIMIT 1`,
		chatID, `"id":`+string(useJSON),
	).Scan(&line) == nil {
		var m bgNoteLine
		if json.Unmarshal([]byte(line), &m) == nil {
			for _, c := range m.Message.Content {
				if c.Type == "tool_use" && c.ID == t.toolUseID && c.Input.Command != "" {
					t.description = c.Input.Command // command beats description as label
				}
			}
		}
	}
	return t
}
