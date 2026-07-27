package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGitCloneArgsFollowRemoteHeadByDefault(t *testing.T) {
	got := gitCloneArgs("https://example.test/rtk.git", "", "/tmp/rtk")
	want := []string{"clone", "--depth", "1", "https://example.test/rtk.git", "/tmp/rtk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gitCloneArgs() = %q, want %q", got, want)
	}
}

func TestGitCloneArgsPreserveExplicitRef(t *testing.T) {
	got := gitCloneArgs("https://example.test/rtk.git", "v0.42.4", "/tmp/rtk")
	want := []string{"clone", "--depth", "1", "--branch", "v0.42.4", "https://example.test/rtk.git", "/tmp/rtk"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gitCloneArgs() = %q, want %q", got, want)
	}
}

func TestSiblingSpecNamesExcludeCuratedCatalogEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "js.json"), []byte(`[{"name":"biome","match":{"command":"biome"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rtk.json"), []byte(`[{"name":"upstream","match":{"command":"upstream"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err := siblingSpecNames(filepath.Join(dir, "rtk.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !names["biome"] || names["upstream"] {
		t.Fatalf("siblingSpecNames() = %v, want biome without generated output entries", names)
	}
}
