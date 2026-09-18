package main

import (
	"reflect"
	"testing"
)

func TestParseDevListeners(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []devListener
	}{
		{
			name: "single pid, single port",
			out:  "p123\nn127.0.0.1:5173\n",
			want: []devListener{{pid: 123, port: "5173", addr: "127.0.0.1:5173"}},
		},
		{
			name: "same pid, same port on v4 and v6 fds dedupes to one row",
			out:  "p123\nn127.0.0.1:5173\nn[::1]:5173\n",
			want: []devListener{{pid: 123, port: "5173", addr: "127.0.0.1:5173"}},
		},
		{
			name: "two pids, distinct ports",
			out:  "p123\nn127.0.0.1:5173\np456\nn*:8080\n",
			want: []devListener{
				{pid: 123, port: "5173", addr: "127.0.0.1:5173"},
				{pid: 456, port: "8080", addr: "*:8080"},
			},
		},
		{
			name: "empty output",
			out:  "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDevListeners(tt.out)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDevListeners(%q) = %+v, want %+v", tt.out, got, tt.want)
			}
		})
	}
}

func TestParseDevCwds(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want map[int]string
	}{
		{
			name: "two pids",
			out:  "p123\nn/Users/jirka/code/burrow\np456\nn/tmp\n",
			want: map[int]string{123: "/Users/jirka/code/burrow", 456: "/tmp"},
		},
		{
			name: "empty output",
			out:  "",
			want: map[int]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDevCwds(tt.out)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDevCwds(%q) = %+v, want %+v", tt.out, got, tt.want)
			}
		})
	}
}

func TestUnderWorkspace(t *testing.T) {
	tests := []struct {
		name          string
		cwd           string
		workspacePath string
		want          bool
	}{
		{"exact match", "/a/b", "/a/b", true},
		{"nested under workspace", "/a/b/pkg", "/a/b", true},
		{"sibling with shared prefix is not under it", "/a/bc", "/a/b", false},
		{"unrelated path", "/x/y", "/a/b", false},
		{"trailing slash on workspace normalizes", "/a/b/pkg", "/a/b/", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := underWorkspace(tt.cwd, tt.workspacePath); got != tt.want {
				t.Errorf("underWorkspace(%q, %q) = %v, want %v", tt.cwd, tt.workspacePath, got, tt.want)
			}
		})
	}
}

func TestIsBurrowCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		selfExeBase string
		want        bool
	}{
		{"daemon binary", "/Applications/Burrow.app/Contents/MacOS/burrow-daemon", "Burrow", true},
		{"mcp sidecar", "/Applications/Burrow.app/Contents/MacOS/burrow-mcp", "Burrow", true},
		{"wails dev run", "wails dev", "Burrow", true},
		{"self exe", "/Applications/Burrow.app/Contents/MacOS/Burrow", "Burrow", true},
		{"ordinary dev server", "node /home/user/project/node_modules/.bin/vite", "Burrow", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBurrowCommand(tt.command, tt.selfExeBase); got != tt.want {
				t.Errorf("isBurrowCommand(%q, %q) = %v, want %v", tt.command, tt.selfExeBase, got, tt.want)
			}
		})
	}
}
