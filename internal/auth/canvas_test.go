package auth

import (
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanvasOpaqueTokenHostBindingAndPermissions(t *testing.T) {
	t.Setenv("CANVAS_TOKEN", "")
	paths := config.Paths{Dir: filepath.Join(t.TempDir(), "state")}
	host := "https://school.example"
	if err := SaveCanvas(paths, host, "opaque-not-a-jwt\n"); err != nil {
		t.Fatal(err)
	}
	token, err := LoadCanvas(paths, host)
	if err != nil || token != "opaque-not-a-jwt" {
		t.Fatalf("load %q %v", token, err)
	}
	if _, err := LoadCanvas(paths, "https://other-school.example"); err == nil {
		t.Fatal("token escaped host binding")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(canvasTokenPath(paths))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatal("unsafe token permissions")
		}
	}
	t.Setenv("CANVAS_TOKEN", "explicit-env-token")
	token, err = LoadCanvas(paths, host)
	if err != nil || token != "explicit-env-token" {
		t.Fatal("environment override ignored")
	}
}
