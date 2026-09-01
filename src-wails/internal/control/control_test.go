package control

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

	first, err := c.collectResults()
	if err != nil {
		t.Fatal(err)
	}
	results := first.([]Result)
	if len(results) != 2 || results[0].Token != "res1" || results[0].Text != "done: res1" {
		t.Fatalf("first collect: %+v", results)
	}

	second, _ := c.collectResults()
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
