package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
)

// App is the Wails-bound struct exposing methods to the frontend, replacing
// the #[tauri::command] surface in src-tauri/src/lib.rs.
type App struct {
	ctx context.Context
	db  *sql.DB
	pty *PtyManager
}

func NewApp() *App {
	return &App{pty: NewPtyManager()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	dataDir, err := appDataDir()
	if err != nil {
		log.Printf("app data dir: %v", err)
		return
	}
	db, err := openDB(dataDir)
	if err != nil {
		log.Printf("open db: %v", err)
		return
	}
	a.db = db
}

func appDataDir() (string, error) {
	base, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Library", "Application Support", "burrow-wails"), nil
}

// --- PTY bindings ---

func (a *App) CreatePty(shell string, args []string, cwd string, env []string) (string, error) {
	return a.pty.CreatePty(a.ctx, shell, args, cwd, env)
}

func (a *App) WritePty(id string, data string) error {
	return a.pty.Write(id, data)
}

func (a *App) ResizePty(id string, cols, rows uint16) error {
	return a.pty.Resize(id, cols, rows)
}

func (a *App) KillPty(id string) error {
	return a.pty.Kill(id)
}

func (a *App) ListPtySessions() []string {
	return a.pty.List()
}
