package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func openAITools() []any {
	tools := []any{}
	add := func(name, description string, properties map[string]any, required ...string) {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": description, "parameters": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}})
	}
	str := func(description string) any { return map[string]any{"type": "string", "description": description} }
	add("read_file", "Read a UTF-8 text file inside the workspace. Check applicable nested AGENTS.md instructions before editing.", map[string]any{"path": str("Workspace-relative file path"), "offset": map[string]any{"type": "integer", "description": "First line, starting at 1"}, "limit": map[string]any{"type": "integer", "description": "Maximum lines (default 400, maximum 2000)"}}, "path")
	add("list_files", "List files and directories inside a workspace directory.", map[string]any{"path": str("Workspace-relative directory, or .")}, "path")
	add("search_files", "Find literal text in workspace text files; skips .git, node_modules, and binary files.", map[string]any{"path": str("Workspace-relative directory, or ."), "query": str("Literal text to find")}, "path", "query")
	add("write_file", "Create or replace a text file inside the workspace. Read existing files before replacing them.", map[string]any{"path": str("Workspace-relative file path"), "content": str("Complete new UTF-8 contents")}, "path", "content")
	add("edit_file", "Replace one unique exact text occurrence in a workspace file. Fails if old_text is absent or ambiguous.", map[string]any{"path": str("Workspace-relative file path"), "old_text": str("Exact existing text, unique within the file"), "new_text": str("Replacement text")}, "path", "old_text", "new_text")
	add("run_command", "Run a shell command in the workspace. Use for builds, tests, git, and other development tools. Returns output and exit status. Do not start long-lived background processes.", map[string]any{"command": str("Shell command"), "timeout_seconds": map[string]any{"type": "integer", "description": "Timeout in seconds (default 120, maximum 300)"}}, "command")
	return tools
}

// File tools stay inside the workspace, including paths traversing symlinks.
// Shell commands are a separate explicit approval and are not a sandbox.
func openAIWorkspacePath(cwd, path string) (string, error) {
	if path == "" {
		path = "."
	}
	root, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target = filepath.Clean(target)
	existing := target
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", err
		}
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	suffix, err := filepath.Rel(existing, target)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(real, suffix)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file tools can only access the workspace")
	}
	return resolved, nil
}

type openAIToolArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
	Query   string `json:"query"`
	Command string `json:"command"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
	Timeout int    `json:"timeout_seconds"`
}

func (s *openAISession) execute(ctx context.Context, call openAIToolCall) (string, bool) {
	var args openAIToolArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "Invalid tool arguments: " + err.Error(), true
	}
	name := call.Function.Name
	if !oneOf(name, "read_file", "list_files", "search_files", "write_file", "edit_file", "run_command") {
		return "Unknown tool: " + name, true
	}
	title := name + ": " + args.Path
	kind := "read"
	if name == "run_command" {
		title = "Run: " + args.Command
		kind = "execute"
	}
	if name == "write_file" || name == "edit_file" {
		kind = "edit"
	}
	raw := map[string]any{}
	_ = json.Unmarshal([]byte(call.Function.Arguments), &raw)
	if name == "edit_file" {
		raw["old_string"], raw["new_string"] = args.OldText, args.NewText
	}
	s.update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": call.ID, "title": title, "kind": kind, "rawInput": raw})
	if err := ctx.Err(); err != nil {
		return "Interrupted before execution.", true
	}
	path := ""
	if name != "run_command" {
		var err error
		path, err = openAIWorkspacePath(s.cwd, args.Path)
		if err != nil {
			return err.Error(), true
		}
	}
	if kind == "edit" || kind == "execute" {
		allowed, err := s.approve(ctx, call.ID, title, kind, raw)
		if err != nil {
			return "Interrupted before execution.", true
		}
		if !allowed {
			return "Permission denied. Do not retry this action without new user authorization.", true
		}
	}
	// Re-resolve after a human decision: the symlink tree may have changed while
	// approval was pending. Errors and cancellations must never run the action.
	if err := ctx.Err(); err != nil {
		return "Interrupted before execution.", true
	}
	if kind == "edit" || kind == "execute" {
		s.mu.Lock()
		planning := s.mode == "plan"
		s.mu.Unlock()
		if planning {
			return "Plan mode does not allow file changes or shell commands.", true
		}
	}
	if name != "run_command" {
		var err error
		path, err = openAIWorkspacePath(s.cwd, args.Path)
		if err != nil {
			return err.Error(), true
		}
	}
	output, err := s.performTool(ctx, name, path, args)
	if err != nil {
		return openAIClip(output+"\nError: "+err.Error(), s.config.ContextWindow), true
	}
	return openAIClip(output, s.config.ContextWindow), false
}

func (s *openAISession) approve(ctx context.Context, toolID, title, kind string, input map[string]any) (bool, error) {
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	if mode == "plan" {
		return false, nil
	}
	if mode == "auto" {
		return true, nil
	}
	rpc := s.sess.rpcID()
	ch := make(chan string, 1)
	s.mu.Lock()
	s.approvals[rpc] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.approvals, rpc); s.mu.Unlock() }()
	s.emit("acp-req", map[string]any{"jsonrpc": "2.0", "id": rpc, "method": "session/request_permission", "params": map[string]any{
		"sessionId": s.sessionID, "toolCall": map[string]any{"toolCallId": toolID, "title": title, "kind": kind, "rawInput": input},
		"options": []any{map[string]any{"optionId": "allow", "name": "Allow once", "kind": "allow_once"}, map[string]any{"optionId": "deny", "name": "Deny", "kind": "reject_once"}},
	}})
	select {
	case option := <-ch:
		s.emit("acp-data", map[string]any{"method": "serverRequest/resolved", "params": map[string]any{"requestId": rpc}})
		return option == "allow", nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

func (s *openAISession) performTool(ctx context.Context, name, path string, args openAIToolArgs) (string, error) {
	switch name {
	case "read_file":
		b, err := openAIReadText(path)
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(b), "\n")
		offset := args.Offset
		if offset < 1 {
			offset = 1
		}
		if offset > len(lines) {
			return "End of file.", nil
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 400
		}
		if limit > 2000 {
			limit = 2000
		}
		end := offset - 1 + limit
		if end > len(lines) {
			end = len(lines)
		}
		var out strings.Builder
		for i := offset - 1; i < end; i++ {
			fmt.Fprintf(&out, "%d: %s\n", i+1, lines[i])
		}
		if end < len(lines) {
			fmt.Fprintf(&out, "\nMore lines available; next offset: %d", end+1)
		}
		return out.String(), nil
	case "list_files":
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", err
		}
		var out strings.Builder
		for i, e := range entries {
			if i >= 1000 {
				out.WriteString("[More entries omitted]\n")
				break
			}
			out.WriteString(e.Name())
			if e.IsDir() {
				out.WriteString("/")
			}
			out.WriteByte('\n')
		}
		return out.String(), nil
	case "search_files":
		if args.Query == "" {
			return "", fmt.Errorf("query must not be empty")
		}
		var out strings.Builder
		count := 0
		visited := 0
		err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			visited++
			if visited > 10000 || count >= 200 {
				return filepath.SkipAll
			}
			b, err := openAIReadText(p)
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(s.cwd, p)
			for i, line := range strings.Split(string(b), "\n") {
				if strings.Contains(line, args.Query) {
					fmt.Fprintf(&out, "%s:%d: %s\n", rel, i+1, line)
					count++
					if count >= 200 {
						break
					}
				}
			}
			return nil
		})
		if count >= 200 || visited > 10000 {
			out.WriteString("[Search limit reached; narrow the path or query]\n")
		}
		return out.String(), err
	case "write_file":
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
			return "", err
		}
		return "File written: " + args.Path, nil
	case "edit_file":
		b, err := openAIReadText(path)
		if err != nil {
			return "", err
		}
		if args.OldText == "" || strings.Count(string(b), args.OldText) != 1 {
			return "", fmt.Errorf("old_text must match exactly one occurrence; read the file and provide a unique match")
		}
		result := strings.Replace(string(b), args.OldText, args.NewText, 1)
		if err := os.WriteFile(path, []byte(result), 0644); err != nil {
			return "", err
		}
		return "File edited: " + args.Path, nil
	case "run_command":
		if strings.TrimSpace(args.Command) == "" {
			return "", fmt.Errorf("command must not be empty")
		}
		timeout := args.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		if timeout > 300 {
			timeout = 300
		}
		cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd := exec.Command(shell, "-lc", args.Command)
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd.exe", "/C", args.Command)
		}
		configureAgentProcessGroup(cmd)
		cmd.Dir = s.cwd
		cmd.Env = append(updateEnv(s.cwd), envSlice(s.env)...)
		output := &openAILimitedOutput{limit: 65536}
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Start(); err != nil {
			return "", err
		}
		finished := make(chan struct{})
		go func() {
			select {
			case <-cmdCtx.Done():
				_ = killAgentProcessTree(cmd)
			case <-finished:
			}
		}()
		err := cmd.Wait()
		close(finished)
		if cmdCtx.Err() != nil {
			return output.String(), cmdCtx.Err()
		}
		if err != nil {
			return output.String(), err
		}
		return output.String() + "\nExit code: 0", nil
	}
	return "", fmt.Errorf("unknown tool")
}
func openAIReadText(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("file exceeds 2 MiB; use a command to inspect a smaller section")
	}
	if bytesContainNUL(b) {
		return nil, fmt.Errorf("binary files are not supported by text tools")
	}
	return b, nil
}
func bytesContainNUL(b []byte) bool {
	for _, v := range b {
		if v == 0 {
			return true
		}
	}
	return false
}
func openAIClip(text string, contextWindow int) string {
	limit := contextWindow
	if limit > 24000 {
		limit = 24000
	}
	if limit < 1024 {
		limit = 1024
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n[Output truncated; request a smaller section.]"
}

type openAILimitedOutput struct {
	mu        sync.Mutex
	text      strings.Builder
	limit     int
	truncated bool
}

func (w *openAILimitedOutput) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(b)
	left := w.limit - w.text.Len()
	if len(b) > left {
		b = b[:left]
		w.truncated = true
	}
	_, _ = w.text.Write(b)
	return n, nil
}
func (w *openAILimitedOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.text.String()
	if w.truncated {
		out += "\n[Output truncated]"
	}
	return out
}
