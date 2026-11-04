package control

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type fakeChatControl struct {
	parent      map[int64]int64
	pending     []PendingRequest
	answered    map[string][]string
	interrupted []int64
	steered     []string
	queued      []string
}

func (f *fakeChatControl) ChatParent(id int64) (int64, error)      { return f.parent[id], nil }
func (f *fakeChatControl) Pending(int64) ([]PendingRequest, error) { return f.pending, nil }
func (f *fakeChatControl) Answer(_ int64, _ string, a map[string][]string) error {
	f.answered = a
	return nil
}
func (f *fakeChatControl) Interrupt(id int64) error {
	f.interrupted = append(f.interrupted, id)
	return nil
}
func (f *fakeChatControl) Steer(_ int64, t string) error {
	f.steered = append(f.steered, t)
	return nil
}
func (f *fakeChatControl) Queue(_ int64, t string) error {
	f.queued = append(f.queued, t)
	return nil
}
func (f *fakeChatControl) Read(id, after int64, limit int, activity bool) (ChatReadResult, error) {
	return ChatReadResult{ChatID: id, Next: after + int64(limit)}, nil
}

func question(id string, qs ...string) PendingRequest {
	r := PendingRequest{ID: id, Kind: "question"}
	for _, q := range qs {
		r.Questions = append(r.Questions, PendingQuestion{ID: q, Question: q})
	}
	return r
}

func TestWaitResultReturnsBlockedChat(t *testing.T) {
	cc := &fakeChatControl{pending: []PendingRequest{question("r1", "Which DB?")}}
	c := newTestCore(t, Deps{Phases: &fakePhases{state: "waiting_input"}, Chats: &fakeChats{}, ChatControl: cc})
	got, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(5), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	b, ok := got.(WaitBlocked)
	if !ok || !b.Blocked || b.Phase != "waiting_input" || len(b.Pending) != 1 || b.Pending[0].ID != "r1" {
		t.Fatalf("got %#v", got)
	}
}

func TestChatAnswerOnlyAnswersOwnSubagentsQuestions(t *testing.T) {
	cc := &fakeChatControl{parent: map[int64]int64{5: 1}, pending: []PendingRequest{
		{ID: "a1", Kind: "approval", Tool: "Bash"}, question("q1", "Which DB?"),
	}}
	c := newTestCore(t, Deps{ChatControl: cc})
	call := func(p Params) error {
		_, err := c.Call(context.Background(), ScopeLocal, "chat_answer", p)
		return err
	}
	if err := call(Params{"chat_id": float64(5), "parent_chat_id": float64(9), "answer": "pg"}); err == nil || !strings.Contains(err.Error(), "not your sub-agent") {
		t.Fatalf("foreign caller: %v", err)
	}
	if err := call(Params{"chat_id": float64(5), "parent_chat_id": float64(1), "answer": "pg"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cc.answered, map[string][]string{"Which DB?": {"pg"}}) {
		t.Fatalf("answered %v", cc.answered)
	}
	// An approval is never answerable, even by id.
	if err := call(Params{"chat_id": float64(5), "request_id": "a1", "answer": "yes"}); err == nil {
		t.Fatal("answered an approval")
	}
}

func TestParseAnswers(t *testing.T) {
	two := question("q", "A", "B")
	if _, err := parseAnswers("", "x", two); err == nil {
		t.Error("single answer to two questions accepted")
	}
	got, err := parseAnswers(`{"A":"1","B":["2","3"]}`, "", two)
	if err != nil || !reflect.DeepEqual(got, map[string][]string{"A": {"1"}, "B": {"2", "3"}}) {
		t.Errorf("got %v %v", got, err)
	}
	if _, err := parseAnswers(`{"C":"1"}`, "", two); err == nil {
		t.Error("unknown question accepted")
	}
}

func TestChatSendModesAndInterrupt(t *testing.T) {
	cc := &fakeChatControl{parent: map[int64]int64{5: 1}}
	c := newTestCore(t, Deps{ChatControl: cc})
	ctx := context.Background()
	for _, mode := range []string{"queue", "steer"} {
		if _, err := c.Call(ctx, ScopeLocal, "chat_send", Params{"chat_id": float64(5), "text": mode, "mode": mode}); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(cc.queued, []string{"queue"}) || !reflect.DeepEqual(cc.steered, []string{"steer"}) {
		t.Fatalf("queued %v steered %v", cc.queued, cc.steered)
	}
	if _, err := c.Call(ctx, ScopeLocal, "chat_send", Params{"chat_id": float64(5), "text": "x", "mode": "restart"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if _, err := c.Call(ctx, ScopeLocal, "chat_interrupt", Params{"chat_id": float64(5), "parent_chat_id": float64(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, ScopeLocal, "chat_interrupt", Params{"chat_id": float64(5), "parent_chat_id": float64(2)}); err == nil {
		t.Fatal("interrupted someone else's sub-agent")
	}
	if !reflect.DeepEqual(cc.interrupted, []int64{5}) {
		t.Fatalf("interrupted %v", cc.interrupted)
	}
}
