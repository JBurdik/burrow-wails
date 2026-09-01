// Package control is Burrow's app-control surface: one implementation of every
// verb an agent (or a remote client) can perform on the running app.
//
// The package deliberately knows nothing about HTTP, MCP, Wails or SQL schema
// beyond the queries it makes — it takes its capabilities as interfaces (Deps)
// and exposes a registry of verbs. Transports sit on top:
//
//	loopback HTTP  → the `burrow` CLI (curl) and the burrow-mcp server
//	tailnet HTTP   → remote clients (mobile), limited to ScopeRemote verbs
//	Wails bindings → the desktop UI itself
//
// One verb, one implementation, three doors. The registry is also the single
// source of truth for the MCP tool list, the CLI's help text and the Manager's
// primer, so those cannot drift from what the app actually supports.
package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Scope controls which transports may invoke a verb. A verb is local-only
// unless it says otherwise: a new verb that forgets to think about the remote
// case stays off the network.
type Scope uint8

const (
	// ScopeLocal is reachable from the loopback transport (CLI, MCP).
	ScopeLocal Scope = 1 << iota
	// ScopeRemote is additionally reachable from the tailnet transport.
	ScopeRemote
)

func (s Scope) Has(want Scope) bool { return s&want != 0 }

// Arg describes one verb parameter. It drives the generated MCP JSON schema,
// the CLI's `--help`, and argument validation, so a verb documents itself once.
type Arg struct {
	Name     string
	Type     string // "string" | "integer" | "boolean"
	Desc     string
	Required bool
}

// Verb is a single callable app action.
type Verb struct {
	Name    string
	Summary string
	Args    []Arg
	Scope   Scope
	Fn      func(context.Context, Params) (any, error)
}

// Params is a verb's decoded arguments. Verbs read through the typed getters so
// a client that sends "42" for an integer (every shell client does) still works.
type Params map[string]any

func (p Params) Str(name string) string {
	switch v := p[name].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%d", int64(v))
	case bool:
		return fmt.Sprintf("%t", v)
	default:
		return ""
	}
}

func (p Params) Int(name string) int64 {
	switch v := p[name].(type) {
	case float64:
		return int64(v)
	case string:
		var n int64
		_, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func (p Params) Bool(name string) bool {
	switch v := p[name].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// CmdRunner runs an external command in a working directory. Implemented by
// the app over git/gh; injected so the package needs no exec of its own and
// tests can assert on the argv instead of touching a real repo.
type CmdRunner interface {
	Run(cwd string, args []string) (stdout, stderr string, code int)
}

// Exec runs an arbitrary allow-listed program — the read-only `run` verb. Kept
// separate from CmdRunner so git/gh stay bound to their own binary and can't be
// redirected by a verb argument.
type Exec interface {
	RunProgram(prog, cwd string, args []string) (stdout, stderr string, code int)
}

// PTYWriter feeds keystrokes to a live terminal tab — how a follow-up message
// reaches an already-running agent.
type PTYWriter interface {
	WritePty(ptyID string, text string) error
}

// UIBridge performs an action only the frontend can perform (open a tab, focus
// a workspace, read a terminal's scrollback) and returns its result. The
// implementation is expected to be request/response with a timeout: a verb that
// cannot reach the UI must fail loudly rather than hang an agent forever.
type UIBridge interface {
	Do(ctx context.Context, action string, args map[string]any) (json.RawMessage, error)
}

// Deps is everything the verbs need from the host app.
type Deps struct {
	DB         *sql.DB
	SessionDir string
	Git        CmdRunner
	Gh         CmdRunner
	Exec       Exec
	PTY        PTYWriter
	Worktrees  Worktrees
	UI         UIBridge
	// WorktreesDir is where a worktree lands when the caller doesn't say:
	// <dir>/<repo>/<branch>, the same convention as the New-worktree dialog.
	WorktreesDir func() string
}

// Core is the verb registry plus the dependencies verbs run against.
type Core struct {
	deps  Deps
	verbs map[string]Verb
}

func New(deps Deps) *Core {
	c := &Core{deps: deps, verbs: map[string]Verb{}}
	c.register(delegationVerbs(c)...)
	c.register(navigationVerbs(c)...)
	c.register(vcsVerbs(c)...)
	return c
}

func (c *Core) register(verbs ...Verb) {
	for _, v := range verbs {
		if _, dup := c.verbs[v.Name]; dup {
			panic("control: duplicate verb " + v.Name) // programmer error, caught by the registry test
		}
		c.verbs[v.Name] = v
	}
}

// Verbs lists every registered verb, sorted, for help/schema generation.
func (c *Core) Verbs() []Verb {
	out := make([]Verb, 0, len(c.verbs))
	for _, v := range c.verbs {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ErrUnknownVerb distinguishes "no such verb" from a verb that failed, so a
// transport can answer 404 vs 500 and the CLI can suggest `burrow help`.
type ErrUnknownVerb struct{ Name string }

func (e ErrUnknownVerb) Error() string { return "unknown verb: " + e.Name }

// ErrForbidden is returned when a verb exists but not for this transport.
type ErrForbidden struct{ Name string }

func (e ErrForbidden) Error() string { return "verb not available on this transport: " + e.Name }

// Call runs a verb after checking it is allowed for the calling transport and
// that every required argument is present.
func (c *Core) Call(ctx context.Context, scope Scope, name string, params Params) (any, error) {
	v, ok := c.verbs[name]
	if !ok {
		return nil, ErrUnknownVerb{Name: name}
	}
	if !v.Scope.Has(scope) {
		return nil, ErrForbidden{Name: name}
	}
	if params == nil {
		params = Params{}
	}
	for _, a := range v.Args {
		if a.Required && params.Str(a.Name) == "" {
			return nil, fmt.Errorf("%s: missing required argument %q", name, a.Name)
		}
	}
	return v.Fn(ctx, params)
}

// ui is a small helper so verbs read as one line: hand the action to the
// frontend, decode whatever it acked with.
func (c *Core) ui(ctx context.Context, action string, args map[string]any, out any) error {
	if c.deps.UI == nil {
		return fmt.Errorf("%s: no UI attached", action)
	}
	raw, err := c.deps.UI.Do(ctx, action, args)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}
