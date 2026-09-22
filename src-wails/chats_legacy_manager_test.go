package main

import "testing"

// The Manager is gone and nothing reads `control` any more, so an upgrade must
// retire those rows — otherwise every project that ever opened the Manager gets
// its thread back as an ordinary chat (auto-opened tab, Sidebar row, phone).
func TestArchiveLegacyManagerChats(t *testing.T) {
	a, _ := newChatApp(t)
	t.Cleanup(busReset)
	busReset()

	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, control) VALUES (1, 1, 'Manager', 1)`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(
		`INSERT INTO chats (id, workspace_id, title, control) VALUES (2, 1, 'real chat', 0)`,
	); err != nil {
		t.Fatal(err)
	}

	a.archiveLegacyManagerChats()

	list, err := a.ListChats()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]Chat{}
	for _, c := range list {
		byID[c.ID] = c
	}
	if byID[1].ArchivedAt == 0 {
		t.Error("the Manager thread must be archived, not left listed")
	}
	if byID[2].ArchivedAt != 0 {
		t.Error("an ordinary chat must not be touched")
	}

	// One-shot: clearing `control` in the same statement is the marker, so a
	// thread the user unarchives later must stay unarchived.
	if _, err := a.db.Exec(`UPDATE chats SET archived_at = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	a.archiveLegacyManagerChats()

	var archived int64
	if err := a.db.QueryRow(`SELECT archived_at FROM chats WHERE id = 1`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived != 0 {
		t.Error("a second run re-archived an unarchived chat — the migration is not one-shot")
	}
}
