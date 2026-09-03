package main

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// GitOutput mirrors the Rust struct returned by run_git/run_gh.
type GitOutput struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Code    int    `json:"code"`
	Success bool   `json:"success"`
}

func runCmd(name, cwd string, args []string) GitOutput {
	c := exec.Command(name, args...)
	if cwd != "" {
		c.Dir = cwd
	}
	var stdout, stderr []byte
	stdout, err := c.Output()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = ee.Stderr
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return GitOutput{Stdout: string(stdout), Stderr: string(stderr), Code: code, Success: code == 0}
}

func (a *App) RunGit(cwd string, args []string) GitOutput {
	return runCmd("git", cwd, args)
}

func (a *App) RunGh(cwd string, args []string) GitOutput {
	bin := "gh"
	if resolved := resolveAgentBin("gh", ""); resolved != "" {
		bin = resolved
	}
	return runCmd(bin, cwd, args)
}

// GenerateCommitMessage drafts a commit message from the staged diff via a
// one-shot, non-interactive `claude -p` call — no PTY, no session. Follows
// t3code's approach (apps/server/src/textGeneration/ClaudeTextGeneration.ts):
// prompt goes over stdin (no arg-length limit, no shell-escaping headaches),
// `--output-format json --json-schema` gets a structured {subject, body}
// response back instead of parsing free-form text.
const commitMessageSchema = `{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"}},"required":["subject","body"],"additionalProperties":false}`

func (a *App) GenerateCommitMessage(cwd string, model string) GitOutput {
	diffOut := runCmd("git", cwd, []string{"diff", "--staged"})
	if !diffOut.Success {
		return diffOut
	}
	diff := strings.TrimSpace(diffOut.Stdout)
	if diff == "" {
		return GitOutput{Stderr: "nothing staged", Code: 1}
	}
	const maxDiffChars = 40000
	if len(diff) > maxDiffChars {
		diff = diff[:maxDiffChars] + "\n\n[truncated]"
	}
	statOut := runCmd("git", cwd, []string{"diff", "--staged", "--stat"})

	prompt := strings.Join([]string{
		"You write concise git commit messages.",
		"Return a JSON object with keys: subject, body.",
		"Rules:",
		"- subject must be imperative, <= 72 chars, and no trailing period",
		"- body can be an empty string or short bullet points",
		"- capture the primary user-visible or developer-visible change",
		"",
		"Staged files:",
		strings.TrimSpace(statOut.Stdout),
		"",
		"Staged patch:",
		diff,
	}, "\n")

	bin := "claude"
	if resolved := resolveAgentBin("claude", ""); resolved != "" {
		bin = resolved
	}
	args := []string{"-p", "--output-format", "json", "--json-schema", commitMessageSchema}
	if model != "" {
		args = append(args, "--model", model)
	}
	c := exec.Command(bin, args...)
	c.Dir = cwd
	c.Stdin = strings.NewReader(prompt)
	stdout, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return GitOutput{Stderr: string(ee.Stderr), Code: ee.ExitCode()}
		}
		return GitOutput{Stderr: err.Error(), Code: -1}
	}

	var envelope struct {
		StructuredOutput struct {
			Subject string `json:"subject"`
			Body    string `json:"body"`
		} `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		return GitOutput{Stderr: "claude returned unexpected output format", Code: -1}
	}

	msg := sanitizeCommitSubject(envelope.StructuredOutput.Subject)
	if body := strings.TrimSpace(envelope.StructuredOutput.Body); body != "" {
		msg += "\n\n" + body
	}
	return GitOutput{Stdout: msg, Success: true}
}

// sanitizeCommitSubject mirrors t3code's TextGenerationUtils.sanitizeCommitSubject.
func sanitizeCommitSubject(raw string) string {
	line := strings.TrimSpace(raw)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(line), "."))
	if line == "" {
		return "Update project files"
	}
	if len(line) > 72 {
		line = strings.TrimSpace(line[:72])
	}
	return line
}

// --- worktrees ---

func (a *App) CreateWorktree(repoPath, worktreeName, path, branch, baseRef string) (Workspace, error) {
	var parentID int64
	if err := a.db.QueryRow(`SELECT id FROM workspaces WHERE path = ?`, repoPath).Scan(&parentID); err != nil {
		return Workspace{}, err
	}
	if baseRef == "" {
		baseRef = "HEAD"
	}
	args := []string{"worktree", "add", path, "-b", branch, baseRef}
	if out := runCmd("git", repoPath, args); !out.Success {
		// Branch may already exist — retry without -b.
		args = []string{"worktree", "add", path, branch}
		if out2 := runCmd("git", repoPath, args); !out2.Success {
			return Workspace{}, gitErr(out2)
		}
	}
	ws, err := a.CreateWorkspace(worktreeName, path)
	if err != nil {
		return ws, err
	}
	_, err = a.db.Exec(`UPDATE workspaces SET parent_id = ?, worktree_branch = ?, is_git = 1 WHERE id = ?`, parentID, branch, ws.ID)
	ws.ParentID = &parentID
	ws.WorktreeBranch = &branch
	ws.IsGit = true
	return ws, err
}

func (a *App) RemoveWorktree(id int64, force bool) error {
	var path, parentPath string
	err := a.db.QueryRow(`SELECT w.path, p.path FROM workspaces w LEFT JOIN workspaces p ON p.id = w.parent_id WHERE w.id = ?`, id).Scan(&path, &parentPath)
	if err != nil {
		return err
	}
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	if out := runCmd("git", parentPath, args); !out.Success {
		return gitErr(out)
	}
	return a.DeleteWorkspace(id)
}

func gitErr(out GitOutput) error {
	if out.Stderr == "" {
		return errors.New("git command failed")
	}
	return errors.New(out.Stderr)
}

// --- chat titles ---

const chatTitleSchema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`

// GenerateChatTitle names a chat thread from its first user message, the same
// headless-claude trick GenerateCommitMessage uses (t3code's
// TextGenerationModel). Best-effort: any failure returns "" and the caller
// keeps the heuristic title it already showed.
func (a *App) GenerateChatTitle(cwd string, model string, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	const maxPromptChars = 4000
	if len(text) > maxPromptChars {
		text = text[:maxPromptChars] + "\n\n[truncated]"
	}

	prompt := strings.Join([]string{
		"You name chat threads.",
		"Return a JSON object with key: title.",
		"Rules:",
		"- at most 5 words, no quotes, no trailing period",
		"- name the task, not the conversation (\"Fix PTY status dots\", not \"User asks about dots\")",
		"- Title Case is not required; sentence case is fine",
		"",
		"First message:",
		text,
	}, "\n")

	bin := "claude"
	if resolved := resolveAgentBin("claude", cwd); resolved != "" {
		bin = resolved
	}
	args := []string{"-p", "--output-format", "json", "--json-schema", chatTitleSchema}
	if model != "" {
		args = append(args, "--model", model)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin, args...)
	c.Dir = cwd
	c.Stdin = strings.NewReader(prompt)
	stdout, err := c.Output()
	if err != nil {
		return ""
	}

	var envelope struct {
		StructuredOutput struct {
			Title string `json:"title"`
		} `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		return ""
	}
	return sanitizeChatTitle(envelope.StructuredOutput.Title)
}

// sanitizeChatTitle trims the model's answer to one short line. Empty in, empty
// out — an empty title must never overwrite the heuristic one.
func sanitizeChatTitle(raw string) string {
	line := strings.TrimSpace(raw)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), `"'`))
	line = strings.TrimRight(line, ".")
	if len(line) > 60 {
		line = strings.TrimSpace(line[:60])
	}
	return line
}
