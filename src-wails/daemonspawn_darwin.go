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
//
// name tells jobs apart ("daemon", "core"); readyPath is a file the process
// creates or rewrites once it is up (its socket / port file).
func spawnDetached(name string, argv, env []string, logPath, readyPath string) error {
	label := "dev.burrow." + name + "." + filepath.Base(filepath.Dir(logPath))
	plist := filepath.Join(filepath.Dir(logPath), name+"-launch.plist")
	if err := os.WriteFile(plist, launcherPlist(label, argv, env, logPath), 0o600); err != nil {
		return spawnChildLogged(argv, env, logPath)
	}
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	_ = exec.Command("launchctl", "bootout", target).Run() // a leftover from a crashed run
	if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist).CombinedOutput(); err != nil {
		log.Printf("%s: launchctl bootstrap: %v %s; spawning as a child", name, err, out)
		return spawnChildLogged(argv, env, logPath)
	}
	started := time.Now().Add(-time.Second) // mtime granularity
	go func() {
		for i := 0; i < 100; i++ { // readyPath (re)written ⇒ sh has forked the process
			if fi, err := os.Stat(readyPath); err == nil && fi.ModTime().After(started) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = exec.Command("launchctl", "bootout", target).Run()
		_ = os.Remove(plist)
	}()
	return nil
}

func spawnChildLogged(argv, env []string, logPath string) error {
	f, err := openLogFile(logPath)
	if err != nil {
		return spawnChild(argv, env, nil)
	}
	defer f.Close() // the child keeps its own fd
	return spawnChild(argv, env, f)
}

func launcherPlist(label string, argv, env []string, logPath string) []byte {
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
	// $0 = log, "$@" = argv: background it and exit, so the job is a launcher.
	str(`log=$0; "$@" </dev/null >>"$log" 2>&1 &`)
	str(logPath)
	for _, a := range argv {
		str(a)
	}
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
