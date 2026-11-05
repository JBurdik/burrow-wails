package main

import (
	"reflect"
	"testing"
)

func TestOpenInCommand(t *testing.T) {
	cases := []struct {
		target string
		line   int
		name   string
		args   []string
	}{
		{"finder", 3, "open", []string{"-R", "/p/a.go"}},
		{"/Applications/Zed.app", 0, "open", []string{"-a", "/Applications/Zed.app", "/p/a.go"}},
		{"/Applications/Zed.app", 3, "/Applications/Zed.app/Contents/MacOS/cli", []string{"/p/a.go:3"}},
		{"/Applications/Cursor.app", 3, "/Applications/Cursor.app/Contents/Resources/app/bin/cursor", []string{"--goto", "/p/a.go:3"}},
		{"/Applications/Visual Studio Code.app", 3, "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code", []string{"--goto", "/p/a.go:3"}},
		{"/Applications/IntelliJ IDEA CE.app", 3, "open", []string{"-na", "/Applications/IntelliJ IDEA CE.app", "--args", "--line", "3", "/p/a.go"}},
		{"/Applications/Warp.app", 3, "open", []string{"-a", "/Applications/Warp.app", "/p/a.go"}},
	}
	for _, c := range cases {
		name, args := openInCommand(c.target, "/p/a.go", c.line)
		if name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("%s:%d = %s %v, want %s %v", c.target, c.line, name, args, c.name, c.args)
		}
	}
}
