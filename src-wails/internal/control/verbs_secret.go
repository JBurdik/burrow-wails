package control

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

// Secrets parks a secret request for the user and returns the path of a 0600
// file holding the answer — never the value itself. owner is "chat:<id>" or
// "pty:<id>". It blocks until answered, declined, the owner's turn ends or the
// host's timeout; a decline is an error the agent sees.
type Secrets interface {
	RequestSecret(ctx context.Context, owner, name, reason string) (string, error)
}

var secretNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func secretVerbs(c *Core) []Verb {
	return []Verb{{
		Name: "secret_request",
		Summary: "Ask the user for a secret (API key, token, password) instead of having them paste it into chat. Blocks until they answer; " +
			"returns {path, name}: read it with VAR=$(cat <path>), never print the value. The file is deleted when this turn ends",
		Args: []Arg{
			{Name: "name", Type: "string", Desc: "Env-var-like label shown to the user, e.g. STRIPE_API_KEY ([A-Za-z0-9_.-], ≤ 64)", Required: true},
			{Name: "reason", Type: "string", Desc: "One short sentence: what the secret is for", Required: true},
		},
		// Local only: the asking side holds tool permissions; a paired device
		// answers through answer_secret_request, never asks.
		Scope: ScopeLocal,
		Fn:    func(ctx context.Context, p Params) (any, error) { return c.secretRequest(ctx, p) },
	}}
}

// secretOwner is the calling chat (parent_chat_id, from BURROW_CHAT_ID), else
// the calling tab (caller_pty_id, from BURROW_PTY_ID).
func secretOwner(p Params) (string, error) {
	if id := p.Int("parent_chat_id"); id > 0 {
		return "chat:" + strconv.FormatInt(id, 10), nil
	}
	if id := p.Int("caller_pty_id"); id > 0 {
		return "pty:" + strconv.FormatInt(id, 10), nil
	}
	return "", fmt.Errorf("secret_request: run it from a Burrow chat or terminal tab (no BURROW_CHAT_ID / BURROW_PTY_ID)")
}

func (c *Core) secretRequest(ctx context.Context, p Params) (any, error) {
	if c.deps.Secrets == nil {
		return nil, fmt.Errorf("secret_request: not available")
	}
	name := p.Str("name")
	if !secretNameRe.MatchString(name) {
		return nil, fmt.Errorf("secret_request: name must match [A-Za-z0-9_.-]{1,64}")
	}
	owner, err := secretOwner(p)
	if err != nil {
		return nil, err
	}
	path, err := c.deps.Secrets.RequestSecret(ctx, owner, name, p.Str("reason"))
	if err != nil {
		return nil, fmt.Errorf("secret_request: %w", err)
	}
	return map[string]string{"path": path, "name": name}, nil
}
