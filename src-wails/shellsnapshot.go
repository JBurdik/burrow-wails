package main

import "burrow/internal/agentphase"

// ShellSnapshot is everything a client needs for its first paint, in one round
// trip. Deliberately NOT in here: chat transcript bodies (chat_stream plus
// folded_ord already replay them from SQLite, and a second replay log for the
// same thing is worse than none) and PTY scrollback (the daemon's own ring
// replays it on reattach).
type ShellSnapshot struct {
	Seq           int64                       `json:"seq"`
	EnvironmentID string                      `json:"environment_id"`
	Workspaces    []Workspace                 `json:"workspaces"`
	Tabs          map[int64][]TerminalTab     `json:"tabs"`
	Phases        map[string]agentphase.Phase `json:"phases"`
	Chats         []map[string]any            `json:"chats"`
}

// ShellSnapshot reads the sequence FIRST, before any data. Taken afterwards, an
// event landing between the data read and the seq read would be counted as
// already seen and lost for good; taken first, the worst case is that the
// client sees something twice, which its reducers tolerate.
func (a *App) ShellSnapshot() (ShellSnapshot, error) {
	snap := ShellSnapshot{
		Seq:           currentSeq(),
		EnvironmentID: a.environmentID,
		Tabs:          map[int64][]TerminalTab{},
		Phases:        map[string]agentphase.Phase{},
	}

	if a.phases != nil {
		snap.Phases = a.phases.All()
	}
	if a.db == nil {
		// No database: an empty shell rather than an error. The client's first
		// paint should show an app with nothing in it, not a failure.
		return snap, nil
	}

	ws, err := a.ListWorkspaces()
	if err != nil {
		return snap, err
	}
	snap.Workspaces = ws

	// Tabs for EVERY workspace, not just the mounted one — a phone opens on a
	// workspace the desktop never mounted.
	for _, w := range ws {
		tabs, err := a.ListTerminalTabs(w.ID)
		if err != nil {
			return snap, err
		}
		snap.Tabs[w.ID] = tabs
	}

	if chats, err := a.RemoteListChats(); err == nil {
		snap.Chats = chats
	}
	return snap, nil
}
