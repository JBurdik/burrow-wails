// burrow-mcp is the stdio MCP server binary — spawned by an agent client's
// MCP config (e.g. Claude Code's ~/.claude.json mcpServers block) to give
// it board/spawn tools without going through the app's own IPC.
package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"burrow/internal/mcpserver"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("home dir: %v", err)
	}
	dataDir := filepath.Join(home, "Library", "Application Support", "burrow-wails")

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "workspaces.db"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	srv := mcpserver.New(db, filepath.Join(dataDir, "sessions"))
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("mcp serve: %v", err)
	}
}
