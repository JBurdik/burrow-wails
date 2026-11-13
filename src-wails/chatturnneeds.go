package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
		out = append(out, LooseEnd{Kind: "uncommitted", Text: plural(f.Uncommitted, "uncommitted file", "uncommitted files")})
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
		if out := runCmd("git", cwd, []string{"status", "--porcelain"}); out.Success {
			f.Uncommitted = countLines(out.Stdout)
			if r := runCmd("git", cwd, []string{"remote"}); r.Success && strings.TrimSpace(r.Stdout) != "" {
				if n := runCmd("git", cwd, []string{"rev-list", "--count", "HEAD", "--not", "--remotes"}); n.Success {
					f.Unpushed, _ = strconv.Atoi(strings.TrimSpace(n.Stdout))
				}
			}
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

// runAskShell is what executes a pressed command; a var for tests.
var runAskShell = func(ctx context.Context, cwd, cmd string) (string, int) {
	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	c.Dir = cwd
	c.Env = append(os.Environ(), "PATH="+augmentedPath(cwd))
	// No stdin: a command that asks "are you sure?" fails rather than hangs.
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		if ctx.Err() != nil {
			out = append(out, []byte("\n[stopped after 10 minutes]")...)
		}
	}
	return string(out), code
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
	run.State = "running"
	if !a.swapTurnNeeds(chatID, c.TurnNeeds, n) {
		return fmt.Errorf("the card changed — look again")
	}
	cwd, cmd := a.chatCwd(c), run.Command
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
		out = "…(earlier output cut)\n" + out[len(out)-runAskOutputCap:]
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		out = "(no output)"
	}
	return fmt.Sprintf("I ran your command from the card in `%s` — exit %d:\n\n```\n$ %s\n%s\n```", cwd, code, cmd, out)
}
