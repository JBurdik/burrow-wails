package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentIDIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := environmentID(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first == "" {
		t.Fatal("empty id")
	}
	second, err := environmentID(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Fatalf("id changed across calls: %q != %q", first, second)
	}
}

func TestEnvironmentIDDiffersPerDir(t *testing.T) {
	a, _ := environmentID(t.TempDir())
	b, _ := environmentID(t.TempDir())
	if a == b {
		t.Fatal("two installs share an id")
	}
}

func TestEnvironmentIDRegeneratesOnCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := environmentID(dir)
	if err != nil {
		t.Fatalf("corrupt file should not be fatal: %v", err)
	}
	if id == "" {
		t.Fatal("empty id after regeneration")
	}
	again, _ := environmentID(dir)
	if id != again {
		t.Fatal("regenerated id was not persisted")
	}
}
