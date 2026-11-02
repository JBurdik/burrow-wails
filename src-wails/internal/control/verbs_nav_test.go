package control

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, tc := range []struct {
		path, def, want, err string
	}{
		{path: "/tmp/x/", want: "/tmp/x"},
		{path: "~/code/app", want: filepath.Join(home, "code/app")},
		{path: "app", def: "~/code", want: filepath.Join(home, "code/app")},
		{path: "app", def: "/srv/p/", want: "/srv/p/app"},
		{path: "app", want: filepath.Join(home, "app")},
		{path: " app ", def: "  ", want: filepath.Join(home, "app")},
		{path: "", err: "needs a path"},
		{path: "..", err: "needs a path"},
		{path: "a/b", err: "absolute path or a bare"},
		{path: "../app", err: "absolute path or a bare"},
	} {
		got, err := projectDir(tc.path, tc.def)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("projectDir(%q, %q) err = %v, want %q", tc.path, tc.def, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("projectDir(%q, %q) = %q, %v; want %q", tc.path, tc.def, got, err, tc.want)
		}
	}
}

type recordGit struct{ calls [][]string }

func (g *recordGit) Run(cwd string, args []string) (string, string, int) {
	g.calls = append(g.calls, append([]string{cwd}, args...))
	return "", "", 0
}

func TestWorkspaceCreateMakesProject(t *testing.T) {
	root := t.TempDir()
	ui, git := &fakeUI{result: map[string]any{"workspace_id": 7}}, &recordGit{}
	c := newTestCore(t, Deps{UI: ui, Git: git, DefaultProjectDir: func() string { return root }})
	call := func(p Params) error {
		_, err := c.Call(context.Background(), ScopeLocal, "workspace_create", p)
		return err
	}

	if err := call(Params{"path": "app", "create": true, "git_init": true}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "app")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	if ui.action != "workspace_create" || ui.args["path"] != dir {
		t.Fatalf("ui got %q %v", ui.action, ui.args)
	}
	if len(git.calls) != 1 || git.calls[0][0] != dir || git.calls[0][1] != "init" {
		t.Fatalf("git calls %v", git.calls)
	}

	// Non-empty dir: create refuses, plain add works.
	os.WriteFile(filepath.Join(dir, "main.go"), nil, 0o644)
	if err := call(Params{"path": dir, "create": true}); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("create over non-empty dir: %v", err)
	}
	if err := call(Params{"path": dir}); err != nil {
		t.Fatal(err)
	}
	// Missing dir without create is refused, not created.
	if err := call(Params{"path": "nope"}); err == nil {
		t.Fatal("missing dir without create was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "nope")); err == nil {
		t.Fatal("dir created without create")
	}
	// .git present: git_init is a no-op.
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	if err := call(Params{"path": dir, "git_init": true}); err != nil || len(git.calls) != 1 {
		t.Fatalf("git_init on a repo: %v, calls %v", err, git.calls)
	}
}

// workspace_id picks the workspace a detached chat lands in, over the caller's cwd.
func TestSpawnWorkspaceIDOverridesCwd(t *testing.T) {
	ui := &fakeUI{result: map[string]any{"chat_id": 5}}
	c := newTestCore(t, Deps{DB: openTestDB(t), UI: ui})
	_, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{
		"task": "scaffold it", "target": "chat", "detach": true, "parent_chat_id": "3",
		"cwd": "/tmp/repo", "workspace_id": "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ui.args["cwd"] != "/tmp/wt/feat-x" || ui.args["target"] != "chat" || ui.args["parent_chat_id"] != int64(0) {
		t.Fatalf("ui args %v", ui.args)
	}
	if _, err := c.Call(context.Background(), ScopeLocal, "spawn", Params{"task": "x", "workspace_id": "99"}); err == nil {
		t.Fatal("unknown workspace accepted")
	}
}
