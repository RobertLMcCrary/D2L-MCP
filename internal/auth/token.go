package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/robertmccrary/d2l-mcp/internal/config"
	"github.com/robertmccrary/d2l-mcp/internal/d2l"
)

const (
	expectedIssuer   = "https://api.brightspace.com/auth"
	expectedAudience = "https://api.brightspace.com/auth/token"
)

type TokenFile struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"exp,omitempty"`
	Subject    string `json:"sub,omitempty"`
	Tenant     string `json:"tenant,omitempty"`
	CapturedAt int64  `json:"captured_at,omitempty"`
}

type Claims struct {
	Issuer   string          `json:"iss"`
	Audience json.RawMessage `json:"aud"`
	Expires  int64           `json:"exp"`
	Subject  string          `json:"sub"`
	Tenant   string          `json:"tenant"`
	TenantID string          `json:"tenantid"`
}

type Status struct {
	Configured bool      `json:"configured"`
	Valid      bool      `json:"valid"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	Subject    string    `json:"subject,omitempty"`
	Tenant     string    `json:"tenant,omitempty"`
	Source     string    `json:"source,omitempty"`
}

func Load(paths config.Paths) (string, Status, error) {
	data, err := os.ReadFile(paths.Token)

	var fileErr error

	if err == nil {
		var stored TokenFile

		if err := json.Unmarshal(data, &stored); err != nil {
			return "", Status{}, fmt.Errorf("parse token: %w", err)
		}

		status, validateErr := Validate(stored.Token)
		status.Source = paths.Token

		if validateErr == nil {
			return stored.Token, status, nil
		}

		fileErr = validateErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", Status{}, fmt.Errorf("read token: %w", err)
	}

	if raw := dotenvToken(".env"); raw != "" {
		status, validateErr := Validate(raw)
		status.Source = ".env"

		if validateErr == nil {
			return raw, status, nil
		}
	}

	if raw := os.Getenv("D2L_TOKEN"); raw != "" {
		status, validateErr := Validate(raw)
		status.Source = "environment"

		return raw, status, validateErr
	}

	if fileErr != nil {
		return "", Status{}, fileErr
	}

	return "", Status{}, &d2l.Error{
		Code:     d2l.ErrAuth,
		Message:  "No D2L token found.",
		NextStep: "Run: d2l-mcp login",
	}
}

func Validate(token string) (Status, error) {
	status := Status{Configured: token != ""}

	claims, err := ParseClaims(token)
	if err != nil {
		return status, &d2l.Error{
			Code:     d2l.ErrAuth,
			Message:  "Stored D2L token is malformed.",
			NextStep: "Run: d2l-mcp login",
			Cause:    err,
		}
	}

	status.ExpiresAt = time.Unix(claims.Expires, 0).UTC()
	status.Subject = claims.Subject
	status.Tenant = claims.Tenant

	if status.Tenant == "" {
		status.Tenant = claims.TenantID
	}

	if claims.Issuer != expectedIssuer || !audienceContains(claims.Audience, expectedAudience) {
		return status, &d2l.Error{
			Code:     d2l.ErrAuth,
			Message:  "Stored token is not a Brightspace API token.",
			NextStep: "Run: d2l-mcp login",
		}
	}

	if time.Now().Add(30 * time.Second).After(status.ExpiresAt) {
		return status, &d2l.Error{
			Code:     d2l.ErrAuth,
			Message:  "D2L token has expired.",
			NextStep: "Run: d2l-mcp login",
		}
	}

	status.Valid = true

	return status, nil
}

func ParseClaims(token string) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("JWT must have three parts")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, err
	}

	return claims, nil
}

func Save(paths config.Paths, token string) error {
	claims, err := ParseClaims(token)
	if err != nil {
		return err
	}
	if _, err := Validate(token); err != nil {
		return err
	}
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		return err
	}

	stored := TokenFile{
		Token:      token,
		ExpiresAt:  claims.Expires,
		Subject:    claims.Subject,
		Tenant:     statusTenant(claims),
		CapturedAt: time.Now().Unix(),
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	data = append(data, '\n')

	tmp, err := os.CreateTemp(paths.Dir, ".token-*")
	if err != nil {
		return err
	}

	name := tmp.Name()
	defer os.Remove(name)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(name, paths.Token)
}

func statusTenant(claims Claims) string {
	if claims.Tenant != "" {
		return claims.Tenant
	}
	return claims.TenantID
}

func dotenvToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")

		if found && key == "D2L_TOKEN" {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}

	return ""
}

func audienceContains(raw json.RawMessage, expected string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == expected
	}

	var multiple []string

	if json.Unmarshal(raw, &multiple) == nil {
		for _, value := range multiple {
			if value == expected {
				return true
			}
		}
	}

	return false
}
