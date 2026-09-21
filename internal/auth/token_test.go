package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robertmccrary/d2l-mcp/internal/config"
)

func makeToken(t *testing.T, expires time.Time) string {
	t.Helper()

	header, _ := json.Marshal(map[string]any{"alg": "none"})
	payload, _ := json.Marshal(map[string]any{
		"iss":    expectedIssuer,
		"aud":    expectedAudience,
		"exp":    expires.Unix(),
		"sub":    "student",
		"tenant": "school",
	})

	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)

	return encodedHeader + "." + encodedPayload + ".signature"
}

func TestValidateAndSaveToken(t *testing.T) {
	token := makeToken(t, time.Now().Add(time.Hour))

	status, err := Validate(token)
	if err != nil {
		t.Fatal(err)
	}

	if !status.Valid || status.Subject != "student" {
		t.Fatalf("unexpected status: %+v", status)
	}

	dir := t.TempDir()
	paths := config.Paths{Dir: dir, Token: filepath.Join(dir, "token.json")}

	if err := Save(paths, token); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(paths.Token)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode is %o, want 600", info.Mode().Perm())
	}
}

func TestExpiredToken(t *testing.T) {
	if _, err := Validate(makeToken(t, time.Now().Add(-time.Minute))); err == nil {
		t.Fatal("expected expired token error")
	}
}

func TestLoadsUpstreamTokenFileFormat(t *testing.T) {
	token := makeToken(t, time.Now().Add(time.Hour))
	dir := t.TempDir()
	paths := config.Paths{Dir: dir, Token: filepath.Join(dir, "token.json")}

	data, _ := json.Marshal(map[string]any{
		"token":       token,
		"exp":         time.Now().Add(time.Hour).Unix(),
		"sub":         "student",
		"tenant":      "school",
		"captured_at": time.Now().Unix(),
	})

	if err := os.WriteFile(paths.Token, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, status, err := Load(paths)

	if err != nil || loaded != token || !status.Valid {
		t.Fatalf("load failed: status=%+v err=%v", status, err)
	}
}

func TestBrowserProfileLockHonorsCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browser.lock")

	release, err := acquireLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := acquireLock(ctx, path); err == nil {
		t.Fatal("expected lock acquisition to be cancelled")
	}
}
