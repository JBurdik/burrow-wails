package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Background text generation: the small writing jobs the app does for the user
// (commit messages, PR content, branch names, chat titles) via a one-shot,
// non-interactive provider call — no PTY, no session.
//
// Ported from t3code's apps/server/src/textGeneration/*: the prompts, the
// per-section truncation, the sanitizers and the CLI contracts are theirs, so a
// model that writes well for them writes well here. What differs is the
// plumbing — they resolve a provider instance through an Effect registry, we
// switch on the persisted selection string.

// Every generation shares one budget. 180 s is t3code's CLAUDE_TIMEOUT_MS /
// CODEX_TIMEOUT_MS: enough for a reasoning model on a cold start, and the
// callers all treat a timeout as "keep what you had".
const textGenTimeout = 180 * time.Second

// Reasoning effort for a text-gen call that doesn't pin one. These are cheap
// jobs, so Codex gets t3code's CODEX_GIT_TEXT_GENERATION_REASONING_EFFORT
// rather than whatever the user's config.toml defaults to.
const defaultCodexEffort = "low"

// --- selection ------------------------------------------------------------

// textGenSelection is the persisted "kind::provider::model::effort" preference
// pulled apart. Effort is optional and the earlier formats stay readable.
type textGenSelection struct {
	provider string
	model    string
	effort   string
}

// parseTextGenerationSelection accepts every format the preference has ever
// held: a bare Claude model id, "kind::model", "kind::provider::model" and the
// current "kind::provider::model::effort".
func parseTextGenerationSelection(selection string) textGenSelection {
	parts := strings.Split(selection, "::")
	switch {
	case len(parts) >= 4:
		// The effort is the last field; anything between provider and it is the
		// model, so a model id containing "::" still round-trips.
		return textGenSelection{
			provider: parts[1],
			model:    strings.Join(parts[2:len(parts)-1], "::"),
			effort:   parts[len(parts)-1],
		}
	case len(parts) == 3:
		return textGenSelection{provider: parts[1], model: parts[2]}
	case len(parts) == 2 && (parts[0] == "claude" || parts[0] == "codex"):
		// Short-lived first provider-aware format: kind::model.
		return textGenSelection{provider: parts[0], model: parts[1]}
	default:
		return textGenSelection{provider: "claude", model: selection}
	}
}

// claudeCliEffort mirrors t3code's normalizeClaudeCliEffort: "ultrathink" is a
// prompt-prefix mode rather than an effort level and "ultracode" is a setting
// that pairs with xhigh, so neither can go to --effort as-is.
func claudeCliEffort(effort string) string {
	switch effort {
	case "ultracode":
		return "xhigh"
	case "low", "medium", "high", "xhigh", "max":
		return effort
	default: // "", "ultrathink", or anything this build doesn't know
		return ""
	}
}

// --- policy ---------------------------------------------------------------

// textGenPolicy is t3code's TextGenerationPolicy: extra instructions folded
// into each prompt so a repo can get commit messages in its own house style.
type textGenPolicy struct {
	kind                       string
	commitInstructions         string
	changeRequestInstructions  string
	branchInstructions         string
	threadTitleInstructions    string
	inferRepositoryConventions bool
}

// textGenPolicyFor resolves a preset by name (t3code's textGenerationPresets).
// An unknown name is the default policy — a stored preference from a newer
// build must not break generation.
func textGenPolicyFor(kind string) textGenPolicy {
	switch kind {
	case "conventional_commits":
		return textGenPolicy{
			kind:                      kind,
			commitInstructions:        "Use Conventional Commits when generating commit subjects. Prefer the narrowest accurate type and include a scope only when it is obvious from the diff.",
			changeRequestInstructions: "Keep the change request title concise. Do not force Conventional Commit syntax into the title unless the repository already uses it.",
		}
	case "repo_conventions":
		return textGenPolicy{
			kind:                       kind,
			commitInstructions:         "Follow the repository's established commit message style when examples are available.",
			changeRequestInstructions:  "Follow the repository's established change request title and body style when examples are available.",
			inferRepositoryConventions: true,
		}
	default:
		return textGenPolicy{kind: "default"}
	}
}

// --- prompt building ------------------------------------------------------

// limitSection truncates one prompt section, marking the cut so the model knows
// it is looking at a fragment.
func limitSection(value string, maxChars int) string {
	if len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n\n[truncated]"
}

// policyInstruction renders a policy's extra instructions as its own prompt
// section, or nothing when the policy has none for this operation.
func policyInstruction(instruction string) []string {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return nil
	}
	return []string{"", "Additional instructions:", limitSection(trimmed, 4000)}
}

// --- provider invocation --------------------------------------------------

// generateTextJSON runs the provider named by the persisted selection and hands
// back the JSON object the prompt asked for. Both the prompt and the schema are
// provider-neutral, so callers never learn which CLI answered.
func generateTextJSON(cwd, selection, prompt, schema string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), textGenTimeout)
	defer cancel()
	return generateTextJSONContext(ctx, cwd, selection, prompt, schema)
}

func generateTextJSONContext(ctx context.Context, cwd, selection, prompt, schema string) ([]byte, error) {
	sel := parseTextGenerationSelection(selection)
	switch sel.provider {
	case "claude":
		// Prompt goes over stdin (no arg-length limit, no shell-escaping
		// headaches); --json-schema takes the contract inline and comes back in
		// the envelope's structured_output.
		bin := "claude"
		if resolved := resolveAgentBin("claude", cwd); resolved != "" {
			bin = resolved
		}
		args := []string{"-p", "--output-format", "json", "--json-schema", schema}
		if sel.model != "" {
			args = append(args, "--model", sel.model)
		}
		if effort := claudeCliEffort(sel.effort); effort != "" {
			args = append(args, "--effort", effort)
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
		// --output-schema is what makes Codex answer in JSON at all: without it
		// it replies in prose as often as not, and scraping an object out of
		// that silently dropped whole generations.
		schemaFile, err := writeTempFile("burrow-codex-schema-*.json", schema)
		if err != nil {
			return nil, err
		}
		defer os.Remove(schemaFile)
		outFile, err := writeTempFile("burrow-codex-text-*.json", "")
		if err != nil {
			return nil, err
		}
		defer os.Remove(outFile)
		effort := sel.effort
		if effort == "" {
			effort = defaultCodexEffort
		}
		args := []string{
			"exec",
			"--ephemeral", // no rollout file for a one-shot writing job
			"--skip-git-repo-check",
			"-s", "read-only", // writing text needs no write access
			"--config", `model_reasoning_effort="` + effort + `"`,
			"--output-schema", schemaFile,
			"--output-last-message", outFile,
		}
		if sel.model != "" {
			args = append(args, "--model", sel.model)
		}
		args = append(args, "-") // prompt arrives on stdin
		c := exec.CommandContext(ctx, bin, args...)
		c.Dir = cwd
		c.Stdin = strings.NewReader(prompt)
		if combined, err := c.CombinedOutput(); err != nil {
			return nil, errors.New(strings.TrimSpace(string(combined)))
		}
		out, err := os.ReadFile(outFile)
		if err != nil {
			return nil, err
		}
		// An older codex that ignored --output-schema still answers in prose;
		// dig the object out when there is one, hand the raw text back when
		// there isn't, so a bare title still reaches a caller that can use it.
		if extracted, err := extractGeneratedJSON(out); err == nil {
			return extracted, nil
		}
		return out, nil
	case "gemini":
		bin := "gemini"
		if resolved := resolveAgentBin("gemini", cwd); resolved != "" {
			bin = resolved
		}
		args := []string{"--prompt", prompt, "--output-format", "json"}
		if sel.model != "" {
			args = append(args, "--model", sel.model)
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
		if sel.model != "" {
			args = append(args, "--model", sel.model)
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

func writeTempFile(pattern, content string) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			os.Remove(f.Name())
			return "", err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
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

// --- sanitizers -----------------------------------------------------------
// Every generated string passes through one of these before it reaches git or
// the UI, because "the model followed the rules" is not something to rely on.

// sanitizeCommitSubject mirrors t3code's TextGenerationUtils.sanitizeCommitSubject.
func sanitizeCommitSubject(raw string) string {
	line := firstLine(raw)
	line = strings.TrimSpace(strings.TrimRight(line, "."))
	if line == "" {
		return "Update project files"
	}
	if len(line) > 72 {
		line = strings.TrimSpace(line[:72])
	}
	return line
}

// sanitizePrTitle mirrors t3code's sanitizePrTitle.
func sanitizePrTitle(raw string) string {
	if line := strings.Trim(firstLine(raw), `"'`+"`"); strings.TrimSpace(line) != "" {
		return strings.TrimSpace(line)
	}
	return "Update project changes"
}

// sanitizeChatTitle trims the model's answer to one short sidebar-safe line.
// Empty in, empty out — unlike t3code's sanitizeThreadTitle, which falls back
// to "New thread": here an empty title must never overwrite the heuristic one
// the caller is already showing.
func sanitizeChatTitle(raw string) string {
	line := strings.Join(strings.Fields(firstLine(raw)), " ")
	line = strings.TrimSpace(strings.Trim(line, `"'`+"`"))
	line = strings.TrimRight(line, ".")
	if len(line) > 50 {
		line = strings.TrimSpace(line[:47]) + "..."
	}
	return line
}

// sanitizeBranchFragment mirrors t3code's sanitizeBranchFragment: lowercase,
// slug-safe, and never empty — the result goes straight into `git worktree add
// -b`, so a model that answers with a sentence must not produce an invalid ref.
func sanitizeBranchFragment(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range lowered {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '/', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	slug := b.String()
	for _, pair := range [][2]string{{"//", "/"}, {"--", "-"}, {"__", "_"}} {
		for strings.Contains(slug, pair[0]) {
			slug = strings.ReplaceAll(slug, pair[0], pair[1])
		}
	}
	slug = strings.Trim(slug, "./_-")
	if len(slug) > 64 {
		slug = strings.TrimRight(slug[:64], "./_-")
	}
	if slug == "" {
		return "update"
	}
	return slug
}

func firstLine(raw string) string {
	line := strings.TrimSpace(raw)
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

// --- commit messages ------------------------------------------------------

const commitMessageSchema = `{"type":"object","properties":{"subject":{"type":"string"},"body":{"type":"string"}},"required":["subject","body"],"additionalProperties":false}`

// GenerateCommitMessage drafts a commit message from the staged diff.
func (a *App) GenerateCommitMessage(cwd, selection, policyKind string) GitOutput {
	diffOut := runCmd("git", cwd, []string{"diff", "--staged"})
	if !diffOut.Success {
		return diffOut
	}
	diff := strings.TrimSpace(diffOut.Stdout)
	if diff == "" {
		return GitOutput{Stderr: "nothing staged", Code: 1}
	}
	statOut := runCmd("git", cwd, []string{"diff", "--staged", "--stat"})
	branchOut := runCmd("git", cwd, []string{"branch", "--show-current"})
	branch := strings.TrimSpace(branchOut.Stdout)
	if branch == "" {
		branch = "(detached)"
	}
	policy := textGenPolicyFor(policyKind)

	sections := []string{
		"You write concise git commit messages.",
		"Return a JSON object with keys: subject, body.",
		"Rules:",
		"- subject must be imperative, <= 72 chars, and no trailing period",
		"- body can be an empty string or short bullet points",
		"- capture the primary user-visible or developer-visible change",
	}
	sections = append(sections, policyInstruction(policy.commitInstructions)...)
	if examples := recentCommitSubjects(cwd, policy); examples != "" {
		sections = append(sections, "", "Recent commit subjects from this repository:", examples)
	}
	sections = append(sections,
		"",
		"Branch: "+branch,
		"",
		"Staged files:",
		limitSection(strings.TrimSpace(statOut.Stdout), 6000),
		"",
		"Staged patch:",
		limitSection(diff, 40000),
	)

	jsonOutput, err := generateTextJSON(cwd, selection, strings.Join(sections, "\n"), commitMessageSchema)
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

// recentCommitSubjects gives the repo_conventions policy something to imitate.
// t3code's preset only asks the model to follow the house style "when examples
// are available" — this is what makes them available.
func recentCommitSubjects(cwd string, policy textGenPolicy) string {
	if !policy.inferRepositoryConventions {
		return ""
	}
	out := runCmd("git", cwd, []string{"log", "-20", "--no-merges", "--format=%s"})
	if !out.Success {
		return ""
	}
	return limitSection(strings.TrimSpace(out.Stdout), 2000)
}

// --- pull request content -------------------------------------------------

const prContentSchema = `{"type":"object","properties":{"title":{"type":"string"},"body":{"type":"string"}},"required":["title","body"],"additionalProperties":false}`

// GeneratePrContent drafts a pull request title and body from the commits and
// diff between two branches. Keys of the result: title, body.
func (a *App) GeneratePrContent(cwd, selection, policyKind, baseBranch, headBranch string) (map[string]string, error) {
	if baseBranch == "" || headBranch == "" {
		return nil, errors.New("pull request generation needs a base and a head branch")
	}
	rangeSpec := baseBranch + "..." + headBranch
	commits := runCmd("git", cwd, []string{"log", "--no-merges", "--format=%s%n%b", rangeSpec})
	if !commits.Success {
		return nil, gitErr(commits)
	}
	if strings.TrimSpace(commits.Stdout) == "" {
		return nil, errors.New("no commits between " + baseBranch + " and " + headBranch)
	}
	stat := runCmd("git", cwd, []string{"diff", "--stat", rangeSpec})
	patch := runCmd("git", cwd, []string{"diff", rangeSpec})
	policy := textGenPolicyFor(policyKind)

	sections := []string{
		"You write GitHub pull request content.",
		"Return a JSON object with keys: title, body.",
		"Rules:",
		"- title should be concise and specific",
		"- body must be markdown and include headings '## Summary' and '## Testing'",
		"- under Summary, provide short bullet points",
		"- under Testing, include bullet points with concrete checks or 'Not run' where appropriate",
	}
	sections = append(sections, policyInstruction(policy.changeRequestInstructions)...)
	sections = append(sections,
		"",
		"Base branch: "+baseBranch,
		"Head branch: "+headBranch,
		"",
		"Commits:",
		limitSection(strings.TrimSpace(commits.Stdout), 12000),
		"",
		"Diff stat:",
		limitSection(strings.TrimSpace(stat.Stdout), 12000),
		"",
		"Diff patch:",
		limitSection(strings.TrimSpace(patch.Stdout), 40000),
	)

	jsonOutput, err := generateTextJSON(cwd, selection, strings.Join(sections, "\n"), prContentSchema)
	if err != nil {
		return nil, err
	}
	var result struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(jsonOutput, &result); err != nil {
		return nil, errors.New("text generation provider returned unexpected JSON")
	}
	return map[string]string{
		"title": sanitizePrTitle(result.Title),
		"body":  strings.TrimSpace(result.Body),
	}, nil
}

// --- branch names ---------------------------------------------------------

const branchNameSchema = `{"type":"object","properties":{"branch":{"type":"string"}},"required":["branch"],"additionalProperties":false}`

// GenerateBranchName names a branch after the task the user just described.
// Best-effort: any failure returns "" and the caller keeps the name it had.
func (a *App) GenerateBranchName(cwd, selection, policyKind, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	policy := textGenPolicyFor(policyKind)
	sections := []string{
		"You generate concise git branch names.",
		"Return a JSON object with key: branch.",
		"Rules:",
		"- branch should describe the requested work from the user message",
		"- keep it short and specific (2-6 words)",
		"- use plain words only, no issue prefixes and no punctuation-heavy text",
		"",
		"User message:",
		limitSection(message, 8000),
	}
	sections = append(sections, policyInstruction(policy.branchInstructions)...)

	jsonOutput, err := generateTextJSON(cwd, selection, strings.Join(sections, "\n"), branchNameSchema)
	if err != nil {
		log.Printf("branch name generation failed (%s): %v", selection, err)
		return ""
	}
	var result struct {
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal(jsonOutput, &result); err != nil || strings.TrimSpace(result.Branch) == "" {
		return ""
	}
	return sanitizeBranchFragment(result.Branch)
}

// --- chat titles ----------------------------------------------------------

const chatTitleSchema = `{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`

// GenerateChatTitle names a chat thread from its first user message.
// Best-effort: any failure returns "" and the caller keeps the heuristic title
// it already showed.
func (a *App) GenerateChatTitle(cwd, selection, policyKind, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	policy := textGenPolicyFor(policyKind)
	sections := []string{
		"You write concise thread titles for coding conversations.",
		"Return a JSON object with key: title.",
		"Rules:",
		"- title should summarize the user's request, not restate it verbatim",
		"- keep it short and specific (3-8 words)",
		"- avoid quotes, filler, prefixes, and trailing punctuation",
		"- name the task, not the conversation (\"Fix PTY status dots\", not \"User asks about dots\")",
		"",
		"User message:",
		limitSection(text, 8000),
	}
	sections = append(sections, policyInstruction(policy.threadTitleInstructions)...)

	jsonOutput, err := generateTextJSON(cwd, selection, strings.Join(sections, "\n"), chatTitleSchema)
	if err != nil {
		// The caller swallows a "" and keeps the heuristic title, which made a
		// broken provider selection indistinguishable from "the model was slow".
		log.Printf("chat title generation failed (%s): %v", selection, err)
		return ""
	}
	return chatTitleFromOutput(jsonOutput)
}

// chatTitleFromOutput reads the title out of whatever the provider produced:
// the requested JSON object, or — when it ignored the contract and just wrote
// the name — the bare line itself. Throwing prose away was the whole reason
// codex-backed titles silently never replaced the heuristic one.
func chatTitleFromOutput(out []byte) string {
	if extracted, err := extractGeneratedJSON(out); err == nil {
		out = extracted
	}
	var result struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &result); err == nil {
		return sanitizeChatTitle(result.Title)
	}
	return sanitizeChatTitle(string(out))
}
