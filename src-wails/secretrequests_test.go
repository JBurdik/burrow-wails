package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"burrow/internal/agentphase"
)

const testSecret = "sk-live-TOPSECRET-\"quoted\" $HOME\\n\nline2\ttab"

type secretRes struct {
	path string
	err  error
}

// newTestSecrets wires a registry to a real PhaseStore with onChange → the
// registry, like app.go, and records every published list.
func newTestSecrets(t *testing.T) (*secretRegistry, *PhaseStore, *[][]secretRequest) {
	t.Helper()
	a := newTestApp(t)
	store, err := NewPhaseStore(a.db)
	if err != nil {
		t.Fatal(err)
	}
	r := newSecretRegistry(filepath.Join(t.TempDir(), "secrets"), store)
	var published [][]secretRequest
	r.emit = func(l []secretRequest) { published = append(published, l) }
	store.onChange = func(id string, prev, next agentphase.Phase) { r.phaseChanged(id, prev, next) }
	return r, store, &published
}

func requestAsync(r *secretRegistry, ctx context.Context, owner, name string, repo int64) chan secretRes {
	out := make(chan secretRes, 1)
	go func() {
		p, err := r.Request(ctx, owner, name, "for the tests", repo)
		out <- secretRes{p, err}
	}()
	return out
}

func waitSecrets(t *testing.T, r *secretRegistry, n int) []secretRequest {
	t.Helper()
	for i := 0; i < 400; i++ {
		if l := r.List(); len(l) == n {
			return l
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("want %d pending, have %d", n, len(r.List()))
	return nil
}

func recvSecret(t *testing.T, ch chan secretRes) secretRes {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(2 * time.Second):
		t.Fatal("request did not release")
		return secretRes{}
	}
}

func TestSecretAnswerWritesPrivateFileAndMarksWaiting(t *testing.T) {
	r, store, published := newTestSecrets(t)
	store.Apply("chat:5", agentphase.Event{Kind: agentphase.HookRunning})

	ch := requestAsync(r, context.Background(), "chat:5", "API_KEY", 0)
	req := waitSecrets(t, r, 1)[0]
	ph := store.Get("chat:5")
	if ph.State != agentphase.WaitingInput || ph.Detail != "Secret requested: API_KEY" {
		t.Fatalf("pending phase = %+v, want waiting_input with the name", ph)
	}
	if err := r.Answer(req.ID, testSecret, false, false); err != nil {
		t.Fatal(err)
	}
	res := recvSecret(t, ch)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if got := store.Get("chat:5").State; got != agentphase.Running {
		t.Fatalf("after answer phase = %q, want running", got)
	}
	st, err := os.Stat(res.path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, want 0600", st.Mode().Perm())
	}
	if dst, _ := os.Stat(filepath.Dir(res.path)); dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", dst.Mode().Perm())
	}
	if !strings.HasSuffix(res.path, "-API_KEY") {
		t.Fatalf("path %q does not end in the name", res.path)
	}
	if b, _ := os.ReadFile(res.path); string(b) != testSecret {
		t.Fatalf("file content mismatch")
	}
	// The value never reaches a published list (or the phase).
	for _, l := range *published {
		b, _ := json.Marshal(l)
		if strings.Contains(string(b), "TOPSECRET") {
			t.Fatalf("value in published list: %s", b)
		}
	}
	if strings.Contains(store.Get("chat:5").Detail, "TOPSECRET") {
		t.Fatal("value in phase detail")
	}

	// Turn end deletes the delivered file.
	store.Apply("chat:5", agentphase.Event{Kind: agentphase.HookDone})
	if _, err := os.Stat(res.path); !os.IsNotExist(err) {
		t.Fatalf("file survived turn end: %v", err)
	}
}

func TestSecretDecline(t *testing.T) {
	r, store, _ := newTestSecrets(t)
	store.Apply("pty:3", agentphase.Event{Kind: agentphase.HookRunning})
	ch := requestAsync(r, context.Background(), "pty:3", "TOKEN", 0)
	req := waitSecrets(t, r, 1)[0]
	if err := r.Decline(req.ID); err != nil {
		t.Fatal(err)
	}
	res := recvSecret(t, ch)
	if res.err == nil || res.path != "" || !strings.Contains(res.err.Error(), "declined") {
		t.Fatalf("decline = %+v", res)
	}
	if got := store.Get("pty:3").State; got != agentphase.Running {
		t.Fatalf("after decline phase = %q, want running", got)
	}
	if err := r.Answer(req.ID, "x", false, false); err == nil {
		t.Fatal("answering a closed request succeeded")
	}
}

func TestSecretCancelAndTurnEndRelease(t *testing.T) {
	r, store, _ := newTestSecrets(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch := requestAsync(r, ctx, "chat:1", "A", 0)
	waitSecrets(t, r, 1)
	cancel() // caller gone / the verb's deadline
	if res := recvSecret(t, ch); res.err == nil {
		t.Fatal("cancelled request returned no error")
	}
	waitSecrets(t, r, 0)

	store.Apply("chat:2", agentphase.Event{Kind: agentphase.HookRunning})
	ch = requestAsync(r, context.Background(), "chat:2", "B", 0)
	waitSecrets(t, r, 1)
	store.Apply("chat:2", agentphase.Event{Kind: agentphase.Interrupt})
	if res := recvSecret(t, ch); res.err == nil || !strings.Contains(res.err.Error(), "turn ended") {
		t.Fatalf("turn end = %+v", res)
	}
	waitSecrets(t, r, 0)
}

func TestSecretNotInFlightIsNotMarked(t *testing.T) {
	r, store, _ := newTestSecrets(t)
	ch := requestAsync(r, context.Background(), "pty:9", "X", 0)
	req := waitSecrets(t, r, 1)[0]
	if got := store.Get("pty:9").State; got == agentphase.WaitingInput {
		t.Fatal("an idle shell's request invented a waiting turn")
	}
	_ = r.Answer(req.ID, "v", false, false)
	if res := recvSecret(t, ch); res.err != nil {
		t.Fatal(res.err)
	}
}

func TestSecretSweepDeletesOldFiles(t *testing.T) {
	r, _, _ := newTestSecrets(t)
	ch := requestAsync(r, context.Background(), "chat:4", "S", 0)
	req := waitSecrets(t, r, 1)[0]
	_ = r.Answer(req.ID, "v", false, false)
	res := recvSecret(t, ch)
	r.sweep(time.Now(), time.Hour)
	if _, err := os.Stat(res.path); err != nil {
		t.Fatal("fresh file swept")
	}
	r.sweep(time.Now().Add(31*time.Minute), secretFileTTL)
	if _, err := os.Stat(res.path); !os.IsNotExist(err) {
		t.Fatal("old file survived the sweep")
	}
}

func TestSecretInvalidName(t *testing.T) {
	r, _, _ := newTestSecrets(t)
	for _, n := range []string{"", "a b", "../x", "x/y", strings.Repeat("a", 65)} {
		if _, err := r.Request(context.Background(), "chat:1", n, "", 0); err == nil {
			t.Fatalf("name %q accepted", n)
		}
	}
}

func TestSavedSecretRoundTripAndUseSaved(t *testing.T) {
	r, store, published := newTestSecrets(t)
	store.Apply("chat:7", agentphase.Event{Kind: agentphase.HookRunning})

	// Remember: typed value saved for repo 42.
	ch := requestAsync(r, context.Background(), "chat:7", "DB_PASS", 42)
	req := waitSecrets(t, r, 1)[0]
	if req.HasSaved {
		t.Fatal("hasSaved before anything was saved")
	}
	if err := r.Answer(req.ID, testSecret, false, true); err != nil {
		t.Fatal(err)
	}
	recvSecret(t, ch)

	envPath := r.projectFile(42)
	st, err := os.Stat(envPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("project env: %v %v", err, st)
	}
	v, ok, err := r.savedValue(42, "DB_PASS")
	if err != nil || !ok || v != testSecret {
		t.Fatalf("round trip: ok=%v err=%v equal=%v", ok, err, v == testSecret)
	}
	raw, _ := os.ReadFile(envPath)
	if strings.Contains(string(raw), "\nline2") || !strings.Contains(string(raw), `\$HOME`) {
		t.Fatalf("dotenv not escaped:\n%s", raw)
	}

	// Next request: hasSaved, and Use saved delivers it with no value from the client.
	ch = requestAsync(r, context.Background(), "chat:7", "DB_PASS", 42)
	req = waitSecrets(t, r, 1)[0]
	if !req.HasSaved {
		t.Fatal("hasSaved missing")
	}
	if err := r.Answer(req.ID, "", true, false); err != nil {
		t.Fatal(err)
	}
	res := recvSecret(t, ch)
	if b, _ := os.ReadFile(res.path); string(b) != testSecret {
		t.Fatal("use saved delivered the wrong value")
	}

	// List: names + dates only.
	list, err := r.ListSaved(42)
	if err != nil || len(list) != 1 || list[0].Name != "DB_PASS" || list[0].Created == 0 {
		t.Fatalf("list = %+v %v", list, err)
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), "TOPSECRET") || strings.Contains(string(b), "value") {
		t.Fatalf("list leaks a value: %s", b)
	}
	for _, l := range *published {
		b, _ := json.Marshal(l)
		if strings.Contains(string(b), "TOPSECRET") {
			t.Fatalf("value in published list: %s", b)
		}
	}

	// Startup wipes per-turn files, never project-*.env.
	r2 := newSecretRegistry(r.dir, nil)
	if _, err := os.Stat(res.path); !os.IsNotExist(err) {
		t.Fatal("turn file survived a restart")
	}
	if _, err := os.Stat(envPath); err != nil {
		t.Fatal("restart removed the saved secrets")
	}

	if err := r2.DeleteSaved(42, "DB_PASS"); err != nil {
		t.Fatal(err)
	}
	if list, _ := r2.ListSaved(42); len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
}

func TestDotenvQuoteRoundTrip(t *testing.T) {
	for _, v := range []string{"", "plain", testSecret, `a\b`, "trailing\\", "ünïcode ✓", "x=y#z"} {
		list := parseSavedSecrets(formatSavedSecrets([]savedSecret{{Name: "K", Created: 1, Updated: 2, value: v}}))
		if len(list) != 1 || list[0].value != v || list[0].Created != 1 || list[0].Updated != 2 {
			t.Fatalf("round trip of %q = %+v", v, list)
		}
	}
}
