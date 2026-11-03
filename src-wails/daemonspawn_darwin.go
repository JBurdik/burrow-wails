//go:build darwin

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// spawnDetached starts the daemon through a throwaway launchd job. Anything
// Burrow.app spawns or loads stays its "subordinate": a posix_spawned child
// shares the app's coalition, and a job the app bootstraps is listed under
// its loadedJobs. Since the daemon outlives the app on purpose, the Dock kept
// the icon with "running in background" after ⌘Q. So the job is only a
// launcher — sh backgrounds the daemon and exits — and is booted out as soon
// as the socket is up; AbandonProcessGroup keeps launchd from reaping the
// daemon with it. Falls back to a plain child if launchctl refuses.
func spawnDetached(path string, env []string, logPath, sockPath string) error {
	label := "dev.burrow.daemon." + filepath.Base(filepath.Dir(logPath))
	plist := filepath.Join(filepath.Dir(logPath), "daemon-launch.plist")
	if err := os.WriteFile(plist, daemonPlist(label, path, env, logPath), 0o600); err != nil {
		return spawnChildLogged(path, env, logPath)
	}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	_ = exec.Command("launchctl", "bootout", target).Run() // a leftover from a crashed run
	if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist).CombinedOutput(); err != nil {
		log.Printf("daemon: launchctl bootstrap: %v %s; spawning as a child", err, out)
		return spawnChildLogged(path, env, logPath)
	}
	go func() {
		for i := 0; i < 50; i++ { // the socket exists ⇒ sh has forked the daemon
			if _, err := os.Stat(sockPath); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = exec.Command("launchctl", "bootout", target).Run()
		_ = os.Remove(plist)
	}()
	return nil
}

func spawnChildLogged(path string, env []string, logPath string) error {
	f, err := openLogFile(logPath)
	if err != nil {
		return spawnChild(path, env, nil)
	}
	defer f.Close() // the child keeps its own fd
	return spawnChild(path, env, f)
}

func daemonPlist(label, path string, env []string, logPath string) []byte {
	var b bytes.Buffer
	str := func(s string) {
		b.WriteString("<string>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</string>")
	}
	key := func(s string) {
		b.WriteString("<key>")
		_ = xml.EscapeText(&b, []byte(s))
		b.WriteString("</key>")
	}
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>`)
	key("Label")
	str(label)
	key("ProgramArguments")
	b.WriteString("<array>")
	str("/bin/sh")
	str("-c")
	str(`"$0" </dev/null >>"$1" 2>&1 &`)
	str(path)
	str(logPath)
	b.WriteString("</array>")
	key("RunAtLoad")
	b.WriteString("<true/>")
	key("AbandonProcessGroup")
	b.WriteString("<true/>")
	key("ProcessType")
	b.WriteString("<string>Interactive</string>")
	key("EnvironmentVariables")
	b.WriteString("<dict>")
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			key(k)
			str(v)
		}
	}
	b.WriteString("</dict></dict></plist>")
	return b.Bytes()
}
