package main

import (
	"encoding/json"
	"fmt"
)

// Permission/control-request responses. Both Claude Code and ACP agents
// read their control responses off the same stdin they read turns from,
// so responding is just a specially-shaped Send — matches
// claude_respond_control/acp_respond_permission/acp_respond_user_input in
// src-tauri/src/lib.rs.

func (a *App) ClaudeRespondControl(id, requestID string, response map[string]any) error {
	// The CLI only accepts the nested envelope: request_id and the decision both
	// live inside `response`, tagged with subtype "success". A flat
	// {type,request_id,response} is silently dropped — the tool stays parked
	// forever, which is what made AskUserQuestion never resume after Submit.
	payload, err := json.Marshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   response,
		},
	})
	if err != nil {
		return err
	}
	return a.claudeWrite(id, string(payload))
}

// AcpRespondPermission answers a session/request_permission (ACP) or an
// approval request (Codex app-server, which takes a plain decision string).
func (a *App) AcpRespondPermission(id string, rpcID int64, optionID string) error {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return fmt.Errorf("acp adapter not running")
	}
	if sess.proto == protoCodexAppServer {
		decision := "decline"
		switch optionID {
		case "codex:accept":
			decision = "accept"
		case "codex:acceptForSession":
			decision = "acceptForSession"
		}
		return sess.write(map[string]any{
			"jsonrpc": "2.0", "id": rpcID, "result": map[string]any{"decision": decision},
		})
	}
	outcome := map[string]any{"outcome": "cancelled"}
	if optionID != "" {
		outcome = map[string]any{"outcome": "selected", "optionId": optionID}
	}
	return sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpcID, "result": map[string]any{"outcome": outcome},
	})
}

// AcpRespondUserInput answers a tool's request for structured user input.
// Codex maps every question id to {answers: [...]}; ACP adapters retain their
// legacy single-content response until they expose the same contract.
func (a *App) AcpRespondUserInput(id string, rpcID int64, answers map[string][]string) error {
	sess, ok := a.acpReg().get(id)
	if !ok {
		return fmt.Errorf("acp adapter not running")
	}
	if sess.proto == protoCodexAppServer {
		out := map[string]any{}
		for questionID, values := range answers {
			out[questionID] = map[string]any{"answers": values}
		}
		return sess.write(map[string]any{"jsonrpc": "2.0", "id": rpcID, "result": map[string]any{"answers": out}})
	}
	text := ""
	for _, values := range answers {
		if len(values) > 0 {
			text = values[0]
			break
		}
	}
	return sess.write(map[string]any{
		"jsonrpc": "2.0", "id": rpcID, "result": map[string]any{"content": text},
	})
}
