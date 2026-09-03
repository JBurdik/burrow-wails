package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
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
// one-shot, non-interactive provider call — no PTY, no session. Follows
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

	jsonOutput, err := generateTextJSON(cwd, model, prompt, commitMessageSchema)
	if err != nil {
		return GitOutput{Stderr: err.Error(), Code: -1}
	}
	var result struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return GitOutput{Stderr: "text generation provider returned unexpected JSON", Code: -1}
	}

	msg := sanitizeCommitSubject(result.Subject)
	if body := strings.TrimSpace(result.Body); body != "" {
		msg += "\n\n" + body
	}
	return GitOutput{Stdout: msg, Success: true}
}

// generateTextJSON runs a provider selected by the persisted "kind::model"
// preference. Older bare model ids remain Claude selections for compatibility.
// Both CLIs are asked for the same JSON contract, leaving callers provider-free.
func generateTextJSON(cwd, selection, prompt, schema string) ([]byte, error) {
	return generateTextJSONContext(context.Background(), cwd, selection, prompt, schema)
}

func generateTextJSONContext(ctx context.Context, cwd, selection, prompt, schema string) ([]byte, error) {
	provider, model := parseTextGenerationSelection(selection)
	switch provider {
	case "claude":
		bin := "claude"
		if resolved := resolveAgentBin("claude", cwd); resolved != "" {
			bin = resolved
		}
		args := []string{"-p", "--output-format", "json", "--json-schema", schema}
		if model != "" {
			args = append(args, "--model", model)
		}
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir = cwd
		c.Stdin = strings.NewReader(prompt)
		out, err := c.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return nil, errors.New(strings.TrimSpace(string(ee.Stderr)))
			}
			return nil, err
		}
		var envelope struct {
			StructuredOutput json.RawMessage `json:"structured_output"`
		}
		if err := json.Unmarshal(out, &envelope); err != nil || len(envelope.StructuredOutput) == 0 {
			return nil, errors.New("claude returned unexpected output format")
		}
		return envelope.StructuredOutput, nil
	case "codex":
		bin := "codex"
		if resolved := resolveAgentBin("codex", cwd); resolved != "" {
			bin = resolved
		}
		lastMessage, err := os.CreateTemp("", "burrow-codex-text-*.json")
		if err != nil {
			return nil, err
		}
		path := lastMessage.Name()
		lastMessage.Close()
		defer os.Remove(path)
		args := []string{"exec", "--skip-git-repo-check", "--output-last-message", path}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir = cwd
		if out, err := c.CombinedOutput(); err != nil {
			return nil, errors.New(strings.TrimSpace(string(out)))
		}
		out, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return out, nil
	case "gemini":
		bin := "gemini"
		if resolved := resolveAgentBin("gemini", cwd); resolved != "" {
			bin = resolved
		}
		args := []string{"--prompt", prompt, "--output-format", "json"}
		if model != "" {
			args = append(args, "--model", model)
		}
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir = cwd
		out, err := c.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return nil, errors.New(strings.TrimSpace(string(ee.Stderr)))
			}
			return nil, err
		}
		return extractGeneratedJSON(out)
	case "opencode":
		bin := "opencode"
		if resolved := resolveAgentBin("opencode", cwd); resolved != "" {
			bin = resolved
		}
		args := []string{"run"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir = cwd
		out, err := c.CombinedOutput()
		if err != nil {
			return nil, errors.New(strings.TrimSpace(string(out)))
		}
		return extractGeneratedJSON(out)
	default:
		return nil, errors.New("selected provider does not support background text generation")
	}
}

// extractGeneratedJSON accepts the direct JSON requested in the prompt and
// Gemini's JSON envelope, whose response field contains the final text.
func extractGeneratedJSON(out []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(out)
	if json.Valid(trimmed) {
		var envelope struct {
			Response string `json:"response"`
		}
		if json.Unmarshal(trimmed, &envelope) == nil && envelope.Response != "" {
			trimmed = []byte(envelope.Response)
		} else {
			return trimmed, nil
		}
	}
	start, end := bytes.IndexByte(trimmed, '{'), bytes.LastIndexByte(trimmed, '}')
	if start >= 0 && end > start {
		candidate := trimmed[start : end+1]
		if json.Valid(candidate) {
			return candidate, nil
		}
	}
	return nil, errors.New("provider did not return the requested JSON object")
}

func parseTextGenerationSelection(selection string) (provider, model string) {
	parts := strings.Split(selection, "::")
	if len(parts) >= 3 {
		return parts[1], strings.Join(parts[2:], "::")
	}
	if len(parts) == 2 && (parts[0] == "claude" || parts[0] == "codex") {
		return parts[0], parts[1]
	}
	return "claude", selection
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jsonOutput, err := generateTextJSONContext(ctx, cwd, model, prompt, chatTitleSchema)
	if err != nil {
		return ""
	}

	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return ""
	}
	return sanitizeChatTitle(result.Title)
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
