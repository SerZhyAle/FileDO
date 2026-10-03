package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathOverrideSkipsLegacyByDefault(t *testing.T) {
	stateDir := t.TempDir()
	legacyDir := t.TempDir()
	t.Setenv(EnvOverride, stateDir)

	legacyFile := filepath.Join(legacyDir, "legacy.txt")
	if err := os.WriteFile(legacyFile, []byte("legacy data"), 0o644); err != nil {
		t.Fatal(err)
	}

	targetName := "file.txt"
	gotPath, err := Path(targetName, legacyFile)
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	expectedPath := filepath.Join(stateDir, targetName)
	if gotPath != expectedPath {
		t.Fatalf("got path %q, want %q", gotPath, expectedPath)
	}
	if _, err := os.Stat(gotPath); !os.IsNotExist(err) {
		t.Fatalf("target file %q should not exist on disk, err: %v", gotPath, err)
	}
	if data, err := os.ReadFile(legacyFile); err != nil || string(data) != "legacy data" {
		t.Fatalf("legacy file was modified or unreadable: %v, %s", err, string(data))
	}
}

func TestPathOverrideImportsLegacyWhenOptedIn(t *testing.T) {
	stateDir := t.TempDir()
	legacyDir := t.TempDir()
	t.Setenv(EnvOverride, stateDir)
	t.Setenv(EnvLegacy, "1")

	legacyFile := filepath.Join(legacyDir, "legacy.txt")
	if err := os.WriteFile(legacyFile, []byte("legacy data"), 0o644); err != nil {
		t.Fatal(err)
	}

	targetName := "file.txt"
	gotPath, err := Path(targetName, legacyFile)
	if err != nil {
		t.Fatalf("Path returned error: %v", err)
	}
	expectedPath := filepath.Join(stateDir, targetName)
	if gotPath != expectedPath {
		t.Fatalf("got path %q, want %q", gotPath, expectedPath)
	}
	importedData, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("target file %q was not created: %v", gotPath, err)
	}
	if string(importedData) != "legacy data" {
		t.Fatalf("imported data %q != want %q", string(importedData), "legacy data")
	}
	if data, err := os.ReadFile(legacyFile); err != nil || string(data) != "legacy data" {
		t.Fatalf("legacy file was modified or unreadable: %v, %s", err, string(data))
	}
}
