package main

import (
	"reflect"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	out := "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /wt/feat\nHEAD def\nbranch refs/heads/feat/x\n\n" +
		"worktree /wt/det\nHEAD 123\ndetached\n\n" +
		"worktree /wt/gone\nHEAD 456\nbranch refs/heads/gone\nprunable gitdir file points to non-existent location\n"
	got := parseWorktreeList(out)
	want := []GitWorktree{{Path: "/wt/feat", Branch: "feat/x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
