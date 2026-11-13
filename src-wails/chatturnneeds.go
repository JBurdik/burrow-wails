package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"burrow/internal/agentphase"
	"burrow/internal/control"
)

// end_turn (internal/control/verbs_turn.go) stores what the turn needs from
// the user in chats.turn_needs — read-only on the wire like recap; SaveChats
// never writes it. AgentChat draws it above the composer once the turn has
// settled: a Needs-you card with answer buttons, or a Done card with the
// follow-ups and the loose ends below. applyChatPhase clears it when the next
// turn starts, whoever starts it (a send, a queued item, a schedule).

// SetTurnNeeds implements control.TurnNeedsStore.
func (a *App) SetTurnNeeds(chatID int64, n control.TurnNeeds) error {
	if a.db == nil {
		return fmt.Errorf("no database")
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	res, err := a.db.Exec(`UPDATE chats SET turn_needs = ? WHERE id = ?`, string(raw), chatID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("chat %d not found", chatID)
	}
	busEmit("chats-changed", nil)
	return nil
}

// TurnNeedsPending implements control.TurnNeedsStore: no end_turn stored for
// the current turn (applyChatPhase clears it when a turn starts).
func (a *App) TurnNeedsPending(chatID int64) (bool, error) {
	if a.db == nil {
		return false, fmt.Errorf("no database")
	}
	var raw string
	if err := a.db.QueryRow(`SELECT turn_needs FROM chats WHERE id = ?`, chatID).Scan(&raw); err != nil {
		return false, err
	}
	return raw == "", nil
}

func (a *App) clearTurnNeeds(chatID string) {
	if a.db == nil {
		return
	}
	res, err := a.db.Exec(`UPDATE chats SET turn_needs = '' WHERE id = ? AND turn_needs != ''`, chatIDOf(chatID))
	if err != nil {
		log.Printf("chat %s: clear turn_needs: %v", chatID, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		busEmit("chats-changed", nil)
	}
}

// LooseEnd is one thing still open on a finished chat, checked by Burrow
// itself (never the agent's word): what archiving would leave behind.
type LooseEnd struct {
	// uncommitted | unpushed | pr | subagent | schedule
	Kind string `json:"kind"`
	Text string `json:"text"`
	// For kind subagent: the child to open.
	ChatID int64 `json:"chat_id,omitempty"`
}

// looseEndFacts is what ChatLooseEnds gathers; looseEnds turns it into lines.
type looseEndFacts struct {
	Uncommitted int // changed/untracked paths in the chat's cwd
	Unpushed    int // commits on HEAD that are on no remote (-1 = no remote)
	PrNumber    int64
	PrState     string
	Children    []looseChild
	Schedules   int
	SharedWith  int // other open threads in the same (non-worktree) folder
}

type looseChild struct {
	ID    int64
	Title string
	State agentphase.State
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func looseEnds(f looseEndFacts) []LooseEnd {
	out := []LooseEnd{}
	if f.Uncommitted > 0 {
		text := plural(f.Uncommitted, "uncommitted file", "uncommitted files")
		if f.SharedWith > 0 {
			text += " in this folder (shared with " + plural(f.SharedWith, "other thread", "other threads") + ")"
		}
		out = append(out, LooseEnd{Kind: "uncommitted", Text: text})
	}
	if f.Unpushed > 0 {
		out = append(out, LooseEnd{Kind: "unpushed", Text: plural(f.Unpushed, "commit not pushed", "commits not pushed")})
	}
	if f.PrNumber > 0 && (f.PrState == "open" || f.PrState == "draft") {
		out = append(out, LooseEnd{Kind: "pr", Text: fmt.Sprintf("PR #%d is %s, not merged", f.PrNumber, f.PrState)})
	}
	for _, c := range f.Children {
		var what string
		switch c.State {
		case agentphase.Running:
			what = "still working"
		case agentphase.WaitingInput, agentphase.WaitingApproval:
			what = "waiting for you"
		default:
			continue
		}
		title := c.Title
		if title == "" {
			title = fmt.Sprintf("Sub-agent %d", c.ID)
		}
		out = append(out, LooseEnd{Kind: "subagent", Text: title + " — " + what, ChatID: c.ID})
	}
	if f.Schedules > 0 {
		out = append(out, LooseEnd{Kind: "schedule", Text: plural(f.Schedules, "scheduled check-in", "scheduled check-ins")})
	}
	return out
}

// ChatLooseEnds lists what is still open on a chat (Done card). Git facts come
// from the chat's cwd; a non-repo simply has none.
func (a *App) ChatLooseEnds(chatID int64) ([]LooseEnd, error) {
	c, err := a.chatByID(chatID)
	if err != nil {
		return []LooseEnd{}, err
	}
	f := looseEndFacts{Unpushed: -1, PrNumber: c.PrNumber, PrState: c.PrState}
	if cwd := a.chatCwd(c); cwd != "" {
		if out, ok := untrustedGit(cwd, "status", "--porcelain", "--untracked-files=all"); ok {
			f.Uncommitted = countLines(out)
			if r, ok := untrustedGit(cwd, "remote"); ok && strings.TrimSpace(r) != "" {
				if n, ok := untrustedGit(cwd, "rev-list", "--count", "HEAD", "--not", "--remotes"); ok {
					f.Unpushed, _ = strconv.Atoi(strings.TrimSpace(n))
				}
			}
		}
		// A repo's own checkout can hold other threads' work too: say so
		// rather than pin every change on this one (a worktree is the thread's).
		var parent sql.NullInt64
		_ = a.db.QueryRow(`SELECT parent_id FROM workspaces WHERE id = ?`, c.WorkspaceID).Scan(&parent)
		if !parent.Valid || parent.Int64 == 0 {
			_ = a.db.QueryRow(`SELECT COUNT(*) FROM chats WHERE workspace_id = ? AND id != ? AND archived_at = 0 AND parent_chat_id = 0`, c.WorkspaceID, chatID).Scan(&f.SharedWith)
		}
	}
	rows, err := a.db.Query(`SELECT id, title FROM chats WHERE parent_chat_id = ? AND archived_at = 0 AND kind != 'side'`, chatID)
	if err == nil {
		for rows.Next() {
			var ch looseChild
			if rows.Scan(&ch.ID, &ch.Title) == nil {
				f.Children = append(f.Children, ch)
			}
		}
		rows.Close()
	}
	for i := range f.Children {
		if a.phases != nil {
			f.Children[i].State = a.phases.Get(fmt.Sprintf("chat:%d", f.Children[i].ID)).State
		}
	}
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_schedules WHERE chat_id = ? AND paused = 0 AND new_chat = 0`, chatID).Scan(&f.Schedules)
	return looseEnds(f), nil
}

func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// Run on the card: an end_turn ask may carry a command that is the user's to
// run. ChatRunAsk runs the STORED command (never one a client sends) once, in
// the chat's folder, and queues its output to the chat as the user's reply.
const (
	runAskTimeout   = 10 * time.Minute
	runAskOutputCap = 16 << 10
)

// runAskShell is what executes a pressed command or a schedule check; a var
// for tests. The command gets its own process group and the deadline kills
// the whole group: killing only `sh` left a child holding the output pipe,
// and the read hung past the limit. No stdin: a command that asks "are you
// sure?" fails rather than hangs.
var runAskShell = func(ctx context.Context, cwd, cmd string) (string, int) {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = cwd
	c.Env = append(os.Environ(), "PATH="+augmentedPath(cwd))
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = 2 * time.Second
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		if ctx.Err() != nil {
			out = append(out, []byte("\n[stopped: it ran past its time limit]")...)
		}
	}
	return strings.ToValidUTF8(string(out), "\uFFFD"), code
}

// untrustedGit reads a thread's folder without running anything the folder
// itself names (threadbear's safe-git): a repo's .git/config can set
// core.fsmonitor to a program git starts on `status`, and a hook path, and a
// lazy fetch through a planted transport. -c beats the repo's config; the
// environment drops every GIT_ variable, which could come after the -c.
func untrustedGit(cwd string, args ...string) (string, bool) {
	full := append([]string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false",
		"-c", "core.hooksPath=/dev/null", "-c", "protocol.allow=never"}, args...)
	c := exec.Command("git", full...)
	c.Dir = cwd
	env := []string{"GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	c.Env = env
	out, err := c.Output()
	return string(out), err == nil
}

// ChatRunAsk presses ask `index`'s Run. It returns once the command has
// started; the result arrives as a queued message (and the card's state).
func (a *App) ChatRunAsk(chatID int64, index64 int64) error {
	index := int(index64)
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	var n control.TurnNeeds
	if c.TurnNeeds == "" || json.Unmarshal([]byte(c.TurnNeeds), &n) != nil {
		return fmt.Errorf("nothing to run: the turn's asks are gone")
	}
	if index < 0 || index >= len(n.Asks) || n.Asks[index].Run == nil {
		return fmt.Errorf("ask %d has no command", index)
	}
	run := n.Asks[index].Run
	if run.State != "" {
		return fmt.Errorf("already run")
	}
	if c.ArchivedAt != 0 {
		return fmt.Errorf("this thread is archived — unarchive it to run its command")
	}
	cwd := a.chatCwd(c)
	if st, err := os.Stat(cwd); cwd == "" || err != nil || !st.IsDir() {
		return fmt.Errorf("the thread's folder is gone (%s)", cwd)
	}
	run.State = "running"
	if !a.swapTurnNeeds(chatID, c.TurnNeeds, n) {
		return fmt.Errorf("the card changed — look again")
	}
	cmd := run.Command
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), runAskTimeout)
		defer cancel()
		out, code := runAskShell(ctx, cwd, cmd)
		a.finishRunAsk(chatID, index, cmd, code)
		if _, err := a.ChatQueueAdd(chatID, runAskReply(cmd, cwd, out, code), nil); err != nil {
			log.Printf("chat %d: queue run result: %v", chatID, err)
		}
	}()
	return nil
}

// swapTurnNeeds writes n only if the row still holds `was` (a new turn may
// have cleared it, or another click got there first).
func (a *App) swapTurnNeeds(chatID int64, was string, n control.TurnNeeds) bool {
	raw, err := json.Marshal(n)
	if err != nil {
		return false
	}
	res, err := a.db.Exec(`UPDATE chats SET turn_needs = ? WHERE id = ? AND turn_needs = ?`, string(raw), chatID, was)
	if err != nil {
		return false
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return false
	}
	busEmit("chats-changed", nil)
	return true
}

// finishRunAsk marks the ask done on the card, if the card is still that one.
func (a *App) finishRunAsk(chatID int64, index int, cmd string, code int) {
	c, err := a.chatByID(chatID)
	if err != nil {
		return
	}
	var n control.TurnNeeds
	if json.Unmarshal([]byte(c.TurnNeeds), &n) != nil || index >= len(n.Asks) || n.Asks[index].Run == nil || n.Asks[index].Run.Command != cmd {
		return
	}
	n.Asks[index].Run.State, n.Asks[index].Run.Exit = "done", code
	a.swapTurnNeeds(chatID, c.TurnNeeds, n)
}

func runAskReply(cmd, cwd, out string, code int) string {
	if len(out) > runAskOutputCap {
		out = "…(earlier output cut)\n" + strings.ToValidUTF8(out[len(out)-runAskOutputCap:], "")
	}
	out = strings.ReplaceAll(strings.TrimRight(out, "\n"), "```", "ˋˋˋ") // a fence in the output would end ours
	if out == "" {
		out = "(no output)"
	}
	return fmt.Sprintf("I ran your command from the card in `%s` — exit %d:\n\n```\n$ %s\n%s\n```", cwd, code, cmd, out)
}

// endTurnGateSettings is the --settings layer a Claude chat starts with: one
// Stop hook running `burrow end-turn-gate`. Merged with the user's own
// settings by the CLI, not replacing them.
func endTurnGateSettings(burrowBin string) string {
	raw, _ := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{
		map[string]any{"hooks": []any{map[string]any{"type": "command", "command": shellQuote(burrowBin) + " end-turn-gate", "timeout": 15}}},
	}}})
	return string(raw)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// resetInterruptedRuns: a restart while a card's command ran left it
// "running" for good — the card spun and Run refused ("already run"). At
// startup nothing of ours is running, so such a run is marked interrupted and
// can be pressed again.
func (a *App) resetInterruptedRuns() {
	if a.db == nil {
		return
	}
	rows, err := a.db.Query(`SELECT id, turn_needs FROM chats WHERE turn_needs LIKE '%"state":"running"%'`)
	if err != nil {
		return
	}
	type row struct {
		id  int64
		raw string
	}
	var hits []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.raw) == nil {
			hits = append(hits, r)
		}
	}
	rows.Close()
	for _, r := range hits {
		var n control.TurnNeeds
		if json.Unmarshal([]byte(r.raw), &n) != nil {
			continue
		}
		for _, ask := range n.Asks {
			if ask.Run != nil && ask.Run.State == "running" {
				ask.Run.State, ask.Run.Interrupted = "", true
			}
		}
		a.swapTurnNeeds(r.id, r.raw, n)
	}
}

// ChatQuietArchivable: may a finished sub-agent leave the list at once
// (quiet inbox)? Only when it has nothing open — uncommitted or unpushed
// work, an open PR, children in flight, a booked check-in — and nobody wrote
// to it after its task (one prompt in its transcript). Threadbear's rule:
// archiving a worktree with work in it, or a thread someone was talking to,
// would lose it. Otherwise the ordinary 30-minute rule applies.
func (a *App) ChatQuietArchivable(chatID int64) (bool, error) {
	c, err := a.chatByID(chatID)
	if err != nil || c.ParentChatID == 0 || c.Kind == "side" || c.ArchivedAt != 0 {
		return false, err
	}
	ends, err := a.ChatLooseEnds(chatID)
	if err != nil || len(ends) > 0 {
		return false, err
	}
	var folded, stored, unfolded int
	_ = a.db.QueryRow(`SELECT COALESCE(folded_ord, 0) FROM chat_stream_state WHERE chat_id = ?`, chatID).Scan(&folded)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_messages WHERE chat_id = ? AND json_extract(payload_json, '$.role') = 'user'`, chatID).Scan(&stored)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM chat_stream WHERE chat_id = ? AND kind = ? AND ord >= ?`, chatID, chatUserKind, folded).Scan(&unfolded)
	return stored+unfolded <= 1, nil
}
