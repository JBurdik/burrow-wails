package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"burrow/internal/control"
)

// Moving a chat to another workspace (`burrow move_to_worktree` /
// `leave_worktree`). A chat's cwd IS its workspace's path (chatCwd), so a move
// is a workspace_id update plus a restart of the live process — on the next
// send, never mid-turn: the agent asking to move is itself in a turn, and
// killing it would drop the very call that asked.
//
// Session continuity:
//   - Claude keeps a session at <config>/projects/<cwd-slug>/<id>.jsonl. claude
//     2.1.311 resumes an id from any cwd and keeps appending to the original
//     file, but older CLIs (and a worktree whose .burrow/config.toml resolves a
//     different CLAUDE_CONFIG_DIR) look only under the new cwd's slug. So the
//     move symlinks the session file (and its <id>/ sidecar dir) into the new
//     project dir: one file, whichever lookup the CLI does — verified that a
//     resume through the link appends to the original and leaves the link.
//   - Codex threads are not cwd-keyed; thread/resume takes the new cwd.
//   - Generic ACP: session/load semantics across cwds are the adapter's own, so
//     the move is refused.

type chatMoverAdapter struct{ a *App }

func (m chatMoverAdapter) ChatCwd(chatID int64) (string, error) {
	c, err := m.a.chatByID(chatID)
	if err != nil {
		return "", err
	}
	path := m.a.chatCwd(c)
	if path == "" {
		return "", fmt.Errorf("chat %d has no workspace", chatID)
	}
	return path, nil
}

func (m chatMoverAdapter) MoveChat(chatID, workspaceID int64) error {
	return m.a.moveChat(chatID, workspaceID)
}

var _ control.ChatMover = chatMoverAdapter{}

func (a *App) moveChat(chatID, workspaceID int64) error {
	c, err := a.chatByID(chatID)
	if err != nil {
		return err
	}
	if chatTransport(c) == "acp" {
		return fmt.Errorf("a %s chat can't move: its ACP session isn't portable across directories (Claude and Codex chats can)", firstNonEmpty(c.AgentKind, "generic ACP"))
	}
	var path string
	var wtBranch sql.NullString
	if err := a.db.QueryRow(`SELECT path, worktree_branch FROM workspaces WHERE id = ?`, workspaceID).Scan(&path, &wtBranch); err != nil {
		return fmt.Errorf("workspace %d: %w", workspaceID, err)
	}
	oldCwd := a.chatCwd(c)
	id := strconv.FormatInt(chatID, 10)
	defer a.chatRT.lock(id)()

	// Destination tree's lock (taken inside the chat lock, the order every
	// caller uses): no move into a worktree mid-create/remove.
	withWorkspaceLock(path, func() {
		if chatIsClaude(c) && c.ClaudeSessionID != "" {
			src := a.claudeLaunchFor(c, oldCwd).ConfigDir
			dst := a.claudeLaunchFor(c, path).ConfigDir
			if err = linkClaudeSession(src, oldCwd, dst, path, c.ClaudeSessionID); err != nil {
				err = fmt.Errorf("carry the Claude session over: %w", err)
				return
			}
		}
		branch := wtBranch.String
		if !wtBranch.Valid || branch == "" {
			if out := runCmd("git", path, []string{"rev-parse", "--abbrev-ref", "HEAD"}); out.Success {
				branch = strings.TrimSpace(out.Stdout)
			}
		}
		_, err = a.db.Exec(`UPDATE chats SET workspace_id = ?, branch = ? WHERE id = ?`, workspaceID, branch, chatID)
	})
	if err != nil {
		return err
	}
	a.chatRT.moved.Store(id, struct{}{})
	busEmit("chats-changed", nil)
	return nil
}

// restartIfMoved stops a live process still running in the pre-move cwd, so
// the ensure that called it starts one in the new cwd. Only once the turn has
// settled: a send into a running turn is a steer and belongs to the old
// process. Called with the chat lock.
func (a *App) restartIfMoved(id string) {
	if _, ok := a.chatRT.moved.Load(id); !ok {
		return
	}
	_, rpcLive := a.acpReg().get(id)
	if !a.claudeMgr().Alive(id) && !rpcLive {
		a.chatRT.moved.Delete(id)
		return
	}
	if !a.chatSettled(id) {
		return
	}
	a.chatRT.moved.Delete(id)
	a.stopAndReset(id, "reset")
}

// claudeConfigRoot resolves a launch's ConfigDir the way the CLI will see it:
// the explicit dir, else Burrow's CLAUDE_CONFIG_DIR, else ~/.claude.
func claudeConfigRoot(dir string) string {
	if dir = strings.TrimSpace(dir); dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".claude")
	}
	return expandHome(dir)
}

// claudeProjectSlug is Claude Code's cwd → projects/<dir> encoding: every
// non-alphanumeric byte becomes '-'.
func claudeProjectSlug(cwd string) string {
	b := []byte(cwd)
	for i, ch := range b {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// claudeSlugMax: past this Claude shortens the dir name with a hash we don't
// reproduce; such a move relies on the CLI's own by-id lookup.
const claudeSlugMax = 200

// linkClaudeSession makes session sid resumable from dstCwd: a symlink to the
// real file (and its <sid>/ sidecar dir) under dst's project dir. No-op when
// the target already has it or there is nothing on disk to carry.
func linkClaudeSession(srcCfg, srcCwd, dstCfg, dstCwd, sid string) error {
	if sid == "" || strings.ContainsAny(sid, `/\*?[`) {
		return nil
	}
	slug := claudeProjectSlug(dstCwd)
	if len(slug) > claudeSlugMax {
		return nil
	}
	dstDir := filepath.Join(claudeConfigRoot(dstCfg), "projects", slug)
	dst := filepath.Join(dstDir, sid+".jsonl")
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	srcRoot := filepath.Join(claudeConfigRoot(srcCfg), "projects")
	src := filepath.Join(srcRoot, claudeProjectSlug(srcCwd), sid+".jsonl")
	if _, err := os.Stat(src); err != nil {
		m, _ := filepath.Glob(filepath.Join(srcRoot, "*", sid+".jsonl"))
		if len(m) == 0 {
			return nil
		}
		src = m[0]
	}
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return err
	}
	if err := os.Symlink(real, dst); err != nil {
		return err
	}
	side := strings.TrimSuffix(real, ".jsonl")
	if st, err := os.Stat(side); err == nil && st.IsDir() {
		if _, err := os.Lstat(filepath.Join(dstDir, sid)); os.IsNotExist(err) {
			_ = os.Symlink(side, filepath.Join(dstDir, sid))
		}
	}
	return nil
}
