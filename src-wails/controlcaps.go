package main

import (
	"database/sql"
	"fmt"

	"burrow/internal/control"
)

// What a chat's own key may do over the control API. A chat in full access
// (bypass) is unlimited, as is the master token; any other mode can't use
// Burrow to get past its own permission mode: no more permissive sub-agents,
// no driving a chat that runs freer than it, no other thread's sub-agents.

// capChat is one chat as the gate sees it.
type capChat struct {
	ID, Parent int64
	Mode       string
}

// capCall is one verb call from a chat key, with what it touches resolved.
type capCall struct {
	Verb    string
	Caller  capChat
	Target  *capChat // the chat the verb acts on; nil = none / unknown
	Mode    string   // the permission_mode the call asks for
	Detach  bool     // spawn: a top-level chat/tab, not a sub-agent
	NewChat bool     // schedule: runs start new chats
	Check   bool     // schedule: a shell command run by the core
}

// verbs that act on Target (reads are not gated).
var capActs = map[string]bool{
	"chat_send": true, "chat_answer": true, "chat_interrupt": true, "close_chat": true,
	"schedule": true, "schedule_cancel": true, "schedule_pause": true, "schedule_resume": true,
	"schedule_run_now": true, "move_to_worktree": true, "leave_worktree": true,
}

const capAskUser = " Ask the user to do it instead (or to give this thread a more permissive mode)."

// capabilityProblem is the whole rule: "" = allowed, else why not.
func capabilityProblem(c capCall) string {
	own := permRank(c.Caller.Mode)
	if own >= permRank("full-access") {
		return ""
	}
	if c.Mode != "" && permRank(c.Mode) > own {
		return "This thread runs in " + modeName(c.Caller.Mode) + " and cannot start or switch anything to " + modeName(c.Mode) + "." + capAskUser
	}
	if c.Verb == "spawn" && c.Detach {
		return "This thread runs in " + modeName(c.Caller.Mode) + ": a detached chat or tab would not keep its mode." + capAskUser
	}
	if c.NewChat || c.Check {
		return "This thread runs in " + modeName(c.Caller.Mode) + ": a schedule that starts new chats or runs a check command would act outside it." + capAskUser
	}
	t := c.Target
	if !capActs[c.Verb] || t == nil || t.ID == c.Caller.ID {
		return ""
	}
	if t.Parent > 0 && t.Parent != c.Caller.ID {
		return "Chat is another thread's sub-agent; you may act only on your own." + capAskUser
	}
	if c.Verb == "close_chat" && t.Parent != c.Caller.ID {
		return "close_chat deletes only your own sub-agents." + capAskUser
	}
	if permRank(t.Mode) > own {
		return "Chat runs in " + modeName(t.Mode) + ", more permissive than this thread's " + modeName(c.Caller.Mode) + "." + capAskUser
	}
	return ""
}

func modeName(m string) string { return firstNonEmpty(m, "default") }

// chatCapProblem resolves a chat-key call against the database and applies
// capabilityProblem. Unknown targets pass: the verb itself reports them.
func (a *App) chatCapProblem(verb string, callerID int64, p control.Params) string {
	caller, err := a.chatByID(callerID)
	if err != nil {
		return fmt.Sprintf("Your chat key names chat %d, which does not exist.", callerID)
	}
	c := capCall{
		Verb:    verb,
		Caller:  capChat{ID: caller.ID, Parent: caller.ParentChatID, Mode: caller.PermissionMode},
		Mode:    p.Str("permission_mode"),
		Detach:  p.Bool("detach"),
		NewChat: p.Bool("new_chat"),
		Check:   p.Str("check") != "",
	}
	target := p.Int("chat_id")
	if id := p.Int("id"); id > 0 && verb != "schedule" && capActs[verb] && a.db != nil {
		var chatID int64
		var newChat bool
		if err := a.db.QueryRow(`SELECT chat_id, new_chat FROM chat_schedules WHERE id = ?`, id).Scan(&chatID, &newChat); err == nil {
			target, c.NewChat = chatID, c.NewChat || newChat
		} else if err != sql.ErrNoRows {
			return "schedule " + p.Str("id") + ": " + err.Error()
		}
	}
	if target > 0 {
		if t, err := a.chatByID(target); err == nil {
			c.Target = &capChat{ID: t.ID, Parent: t.ParentChatID, Mode: t.PermissionMode}
		}
	}
	return capabilityProblem(c)
}
