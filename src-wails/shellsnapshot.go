package main

import (
	"strconv"
	"strings"

	"burrow/internal/agentphase"
)

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
	// Unarchived chats only (no client reads this list — the stores use
	// list_chats; it stays for the shape). Archived ones are a count.
	Chats         []map[string]any `json:"chats"`
	ArchivedChats int              `json:"archived_chats"`
}

// shellSnapshotAfterSeqHook runs immediately after Seq is captured, before any
// data read. It exists only so a test can simulate an event landing in that
// window without engineering a real goroutine race; always nil in
// production, so it costs one nil check on the real path.
var shellSnapshotAfterSeqHook func()

// ShellSnapshot reads the sequence FIRST, before any data. Taken afterwards, an
// event landing between the data read and the seq read would be counted as
// already seen and lost for good; taken first, the worst case is that the
// client sees something twice, which its reducers tolerate.
func (a *App) ShellSnapshot() (ShellSnapshot, error) {
	seq := currentSeq()
	if shellSnapshotAfterSeqHook != nil {
		shellSnapshotAfterSeqHook()
	}

	snap := ShellSnapshot{
		Seq:           seq,
		EnvironmentID: a.environmentID,
		// Empty, never nil, all four of them: a client indexes and maps over
		// these without a guard, and JSON null there kills the whole first
		// paint over a missing chat list or an empty database. The reads below
		// append rather than assign so an empty result cannot put the nil back.
		Workspaces: []Workspace{},
		Tabs:       map[int64][]TerminalTab{},
		Phases:     map[string]agentphase.Phase{},
		Chats:      []map[string]any{},
	}

	var all map[string]agentphase.Phase
	if a.phases != nil {
		all = a.phases.All()
	}
	if a.db == nil {
		if all != nil {
			snap.Phases = all
		}
		// No database: an empty shell rather than an error. The client's first
		// paint should show an app with nothing in it, not a failure.
		return snap, nil
	}

	ws, err := a.ListWorkspaces()
	if err != nil {
		return snap, err
	}
	snap.Workspaces = append(snap.Workspaces, ws...)

	// Tabs for EVERY workspace, not just the mounted one — a phone opens on a
	// workspace the desktop never mounted.
	for _, w := range ws {
		tabs, err := a.ListTerminalTabs(w.ID)
		if err != nil {
			return snap, err
		}
		snap.Tabs[w.ID] = tabs
	}

	liveChats := map[int64]bool{}
	if chats, err := a.ListChats(); err == nil {
		for _, c := range chats {
			if c.ArchivedAt > 0 {
				snap.ArchivedChats++
				continue
			}
			liveChats[c.ID] = true
			// A subset of remoteChatShape's keys: every client reads chats
			// from list_chats, so this only names them for an old reader.
			snap.Chats = append(snap.Chats, map[string]any{
				"id": c.ID, "workspaceId": c.WorkspaceID, "title": c.Title,
				"agentKind": c.AgentKind, "transport": c.Transport, "lastActivityAt": c.LastActivityAt,
			})
		}
	}
	var livePtys map[string]bool // nil = daemon unreadable: keep every pty phase
	if a.daemon != nil {
		if ids, err := a.daemon.List(); err == nil {
			livePtys = make(map[string]bool, len(ids))
			for _, id := range ids {
				livePtys[id] = true
			}
		}
	}
	for k, p := range all {
		if snapshotKeepsPhase(k, livePtys, liveChats) {
			snap.Phases[k] = p
		}
	}
	return snap, nil
}

// snapshotKeepsPhase: only phases a client can show — a PTY the daemon still
// lists, a chat that exists and is not archived. Dead PTYs and archived or
// deleted chats were ~90% of the map. A client missing a key reads idle, which
// is what those displayed anyway.
func snapshotKeepsPhase(key string, livePtys map[string]bool, liveChats map[int64]bool) bool {
	if id, ok := strings.CutPrefix(key, "pty:"); ok {
		return livePtys == nil || livePtys[id]
	}
	if id, ok := strings.CutPrefix(key, "chat:"); ok {
		n, err := strconv.ParseInt(id, 10, 64)
		return err == nil && liveChats[n]
	}
	return false
}
