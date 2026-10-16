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
	t.Setenv("USERPROFILE", home)

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

func TestCanvasSetupAndOpaqueAuthenticationConfiguration(t *testing.T) {
	t.Setenv("LMS_STATE_DIR", t.TempDir())
	t.Setenv("CANVAS_HOST", "")
	t.Setenv("LMS_PROVIDER", "")
	if err := setup([]string{"--provider", "canvas", "--host", "school.instructure.com", "--enable-writes", "--tool-profile", "compact"}); err != nil {
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
	if cfg.Provider != "canvas" || cfg.LMSHost != "https://school.instructure.com" || !cfg.EnableWrites || cfg.ToolProfile != "compact" {
		t.Fatalf("bad Canvas config %+v", cfg)
	}
	if err := setup([]string{"--enable-writes=false"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(paths)
	if err != nil || cfg.EnableWrites {
		t.Fatal("write disable ignored")
	}
	if err := login(nil); err == nil {
		t.Fatal("Canvas attempted Brightspace browser login")
	}
}

func TestBrightspaceWritesSurviveOnlyExplicitPresetChanges(t *testing.T) {
	t.Setenv("LMS_STATE_DIR", t.TempDir())
	t.Setenv("CANVAS_HOST", "")
	t.Setenv("LMS_PROVIDER", "")
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	load := func() config.Config {
		t.Helper()
		cfg, err := config.Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	if err := setup([]string{"--school", "ksu", "--enable-writes"}); err != nil {
		t.Fatal(err)
	}
	if cfg := load(); cfg.Provider != "d2l" || !cfg.EnableWrites {
		t.Fatalf("explicit --enable-writes with a preset was dropped: %+v", cfg)
	}

	if err := setup([]string{"--school", "gsu"}); err != nil {
		t.Fatal(err)
	}
	if load().EnableWrites {
		t.Fatal("switching schools carried write access over")
	}
}
