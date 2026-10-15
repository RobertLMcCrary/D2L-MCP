package app

import (
	"strings"
	"testing"

	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
)

func TestWriteAccessReportsRunnableCommands(t *testing.T) {
	t.Setenv("LMS_STATE_DIR", "/Users/me/My State")

	off := (&Service{}).WriteAccess()
	if off.Enabled || off.DisableCommand != "" {
		t.Fatalf("disabled access = %+v", off)
	}
	if !strings.HasPrefix(off.EnableCommand, "LMS_STATE_DIR='/Users/me/My State' ") ||
		!strings.HasSuffix(off.EnableCommand, " setup --enable-writes") {
		t.Fatalf("enable command = %q; want the state dir quoted and the setup flag", off.EnableCommand)
	}
	if !strings.Contains(off.Note, "reconnect") {
		t.Fatalf("note %q does not mention reconnecting", off.Note)
	}

	on := (&Service{Config: config.Config{EnableWrites: true}}).WriteAccess()
	if !on.Enabled || on.EnableCommand != "" || !strings.HasSuffix(on.DisableCommand, " setup --enable-writes=false") {
		t.Fatalf("enabled access = %+v", on)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/d2l-mcp":   "/usr/local/bin/d2l-mcp",
		"/Users/me/My Apps/d2l":    "'/Users/me/My Apps/d2l'",
		"/tmp/it's/d2l":            `'/tmp/it'\''s/d2l'`,
		"/tmp/$(rm -rf ~)/d2l-mcp": "'/tmp/$(rm -rf ~)/d2l-mcp'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
