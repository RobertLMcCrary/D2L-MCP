package d2l

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}

	return client.WithHTTPClient(server.Client())
}

func TestEnrollmentsPaginatesBookmarks(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("bookmark") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Items":      []any{map[string]any{"OrgUnit": map[string]any{"Id": 1}}},
				"PagingInfo": map[string]any{"HasMoreItems": true, "Bookmark": "next"},
			})

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"Items":      []any{map[string]any{"OrgUnit": map[string]any{"Id": 2}}},
			"PagingInfo": map[string]any{"HasMoreItems": false},
		})
	}))

	items, err := client.Enrollments(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
}

func TestRateLimitRetry(t *testing.T) {
	var calls atomic.Int32
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		_, _ = w.Write([]byte(`{"Identifier":"student"}`))
	}))

	if _, err := client.WhoAmI(t.Context()); err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 2 {
		t.Fatalf("got %d calls, want 2", calls.Load())
	}
}

func TestRetryHonorsCancellation(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.WhoAmI(ctx)

	if err == nil {
		t.Fatal("expected cancellation")
	}
}

func TestResponseCodesAreTyped(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))

	_, err := client.WhoAmI(t.Context())

	if got := AsError(err).Code; got != ErrForbidden {
		t.Fatalf("got %s, want %s", got, ErrForbidden)
	}
}
