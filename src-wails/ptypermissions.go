package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"burrow/internal/agentphase"
)

// Answerable PTY permission requests.
//
// Claude's PermissionRequest hook (`burrow approve`, a second entry beside the
// status hook in the burrow plugin's hooks.json) long-polls
// POST /v1/permission_request and parks here until the user answers in the
// desktop UI. Whatever the answer, the TUI prompt stays up the whole time:
// every path that does not produce an explicit answer returns NO body, which
// is how Claude is told "no decision, ask the user". Nothing here ever
// falls back to allow.
//
// A held request closes — releasing the hook with an empty reply — when it is
// answered, when the hook's connection drops, or when the tab's phase leaves
// waiting_approval (PostToolUse / Stop / the next prompt).
//
// SPIKE (claude 2.1.291): when the user answers in the TUI, Claude does NOT
// kill the losing hook — it kept running (and its curl child with it) for the
// rest of the session and got SIGTERM only when Claude exited. So the
// connection-drop path covers Claude exiting / the hook being killed, and the
// phase path is what actually clears a request answered in the terminal.

// permRequest is one parked prompt, keyed by pty id + hash(tool_name,
// tool_input): the hook payload has no tool_use_id (it is minted after the
// permission check), so the pair is the only handle there is.
type permRequest struct {
	ID        string          `json:"id"`
	PtyID     string          `json:"pty_id"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	At        int64           `json:"at"`

	suggestions json.RawMessage
	// waiters: two byte-identical requests in flight (parallel tool calls)
	// share one card and one answer; each hook gets its own channel.
	waiters []chan []byte
	// sawApproval gates the phase-leaves-waiting_approval close. The approve
	// hook and the status hook run in parallel, so a request can be parked a
	// moment before the phase reads waiting_approval — a stray running event
	// in that gap must not dismiss it.
	sawApproval bool
}

type permRegistry struct {
	mu     sync.Mutex
	reqs   []*permRequest
	phases *PhaseStore
}

func newPermRegistry(phases *PhaseStore) *permRegistry {
	return &permRegistry{phases: phases}
}

func permKey(ptyID, tool string, input json.RawMessage) string {
	h := sha256.Sum256([]byte(tool + "\x00" + string(input)))
	return ptyID + ":" + hex.EncodeToString(h[:6])
}

// forPty returns the pty's requests oldest first. Caller holds mu.
func (p *permRegistry) forPty(ptyID string) []permRequest {
	out := []permRequest{}
	for _, r := range p.reqs {
		if r.PtyID == ptyID {
			out = append(out, *r)
		}
	}
	return out
}

// emit publishes the pty's whole pending list (state, not a delta — a missed
// or replayed event cannot leave the UI wrong).
func (p *permRegistry) emit(ptyID string, list []permRequest) {
	busEmit("pty-permissions", map[string]any{"pty_id": ptyID, "requests": list})
}

// Hold parks until answered; nil means "no decision" (see file comment).
func (p *permRegistry) Hold(ctx context.Context, ptyID, tool string, input, suggestions json.RawMessage) []byte {
	id := permKey(ptyID, tool, input)
	ch := make(chan []byte, 1)

	p.mu.Lock()
	var req *permRequest
	for _, r := range p.reqs {
		if r.ID == id {
			req = r
		}
	}
	if req == nil {
		req = &permRequest{ID: id, PtyID: ptyID, ToolName: tool, ToolInput: input, At: time.Now().UnixMilli(), suggestions: suggestions}
		if p.phases != nil {
			req.sawApproval = p.phases.Get("pty:"+ptyID).State == agentphase.WaitingApproval
		}
		p.reqs = append(p.reqs, req)
	}
	req.waiters = append(req.waiters, ch)
	list := p.forPty(ptyID)
	p.mu.Unlock()
	p.emit(ptyID, list)

	select {
	case out := <-ch:
		return out
	case <-ctx.Done():
	}

	p.mu.Lock()
	for i, w := range req.waiters {
		if w == ch {
			req.waiters = append(req.waiters[:i], req.waiters[i+1:]...)
			break
		}
	}
	if len(req.waiters) == 0 {
		p.removeLocked(req)
	}
	list = p.forPty(ptyID)
	p.mu.Unlock()
	p.emit(ptyID, list)
	return nil
}

func (p *permRegistry) removeLocked(req *permRequest) {
	for i, r := range p.reqs {
		if r == req {
			p.reqs = append(p.reqs[:i], p.reqs[i+1:]...)
			return
		}
	}
}

// finishLocked releases every waiter of req with out (nil = no decision).
func (p *permRegistry) finishLocked(req *permRequest, out []byte) {
	for _, w := range req.waiters {
		w <- out // buffered 1, one send per waiter
	}
	req.waiters = nil
	p.removeLocked(req)
}

// Answer resolves a request with the user's decision.
func (p *permRegistry) Answer(id, behavior, message string) error {
	p.mu.Lock()
	var req *permRequest
	for _, r := range p.reqs {
		if r.ID == id {
			req = r
		}
	}
	if req == nil {
		p.mu.Unlock()
		return fmt.Errorf("permission request %s is no longer pending", id)
	}
	out, err := permOutput(req, behavior, message)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	p.finishLocked(req, out)
	list := p.forPty(req.PtyID)
	p.mu.Unlock()
	p.emit(req.PtyID, list)
	return nil
}

// phaseChanged closes the pty's requests once it has left waiting_approval.
func (p *permRegistry) phaseChanged(key string, ph agentphase.Phase) {
	ptyID, ok := strings.CutPrefix(key, "pty:")
	if !ok {
		return
	}
	p.mu.Lock()
	changed := false
	for _, r := range append([]*permRequest(nil), p.reqs...) {
		if r.PtyID != ptyID {
			continue
		}
		if ph.State == agentphase.WaitingApproval {
			r.sawApproval = true
		} else if r.sawApproval || ph.State == agentphase.Done || ph.State == agentphase.Idle ||
			ph.State == agentphase.Failed || ph.State == agentphase.Stale {
			// Not yet seen waiting_approval, but the turn ended: no tool can
			// still be pending on a prompt.
			p.finishLocked(r, nil)
			changed = true
		}
	}
	list := p.forPty(ptyID)
	p.mu.Unlock()
	if changed {
		p.emit(ptyID, list)
	}
}

func (p *permRegistry) List() []permRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]permRequest, 0, len(p.reqs))
	for _, r := range p.reqs {
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// permOutput builds the hook's stdout for an answer. Unknown behaviour is an
// error, never an implicit allow.
func permOutput(req *permRequest, behavior, message string) ([]byte, error) {
	var decision map[string]any
	switch behavior {
	case "allow":
		decision = map[string]any{"behavior": "allow"}
	case "always":
		decision = map[string]any{"behavior": "allow", "updatedPermissions": []any{alwaysRule(req)}}
	case "deny":
		decision = map[string]any{"behavior": "deny", "message": message, "interrupt": false}
	default:
		return nil, fmt.Errorf("unknown permission answer %q", behavior)
	}
	return json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PermissionRequest",
		"decision":      decision,
	}})
}

// alwaysRule is the session-scoped allow rule for "Always allow": the first
// addRules suggestion Claude offered, re-pointed at the session (persisted
// destinations stay the TUI's business), else a rule for this exact call.
func alwaysRule(req *permRequest) map[string]any {
	var sugg []map[string]any
	_ = json.Unmarshal(req.suggestions, &sugg)
	for _, s := range sugg {
		if s["type"] == "addRules" {
			s["behavior"] = "allow"
			s["destination"] = "session"
			return s
		}
	}
	rule := map[string]any{"toolName": req.ToolName}
	if req.ToolName == "Bash" {
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(req.ToolInput, &in) == nil && in.Command != "" {
			rule["ruleContent"] = in.Command
		}
	}
	return map[string]any{"type": "addRules", "rules": []any{rule}, "behavior": "allow", "destination": "session"}
}

// --- App surface --------------------------------------------------------------

// ListPtyPermissions is the desktop's first paint of pending PTY permission
// requests; later changes arrive as `pty-permissions` events.
func (a *App) ListPtyPermissions() []permRequest {
	if a.perms == nil {
		return []permRequest{}
	}
	return a.perms.List()
}

// AnswerPtyPermission answers one parked request: behavior is allow, deny
// (message optional, shown to Claude) or always (allow + session rule).
func (a *App) AnswerPtyPermission(id, behavior, message string) error {
	if a.perms == nil {
		return fmt.Errorf("permissions not ready")
	}
	return a.perms.Answer(id, behavior, message)
}

// handlePermissionRequest is the `burrow approve` long-poll. 204 = no
// decision (the TUI decides); only an explicit answer produces a body.
func (a *App) handlePermissionRequest(w http.ResponseWriter, r *http.Request) {
	if !a.controlAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	var in struct {
		ToolName    string          `json:"tool_name"`
		ToolInput   json.RawMessage `json:"tool_input"`
		Suggestions json.RawMessage `json:"permission_suggestions"`
	}
	ptyID := r.URL.Query().Get("pty_id")
	// The CLI already skips these two; repeating it here keeps a hand-rolled
	// client from parking a question/plan the v1 UI cannot answer.
	if err != nil || json.Unmarshal(body, &in) != nil || in.ToolName == "" || ptyID == "" || a.perms == nil ||
		in.ToolName == "AskUserQuestion" || in.ToolName == "ExitPlanMode" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out := a.perms.Hold(r.Context(), ptyID, in.ToolName, in.ToolInput, in.Suggestions)
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}
