package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"github.com/RobertLMcCrary/D2L-MCP/internal/state"
)

// Canvas tokens are opaque, not Brightspace JWTs. Stored tokens are bound to
// their origin so switching schools cannot send an old token to a new school.
type canvasToken struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

func canvasTokenPath(paths config.Paths) string {
	if paths.CanvasToken != "" {
		return paths.CanvasToken
	}
	return filepath.Join(paths.Dir, "canvas-token.json")
}

func LoadCanvas(paths config.Paths, host string) (string, error) {
	if token := os.Getenv("CANVAS_TOKEN"); token != "" {
		return token, nil
	}
	data, err := os.ReadFile(canvasTokenPath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no Canvas token; set CANVAS_TOKEN or run login --token-stdin")
	}
	if err != nil {
		return "", err
	}
	var saved canvasToken
	if err := json.Unmarshal(data, &saved); err != nil {
		return "", fmt.Errorf("invalid Canvas token file")
	}
	if saved.Host != host {
		return "", fmt.Errorf("Canvas token belongs to a different host; authenticate for the configured host")
	}
	if strings.TrimSpace(saved.Token) == "" {
		return "", fmt.Errorf("empty Canvas token")
	}
	return saved.Token, nil
}

func SaveCanvas(paths config.Paths, host, token string) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("invalid Canvas token")
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(canvasToken{Host: host, Token: token})
	if err != nil {
		return err
	}
	return state.WriteFile(canvasTokenPath(paths), data, 0o600)
}
