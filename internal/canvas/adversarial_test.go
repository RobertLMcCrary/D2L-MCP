package canvas

import (
	"encoding/json"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommentAttachmentUsesActualCanvasRoute(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "comment.txt"), []byte("feedback"), 0o600); err != nil {
		t.Fatal(err)
	}
	var host string
	var calls atomic.Int32
	c, s := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		// Literal expectation from Submission Comments API, not UploadRoutes.
		case "/api/v1/courses/42/assignments/7/submissions/self/comments/files":
			json.NewEncoder(w).Encode(map[string]any{"upload_url": host + "/storage", "upload_params": map[string]string{"policy": "signed"}})
		case "/storage":
			if r.Header.Get("Authorization") != "" {
				t.Error("token leaked to storage")
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(201)
			io.WriteString(w, `{"id":99,"size":8}`)
		default:
			t.Errorf("wrong comment route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	host = s.URL
	if _, err := c.Upload(t.Context(), root, UploadInput{Path: "comment.txt", CourseID: "42", AssignmentID: "7", Comment: true}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("comment upload did not complete")
	}
}

func TestAutoGradedMissingAndReassignedWorkStayInDeadlines(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).UTC()
	future := time.Now().Add(24 * time.Hour).UTC()
	assignments := []Assignment{
		{Identity: Identity{ID: "1", Name: "Auto-zeroed missing"}, Availability: Availability{DueAt: &past}, Submission: &Submission{State: Graded, Missing: true}},
		{Identity: Identity{ID: "2", Name: "Reassigned"}, Availability: Availability{DueAt: &past}, Submission: &Submission{State: Graded, SubmittedAt: &past, RedoRequest: true}},
		{Identity: Identity{ID: "3", Name: "Complete"}, Availability: Availability{DueAt: &past}, Submission: &Submission{State: Graded, SubmittedAt: &past}},
		{Identity: Identity{ID: "4", Name: "Excused"}, Availability: Availability{DueAt: &past}, Submission: &Submission{State: Graded, Missing: true, Excused: true}},
		{Identity: Identity{ID: "5", Name: "Future reassignment"}, Availability: Availability{DueAt: &future}, Submission: &Submission{State: Submitted, SubmittedAt: &past, RedoRequest: true}},
	}
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(assignments) })
	overdue, err := c.Deadlines(t.Context(), []string{"42"}, 7, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue.Items) != 2 || overdue.Items[0].State != "missing" || overdue.Items[1].State != "reassigned" || !overdue.Items[1].RedoRequest {
		t.Fatalf("unfinished work hidden: %+v", overdue)
	}
	due, err := c.Deadlines(t.Context(), []string{"42"}, 7, false)
	if err != nil || len(due.Items) != 1 || due.Items[0].AssignmentID != "5" {
		t.Fatalf("future reassignment hidden: %+v %v", due, err)
	}
}

func TestSubmissionAttemptsMatchCanvasEffectiveLimit(t *testing.T) {
	for _, tc := range []struct {
		name, assignment string
		allowed          bool
	}{
		{"extra", `{"submission_types":["online_text_entry"],"allowed_attempts":1,"submission":{"attempt":1,"extra_attempts":1}}`, true},
		{"exhausted_extra", `{"submission_types":["online_text_entry"],"allowed_attempts":1,"submission":{"attempt":2,"extra_attempts":1}}`, false},
		{"unlimited", `{"submission_types":["online_text_entry"],"allowed_attempts":-1,"submission":{"attempt":50}}`, true},
		{"zero", `{"submission_types":["online_text_entry"],"allowed_attempts":0}`, false},
		{"redo_without_extra", `{"submission_types":["online_text_entry"],"allowed_attempts":1,"submission":{"attempt":1,"redo_request":true}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int32
			c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					io.WriteString(w, tc.assignment)
					return
				}
				writes.Add(1)
				io.WriteString(w, `{"workflow_state":"submitted"}`)
			})
			_, err := c.Submit(t.Context(), SubmissionInput{CourseID: "42", AssignmentID: "7", Type: TextEntry, Text: "user-selected work"})
			if (err == nil) != tc.allowed || int(writes.Load()) != map[bool]int{true: 1, false: 0}[tc.allowed] {
				t.Fatalf("incorrect attempt preflight %v writes=%d", err, writes.Load())
			}
		})
	}
}

func TestReadStateIncludesCannotMutateThroughReadTools(t *testing.T) {
	for _, writes := range []bool{false, true} {
		c, _ := fixture(t, writes, func(w http.ResponseWriter, r *http.Request) { t.Error("read-state mutation reached network") })
		for _, key := range []string{"include[]", "include", "include[0]"} {
			for _, value := range []string{"read_state", "read_status"} {
				_, err := c.Read(t.Context(), "get_submission", map[string]string{"course_id": "42", "assignment_id": "7"}, url.Values{key: {value}})
				if err == nil || lms.AsError(err).Code != "read_only_violation" {
					t.Fatalf("unsafe include accepted %s %s: %v", key, value, err)
				}
			}
		}
	}
	var host string
	var calls atomic.Int32
	c, s := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Link", "<"+host+"/api/v1/courses?page=2&include%5B%5D=read_state>; rel=next")
		io.WriteString(w, `[]`)
	})
	host = s.URL
	if _, err := c.Read(t.Context(), "list_courses", nil, nil); err == nil || calls.Load() != 1 {
		t.Fatalf("pagination bypassed read gate: %v calls=%d", err, calls.Load())
	}
}

func TestErrorDiagnosticsAreBoundedAndCredentialRedacted(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"Course progress requires module completion requirements"},"errors":[{"message":"credential test-secret must not leak"}]}`)
	})
	_, err := c.Read(t.Context(), "get_course_progress", map[string]string{"course_id": "42"}, nil)
	if err == nil {
		t.Fatal("expected feature error")
	}
	api := lms.AsError(err)
	if len(api.Details) != 2 || strings.Contains(err.Error(), "test-secret") || !strings.Contains(err.Error(), "module completion requirements") {
		t.Fatalf("unsafe or unhelpful diagnostics %+v", api)
	}
}

func TestDiscussionViewRequestsRecentUncachedEntries(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_new_entries") != "true" {
			t.Error("cached discussion can omit new posts")
		}
		io.WriteString(w, `{"view":[],"new_entries":[{"id":1,"message":"recent"}]}`)
	})
	r, err := c.Read(t.Context(), "get_discussion_view", map[string]string{"course_id": "42", "topic_id": "7"}, nil)
	if err != nil || !strings.Contains(string(r.Data), "recent") {
		t.Fatal("new entries unavailable")
	}
}

func TestAttributeValidationErrors(t *testing.T) {
	for _, body := range []string{
		`{"errors":{"name":[{"message":"cannot be blank"}]}}`,
		`{"errors":{"submission_type":["is invalid"]}}`,
		`{"name":["cannot be blank"],"private_record":"do not echo"}`,
	} {
		c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			io.WriteString(w, body)
		})
		_, err := c.Read(t.Context(), "get_user", nil, nil)
		api := lms.AsError(err)
		if len(api.Details) != 1 || strings.Contains(err.Error(), "do not echo") {
			t.Fatalf("validation error lost or unsafe: %+v", api)
		}
	}
}
