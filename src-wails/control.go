package main

import "encoding/json"

// Permission/control-request responses. Both Claude Code and ACP agents
// read their control responses off the same stdin they read turns from,
// so responding is just a specially-shaped Send — matches
// claude_respond_control/acp_respond_permission/acp_respond_user_input in
// src-tauri/src/lib.rs.

func (a *App) ClaudeRespondControl(id, requestID string, response map[string]any) error {
	payload, err := json.Marshal(map[string]any{
		"type":       "control_response",
		"request_id": requestID,
		"response":   response,
	})
	if err != nil {
		return err
	}
	return a.ClaudeSend(id, string(payload))
}

func (a *App) AcpRespondPermission(id string, rpcID int64, optionID string) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID,
		"result":  map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}},
	})
	if err != nil {
		return err
	}
	return a.AcpSend(id, string(payload))
}

func (a *App) AcpRespondUserInput(id string, rpcID int64, text string) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID,
		"result":  map[string]any{"content": text},
	})
	if err != nil {
		return err
	}
	return a.AcpSend(id, string(payload))
}
