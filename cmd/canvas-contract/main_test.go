package main

import "testing"

func TestMatchesPublishedRoutes(t *testing.T) {
	for _, tt := range []struct {
		actual, published string
		want              bool
	}{
		{"/api/v1/conversations/{conversation_id}", "/api/v1/conversations/:id", true},
		{"/api/v1/conversations/42", "/api/v1/conversations/:id", true},
		{"/api/v1/conversations/{conversation_id}", "/api/v1/conversations/batches", false},
		{"/api/v1/files/42", "/api/v1/files/:id.json", false},
		{"/api/v1/files/", "/api/v1/files/:id", false},
		{"/api/v1/files/42/extra", "/api/v1/files/:id", false},
	} {
		if got := matches(tt.actual, tt.published); got != tt.want {
			t.Errorf("matches(%q, %q) = %v; want %v", tt.actual, tt.published, got, tt.want)
		}
	}
}
