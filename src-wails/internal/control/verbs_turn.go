package control

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Details[i] is one line on what picking Options[i] leads to ("" = none);
	// same length as Options whenever set.
	Details []string `json:"details,omitempty"`
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
	// Interrupted: a restart cut the last run short; it may be run again.
	Interrupted bool `json:"interrupted,omitempty"`
	// Reply: on a card with other asks, the run's result is kept here and goes
	// with the user's answers (the client sends it), instead of a message of
	// its own that would start a turn and clear the unanswered asks.
	Reply string `json:"reply,omitempty"`
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
	// TurnNeedsPending: the chat's current turn has not called end_turn yet.
	TurnNeedsPending(chatID int64) (bool, error)
}

// Limits: a card is something to read at a glance. Past these an agent is
// rambling, and the overflow is cut rather than refused.
const (
	maxTurnAsks    = 6
	maxTurnOptions = 6
	maxDetailLen   = 200
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
			"question": map[string]any{"type": "string", "description": "One decision or action, self-contained (the user may answer without reading the reply)"},
			"options": map[string]any{"type": "array", "description": "2-6 answers shown as buttons; omit for a free-text answer. Give each a description: what picking it leads to (the user decides from the card, not from your reply)",
				"items": map[string]any{"type": "object", "properties": map[string]any{
					"label":       map[string]any{"type": "string", "description": "Short button text"},
					"description": map[string]any{"type": "string", "description": "One line: what happens if picked — scope, cost, trade-off"},
				}, "required": []string{"label"}}},
			"suggested": map[string]any{"type": "integer", "description": "Index into options of the answer you would pick"},
			"run":       map[string]any{"type": "string", "description": "A shell command that is the user's to run, not yours (a production deploy or migration, something needing their machine/credentials). Shown with a Run button; runs once in this chat's folder with no stdin (no prompts), 10 min max, and its output comes back to you as their reply"},
		},
		"required": []string{"question"},
	},
}

var turnNextSchema = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

func turnVerbs(c *Core) []Verb {
	return []Verb{pinVerb(c), {
		Name: "end_turn",
		Summary: "Call at the end of every turn of a Burrow chat, once the work is done — then write your final reply AFTER this call as your last message (not in thinking). Says whether the user must act: " +
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
	}, {
		// The chat's Stop hook (`burrow end-turn-gate`) asks this: a turn about
		// to end without end_turn is sent back once to call it.
		Name:    "end_turn_pending",
		Summary: "Internal (a Burrow chat's Stop hook): whether the calling chat's turn has not called end_turn yet",
		Scope:   ScopeLocal,
		Fn: func(_ context.Context, p Params) (any, error) {
			if c.deps.TurnNeeds == nil {
				return map[string]bool{"pending": false}, nil
			}
			id := p.Int("parent_chat_id")
			if id <= 0 {
				return map[string]bool{"pending": false}, nil
			}
			pending, err := c.deps.TurnNeeds.TurnNeedsPending(id)
			return map[string]bool{"pending": pending}, err
		},
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
		Question  string       `json:"question"`
		Options   []turnOption `json:"options"`
		Suggested *int         `json:"suggested"`
		Run       string       `json:"run"`
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
		described := false
		for _, o := range a.Options {
			if l := clip(o.Label, maxOptionLen); l != "" && len(ask.Options) < maxTurnOptions {
				d := clip(o.Description, maxDetailLen)
				ask.Options = append(ask.Options, l)
				ask.Details = append(ask.Details, d)
				described = described || d != ""
			}
		}
		if !described {
			ask.Details = nil
		}
		// One option is not a choice: it's a question with a hint.
		if len(ask.Options) == 1 {
			ask.Options, ask.Details = nil, nil
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

// turnOption is one end_turn option as sent: a plain string (older form) or
// {label, description}.
type turnOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

func (o *turnOption) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		o.Label = s
		return nil
	}
	type plain turnOption
	return json.Unmarshal(b, (*plain)(o))
}

// Pins stores a thread's pins (the Highlights surface). nil = pin refuses.
type Pins interface {
	AddPin(chatID int64, target, note string) (any, error)
}

func pinVerb(c *Core) Verb {
	return Verb{
		Name: "pin",
		Summary: "Pin something the user should look at to this thread's Highlights (Right Panel): a file you made for them to check (mockup, report, screenshot), " +
			"a preview URL, a pull request. With a one-line note on why. Files you write and links you mention show up there by themselves — pin only the few that matter most",
		Args: []Arg{
			{Name: "target", Type: "string", Desc: "A file path (relative to your working directory, or absolute) or an http(s) URL", Required: true},
			{Name: "note", Type: "string", Desc: "One line: why the user should look at it"},
		},
		Scope: ScopeLocal,
		Fn: func(_ context.Context, p Params) (any, error) {
			if c.deps.Pins == nil {
				return nil, fmt.Errorf("pin: not available")
			}
			chatID := p.Int("parent_chat_id")
			if chatID <= 0 {
				return nil, fmt.Errorf("pin: only for a Burrow chat (no BURROW_CHAT_ID)")
			}
			target := p.Str("target")
			if target == "" {
				return nil, fmt.Errorf("pin needs a target")
			}
			if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
				if strings.HasPrefix(target, "~/") {
					if home, err := os.UserHomeDir(); err == nil {
						target = filepath.Join(home, target[2:])
					}
				} else if !filepath.IsAbs(target) {
					cwd := p.Str("cwd")
					if cwd == "" {
						return nil, fmt.Errorf("pin: a relative path needs a working directory — pass an absolute path")
					}
					target = filepath.Join(cwd, target)
				}
				target = filepath.Clean(target)
			}
			return c.deps.Pins.AddPin(chatID, target, clip(p.Str("note"), 200))
		},
	}
}
