//go:build !darwin

package main

func spawnDetached(_ string, argv, env []string, logPath, _ string) error {
	f, err := openLogFile(logPath)
	if err != nil {
		return spawnChild(argv, env, nil)
	}
	defer f.Close() // the child keeps its own fd
	return spawnChild(argv, env, f)
}
