//go:build !darwin

package main

func spawnDetached(path string, env []string, logPath, _ string) error {
	f, err := openLogFile(logPath)
	if err != nil {
		return spawnChild(path, env, nil)
	}
	defer f.Close() // the child keeps its own fd
	return spawnChild(path, env, f)
}
