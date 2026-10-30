//go:build dev

package main

// `wails dev` compiles with the `dev` tag (its OutputType); a release build
// carries `production` instead, so this file only exists in a dev binary.
func init() {
	if buildChannel == "" {
		buildChannel = "dev"
	}
}
