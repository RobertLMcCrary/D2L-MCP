package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	got, err := NormalizeHost("school.example.edu/d2l/home", false)
	if err != nil {
		t.Fatal(err)
	}

	if got != "https://school.example.edu" {
		t.Fatalf("got %q", got)
	}
	invalidHosts := []string{
		"http://school.example.edu",
		"https://user:pass@school.example.edu",
		"https://127.0.0.1",
	}

	for _, raw := range invalidHosts {
		if _, err := NormalizeHost(raw, false); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestSaveUsesPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{Dir: dir, Config: filepath.Join(dir, "config.json")}

	if err := Save(paths, Config{LMSHost: "https://school.example.edu"}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(paths.Config)
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode is %o, want 600", info.Mode().Perm())
	}
}
