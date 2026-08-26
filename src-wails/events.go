package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Global (no-suffix) events, matching src-tauri/src/lib.rs's emit_all calls.
func emitBoardTaskMoved(ctx context.Context) { runtime.EventsEmit(ctx, "board-task-moved") }
func emitAgentDone(ctx context.Context)      { runtime.EventsEmit(ctx, "agent-done") }
func emitWorkspacesChanged(ctx context.Context) { runtime.EventsEmit(ctx, "workspaces-changed") }
