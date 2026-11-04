package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// A thread driving its chat sub-agents beyond spawn / wait: read the child's
// timeline, see and answer its questions, interrupt it, and pick how a
// follow-up lands (modelled on t3code's thread tools).

// PendingQuestion is one question of a blocked request. ID is the key its
// answer goes under (Claude: the question text; Codex: the question id).
type PendingQuestion struct {
	ID       string   `json:"id"`
	Header   string   `json:"header,omitempty"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Multi    bool     `json:"multi,omitempty"`
	Secret   bool     `json:"secret,omitempty"`
}

// PendingRequest is what a chat is blocked on. Kind "question" is answerable
// with chat_answer; "approval" and "plan" are the user's.
type PendingRequest struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Tool      string            `json:"tool,omitempty"`
	Detail    string            `json:"detail,omitempty"`
	Questions []PendingQuestion `json:"questions,omitempty"`
}

type ChatReadItem struct {
	// Ord is the stream position; 0 = from the folded transcript, before it.
	Ord       int64  `json:"ord,omitempty"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

type ChatReadResult struct {
	ChatID int64          `json:"chat_id"`
	Phase  string         `json:"phase,omitempty"`
	Items  []ChatReadItem `json:"items"`
	// Next is the `after` for the following incremental read.
	Next int64 `json:"next"`
	// Omitted counts older items cut by limit; Gap = the log was trimmed past
	// `after`, so something between was lost.
	Omitted int              `json:"omitted,omitempty"`
	Gap     bool             `json:"gap,omitempty"`
	Pending []PendingRequest `json:"pending,omitempty"`
}

// ChatControl drives a chat from the host. Optional: nil = these verbs refuse.
type ChatControl interface {
	ChatParent(chatID int64) (int64, error)
	Pending(chatID int64) ([]PendingRequest, error)
	Answer(chatID int64, requestID string, answers map[string][]string) error
	Interrupt(chatID int64) error
	Steer(chatID int64, text string) error
	Queue(chatID int64, text string) error
	Read(chatID, after int64, limit int, activity bool) (ChatReadResult, error)
}

// WaitBlocked is wait_result's answer when the chat stopped on a request
// instead of finishing.
type WaitBlocked struct {
	Token   string           `json:"token"`
	Blocked bool             `json:"blocked"`
	Phase   string           `json:"phase"`
	Pending []PendingRequest `json:"pending"`
	Hint    string           `json:"hint"`
}

const blockedHint = "The sub-agent is waiting. Answer a question with chat_answer (or ask the user); an approval or plan is the user's to decide — tell them. " +
	"Then wait_result again; pass ignore_blocked=true to keep waiting while the user answers."

func chatVerbs(c *Core) []Verb {
	return []Verb{{
		Name: "chat_read",
		Summary: "Read a chat's timeline (any chat, e.g. a sub-agent mid-task): user/assistant messages, with activity also tool calls and turn ends, " +
			"plus its phase and what it is blocked on. Pass the returned next as after to read only what is new",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Chat to read", Required: true},
			{Name: "after", Type: "integer", Desc: "Stream position from a previous read's next; omit for the whole transcript"},
			{Name: "limit", Type: "integer", Desc: "Newest items to return (default 30, max 200)"},
			{Name: "activity", Type: "boolean", Desc: "Include tool calls and turn markers"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			cc, id, err := c.chatTarget(p, "chat_read", false)
			if err != nil {
				return nil, err
			}
			limit := int(p.Int("limit"))
			if limit <= 0 {
				limit = 30
			}
			return cc.Read(id, p.Int("after"), min(limit, 200), p.Bool("activity"))
		},
	}, {
		Name: "chat_answer",
		Summary: "Answer your sub-agent's pending question (see wait_result / chat_read pending). Questions only — tool approvals and plans stay the user's. " +
			"Ask the user instead when the answer is theirs to give",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Your sub-agent", Required: true},
			{Name: "answer", Type: "string", Desc: "The answer, for a single question (an option label or free text)"},
			{Name: "answers", Type: "string", Desc: `JSON object for several questions: {"<question id>": "answer" | ["a","b"]}`},
			{Name: "request_id", Type: "string", Desc: "Which pending request; default the oldest question"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.chatAnswer(p) },
	}, {
		Name:    "chat_interrupt",
		Summary: "Stop your sub-agent's running turn (it keeps its conversation; chat_send continues it). stop=true also ends its process",
		Args: []Arg{
			{Name: "chat_id", Type: "integer", Desc: "Your sub-agent", Required: true},
			{Name: "stop", Type: "boolean", Desc: "Also release the process (the next chat_send resumes it)"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			cc, id, err := c.chatTarget(p, "chat_interrupt", true)
			if err != nil {
				return nil, err
			}
			if p.Bool("stop") {
				if c.deps.ChatStopper == nil {
					return nil, fmt.Errorf("chat_interrupt: stop is not available")
				}
				if err := c.deps.ChatStopper.StopChat(id); err != nil {
					return nil, err
				}
				return map[string]any{"chat_id": id, "stopped": true}, nil
			}
			if err := cc.Interrupt(id); err != nil {
				return nil, err
			}
			return map[string]any{"chat_id": id, "interrupted": true}, nil
		},
	}}
}

// chatTarget resolves chat_id and, when owned, checks the caller may drive
// it: a chat itself or its parent thread; a caller outside any chat (the
// user's shell) may drive anything.
func (c *Core) chatTarget(p Params, verb string, owned bool) (ChatControl, int64, error) {
	cc := c.deps.ChatControl
	if cc == nil {
		return nil, 0, fmt.Errorf("%s is not available in this host", verb)
	}
	id := p.Int("chat_id")
	if id <= 0 {
		return nil, 0, fmt.Errorf("%s needs a chat_id", verb)
	}
	if caller := p.Int("parent_chat_id"); owned && caller > 0 && caller != id {
		parent, err := cc.ChatParent(id)
		if err != nil {
			return nil, 0, err
		}
		if parent != caller {
			return nil, 0, fmt.Errorf("%s: chat %d is not your sub-agent", verb, id)
		}
	}
	return cc, id, nil
}

func (c *Core) chatAnswer(p Params) (any, error) {
	cc, id, err := c.chatTarget(p, "chat_answer", true)
	if err != nil {
		return nil, err
	}
	pending, err := cc.Pending(id)
	if err != nil {
		return nil, err
	}
	reqID := p.Str("request_id")
	var req *PendingRequest
	for i := range pending {
		if pending[i].Kind == "question" && (reqID == "" || pending[i].ID == reqID) {
			req = &pending[i]
			break
		}
	}
	if req == nil {
		return nil, fmt.Errorf("chat %d has no pending question to answer (pending: %d; approvals and plans are the user's)", id, len(pending))
	}
	answers, err := parseAnswers(p.Str("answers"), p.Str("answer"), *req)
	if err != nil {
		return nil, err
	}
	if err := cc.Answer(id, req.ID, answers); err != nil {
		return nil, err
	}
	return map[string]any{"chat_id": id, "request_id": req.ID, "answered": len(answers)}, nil
}

// parseAnswers accepts either one answer (for a single question) or a JSON map
// keyed by question id, values a string or a list.
func parseAnswers(raw, one string, req PendingRequest) (map[string][]string, error) {
	out := map[string][]string{}
	if strings.TrimSpace(raw) != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, fmt.Errorf("answers must be a JSON object: %w", err)
		}
		known := map[string]bool{}
		for _, q := range req.Questions {
			known[q.ID] = true
		}
		for k, v := range m {
			if !known[k] {
				return nil, fmt.Errorf("no question %q in request %s", k, req.ID)
			}
			switch t := v.(type) {
			case string:
				out[k] = []string{t}
			case []any:
				for _, s := range t {
					if str, ok := s.(string); ok {
						out[k] = append(out[k], str)
					}
				}
			default:
				return nil, fmt.Errorf("answer to %q must be a string or a list of strings", k)
			}
		}
	} else if strings.TrimSpace(one) != "" {
		if len(req.Questions) != 1 {
			return nil, fmt.Errorf("request %s has %d questions — pass answers as a JSON object keyed by question id", req.ID, len(req.Questions))
		}
		out[req.Questions[0].ID] = []string{one}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chat_answer needs answer or answers")
	}
	return out, nil
}
