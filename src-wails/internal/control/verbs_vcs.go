package control

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Repo verbs: worktrees, read-only repo inspection, and pull requests.
//
// Everything here runs a binary the user already has (git, gh) in a working
// directory. Nothing writes to the working tree — a Manager reads the repo to
// decide what to delegate; the delegated agents are the ones who edit code.

// CmdResult is what a client gets back from a git/gh/run verb. The exit code is
// reported rather than swallowed: "no changes" and "not a repo" are both
// non-zero, and only the caller can tell which one matters.
type CmdResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr,omitempty"`
	Code   int    `json:"code"`
}

// Worktrees is the app's existing worktree plumbing (git worktree add/remove
// plus the workspace row that makes it show up in the Sidebar), injected so the
// package doesn't reimplement it.
type Worktrees interface {
	Create(repoPath, name, path, branch, baseRef string) (id int64, err error)
	Remove(workspaceID int64, force bool) error
}

func vcsVerbs(c *Core) []Verb {
	return []Verb{{
		Name:    "create_worktree",
		Summary: "Create a git worktree of this repo and open it as a workspace",
		Args: []Arg{
			{Name: "branch", Type: "string", Desc: "Branch to check out; created off base if new", Required: true},
			{Name: "base", Type: "string", Desc: "Base ref for a new branch (default HEAD)"},
			{Name: "path", Type: "string", Desc: "Override the default <worktreesDir>/<repo>/<branch> location"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.createWorktree(ctx, p) },
	}, {
		Name:    "worktree_remove",
		Summary: "Delete a worktree of this repo. Destructive — confirm with the user first",
		Args: []Arg{
			{Name: "branch", Type: "string", Desc: "Worktree branch to remove (or pass path)"},
			{Name: "path", Type: "string", Desc: "Worktree path to remove (or pass branch)"},
			{Name: "force", Type: "boolean", Desc: "Discard uncommitted changes in the worktree"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.removeWorktree(ctx, p) },
	}, {
		Name:    "git_status",
		Summary: "Porcelain status of the repo (short format, with branch header)",
		Args:    []Arg{{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"}},
		Scope:   ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.git(p, "status", "--short", "--branch"), nil
		},
	}, {
		Name:    "git_log",
		Summary: "Recent commits, one line each",
		Args: []Arg{
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
			{Name: "limit", Type: "integer", Desc: "How many commits (default 20)"},
			{Name: "rev", Type: "string", Desc: "Revision range, e.g. main..HEAD"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			limit := p.Int("limit")
			if limit <= 0 {
				limit = 20
			}
			args := []string{"log", "--oneline", "--no-decorate", fmt.Sprintf("-n%d", limit)}
			if rev := p.Str("rev"); rev != "" {
				args = append(args, rev)
			}
			return c.git(p, args...), nil
		},
	}, {
		Name:    "git_diff",
		Summary: "Diff of the working tree, or of a revision range",
		Args: []Arg{
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
			{Name: "rev", Type: "string", Desc: "Revision range, e.g. main..HEAD"},
			{Name: "stat", Type: "boolean", Desc: "Summarise as --stat instead of full patch"},
			{Name: "path", Type: "string", Desc: "Limit to a path"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			args := []string{"diff"}
			if p.Bool("stat") {
				args = append(args, "--stat")
			}
			if rev := p.Str("rev"); rev != "" {
				args = append(args, rev)
			}
			if path := p.Str("path"); path != "" {
				args = append(args, "--", path)
			}
			return c.git(p, args...), nil
		},
	}, {
		Name:    "run",
		Summary: "Run a read-only shell command (ls, cat, grep, rg, find, head, tail, wc, jq, tree)",
		Args: []Arg{
			{Name: "cmd", Type: "string", Desc: "Command line; only read-only programs are allowed", Required: true},
			{Name: "cwd", Type: "string", Desc: "Directory to run in; defaults to the caller's"},
		},
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.run(p) },
	}, {
		Name:    "pr_create",
		Summary: "Open a pull request with the gh CLI",
		Args: []Arg{
			{Name: "title", Type: "string", Desc: "PR title", Required: true},
			{Name: "body", Type: "string", Desc: "PR body", Required: true},
			{Name: "base", Type: "string", Desc: "Base branch (default main)"},
			{Name: "head", Type: "string", Desc: "Head branch (default: the branch in cwd)"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			base := p.Str("base")
			if base == "" {
				base = "main"
			}
			args := []string{"pr", "create", "--title", p.Str("title"), "--body", p.Str("body"), "--base", base}
			if head := p.Str("head"); head != "" {
				args = append(args, "--head", head)
			}
			return c.gh(p, args...), nil
		},
	}, {
		Name:    "pr_list",
		Summary: "List pull requests",
		Args: []Arg{
			{Name: "state", Type: "string", Desc: "open | closed | merged | all (default open)"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			state := p.Str("state")
			if state == "" {
				state = "open"
			}
			return c.gh(p, "pr", "list", "--state", state), nil
		},
	}, {
		Name:    "pr_view",
		Summary: "Show a pull request, with its checks and review state",
		Args: []Arg{
			{Name: "number", Type: "integer", Desc: "PR number", Required: true},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal | ScopeRemote,
		Fn: func(ctx context.Context, p Params) (any, error) {
			return c.gh(p, "pr", "view", p.Str("number")), nil
		},
	}, {
		Name:    "pr_merge",
		Summary: "Merge a pull request. Destructive — confirm with the user first",
		Args: []Arg{
			{Name: "number", Type: "integer", Desc: "PR number", Required: true},
			{Name: "squash", Type: "boolean", Desc: "Squash-merge instead of a merge commit"},
			{Name: "cwd", Type: "string", Desc: "Repo or worktree dir; defaults to the caller's"},
		},
		Scope: ScopeLocal,
		Fn: func(ctx context.Context, p Params) (any, error) {
			args := []string{"pr", "merge", p.Str("number")}
			if p.Bool("squash") {
				args = append(args, "--squash")
			}
			return c.gh(p, args...), nil
		},
	}}
}

func (c *Core) git(p Params, args ...string) CmdResult {
	stdout, stderr, code := c.deps.Git.Run(p.Str("cwd"), args)
	return CmdResult{Stdout: stdout, Stderr: stderr, Code: code}
}

func (c *Core) gh(p Params, args ...string) CmdResult {
	stdout, stderr, code := c.deps.Gh.Run(p.Str("cwd"), args)
	return CmdResult{Stdout: stdout, Stderr: stderr, Code: code}
}

// readOnlyPrograms is what `run` will execute. An allow-list, not a deny-list:
// the Manager is an orchestrator, so a command that could write is a bug in the
// Manager's reasoning, not something to sanitise after the fact. `git` is absent
// on purpose — the git_* verbs cover reads and can't be talked into `git push`.
var readOnlyPrograms = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true, "grep": true,
	"rg": true, "find": true, "fd": true, "tree": true, "jq": true, "file": true,
	"stat": true, "basename": true, "dirname": true, "echo": true, "which": true,
}

func (c *Core) run(p Params) (any, error) {
	argv, err := splitArgs(p.Str("cmd"))
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("run: empty command")
	}
	prog := filepath.Base(argv[0])
	if !readOnlyPrograms[prog] {
		return nil, fmt.Errorf("run: %q is not allowed — run is read-only (%s). Delegate work that changes files to a spawned agent", prog, allowedList())
	}
	// No shell: a pipeline or redirect would escape the allow-list, and the one
	// legitimate use (piping into grep) is better served by rg in one call.
	stdout, stderr, code := c.deps.Exec.RunProgram(prog, p.Str("cwd"), argv[1:])
	return CmdResult{Stdout: stdout, Stderr: stderr, Code: code}, nil
}

func allowedList() string {
	names := make([]string, 0, len(readOnlyPrograms))
	for n := range readOnlyPrograms {
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

// splitArgs is a minimal POSIX-ish argument splitter: whitespace separates,
// single and double quotes group. Enough for the read-only commands `run`
// accepts, and it rejects the shell metacharacters that would need a real shell
// to mean anything — better an error than silently running half a pipeline.
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	started := false

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n':
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		case r == '|' || r == '>' || r == '<' || r == ';' || r == '&' || r == '`' || r == '$':
			return nil, fmt.Errorf("run: %q needs a shell, which run does not use — issue one command, or delegate to an agent", string(r))
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("run: unbalanced %s quote", string(quote))
	}
	if started {
		args = append(args, cur.String())
	}
	return args, nil
}
