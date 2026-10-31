package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSetMergesOneKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &App{}
	path, _ := configFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"a":1,"b":{"x":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfigSet("a", json.RawMessage(`2`)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["a"] != float64(2) || m["b"] == nil {
		t.Fatalf("got %s, want a=2 and b kept", b)
	}
	os.WriteFile(path, []byte(`{broken`), 0o644)
	if err := a.ConfigSet("a", json.RawMessage(`3`)); err == nil {
		t.Fatal("ConfigSet must refuse to overwrite an unreadable config.json")
	}
}
