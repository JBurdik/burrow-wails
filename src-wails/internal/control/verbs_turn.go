package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// end_turn: the agent says, as its last act in a turn, what the turn needs
// from the user — a structured version of "What I need from you", so the UI
// can draw answer buttons instead of the user re-typing choices from prose.
// Taken from threadbear (runner/src/needs.ts). The app stores it on the chat
// (chats.turn_needs) and clears it when the next turn starts.

// TurnNeeds is end_turn's checked input.
type TurnNeeds struct {
	// "you" = blocked on the user; "nothing" = done or still going by itself.
	Needs string    `json:"needs"`
	Asks  []TurnAsk `json:"asks,omitempty"`
	// Follow-ups the user may pick on the done card, each a short message.
	Next []string `json:"next,omitempty"`
}

// TurnAsk is one question. Options empty = answer in words.
type TurnAsk struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	// Index into Options of the answer the agent would pick; -1 = none.
	Suggested int `json:"suggested"`
	// Run, when set, is a command that is the user's to run (a deploy, a
	// production migration): the card shows it with a Run button, it runs once
	// in the chat's folder and its output goes back to the agent.
	Run *TurnRun `json:"run,omitempty"`
}

// TurnRun is an ask's command and, once pressed, how it went. State and Exit
// are the app's (chatturnneeds.go), never the agent's.
type TurnRun struct {
	Command string `json:"command"`
	// "" (not run) | running | done
	State string `json:"state,omitempty"`
	Exit  int    `json:"exit,omitempty"`
}

// MaxRunCommand caps an ask's command: past it, a script is the honest form.
const MaxRunCommand = 8000

// RunCommandProblem says why a command can't go on a card, if it can't. What
// the card shows is what the user agrees to, so it must be what runs: control,
// bidi, zero-width and other invisible characters could show something else.
func RunCommandProblem(cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return "run is empty"
	}
	if len(cmd) > MaxRunCommand {
		return fmt.Sprintf("run is over %d characters: put it in a script and run that", MaxRunCommand)
	}
	for _, r := range cmd {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Co, unicode.Cs) {
			return "run has control or invisible characters in it, so the card could show something other than what runs: take them out"
		}
	}
	return ""
}

// TurnNeedsStore persists a chat's end_turn. nil = the verb refuses.
type TurnNeedsStore interface {
	SetTurnNeeds(chatID int64, n TurnNeeds) error
}

// Limits: a card is something to read at a glance. Past these an agent is
// rambling, and the overflow is cut rather than refused.
const (
	maxTurnAsks    = 6
	maxTurnOptions = 6
	maxTurnNext    = 3
	maxQuestionLen = 500
	maxOptionLen   = 120
	maxNextLen     = 160
)

var turnAsksSchema = map[string]any{
	"type": "array",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question":  map[string]any{"type": "string", "description": "One decision or action, self-contained (the user may answer without reading the reply)"},
			"options":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2-6 short answers shown as buttons; omit for a free-text answer"},
			"suggested": map[string]any{"type": "integer", "description": "Index into options of the answer you would pick"},
			"run":       map[string]any{"type": "string", "description": "A shell command that is the user's to run, not yours (a production deploy or migration, something needing their machine/credentials). Shown with a Run button; runs once in this chat's folder with no stdin (no prompts), 10 min max, and its output comes back to you as their reply"},
		},
		"required": []string{"question"},
	},
}

var turnNextSchema = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

func turnVerbs(c *Core) []Verb {
	return []Verb{{
		Name: "end_turn",
		Summary: "Call as the LAST thing in every turn of a Burrow chat, after your final reply text. Says whether the user must act: " +
			"needs=\"you\" with asks (each a question, optionally with 2-6 options shown as buttons) when you are blocked on a decision, approval, " +
			"key or test only they can do; needs=\"nothing\" when done (optionally next: up to 3 short follow-ups the user can pick). " +
			"Don't repeat the asks in prose as a list — the UI draws them",
		Args: []Arg{
			{Name: "needs", Type: "string", Desc: "\"you\" or \"nothing\"; left out, it follows asks", Schema: map[string]any{"type": "string", "enum": []string{"you", "nothing"}}},
			{Name: "asks", Type: "array", Desc: "Questions for the user (needs=\"you\")", Schema: turnAsksSchema},
			{Name: "next", Type: "array", Desc: "Up to 3 follow-ups worth doing, each a short message the user can send as is (needs=\"nothing\")", Schema: turnNextSchema},
		},
		Scope: ScopeLocal,
		Fn:    func(_ context.Context, p Params) (any, error) { return c.endTurn(p) },
	}}
}

func (c *Core) endTurn(p Params) (any, error) {
	if c.deps.TurnNeeds == nil {
		return nil, fmt.Errorf("end_turn: not available")
	}
	chatID := p.Int("parent_chat_id")
	if chatID <= 0 {
		return nil, fmt.Errorf("end_turn: only for a Burrow chat (no BURROW_CHAT_ID)")
	}
	n, err := ParseTurnNeeds(p)
	if err != nil {
		return nil, fmt.Errorf("end_turn: %w", err)
	}
	if err := c.deps.TurnNeeds.SetTurnNeeds(chatID, n); err != nil {
		return nil, fmt.Errorf("end_turn: %w", err)
	}
	return map[string]any{"ok": true, "needs": n.Needs, "asks": len(n.Asks)}, nil
}

// ParseTurnNeeds checks end_turn's arguments. asks/next arrive decoded from MCP
// or as a JSON string from the CLI; both are accepted.
func ParseTurnNeeds(p Params) (TurnNeeds, error) {
	var n TurnNeeds
	var asks []struct {
		Question  string   `json:"question"`
		Options   []string `json:"options"`
		Suggested *int     `json:"suggested"`
		Run       string   `json:"run"`
	}
	if err := p.JSON("asks", &asks); err != nil {
		return n, fmt.Errorf("asks: %w", err)
	}
	var next []string
	if err := p.JSON("next", &next); err != nil {
		return n, fmt.Errorf("next: %w", err)
	}
	for _, a := range asks {
		q := clip(a.Question, maxQuestionLen)
		if q == "" {
			continue
		}
		ask := TurnAsk{Question: q, Suggested: -1}
		for _, o := range a.Options {
			if o = clip(o, maxOptionLen); o != "" && len(ask.Options) < maxTurnOptions {
				ask.Options = append(ask.Options, o)
			}
		}
		// One option is not a choice: it's a question with a hint.
		if len(ask.Options) == 1 {
			ask.Options = nil
		}
		if a.Suggested != nil && *a.Suggested >= 0 && *a.Suggested < len(ask.Options) {
			ask.Suggested = *a.Suggested
		}
		if a.Run != "" {
			if problem := RunCommandProblem(a.Run); problem != "" {
				return n, fmt.Errorf("%s", problem)
			}
			ask.Run = &TurnRun{Command: a.Run}
		}
		if len(n.Asks) < maxTurnAsks {
			n.Asks = append(n.Asks, ask)
		}
	}
	switch needs := p.Str("needs"); needs {
	case "you", "nothing":
		n.Needs = needs
	case "":
		n.Needs = "nothing"
		if len(n.Asks) > 0 {
			n.Needs = "you"
		}
	default:
		return n, fmt.Errorf("needs must be \"you\" or \"nothing\", not %q", needs)
	}
	if n.Needs == "you" && len(n.Asks) == 0 {
		return n, fmt.Errorf("needs \"you\" without asks: say what the user should do")
	}
	if n.Needs == "nothing" {
		// Asks on a done turn would show a card that blocks nothing.
		n.Asks = nil
		for _, s := range next {
			if s = clip(s, maxNextLen); s != "" && len(n.Next) < maxTurnNext {
				n.Next = append(n.Next, s)
			}
		}
	}
	return n, nil
}

// JSON decodes an argument that is either already structured (MCP) or a JSON
// string (CLI) into out. Absent/empty leaves out untouched.
func (p Params) JSON(name string, out any) error {
	v, ok := p[name]
	if !ok || v == nil {
		return nil
	}
	var raw []byte
	if s, isStr := v.(string); isStr {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		raw = []byte(s)
	} else {
		var err error
		if raw, err = json.Marshal(v); err != nil {
			return err
		}
	}
	return json.Unmarshal(raw, out)
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max-1])) + "…"
}
