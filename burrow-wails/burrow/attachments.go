package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
)

// task_attachments CRUD — matches write_task_attachment/list_task_
// attachments/delete_task_attachment/read_task_attachment_base64 in
// src-tauri/src/lib.rs. Files are stored under
// <appDataDir>/attachments/<taskId>/<ord>.<ext> and only the path/mime is
// kept in SQLite (matches the Rust layout).

// TaskAttachment mirrors src/stores/boardTasks.ts's TaskAttachment interface.
type TaskAttachment struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Ord       int    `json:"ord"`
	MimeType  string `json:"mime_type"`
	FilePath  string `json:"file_path"`
	CreatedAt int64  `json:"created_at"`
}

func (a *App) attachmentsDir(taskID string) string {
	dataDir, _ := appDataDir()
	return filepath.Join(dataDir, "attachments", taskID)
}

func (a *App) WriteTaskAttachment(taskID, mimeType, base64Data, ext string) (TaskAttachment, error) {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return TaskAttachment{}, err
	}

	var nextOrd int
	_ = a.db.QueryRow(`SELECT COALESCE(MAX(ord), -1) + 1 FROM task_attachments WHERE task_id = ?`, taskID).Scan(&nextOrd)

	dir := a.attachmentsDir(taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return TaskAttachment{}, err
	}
	path := filepath.Join(dir, strconv.Itoa(nextOrd)+"."+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return TaskAttachment{}, err
	}

	createdAt := nowMillis()
	res, err := a.db.Exec(`INSERT INTO task_attachments (task_id, ord, mime_type, file_path, created_at) VALUES (?, ?, ?, ?, ?)`, taskID, nextOrd, mimeType, path, createdAt)
	if err != nil {
		return TaskAttachment{}, err
	}
	id, _ := res.LastInsertId()
	return TaskAttachment{ID: id, TaskID: taskID, Ord: nextOrd, MimeType: mimeType, FilePath: path, CreatedAt: createdAt}, nil
}

func (a *App) ListTaskAttachments(taskID string) ([]TaskAttachment, error) {
	rows, err := a.db.Query(`SELECT id, task_id, ord, mime_type, file_path, created_at FROM task_attachments WHERE task_id = ? ORDER BY ord`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskAttachment
	for rows.Next() {
		var t TaskAttachment
		if err := rows.Scan(&t.ID, &t.TaskID, &t.Ord, &t.MimeType, &t.FilePath, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (a *App) DeleteTaskAttachment(id int64) error {
	var path string
	if err := a.db.QueryRow(`SELECT file_path FROM task_attachments WHERE id = ?`, id).Scan(&path); err == nil {
		_ = os.Remove(path)
	}
	_, err := a.db.Exec(`DELETE FROM task_attachments WHERE id = ?`, id)
	return err
}

func (a *App) ReadTaskAttachmentBase64(id int64) (string, error) {
	var path string
	if err := a.db.QueryRow(`SELECT file_path FROM task_attachments WHERE id = ?`, id).Scan(&path); err != nil {
		return "", err
	}
	return a.ReadFileBase64(path)
}
