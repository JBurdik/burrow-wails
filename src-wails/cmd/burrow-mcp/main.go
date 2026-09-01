// burrow-mcp is the stdio MCP server an agent client spawns to get Burrow's
// control verbs as tools. It holds no state and no database: it forwards every
// call to the running app's loopback control API, reading the port and token the
// app publishes in its data dir (the same two files the `burrow` CLI reads).
package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"burrow/internal/mcpserver"
)

func main() {
	home := os.Getenv("BURROW_HOME_DIR")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("home dir: %v", err)
		}
		home = filepath.Join(userHome, "Library", "Application Support", "burrow-wails")
	}

	port, err := readPort(filepath.Join(home, "hook.port"))
	if err != nil {
		// Exiting is better than serving tools that all fail: the client reports
		// the server as unavailable instead of the model retrying broken calls.
		log.Fatalf("Burrow does not appear to be running (%s): %v", home, err)
	}
	token, err := os.ReadFile(filepath.Join(home, "control.token"))
	if err != nil {
		log.Fatalf("read control token: %v", err)
	}

	cwd := os.Getenv("BURROW_CWD")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	srv := mcpserver.New(port, strings.TrimSpace(string(token)), cwd, version())
	if err := srv.Serve(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("mcp serve: %v", err)
	}
}

// readPort reads hook.port, whose first line is the port (the second is the
// owning pid, which only the app's reclaim loop cares about).
func readPort(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	first := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	return strconv.Atoi(first)
}

// version is set at build time via -ldflags; "dev" when built plainly.
var buildVersion = "dev"

func version() string { return buildVersion }
