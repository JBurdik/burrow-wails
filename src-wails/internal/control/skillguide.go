package control

import (
	"fmt"
	"strings"
)

// BurrowSkillReferences are optional chapters an agent loads only when needed.
var BurrowSkillReferences = []string{"orchestration", "worktrees"}

// BurrowSkillGuide renders the Burrow agent guide from the live verb registry.
// It has no persisted version: the binary serving it runs the documented commands.
func BurrowSkillGuide(verbs []Verb, reference string) (string, error) {
	var out strings.Builder
	switch reference {
	case "":
		fmt.Fprint(&out, `---
name: burrow
description: Delegate and supervise work through the running Burrow IDE.
---

# Burrow control guide

Burrow runs coding agents in terminal tabs and structured chats. Use it to delegate, supervise, fan out work, or isolate work in a worktree.

Every command below comes from this running Burrow binary. It has two doors: call a control tool when one is available, or run burrow <verb-with-dashes> in a shell. Do not copy this live registry into a local skill.

## Live verbs

`)
		writeVerbList(&out, verbs)
		fmt.Fprint(&out, `
## Chat and terminal tabs are equal targets

Choose deliberately for every delegation workflow. spawn --target chat creates a structured chat: use chat_send, wait_result --chat-id, and its transcript. spawn --target tab creates a visible terminal: use send_to_tab, tab_output, and its result token. agent_status and list_agents cover both target kinds; collect_results collects terminal results and, from a chat parent, completed chat children. Never address a chat with a PTY id or a terminal with a chat id.

### Both doors for each delegation verb

- spawn: choose target chat for a structured chat or target tab for a visible terminal.
- list_agents: lists the configured agents that may be spawned into either a chat or a tab.
- agent_status: reports both chat and tab agents; use its chat_id or pty_id with the matching follow-up verb.
- tab_output: reads a tab by pty_id; for a chat, read its structured transcript or wait_result --chat-id instead.
- send_to_tab: follows up with a tab by pty_id; use chat_send for the equivalent chat follow-up.
- chat_send: follows up with a chat by chat_id; use send_to_tab for the equivalent tab follow-up.
- close_chat: deletes a chat by chat_id; close a terminal tab with tab_close and its pty_id instead.
- wait_result: waits on a tab result token or on a chat_id, depending on the target chosen at spawn time.
- collect_results: takes completed tab results and, when called from a chat parent, its completed chat-child results.

## Optional references (load only when needed)

`)
		for _, name := range BurrowSkillReferences {
			fmt.Fprintf(&out, "- `burrow skills get burrow --reference %s`\n", name)
		}
		fmt.Fprint(&out, "\nList these names with `burrow skills get burrow --references`.\n")
	case "orchestration":
		fmt.Fprint(&out, `# Burrow reference: orchestration

Use one focused task per agent. Put the desired outcome, scope, exclusions, and report-back in spawn, then supervise rather than guessing.

For a chat worker, identify it by chat_id, steer it with chat_send, and wait with wait_result --chat-id. For a tab worker, identify it by pty_id, steer it with send_to_tab, inspect it with tab_output, and wait with its token. agent_status is the shared dashboard for both doors; waiting and permission mean the user must respond.

The live delegation verbs are:

`)
		writeVerbsNamed(&out, verbs, delegationVerbNames)
	case "worktrees":
		fmt.Fprint(&out, `# Burrow reference: worktrees

When independent agents would edit the same checkout, create one worktree per task and pass the returned path as spawn --cwd. This applies equally to chat and tab workers: --target chat changes interaction, not filesystem isolation. Removing a worktree is destructive and requires the user's confirmation.

The live worktree and repository verbs are:

`)
		writeVerbsNamed(&out, verbs, worktreeVerbNames)
	default:
		return "", fmt.Errorf("unknown Burrow skill reference %q", reference)
	}
	return out.String(), nil
}

var delegationVerbNames = map[string]bool{
	"spawn": true, "list_agents": true, "agent_status": true, "tab_output": true,
	"send_to_tab": true, "chat_send": true, "close_chat": true, "wait_result": true,
	"collect_results": true,
}

var worktreeVerbNames = map[string]bool{
	"create_worktree": true, "worktree_remove": true, "git_status": true,
	"git_log": true, "git_diff": true, "run": true,
}

func writeVerbList(out *strings.Builder, verbs []Verb) {
	for _, v := range verbs {
		writeVerb(out, v)
	}
}

func writeVerbsNamed(out *strings.Builder, verbs []Verb, names map[string]bool) {
	for _, v := range verbs {
		if names[v.Name] {
			writeVerb(out, v)
		}
	}
}

func writeVerb(out *strings.Builder, v Verb) {
	args := make([]string, 0, len(v.Args))
	for _, a := range v.Args {
		name := "--" + strings.ReplaceAll(a.Name, "_", "-")
		if a.Required {
			name += " (required)"
		}
		args = append(args, name)
	}
	name := strings.ReplaceAll(v.Name, "_", "-")
	if len(args) == 0 {
		fmt.Fprintf(out, "- `burrow %s` — %s\n", name, v.Summary)
		return
	}
	fmt.Fprintf(out, "- `burrow %s` (%s) — %s\n", name, strings.Join(args, ", "), v.Summary)
}
