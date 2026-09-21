package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
)

func TestSetupWritesCompatibleConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	args := []string{
		"--host", "school.example.edu",
		"--syllabus-host", "syllabus.example.edu",
	}

	if err := setup(args); err != nil {
		t.Fatal(err)
	}

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.LMSHost != "https://school.example.edu" || cfg.SyllabusHost != "https://syllabus.example.edu" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	if _, err := os.Stat(filepath.Join(home, ".d2l", "config.json")); err != nil {
		t.Fatal(err)
	}
}
