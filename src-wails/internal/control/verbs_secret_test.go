package control

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeSecrets struct{ owner, name string }

func (f *fakeSecrets) RequestSecret(_ context.Context, owner, name, _ string) (string, error) {
	f.owner, f.name = owner, name
	return "/tmp/secrets/turn/abc-" + name, nil
}

func TestSecretRequestVerb(t *testing.T) {
	fs := &fakeSecrets{}
	c := New(Deps{Secrets: fs})
	if v := c.verbs["secret_request"]; v.Scope != ScopeLocal {
		t.Fatalf("secret_request scope = %v, want local only", v.Scope)
	}
	if _, err := c.Call(context.Background(), ScopeRemote, "secret_request", Params{"name": "K", "reason": "r", "parent_chat_id": "3"}); err == nil {
		t.Fatal("remote scope reached secret_request")
	}
	out, err := c.Call(context.Background(), ScopeLocal, "secret_request", Params{"name": "API_KEY", "reason": "r", "parent_chat_id": "3"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if string(b) != `{"name":"API_KEY","path":"/tmp/secrets/turn/abc-API_KEY"}` || fs.owner != "chat:3" {
		t.Fatalf("result %s owner %s", b, fs.owner)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "secret_request", Params{"name": "K", "reason": "r", "caller_pty_id": "12"}); err != nil || fs.owner != "pty:12" {
		t.Fatalf("pty owner = %s, %v", fs.owner, err)
	}
	for _, p := range []Params{{"name": "bad name", "reason": "r", "parent_chat_id": "3"}, {"name": "K", "reason": "r"}} {
		if _, err := c.Call(context.Background(), ScopeLocal, "secret_request", p); err == nil {
			t.Fatalf("accepted %v", p)
		}
	}
}
