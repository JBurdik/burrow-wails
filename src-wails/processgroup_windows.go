//go:build windows

package main

import "os/exec"

func configureAgentProcessGroup(*exec.Cmd) {}

func killAgentProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
