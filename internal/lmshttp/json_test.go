package lmshttp

import (
	"net/url"
	"strings"
	"testing"
)

func TestReadJSONEnforcesWholeDocumentAndSize(t *testing.T) {
	for _, tc := range []struct {
		body  string
		limit int64
		valid bool
	}{{`{"n":1}`, 7, true}, {`{} {}`, 10, false}, {`{"n":1}`, 6, false}, {"", 1, true}} {
		raw, err := ReadJSON(strings.NewReader(tc.body), tc.limit)
		if (err == nil) != tc.valid {
			t.Fatalf("%q %s: %v", tc.body, raw, err)
		}
	}
}

func TestSameOriginRejectsPortSchemeAndCredentials(t *testing.T) {
	a, _ := url.Parse("https://school.example:443")
	for _, raw := range []string{"https://school.example:8443", "http://school.example:443", "https://user:pass@school.example:443", "https://other.example:443"} {
		b, _ := url.Parse(raw)
		if SameOrigin(a, b) {
			t.Fatalf("different origin accepted %s", raw)
		}
	}
	b, _ := url.Parse("https://SCHOOL.example:443")
	if !SameOrigin(a, b) {
		t.Fatal("hostname case treated as origin change")
	}
}
