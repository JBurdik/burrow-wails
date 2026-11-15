package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Permission receipts: the transcript row recording how a tool permission was
// answered ("✓ Allowed: Bash — ls"). The desktop's respond() used to append it
// itself; Go writes it now (ADR 0001: Go is the only transcript writer), so
// every client — and an answer from the phone — gets the same row. Same text
// as chatSession.ts produced.

// claudePermissionReceipt records the answer to a Claude can_use_tool
// request. AskUserQuestion / ExitPlanMode are questions, not permissions, and
// get no receipt (as before).
func (a *App) claudePermissionReceipt(chatID, requestID string, response map[string]any) {
	req := a.findStreamRequest(chatID, "claude-data", func(m map[string]any) bool {
		return strField(m["type"]) == "control_request" && strField(m["request_id"]) == requestID
	})
	if req == nil {
		return
	}
	r := mapField(req["request"])
	tool := strField(r["tool_name"])
	if strField(r["subtype"]) != "can_use_tool" || tool == "" || tool == "AskUserQuestion" || tool == "ExitPlanMode" {
		return
	}
	label := "✗ Denied"
	switch strField(response["behavior"]) {
	case "allow":
		label = "✓ Allowed"
		if _, always := response["updatedPermissions"]; always {
			label = "✓ Always allowed"
		}
	case "deny":
	default:
		return
	}
	input := mapField(r["input"])
	detail := ""
	for _, k := range []string{"command", "file_path", "path"} {
		if s := strField(input[k]); s != "" {
			detail = s
			break
		}
	}
	if detail == "" {
		detail = strField(r["description"])
	}
	text := label + ": " + tool
	if detail != "" {
		if r := []rune(detail); len(r) > 80 {
			detail = string(r[:80]) + "…"
		}
		text += " — " + detail
	}
	a.publishReceipt(chatID, text)
}

// acpPermissionReceipt records the answer to an ACP session/request_permission
// or a Codex approval request.
func (a *App) acpPermissionReceipt(chatID string, rpcID int64, optionID string) {
	idStr := fmt.Sprint(rpcID)
	req := a.findStreamRequest(chatID, "acp-req", func(m map[string]any) bool {
		return rpcIDString(m["id"]) == idStr
	})
	if req == nil {
		return
	}
	method := strField(req["method"])
	p := mapField(req["params"])
	var title, name, kind string
	switch method {
	case "item/commandExecution/requestApproval":
		title = "Run command"
		if c := strField(p["command"]); c != "" {
			title = "Run: " + c
		}
	case "item/fileChange/requestApproval":
		title = "Apply file changes"
	case "item/permissions/requestApproval":
		title = "Grant additional permission"
	case "session/request_permission":
		title = strField(mapField(p["toolCall"])["title"])
		if opts, ok := p["options"].([]any); ok {
			for _, o := range opts {
				if om := mapField(o); strField(om["optionId"]) == optionID {
					name, kind = strField(om["name"]), strField(om["kind"])
				}
			}
		}
	default:
		return
	}
	if strings.HasPrefix(method, "item/") {
		switch optionID {
		case "codex:accept":
			name, kind = "Allow once", "allow_once"
		case "codex:acceptForSession":
			name, kind = "Always allow", "allow_always"
		default:
			name, kind = "Deny", "reject_once"
		}
	}
	if name == "" {
		name, kind = "Cancelled", "reject"
	}
	mark := "✓"
	if strings.HasPrefix(kind, "reject") {
		mark = "✗"
	}
	a.publishReceipt(chatID, fmt.Sprintf("%s %s: %s", mark, name, title))
}

func (a *App) publishReceipt(chatID, text string) {
	b, _ := json.Marshal(chatNoteEnvelope{Form: "row", Role: "permission", Text: text})
	a.emitChatLine(chatID, chatNoteKind, string(b))
}

// findStreamRequest finds the newest recorded line of `kind` that match
// accepts, among the last pendingScanLines.
func (a *App) findStreamRequest(chatID, kind string, match func(map[string]any) bool) map[string]any {
	w := a.chatStream()
	if w == nil {
		return nil
	}
	w.flush() // the request may still be queued for its INSERT
	rows, err := a.db.Query(`SELECT line FROM chat_stream WHERE chat_id = ? AND kind = ? ORDER BY ord DESC LIMIT ?`, chatID, kind, pendingScanLines)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var line string
		if rows.Scan(&line) != nil {
			return nil
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && match(m) {
			return m
		}
	}
	return nil
}
