package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"burrow/internal/control"
)

// A parent thread driving its sub-agents (modelled on t3code's
// t3_thread_read / t3_pending_request / t3_thread_interrupt): read what the
// child is doing, see what it is blocked on, answer its questions, interrupt
// it. Everything reads Go's own logs (chat_stream, phases), so it works with
// no window open.
//
// Only QUESTIONS are answerable by an agent. A tool approval is the user's
// decision — an agent approving its own sub-agent's `rm -rf` would make the
// permission prompt decorative — so approvals are reported, never answered.

type chatControlAdapter struct{ a *App }

func (r chatControlAdapter) ChatParent(chatID int64) (int64, error) {
	c, err := r.a.chatByID(chatID)
	return c.ParentChatID, err
}
func (r chatControlAdapter) Interrupt(chatID int64) error { return r.a.ChatInterrupt(chatID) }
func (r chatControlAdapter) Steer(chatID int64, text string) error {
	return r.a.ChatSteer(chatID, text, nil)
}
func (r chatControlAdapter) Queue(chatID int64, text string) error {
	_, err := r.a.ChatQueueAdd(chatID, text, nil)
	return err
}
func (r chatControlAdapter) Pending(chatID int64) ([]control.PendingRequest, error) {
	r.a.flushChatStream()
	lines, err := r.a.LoadPendingChatRequests(strconv.FormatInt(chatID, 10))
	if err != nil {
		return nil, err
	}
	return pendingRequests(lines), nil
}
func (r chatControlAdapter) Answer(chatID int64, requestID string, answers map[string][]string) error {
	return r.a.answerChatQuestion(chatID, requestID, answers)
}
func (r chatControlAdapter) Read(chatID, after int64, limit int, activity bool) (control.ChatReadResult, error) {
	return r.a.readChat(chatID, after, limit, activity)
}

// flushChatStream: lines are numbered at emit but written by a goroutine; a
// read right after a line (an agent polling its sub-agent) must see it.
func (a *App) flushChatStream() {
	if w := a.chatStream(); w != nil {
		w.flush()
	}
}

// pendingRequests turns LoadPendingChatRequests' raw lines into what an agent
// can read: a question with its options, or an approval it cannot answer.
func pendingRequests(lines []ChatStreamLine) []control.PendingRequest {
	out := []control.PendingRequest{}
	for _, l := range lines {
		var m map[string]any
		if json.Unmarshal([]byte(l.Line), &m) != nil {
			continue
		}
		switch l.Kind {
		case "claude-data":
			req := mapField(m["request"])
			tool, input := strField(req["tool_name"]), mapField(req["input"])
			p := control.PendingRequest{ID: strField(m["request_id"]), Tool: tool, Kind: "approval"}
			switch tool {
			case "AskUserQuestion":
				p.Kind = "question"
				for _, q := range sliceField(input["questions"]) {
					qm := mapField(q)
					text := strField(qm["question"])
					// The tool reads its answers keyed by question text.
					p.Questions = append(p.Questions, control.PendingQuestion{
						ID: text, Header: strField(qm["header"]), Question: text,
						Options: optionLabels(qm["options"]), Multi: qm["multiSelect"] == true,
					})
				}
			case "ExitPlanMode":
				p.Kind, p.Detail = "plan", "plan approval — the user's decision"
			default:
				p.Detail = firstNonEmpty(strField(input["command"]), strField(input["file_path"]), strField(input["path"]), strField(input["description"]))
			}
			out = append(out, p)
		case "acp-req":
			method, params := strField(m["method"]), mapField(m["params"])
			p := control.PendingRequest{ID: rpcIDString(m["id"]), Tool: method, Kind: "approval"}
			if method == "item/tool/requestUserInput" {
				p.Kind = "question"
				for _, q := range sliceField(params["questions"]) {
					qm := mapField(q)
					p.Questions = append(p.Questions, control.PendingQuestion{
						ID: strField(qm["id"]), Header: strField(qm["header"]), Question: strField(qm["question"]),
						Options: optionLabels(qm["options"]), Secret: qm["isSecret"] == true,
					})
				}
			} else {
				p.Detail = firstNonEmpty(strField(mapField(params["toolCall"])["title"]), strField(params["command"]), strField(params["reason"]))
			}
			out = append(out, p)
		}
	}
	return out
}

func sliceField(v any) []any {
	s, _ := v.([]any)
	return s
}

func optionLabels(v any) []string {
	var out []string
	for _, o := range sliceField(v) {
		if l := strField(mapField(o)["label"]); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// answerChatQuestion answers a pending question exactly as the chat view's
// respond() does — the same control_response / JSON-RPC result, the same
// "answered" line so every open view drops its card.
func (a *App) answerChatQuestion(chatID int64, requestID string, answers map[string][]string) error {
	id := strconv.FormatInt(chatID, 10)
	a.flushChatStream()
	lines, err := a.LoadPendingChatRequests(id)
	if err != nil {
		return err
	}
	var line *ChatStreamLine
	var req control.PendingRequest
	for i, p := range pendingRequests(lines) {
		if p.Kind == "question" && (requestID == "" || p.ID == requestID) {
			line, req = &lines[i], p
			break
		}
	}
	if line == nil {
		return fmt.Errorf("chat %d has no pending question %q (approvals and plans are the user's to answer)", chatID, requestID)
	}
	for _, q := range req.Questions {
		if q.Secret && len(answers[q.ID]) > 0 {
			return fmt.Errorf("question %q asks for a secret — only the user may answer it", q.Question)
		}
	}
	if line.Kind == "claude-data" {
		var m struct {
			Request struct {
				Input map[string]any `json:"input"`
			} `json:"request"`
		}
		_ = json.Unmarshal([]byte(line.Line), &m)
		input := map[string]any{}
		for k, v := range m.Request.Input {
			input[k] = v
		}
		flat := map[string]any{}
		for _, q := range req.Questions {
			// As AgentChat sends it: one pick a string, a multi-select a list.
			switch v := answers[q.ID]; {
			case len(v) == 1:
				flat[q.ID] = v[0]
			case len(v) > 1:
				flat[q.ID] = v
			}
		}
		input["answers"] = flat
		return a.ClaudeRespondControl(id, req.ID, map[string]any{"behavior": "allow", "updatedInput": input})
	}
	rpcID, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		return fmt.Errorf("request %q: %w", req.ID, err)
	}
	return a.AcpRespondUserInput(id, rpcID, answers)
}

// readChat is chat_read: the chat's visible timeline after a stream position.
// A first read (after = 0) starts with the folded transcript (chat_messages —
// the stream below folded_ord may be trimmed), then the unfolded stream.
func (a *App) readChat(chatID, after int64, limit int, activity bool) (control.ChatReadResult, error) {
	res := control.ChatReadResult{ChatID: chatID, Items: []control.ChatReadItem{}}
	if a.db == nil {
		return res, fmt.Errorf("no database")
	}
	if _, err := a.chatByID(chatID); err != nil {
		return res, err
	}
	id := strconv.FormatInt(chatID, 10)
	a.flushChatStream()
	from := after
	if after <= 0 {
		folded, err := a.ChatFoldedOrd(id)
		if err != nil {
			return res, err
		}
		hist, err := a.transcriptItems(chatID, activity)
		if err != nil {
			return res, err
		}
		res.Items, from = hist, folded
	}
	lines, err := a.LoadChatStreamSince(id, from)
	if err != nil {
		return res, err
	}
	if after > 0 && len(lines) > 0 && lines[0].Ord > after {
		res.Gap = true // trimmed past the caller's position
	}
	res.Items = append(res.Items, streamItems(lines, activity)...)
	res.Next = from
	if len(lines) > 0 {
		res.Next = lines[len(lines)-1].Ord + 1
	}
	if limit > 0 && len(res.Items) > limit {
		res.Omitted = len(res.Items) - limit
		res.Items = res.Items[len(res.Items)-limit:] // the newest
	}
	if st := a.phases.Get("chat:" + id); st.State != "" {
		res.Phase = string(st.State)
	}
	res.Pending, _ = chatControlAdapter{a}.Pending(chatID)
	return res, nil
}

// readItemTextLimit bounds one item's text; Truncated says it was cut.
const readItemTextLimit = 4000

func readItem(ord int64, role, text string) control.ChatReadItem {
	it := control.ChatReadItem{Ord: ord, Role: role, Text: strings.TrimSpace(text)}
	if r := []rune(it.Text); len(r) > readItemTextLimit {
		it.Text, it.Truncated = string(r[:readItemTextLimit]), true
	}
	return it
}

// transcriptItems: the folded transcript's user/assistant rows (and, for the
// activity view, its tool / system rows), positionless — they predate the
// stream the caller pages through.
func (a *App) transcriptItems(chatID int64, activity bool) ([]control.ChatReadItem, error) {
	rows, err := a.db.Query(`SELECT payload_json FROM chat_messages WHERE chat_id = ? ORDER BY ord`, chatID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	out := []control.ChatReadItem{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var m struct {
			Role     string `json:"role"`
			Text     string `json:"text"`
			// A tool row: text is the tool name, toolInput its arguments.
			ToolInput map[string]any `json:"toolInput"`
		}
		if json.Unmarshal([]byte(raw), &m) != nil || strings.TrimSpace(m.Text) == "" {
			continue
		}
		switch {
		case m.Role == "user" || m.Role == "assistant":
			out = append(out, readItem(0, m.Role, m.Text))
		case activity:
			out = append(out, readItem(0, m.Role, m.Text+toolBrief(m.ToolInput)))
		}
	}
	return out, rows.Err()
}

// streamItems folds normalized stream events into timeline items: deltas of
// one message join, a tool is one line, a turn end is a marker.
func streamItems(lines []ChatStreamLine, activity bool) []control.ChatReadItem {
	out := []control.ChatReadItem{}
	var cur *control.ChatReadItem
	curMsg := ""
	flush := func() {
		if cur != nil && strings.TrimSpace(cur.Text) != "" {
			out = append(out, readItem(cur.Ord, cur.Role, cur.Text))
		}
		cur, curMsg = nil, ""
	}
	for _, l := range lines {
		for _, ev := range NormalizeChatLine(l.Kind, l.Line, l.Ord) {
			switch ev.Type {
			case EvtTextDelta, EvtUserDelta:
				role := "assistant"
				if ev.Type == EvtUserDelta {
					role = "user"
				}
				if cur == nil || cur.Role != role || (ev.MessageID != "" && ev.MessageID != curMsg) {
					flush()
					cur, curMsg = &control.ChatReadItem{Ord: l.Ord, Role: role}, ev.MessageID
				}
				cur.Text += ev.Text
			case EvtToolStarted:
				if activity {
					flush()
					out = append(out, readItem(l.Ord, "tool", ev.Name+toolBrief(ev.Input)))
				}
			case EvtTurnCompleted:
				flush()
				if activity {
					out = append(out, readItem(l.Ord, "turn", "turn completed"))
				}
			case EvtTurnFailed:
				flush()
				out = append(out, readItem(l.Ord, "turn", "turn failed: "+ev.Message))
			}
		}
	}
	flush()
	return out
}

func toolBrief(in map[string]any) string {
	s := firstNonEmpty(strField(in["command"]), strField(in["file_path"]), strField(in["path"]), strField(in["pattern"]), strField(in["description"]))
	if s == "" {
		return ""
	}
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return ": " + s
}
