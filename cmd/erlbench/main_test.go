package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "experiment")
	if err := prepareOutput(dir); err != nil {
		t.Fatal(err)
	}
	// An explicitly created but still empty directory is also supported.
	if err := prepareOutput(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "results.jsonl")
	const previous = "previous measurements\n"
	if err := os.WriteFile(path, []byte(previous), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareOutput(dir); err == nil {
		t.Fatal("accepted a directory containing previous measurements")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != previous {
		t.Fatalf("previous evidence changed: %q, %v", got, err)
	}
	if err := prepareOutput(path); err == nil {
		t.Fatal("accepted a file as the output directory")
	}
}
