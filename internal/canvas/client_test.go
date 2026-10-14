package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

func fixture(t *testing.T, writes bool, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	c, err := New(s.URL, "test-secret", writes, s.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func TestPaginationAndHeaders(t *testing.T) {
	var host string
	var calls atomic.Int32
	c, s := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("Accept") != "application/json+canvas-string-ids" {
			t.Error("missing safe authentication/ID headers")
		}
		if r.URL.Query().Get("cursor") == "opaque" {
			io.WriteString(w, `[{"id":"9007199254740994"}]`)
			return
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Error("missing page size")
		}
		w.Header().Add("Link", "<"+host+"/api/v1/courses?cursor=opaque>; rel=\"next\"; title=\"a,b\"")
		io.WriteString(w, `[{"id":"9007199254740993","extension":{"unknown":true}}]`)
	})
	host = s.URL
	query := url.Values{"include[]": {"total_scores"}}
	r, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses", Query: query, Paginate: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pages != 2 || calls.Load() != 2 || !strings.Contains(string(r.Data), "9007199254740993") || query.Has("per_page") {
		t.Fatalf("unexpected result %s (%d)", r.Data, r.Pages)
	}
}

func TestUnsafePathsAndOrigins(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe request reached network") })
	for _, path := range []string{"https://evil.test/api/v1/courses", "//evil.test/api/v1/courses", "/api/v1/../login", "/api/v1/%2e%2e/login", "/api/v1/%252e%252e/login", "/api/v1/files%2f1", "/login", "/api/v1/courses?access_token=secret", "/api/v1/courses#fragment", "/api/v1/courses\\secret"} {
		t.Run(path, func(t *testing.T) {
			if _, err := c.Do(t.Context(), lms.Request{Path: path}); err == nil {
				t.Fatal("unsafe path accepted")
			}
		})
	}
	if _, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses", Query: url.Values{"access_token": {"secret"}}}); err == nil {
		t.Fatal("query token accepted")
	}
}

func TestPaginationRejectsHostPortCyclesAndEnvelopes(t *testing.T) {
	for _, mode := range []string{"external", "port", "cycle", "envelope", "duplicate_next"} {
		t.Run(mode, func(t *testing.T) {
			var host string
			c, s := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
				next := "https://evil.example/api/v1/courses"
				if mode == "port" {
					next = "https://127.0.0.1:1/api/v1/courses"
				}
				if mode == "cycle" {
					next = host + r.URL.RequestURI()
				}
				if mode == "envelope" {
					io.WriteString(w, `{"quiz_submissions":[]}`)
					return
				}
				w.Header().Set("Link", "<"+next+">; rel=next")
				if mode == "duplicate_next" {
					w.Header().Add("Link", "<"+host+"/api/v1/courses?page=2>; rel=next")
				}
				io.WriteString(w, `[]`)
			})
			host = s.URL
			if _, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses", Paginate: true}); err == nil {
				t.Fatal("unsafe/invalid pagination accepted")
			}
		})
	}
}

func TestRetriesCancellationAndNoMutationReplay(t *testing.T) {
	var gets, posts atomic.Int32
	c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			w.WriteHeader(429)
			return
		}
		if gets.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, `[]`)
	})
	if _, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses"}); err != nil {
		t.Fatal(err)
	}
	if gets.Load() != 3 {
		t.Fatal("missing GET retries")
	}
	if _, err := c.Do(t.Context(), lms.Request{Method: "POST", Path: "/api/v1/conversations", Body: json.RawMessage(`{"body":"hello"}`)}); err == nil || posts.Load() != 1 {
		t.Fatal("mutation replayed or error lost")
	}
	c, _ = fixture(t, false, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Retry-After", "30"); w.WriteHeader(429) })
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := c.Do(ctx, lms.Request{Path: "/api/v1/courses"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestCanvas403BudgetThrottlingDiffersFromPermissionDenial(t *testing.T) {
	var calls atomic.Int32
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-Rate-Limit-Remaining", "-0.25")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(403)
			return
		}
		io.WriteString(w, `[]`)
	})
	if _, err := c.Read(t.Context(), "list_courses", nil, nil); err != nil || calls.Load() != 2 {
		t.Fatalf("budget throttle was not retried: %v", err)
	}
	calls.Store(0)
	c, _ = fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Rate-Limit-Remaining", "100")
		w.WriteHeader(403)
	})
	if _, err := c.Read(t.Context(), "list_courses", nil, nil); err == nil || calls.Load() != 1 {
		t.Fatal("permission denial retried")
	}
}

func TestWriteGateRedirectAndErrorRedaction(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://evil.test/")
		w.WriteHeader(302)
		io.WriteString(w, "test-secret sensitive-response")
	})
	if _, err := c.Do(t.Context(), lms.Request{Method: "POST", Path: "/api/v1/conversations"}); err == nil {
		t.Fatal("writes enabled implicitly")
	}
	_, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses"})
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "sensitive-response") {
		t.Fatalf("bad redirect error: %v", err)
	}
}

func TestEmptyMalformedAndOversizedJSON(t *testing.T) {
	for _, tc := range []struct {
		data string
		ok   bool
	}{{"", true}, {"null", true}, {"{\"a\":1}", true}, {"{} {}", false}, {"<html>login</html>", false}, {strings.Repeat(" ", maxJSON+1), false}} {
		_, err := readJSON(strings.NewReader(tc.data))
		if (err == nil) != tc.ok {
			t.Fatalf("length %d: %v", len(tc.data), err)
		}
	}
}

func TestIDAndLosslessDocuments(t *testing.T) {
	for _, raw := range []string{`9007199254740993`, `"9007199254740993"`, `"sis_course_id:biology"`, `null`} {
		var id ID
		if err := json.Unmarshal([]byte(raw), &id); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "9007") && id != "9007199254740993" {
			t.Fatal("ID precision lost")
		}
	}
	for _, raw := range []string{`1.5`, `1e6`, `-1`, `true`, `{}`} {
		var id ID
		if json.Unmarshal([]byte(raw), &id) == nil {
			t.Fatalf("invalid ID accepted: %s", raw)
		}
	}
	raw := json.RawMessage(`{"id":9007199254740993,"name":"Biology","future_field":{"key":true}}`)
	doc, err := lms.Decode[Course](raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(doc)
	if err != nil || string(encoded) != string(raw) || doc.Value.ID != "9007199254740993" {
		t.Fatal("document round-trip lost fields")
	}
}

func TestEveryCatalogOperation(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range Operations {
		t.Run(op.Name, func(t *testing.T) {
			if seen[op.Name] {
				t.Fatal("duplicate operation")
			}
			seen[op.Name] = true
			params := map[string]string{}
			for _, match := range placeholder.FindAllStringSubmatch(op.Path, -1) {
				params[match[1]] = "42"
			}
			r, err := op.Request(params, nil)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := fixture(t, false, func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != r.Path {
					t.Errorf("wrong path %s", req.URL.Path)
				}
				if key := EnvelopeCollections[op.Name]; key != "" {
					json.NewEncoder(w).Encode(map[string]any{key: []any{}})
				} else if op.List {
					io.WriteString(w, `[]`)
				} else {
					io.WriteString(w, `{"id":"42"}`)
				}
			})
			if _, err := c.Read(t.Context(), op.Name, params, nil); err != nil {
				t.Fatal(err)
			}
			for key := range params {
				bad := map[string]string{}
				for k, v := range params {
					bad[k] = v
				}
				bad[key] = "../escape"
				if _, err := op.Request(bad, nil); err == nil {
					t.Fatal("unsafe parameter accepted")
				}
			}
		})
	}
}

func TestEveryMutationUsesNativeJSON(t *testing.T) {
	for _, op := range Mutations {
		t.Run(op.Name, func(t *testing.T) {
			params := map[string]string{}
			for _, match := range placeholder.FindAllStringSubmatch(op.Path, -1) {
				params[match[1]] = "42"
			}
			var calls atomic.Int32
			c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != op.Method {
					t.Errorf("method %s", r.Method)
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["native"] != true {
					t.Error("body lost")
				}
				io.WriteString(w, `{}`)
			})
			if _, err := c.Mutate(t.Context(), op, params, map[string]any{"native": true}); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("write replayed")
			}
		})
	}
}

func FuzzAPIPath(f *testing.F) {
	for _, seed := range []string{"/api/v1/courses", "/api/v1/../login", "//evil.test", "/api/quiz/v1/courses/42/quizzes", "/api/v1/%252f"} {
		f.Add(seed)
	}
	c, err := New("https://school.example", "token", false, nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		u, err := c.apiURL(raw)
		if err == nil && (!c.sameOrigin(u) || (!strings.HasPrefix(u.Path, "/api/v1/") && !strings.HasPrefix(u.Path, "/api/quiz/v1/"))) {
			t.Fatalf("escaped origin: %v", u)
		}
	})
}

func ExampleSubmissionType() {
	fmt.Println(FileUpload) // Output: online_upload
}

func TestEnvelopePaginationAndTypedMetadata(t *testing.T) {
	var host string
	c, s := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			io.WriteString(w, `{"quiz_submissions":[{"id":"9007199254740994"}],"linked":{"users":[{"id":2}]},"future_field":"second"}`)
			return
		}
		w.Header().Set("Link", "<"+host+r.URL.Path+"?page=2>; rel=next")
		io.WriteString(w, `{"quiz_submissions":[{"id":9007199254740993}],"linked":{"users":[{"id":1}]},"future_field":"first"}`)
	})
	host = s.URL
	r, err := ReadAs[QuizSubmissions](t.Context(), c, "list_quiz_submissions", map[string]string{"course_id": "42", "quiz_id": "7"}, nil)
	if err != nil || r.Pages != 2 || len(r.Value.Submissions) != 2 || r.Value.Submissions[0].ID != "9007199254740993" || len(r.PageMetadata) != 2 || !strings.Contains(string(r.PageMetadata[1]), "second") {
		t.Fatalf("envelope pagination lost data: %+v %v", r, err)
	}
	page, err := c.Do(t.Context(), lms.Request{Path: "/api/v1/courses/42/quizzes/7/submissions"})
	if err != nil || page.Next == nil {
		t.Fatalf("no continuation: %+v %v", page, err)
	}
	second, err := c.Do(t.Context(), *page.Next)
	if err != nil || !strings.Contains(string(second.Data), "9007199254740994") {
		t.Fatalf("invalid continuation: %+v %v", second, err)
	}
}
