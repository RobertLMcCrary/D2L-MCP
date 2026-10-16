package app

import (
	"os"
	"regexp"
	"strings"
)

// WriteAccess tells an agent whether writes are on and the exact command
// that toggles them for this installation, so it can offer to run it when
// the user asks for a write. The command is built from the running binary
// and state directory because agents rarely know either.
type WriteAccess struct {
	Enabled        bool   `json:"enabled"`
	EnableCommand  string `json:"enable_command,omitempty"`
	DisableCommand string `json:"disable_command,omitempty"`
	Note           string `json:"note"`
}

func (s *Service) WriteAccess() WriteAccess {
	prefix := setupCommand()
	access := WriteAccess{Enabled: s.Config.EnableWrites}
	if access.Enabled {
		access.DisableCommand = prefix + " --enable-writes=false"
		access.Note = "Writes are enabled. Act only on the user's explicit request for each write."
		return access
	}
	access.EnableCommand = prefix + " --enable-writes"
	access.Note = "Writes are off. Only if the user asks, run enable_command (or ask the user to), then reconnect " +
		"this MCP server (for example /mcp in Claude Code) so the write tools load."
	return access
}

func setupCommand() string {
	binary, err := os.Executable()
	if err != nil {
		binary = "d2l-mcp"
	}
	command := shellQuote(binary) + " setup"
	if dir := os.Getenv("LMS_STATE_DIR"); dir != "" {
		command = "LMS_STATE_DIR=" + shellQuote(dir) + " " + command
	}
	return command
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(value string) string {
	if shellSafe.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
