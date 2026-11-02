package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"burrow/internal/agentphase"
)

// One-click code review: ReviewChanges creates a chat (a sub-agent of
// parentChatID when given, else a top-level chat of the workspace) on the
// user's default chat agent and starts a review of the workspace's changes.
// Codex runs its native `review/start`; every other agent gets a review
// prompt and the same ChatSend path as any message.

// reviewBaseAuto asks ReviewChanges to detect the repo's base branch.
const reviewBaseAuto = "auto"

// reviewPrompt is the prompt path (Claude, ACP, anything without a native
// review). base == "" reviews the uncommitted changes.
func reviewPrompt(base string) string {
	var scope string
	if base == "" {
		scope = "the uncommitted changes in this workspace. Run `git status --short`, `git diff HEAD` and read any untracked files it lists"
	} else {
		scope = fmt.Sprintf("the changes on this branch relative to `%s`. Run `git diff %s...HEAD` (plus `git status --short` and `git diff HEAD` for uncommitted work)", base, base)
	}
	return "Do a code review of " + scope + ".\n\n" +
		"Do not modify any files. Read the surrounding code when a hunk is not enough to judge it.\n\n" +
		"Report in this order:\n" +
		"1. Correctness bugs: logic errors, broken edge cases, races, resource leaks, wrong error handling, regressions to existing callers.\n" +
		"2. Risky changes: security, data loss, migrations, public API or behaviour changes, missing tests for new logic.\n" +
		"3. Minor notes only if they are worth the reader's time.\n\n" +
		"For every finding cite the location as `path/to/file.ext#L12-L20` (or `#L12` for one line), say what is wrong and why, and suggest the fix. " +
		"Skip style nitpicks. If you find nothing serious, say so plainly instead of padding the review."
}

// reviewTarget maps ReviewChanges' base argument onto Codex's ReviewTarget.
func reviewTarget(base string) map[string]any {
	if base == "" {
		return map[string]any{"type": "uncommittedChanges"}
	}
	return map[string]any{"type": "baseBranch", "branch": base}
}

func reviewLabel(base string) string {
	if base == "" {
		return "Review uncommitted changes"
	}
	return "Review changes against " + base
}

// detectBaseBranch finds the branch a feature branch forks from: origin's
// HEAD, else main / master, else "main".
func detectBaseBranch(dir string) string {
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if ref := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		return ref
	}
	for _, b := range []string{"main", "master"} {
		if git("rev-parse", "--verify", "--quiet", b) != "" {
			return b
		}
	}
	return "main"
}

// ReviewChanges starts a review of the workspace's changes and returns the
// chat it runs in. base: "" = uncommitted changes, "auto" = against the
// detected base branch, anything else = against that branch.
func (a *App) ReviewChanges(workspaceID, parentChatID int64, base string) (map[string]any, error) {
	if a.db == nil {
		return nil, fmt.Errorf("no database")
	}
	if parentChatID != 0 {
		parent, err := a.chatByID(parentChatID)
		if err != nil {
			return nil, err
		}
		workspaceID = parent.WorkspaceID
	}
	var path string
	if err := a.db.QueryRow(`SELECT path FROM workspaces WHERE id = ?`, workspaceID).Scan(&path); err != nil {
		return nil, fmt.Errorf("unknown workspace %d", workspaceID)
	}
	base = strings.TrimSpace(base)
	if base == reviewBaseAuto {
		base = detectBaseBranch(path)
	}

	var cfg spawnConfig
	if raw, err := a.ReadConfig(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	def := cfg.UI.DefaultChatAgent
	if def == "" {
		def = "claude"
	}
	inst := providerByIDOrName(def, cfg.Providers, cfg.Aliases)
	if inst == nil {
		for i := range cfg.Providers {
			if cfg.Providers[i].Enabled {
				inst = &cfg.Providers[i]
				break
			}
		}
	}
	if inst == nil {
		return nil, fmt.Errorf("no agent is configured in Settings > Providers")
	}
	if inst.Transport == "none" && inst.Kind != "claude" {
		return nil, fmt.Errorf("The default agent does not offer an embedded chat.")
	}
	transport := chatTransportOf(*inst)

	c, err := a.CreateChat(Chat{
		WorkspaceID:    workspaceID,
		ParentChatID:   parentChatID,
		Title:          reviewLabel(base),
		AgentKind:      inst.ID,
		Transport:      transport,
		ProfileID:      inst.ID,
		LastActivityAt: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	native := transport == "codex-app-server"
	if native {
		err = a.codexReview(c, base)
	} else {
		err = a.ChatSend(c.ID, reviewPrompt(base), nil)
	}
	if err != nil {
		_ = a.DeleteChat(c.ID) // a row whose process never started is unreachable
		return nil, err
	}
	name := firstNonEmpty(inst.Name, inst.ID)
	if parentChatID != 0 {
		a.publishSubagentRow(parentChatID, c.ID, reviewLabel(base), name)
	}
	return map[string]any{"chat_id": c.ID, "workspace_id": workspaceID, "agent_name": name, "native": native, "base": base}, nil
}

// codexReview ensures the Codex process (like ChatSend) and starts a native
// inline review (`review/start`) on its thread. It is turn/start in every way
// that matters to the bridge: pendingTurn is armed, the ack carries the turn
// id, and the review ends with the usual turn/completed.
func (a *App) codexReview(c Chat, base string) error {
	id := strconv.FormatInt(c.ID, 10)
	defer a.chatRT.lock(id)()
	a.chatRT.touchChat(id, time.Now())
	if err := a.ensureRPC(c, "codex-app-server"); err != nil {
		return fmt.Errorf("start codex-app-server: %w", err)
	}
	a.markChatAwake(c.ID)
	sess, ok := a.acpReg().get(id)
	if !ok || sess.proto != protoCodexAppServer {
		return fmt.Errorf("codex app-server not running")
	}
	rpc := sess.rpcID()
	sess.mu.Lock()
	sess.pendingTurn = rpc
	sess.mu.Unlock()
	a.resetCodexTurnWatchdog(id, sess)
	err := sess.write(map[string]any{"jsonrpc": "2.0", "id": rpc, "method": "review/start",
		"params": map[string]any{"threadId": sess.sessionID, "target": reviewTarget(base), "delivery": "inline"}})
	if err != nil {
		sess.mu.Lock()
		sess.pendingTurn = 0
		if sess.turnWatchdog != nil {
			sess.turnWatchdog.Stop()
			sess.turnWatchdog = nil
		}
		sess.mu.Unlock()
		return fmt.Errorf("start review: %w", err)
	}
	a.applyChatPhase(id, agentphase.Event{Kind: agentphase.HookRunning})
	a.emitChatLine(id, chatUserKind, reviewLabel(base))
	return nil
}
