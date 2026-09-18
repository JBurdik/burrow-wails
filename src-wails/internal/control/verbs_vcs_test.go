package control

import (
	"context"
	"testing"
)

type fakeForge struct {
	listCalls  []string // cwd of each List
	mergeCalls []int
	squash     []bool
}

func (f *fakeForge) List(cwd, scope, state string) (any, error) {
	f.listCalls = append(f.listCalls, cwd)
	return []map[string]any{{"number": 1, "title": "t", "state": state}}, nil
}
func (f *fakeForge) View(cwd string, number int) (any, error) {
	return map[string]any{"number": number}, nil
}
func (f *fakeForge) Create(cwd, title, body, base, head string) (any, error) {
	return map[string]any{"title": title, "baseRefName": base, "headRefName": head}, nil
}
func (f *fakeForge) Merge(cwd string, number int, squash bool) error {
	f.mergeCalls = append(f.mergeCalls, number)
	f.squash = append(f.squash, squash)
	return nil
}

func TestPrVerbsKeepTheirContract(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	for _, name := range []string{"pr_create", "pr_list", "pr_view", "pr_merge"} {
		if _, ok := c.verbs[name]; !ok {
			t.Fatalf("verb %q disappeared — it is a public contract", name)
		}
	}
}

func TestPrListGoesThroughTheForge(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	if _, err := c.Call(context.Background(), ScopeLocal, "pr_list", Params{"cwd": "/repo", "state": "open"}); err != nil {
		t.Fatal(err)
	}
	if len(ff.listCalls) != 1 || ff.listCalls[0] != "/repo" {
		t.Errorf("List calls = %v", ff.listCalls)
	}
}

func TestPrMergePassesSquash(t *testing.T) {
	ff := &fakeForge{}
	c := New(Deps{Forge: ff})
	if _, err := c.Call(context.Background(), ScopeLocal, "pr_merge", Params{"cwd": "/repo", "number": float64(5), "squash": true}); err != nil {
		t.Fatal(err)
	}
	if len(ff.mergeCalls) != 1 || ff.mergeCalls[0] != 5 || !ff.squash[0] {
		t.Errorf("Merge(%v, squash=%v)", ff.mergeCalls, ff.squash)
	}
}
