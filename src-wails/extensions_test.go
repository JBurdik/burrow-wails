package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReadExtensionManifest(t *testing.T) {
	dir := t.TempDir()
	valid := `{"apiVersion":1,"id":"hello-burrow","name":"Hello","version":"0.1.0","commands":[{"id":"greet","title":"Greet","command":"node","args":["index.mjs"]}],"surfaces":[{"id":"workspace-pulse","title":"Pulse","kind":"workspace-pulse"}],"settings":[{"id":"host-alias","title":"Host","type":"text","required":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := readExtensionManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "hello-burrow" || len(manifest.Commands) != 1 || len(manifest.Surfaces) != 1 || len(manifest.Settings) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestReadExtensionManifestRejectsPathExecutable(t *testing.T) {
	dir := t.TempDir()
	invalid := `{"apiVersion":1,"id":"hello-burrow","name":"Hello","version":"0.1.0","commands":[{"id":"greet","title":"Greet","command":"./run.sh"}]}`
	if err := os.WriteFile(filepath.Join(dir, "extension.json"), []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readExtensionManifest(dir); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExtractExtensionArchive(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "extension.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(archive)
	manifest, err := writer.Create("workspace-pulse/extension.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte(`{"apiVersion":1,"id":"workspace-pulse","name":"Workspace Pulse","version":"0.1.0"}`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "extension")
	if err := extractExtensionArchive(archivePath, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := readExtensionManifest(destination); err != nil {
		t.Fatalf("extracted manifest was invalid: %v", err)
	}
}

func TestExtensionBridgeWorkspaceCapability(t *testing.T) {
	bridge, err := startExtensionBridge(nil, ExtensionManifest{ID: "sftp-sync", Permissions: []string{"workspace.read"}}, "/project")
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()

	request, err := http.NewRequest(http.MethodPost, bridge.url+"/v1/workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+bridge.token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.StatusCode)
	}
	var payload map[string]string
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["cwd"] != "/project" {
		t.Fatalf("unexpected workspace payload: %#v", payload)
	}
}
