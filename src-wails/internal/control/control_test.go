package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestCore(t *testing.T, deps Deps) *Core {
	t.Helper()
	if deps.SessionDir == "" {
		deps.SessionDir = t.TempDir()
	}
	return New(deps)
}

// The registry is what generates the MCP schema, the CLI help and the Manager's
// primer, so a verb missing its docs is a real defect, not a nit.
func TestRegistryIsSelfDescribing(t *testing.T) {
	c := newTestCore(t, Deps{})
	verbs := c.Verbs()
	if len(verbs) < 20 {
		t.Fatalf("expected the full verb set, got %d", len(verbs))
	}
	for _, v := range verbs {
		if v.Summary == "" {
			t.Errorf("%s: no summary", v.Name)
		}
		if v.Scope == 0 {
			t.Errorf("%s: no scope, so no transport can call it", v.Name)
		}
		if v.Fn == nil {
			t.Errorf("%s: no implementation", v.Name)
		}
		for _, a := range v.Args {
			if a.Desc == "" || a.Type == "" {
				t.Errorf("%s: arg %q is undocumented", v.Name, a.Name)
			}
		}
	}
}

func TestCallRejectsUnknownVerbAndMissingArgs(t *testing.T) {
	c := newTestCore(t, Deps{})

	_, err := c.Call(context.Background(), ScopeLocal, "nope", nil)
	var unknown ErrUnknownVerb
	if !errors.As(err, &unknown) {
		t.Errorf("unknown verb: want ErrUnknownVerb, got %v", err)
	}

	_, err = c.Call(context.Background(), ScopeLocal, "spawn", Params{})
	if err == nil || !strings.Contains(err.Error(), "task") {
		t.Errorf("spawn without a task should name the missing arg, got %v", err)
	}
}

// A verb is local-only unless it opts in, so a remote client can't spawn
// processes just because the desktop can.
func TestRemoteScopeIsOptIn(t *testing.T) {
	c := newTestCore(t, Deps{})

	_, err := c.Call(context.Background(), ScopeRemote, "spawn", Params{"task": "x"})
	var forbidden ErrForbidden
	if !errors.As(err, &forbidden) {
		t.Errorf("spawn from remote: want ErrForbidden, got %v", err)
	}
	for _, name := range []string{"tab_close", "worktree_remove", "pr_merge", "run", "new_tab"} {
		v := c.verbs[name]
		if v.Scope.Has(ScopeRemote) {
			t.Errorf("%s is reachable from the network; it shouldn't be", name)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := splitArgs(`rg -n "two words" src/lib`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"rg", "-n", "two words", "src/lib"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}

	if _, err := splitArgs(`cat foo | rm -rf /`); err == nil {
		t.Error("a pipeline must be rejected, not silently truncated to `cat foo`")
	}
	if _, err := splitArgs(`grep "unbalanced`); err == nil {
		t.Error("unbalanced quote should error")
	}
}

func TestRunRefusesProgramsThatCanWrite(t *testing.T) {
	c := newTestCore(t, Deps{Exec: execStub{}})
	_, err := c.Call(context.Background(), ScopeLocal, "run", Params{"cmd": "rm -rf build"})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("run rm: want a read-only refusal, got %v", err)
	}
	res, err := c.Call(context.Background(), ScopeLocal, "run", Params{"cmd": "ls -la"})
	if err != nil {
		t.Fatalf("run ls: %v", err)
	}
	if got := res.(CmdResult).Stdout; got != "ls -la" {
		t.Errorf("argv did not reach the runner intact: %q", got)
	}
}

type execStub struct{}

func (execStub) RunProgram(prog, cwd string, args []string) (string, string, int) {
	return strings.TrimSpace(prog + " " + strings.Join(args, " ")), "", 0
}

// collect_results is a queue: each finished result is handed out once, then its
// marker files go away. Handing the same result to a Manager twice would have it
// report the same work as done repeatedly.
func TestCollectResultsDrainsEachResultOnce(t *testing.T) {
	dir := t.TempDir()
	c := newTestCore(t, Deps{SessionDir: dir})
	for _, tok := range []string{"res1", "res2"} {
		if err := os.WriteFile(filepath.Join(dir, tok+".result"), []byte("done: "+tok), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, tok+".done"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An in-flight agent has no .done yet and must not be collected.
	if err := os.WriteFile(filepath.Join(dir, "res3.result"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := c.collectResults(Params{})
	if err != nil {
		t.Fatal(err)
	}
	results := first.([]Result)
	if len(results) != 2 || results[0].Token != "res1" || results[0].Text != "done: res1" {
		t.Fatalf("first collect: %+v", results)
	}

	second, _ := c.collectResults(Params{})
	if got := second.([]Result); len(got) != 0 {
		t.Errorf("second collect should be empty, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "res3.result")); err != nil {
		t.Error("an unfinished result was consumed")
	}
}

func TestListWorkspacesAndTabs(t *testing.T) {
	db := openTestDB(t)
	c := newTestCore(t, Deps{DB: db})

	ws, err := c.listWorkspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 || ws[0].Name != "repo" || ws[1].Branch != "feat/x" {
		t.Fatalf("workspaces: %+v", ws)
	}
	if ws[1].ParentID == nil || *ws[1].ParentID != 1 {
		t.Errorf("worktree should carry its parent: %+v", ws[1])
	}

	// A caller that knows only its directory still gets its own tabs.
	tabs, err := c.listTabs(context.Background(), 0, "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].PtyID != 7 || tabs[0].Status != "running" {
		t.Fatalf("tabs: %+v", tabs)
	}
	if _, err := c.listTabs(context.Background(), 0, "/tmp/not-a-workspace"); err == nil {
		t.Error("an unknown cwd should error, not return an empty list")
	}
}

// repoOf always climbs to the root repo: git has no worktree of a worktree, and
// a Manager anchored in a worktree still means "this project" when it says repo.
func TestRepoOfClimbsToRoot(t *testing.T) {
	c := newTestCore(t, Deps{DB: openTestDB(t)})
	id, path, err := c.repoOf("/tmp/wt/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 || path != "/tmp/repo" {
		t.Errorf("got %d %s, want the parent repo", id, path)
	}
}

// fakeUI is a UIBridge test double: it records the action and args it was
// called with and hands back a canned result, so a verb's args map can be
// asserted on without a real frontend to ack it.
type fakeUI struct {
	action string
	args   map[string]any
	result any
}

func (f *fakeUI) Do(ctx context.Context, action string, args map[string]any) (json.RawMessage, error) {
	f.action, f.args = action, args
	return json.Marshal(f.result)
}

// A spawn made from inside a thread produces a sub-agent OF that thread, and a
// sub-agent is a chat: a terminal tab would put it back in the Sidebar, which
// is the arrangement this feature exists to replace.
func TestSpawnFromChatForcesChatTargetAndCarriesParent(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{ChatID: 9, Target: "chat"}}
	c := newTestCore(t, Deps{UI: ui})

	if _, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{
		"task":           "investigate the cache bug",
		"target":         "tab",
		"parent_chat_id": float64(7),
	}); err != nil {
		t.Fatal(err)
	}
	if ui.args["target"] != "chat" {
		t.Errorf("target = %v, want chat", ui.args["target"])
	}
	if ui.args["parent_chat_id"] != int64(7) {
		t.Errorf("parent_chat_id = %v, want 7", ui.args["parent_chat_id"])
	}
}

// IMPORTANT 6: a spawn made from the Manager (a `control` chat) is exempt
// from the parent-forces-chat rule above. The Manager is control:true and
// never the active session, and the Right Panel's Sub-agents list is scoped
// to the active session, so a Manager-spawned CHAT sub-agent would land in no
// list anywhere. `parent_is_control` is set server-side the same way
// caller_is_subagent is (controlapi.go derives both from the DB, never trusts
// the request) — this test exercises the verb with it already set, which is
// the contract the verb owns; controlapi.go's derivation is that param's
// wiring, not the verb's behaviour.
func TestSpawnFromControlChatKeepsTabTarget(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{PtyID: 3, Target: "tab"}}
	c := newTestCore(t, Deps{UI: ui})

	if _, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{
		"task":              "investigate the cache bug",
		"target":            "tab",
		"parent_chat_id":    float64(7),
		"parent_is_control": true,
	}); err != nil {
		t.Fatal(err)
	}
	if ui.args["target"] != "tab" {
		t.Errorf("target = %v, want tab — a Manager spawn must not be forced to chat", ui.args["target"])
	}
}

// Depth is capped at one level: recursive agent trees run away in cost and the
// panel that shows them is a flat list.
func TestSubAgentCannotSpawn(t *testing.T) {
	ui := &fakeUI{result: SpawnResult{ChatID: 9, Target: "chat"}}
	c := newTestCore(t, Deps{UI: ui})

	_, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{
		"task":               "do more work",
		"parent_chat_id":     float64(7),
		"caller_is_subagent": true,
	})
	if err == nil || !strings.Contains(err.Error(), "sub-agent cannot spawn sub-agents") {
		t.Fatalf("err = %v, want the sub-agent guard", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE workspaces (id INTEGER PRIMARY KEY, name TEXT, path TEXT, parent_id INTEGER,
			worktree_branch TEXT, sort_order REAL DEFAULT 0)`,
		`CREATE TABLE terminal_tabs (id INTEGER PRIMARY KEY, workspace_id INTEGER, ord INTEGER,
			pty_id INTEGER, title TEXT, status TEXT)`,
		`INSERT INTO workspaces VALUES (1,'repo','/tmp/repo',NULL,NULL,0)`,
		`INSERT INTO workspaces VALUES (2,'feat-x','/tmp/wt/feat-x',1,'feat/x',1)`,
		`INSERT INTO terminal_tabs VALUES (1,1,0,7,'claude','running')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return db
}

// fakePhases is a Phases test double: every key reports the same canned
// state, which is all wait_result/collect_results need to decide whether a
// chat sub-agent is finished.
//
// `endedAt` is what every call from the SECOND one onward reports;
// `baselineEndedAt` is reported on the very first call only — wait_result's
// own baseline snapshot, taken before its poll loop starts (see
// TestWaitResultRequiresTheTurnToAdvance). Defaults to 0, which is lower than
// any endedAt an existing test cares about, so a test that never sets it
// keeps resolving on the loop's first check exactly as before that baseline
// was added.
type fakePhases struct {
	state           string
	endedAt         int64
	baselineEndedAt int64
	calls           int
}

func (f *fakePhases) Phase(key string) (string, int64) {
	f.calls++
	if f.calls == 1 {
		return f.state, f.baselineEndedAt
	}
	return f.state, f.endedAt
}

// fakeChats is a ChatReader test double with a single canned transcript tail.
type fakeChats struct{ last string }

func (f *fakeChats) LastAssistantMessage(int64) (string, error) { return f.last, nil }
func (f *fakeChats) UncollectedChildren(int64) ([]int64, error) { return nil, nil }
func (f *fakeChats) MarkCollected(int64) error                  { return nil }

// A chat sub-agent writes no capture files, so waiting on one has to read the
// phase Go already derives — which also means waiting works with no view of
// the child mounted anywhere.
func TestWaitResultResolvesFromChatPhase(t *testing.T) {
	c := newTestCore(t, Deps{Phases: &fakePhases{state: "done", endedAt: 123}, Chats: &fakeChats{last: "found it: an off-by-one"}})

	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "found it: an off-by-one" {
		t.Fatalf("text = %q", got)
	}
}

// A failed turn still ends the wait: the caller wants to know, and blocking
// until the timeout tells it nothing it can act on.
func TestWaitResultReturnsOnFailedPhase(t *testing.T) {
	c := newTestCore(t, Deps{Phases: &fakePhases{state: "failed", endedAt: 1}, Chats: &fakeChats{last: "could not build"}})
	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "could not build" {
		t.Fatalf("text = %q, want the failed turn's transcript tail", got)
	}
}

// IMPORTANT 4: the documented workflow is chat_send then
// wait_result --chat-id, but chat_send does not move the phase to `running`
// synchronously — the CLI has to pick the prompt up first. A phase already
// sitting `done` from the PREVIOUS turn (or one that was never going to
// start a new turn at all) must not be mistaken for this call's answer: it
// has to be a real timeout, not an instant stale result plus a wrongly
// collected child. `endedAt == baselineEndedAt` here models exactly that —
// the phase never advances past what wait_result saw when it started.
func TestWaitResultRequiresTheTurnToAdvance(t *testing.T) {
	c := newTestCore(t, Deps{
		Phases: &fakePhases{state: "done", endedAt: 100, baselineEndedAt: 100},
		Chats:  &fakeChats{last: "the previous turn's answer"},
	})
	start := time.Now()
	_, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(1)})
	if err == nil {
		t.Fatal("want a timeout error — the phase never advanced past its baseline")
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("err = %v, want a timeout message", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("returned after %s, before its own 1s timeout — it must have accepted the stale phase", elapsed)
	}
}

// The mirror image: once the phase's turn_ended_at genuinely advances past
// the baseline wait_result captured at the start, it must resolve — this is
// what stops the fix above from becoming "chat sub-agents never finish".
func TestWaitResultResolvesOnceTheTurnAdvancesPastBaseline(t *testing.T) {
	c := newTestCore(t, Deps{
		// baselineEndedAt (0, the default) < endedAt (200): the wait starts
		// before this turn's phase has settled, exactly like a real
		// chat_send whose phase hasn't flipped to running yet.
		Phases: &fakePhases{state: "done", endedAt: 200},
		Chats:  &fakeChats{last: "this turn's real answer"},
	})
	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "this turn's real answer" {
		t.Fatalf("text = %q", got)
	}
}

// Regression from the fix above: `stale` (the dead-PTY-equivalent watchdog
// for a chat — its CLI process died) is NOT a turn boundary the way
// done/failed are, so it must never be gated on the baseline advancing. A
// dead process can't send anything that would advance turn_ended_at, so
// requiring an advance here would turn "instantly wrong" (the original
// finding-4 bug) into "block for the whole timeout, then STILL wrong" — worse,
// not better. `endedAt == baselineEndedAt` here is deliberate: even with no
// advance at all, a stale phase must resolve immediately.
func TestWaitResultOnStaleChildAnswersPromptly(t *testing.T) {
	c := newTestCore(t, Deps{
		Phases: &fakePhases{state: "stale", endedAt: 100, baselineEndedAt: 100},
		Chats:  &fakeChats{last: "whatever it left behind"},
	})
	start := time.Now()
	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(30)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "whatever it left behind" {
		t.Fatalf("text = %q", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %s to answer a dead child — want near-instant, not a poll-cycle-scale wait", elapsed)
	}
}

// Another regression from the fix above: TestWaitResultRequiresTheTurnToAdvance
// proved a same-baseline done/failed phase is not an instant match. Without a
// bound, that correctly-strict rule became "block for the FULL timeout" (often
// 10 minutes) even for the common case — a fast child whose only turn already
// finished before wait_result's baseline was even captured, so there never was
// a "previous" turn to confuse it with, AND was never observed in flight
// during the whole wait. waitResultIdleGrace bounds that: stuck-at-baseline
// with no in-flight sighting at all is trusted after a window rather than the
// whole timeout.
//
// This test shrinks the grace window (rather than sleeping for the real one)
// and checks BOTH directions: the answer is not handed back before the grace
// window elapses (finding-4's actual guarantee — no INSTANT stale read), and
// it IS handed back once the grace window passes rather than blocking for the
// full 5s timeout configured below.
func TestWaitResultIdleGraceBoundsTheBaselineWait(t *testing.T) {
	old := waitResultIdleGrace
	waitResultIdleGrace = 300 * time.Millisecond
	t.Cleanup(func() { waitResultIdleGrace = old })

	c := newTestCore(t, Deps{
		// Pinned at the baseline for the whole wait, NEVER running — the
		// "genuinely idle child" case the grace window exists for.
		Phases: &fakePhases{state: "done", endedAt: 100, baselineEndedAt: 100},
		Chats:  &fakeChats{last: "settled before this wait ever started"},
	})
	start := time.Now()
	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < waitResultIdleGrace {
		t.Fatalf("returned after %s, before its own %s grace window — an unadvanced, never-in-flight baseline must not resolve instantly", elapsed, waitResultIdleGrace)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("took %s to answer — the grace window must cap the wait, not the 5s timeout", elapsed)
	}
	if got := out.(Result).Text; got != "settled before this wait ever started" {
		t.Fatalf("text = %q", got)
	}
}

// timedPhases scripts a phase sequence by ELAPSED WALL-CLOCK TIME (rather
// than call count), so it can model a specific interleaving against
// wait_result's real 500ms poll ticks: a cold start that looks idle for
// longer than a naive grace window, THEN is observed running, THEN drops
// back to a `done` reading with the SAME turn_ended_at (a poll simply
// catching a stale snapshot) for a while, and only later genuinely advances.
type timedPhases struct{ start time.Time }

func (p *timedPhases) Phase(string) (string, int64) {
	switch e := time.Since(p.start); {
	case e < 200*time.Millisecond:
		return "done", 100 // baseline + first poll: looks idle
	case e < 600*time.Millisecond:
		return "running", 0 // the CLI finally picked up the prompt
	case e < 1200*time.Millisecond:
		return "done", 100 // a poll catches it between output bursts, unadvanced
	default:
		return "done", 200 // the turn genuinely finishes
	}
}

// timedChats hands back a different "current answer" before and after the
// point timedPhases' turn genuinely completes — standing in for a real
// transcript, whose content actually differs before/after a turn finishes.
type timedChats struct{ start time.Time }

func (c *timedChats) LastAssistantMessage(int64) (string, error) {
	if time.Since(c.start) < 1200*time.Millisecond {
		return "the previous turn's answer", nil
	}
	return "this turn's real answer", nil
}
func (c *timedChats) UncollectedChildren(int64) ([]int64, error) { return nil, nil }
func (c *timedChats) MarkCollected(int64) error                  { return nil }

// This is the interleaving the coordinator's re-review flagged: a plain
// elapsed-time grace fires the moment it expires regardless of what has
// happened since, so a poll that catches the phase back at `done` with its
// UNCHANGED turn_ended_at — after already having been seen `running` once —
// used to be treated exactly like a child that was never going to start a
// new turn at all, returning chat_send's PREVIOUS answer and marking the
// child collected before its real answer ever lands. Requiring an observed
// in-flight sighting to permanently disable the grace escape (no matter how
// much more time passes at `done` afterward) is what closes this: only
// turn_ended_at actually advancing can end the wait once running has been
// seen even once.
func TestWaitResultDoesNotReturnStaleAnswerAfterBeingSeenInFlight(t *testing.T) {
	old := waitResultIdleGrace
	waitResultIdleGrace = 400 * time.Millisecond
	t.Cleanup(func() { waitResultIdleGrace = old })

	start := time.Now()
	c := newTestCore(t, Deps{
		Phases: &timedPhases{start: start},
		Chats:  &timedChats{start: start},
	})
	out, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{"chat_id": float64(9), "timeout": float64(5)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.(Result).Text; got != "this turn's real answer" {
		t.Fatalf("text = %q, want this turn's real answer — a stale previous answer slipped through", got)
	}
}

// Neither a token nor a chat id is a caller error, not a ten-minute block.
func TestWaitResultNeedsATarget(t *testing.T) {
	c := newTestCore(t, Deps{})
	_, err := c.Call(context.Background(), ScopeLocal, "wait_result", Params{})
	if err == nil || !strings.Contains(err.Error(), "needs a token or a chat_id") {
		t.Fatalf("err = %v", err)
	}
}

// countingChats is a ChatReader test double whose UncollectedChildren returns
// its children slice until MarkCollected empties it — enough to prove
// collect_results doesn't hand back the same finished child forever.
type countingChats struct {
	children []int64
	last     string
	marked   int
}

func (c *countingChats) LastAssistantMessage(int64) (string, error) { return c.last, nil }

func (c *countingChats) UncollectedChildren(int64) ([]int64, error) { return c.children, nil }

func (c *countingChats) MarkCollected(id int64) error {
	c.marked++
	remaining := c.children[:0]
	for _, existing := range c.children {
		if existing != id {
			remaining = append(remaining, existing)
		}
	}
	c.children = remaining
	return nil
}

// Without collected_at, every call would hand back the same finished child —
// which is how a supervising loop turns into an infinite one.
func TestCollectResultsTakesEachChildOnce(t *testing.T) {
	chats := &countingChats{children: []int64{5}, last: "done deal"}
	c := newTestCore(t, Deps{Phases: &fakePhases{state: "done", endedAt: 1}, Chats: chats})

	first, err := c.Call(context.Background(), ScopeLocal, "collect_results", Params{"parent_chat_id": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.([]Result)) != 1 {
		t.Fatalf("first sweep returned %d results, want 1", len(first.([]Result)))
	}
	if chats.marked != 1 {
		t.Fatalf("MarkCollected called %d times, want 1", chats.marked)
	}
}

// A child still mid-turn is not a result yet: the sweep must skip it and must
// not mark it collected, or a Manager polling collect_results would lose the
// answer the moment the turn does finish (nothing would ever ask again).
func TestCollectResultsSkipsRunningChild(t *testing.T) {
	chats := &countingChats{children: []int64{5}, last: "should not be read"}
	c := newTestCore(t, Deps{Phases: &fakePhases{state: "running", endedAt: 0}, Chats: chats})

	out, err := c.Call(context.Background(), ScopeLocal, "collect_results", Params{"parent_chat_id": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.([]Result); len(got) != 0 {
		t.Fatalf("collect_results returned %+v for a still-running child, want none", got)
	}
	if chats.marked != 0 {
		t.Fatalf("MarkCollected called %d times for a still-running child, want 0", chats.marked)
	}
}

// send_to_tab types into a PTY, which a chat sub-agent does not have. Without
// this verb a parent can only start a child and wait — it cannot correct one
// that is heading the wrong way.
func TestChatSendReachesTheUI(t *testing.T) {
	ui := &fakeUI{}
	c := newTestCore(t, Deps{UI: ui})

	if _, err := c.Call(context.Background(), ScopeLocal, "chat_send", Params{"chat_id": float64(9), "text": "stop and summarise"}); err != nil {
		t.Fatal(err)
	}
	if ui.action != "chat_send" {
		t.Fatalf("action = %q", ui.action)
	}
	if ui.args["chatId"] != int64(9) || ui.args["text"] != "stop and summarise" {
		t.Fatalf("args = %v", ui.args)
	}
}
