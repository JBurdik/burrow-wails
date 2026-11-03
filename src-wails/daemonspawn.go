package main

import (
	"os"
	"os/exec"
)

func spawnChild(path string, env []string, out *os.File) error {
	cmd := exec.Command(path)
	cmd.Env = env
	if out != nil {
		cmd.Stdout, cmd.Stderr = out, out
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it if it dies while we run
	return nil
}
