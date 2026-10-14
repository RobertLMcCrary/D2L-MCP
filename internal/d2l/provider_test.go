package d2l

import (
	"context"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"net/http"
	"testing"
)

type forbiddenTransport struct{ t *testing.T }

func (f forbiddenTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	f.t.Fatal("credential request reached another origin")
	return nil, nil
}

func TestGenericD2LReadCannotEscapeValencePaths(t *testing.T) {
	c, err := NewClient("https://school.example", "secret")
	if err != nil {
		t.Fatal(err)
	}
	c.http.Transport = forbiddenTransport{t}
	for _, path := range []string{"/d2l/api/../../logout", "/d2l/api/%2e%2e/logout", "/d2l/api/lp/1.47/users/whoami?token=secret", "/d2l/api/lp\\escape"} {
		if _, err := c.Do(t.Context(), lms.Request{Path: path}); err == nil {
			t.Fatalf("unsafe path accepted %s", path)
		}
	}
}

func TestBearerTransportNeverLeaksAcrossPortsOrProtocols(t *testing.T) {
	tx := &authTransport{base: forbiddenTransport{t}, origin: "https://school.example", token: "secret"}
	for _, url := range []string{"https://school.example:8443/d2l/api/lp/1.47/users/whoami", "http://school.example/d2l/api/lp/1.47/users/whoami", "https://other.example/d2l/api/lp/1.47/users/whoami"} {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
		if _, err := tx.RoundTrip(req); err == nil {
			t.Fatalf("origin escape allowed %s", url)
		}
	}
}
