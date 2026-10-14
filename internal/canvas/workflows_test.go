package canvas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeadlinesRespectStudentSubmissionState(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	assignments := fmt.Sprintf(`[
	{"id":1,"name":"missing","due_at":%q,"submission":{"workflow_state":"unsubmitted","missing":true}},
	{"id":2,"name":"late but submitted","due_at":%q,"submission":{"workflow_state":"submitted","late":true,"submitted_at":%q}},
	{"id":3,"name":"excused","due_at":%q,"submission":{"excused":true}},
	{"id":4,"name":"graded","due_at":%q,"submission":{"workflow_state":"graded"}},
	{"id":5,"name":"soon","due_at":%q,"submission":{"workflow_state":"unsubmitted"}},
	{"id":6,"name":"undated","submission":{"workflow_state":"unsubmitted"}},
	{"id":7,"name":"unknown","due_at":%q}
	]`, past, past, past, past, past, future, future)
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/courses" {
			io.WriteString(w, `[{"id":42}]`)
			return
		}
		if r.URL.Query().Get("include[]") != "submission" {
			t.Error("missing own submission include")
		}
		io.WriteString(w, assignments)
	})
	for _, overdue := range []bool{false, true} {
		report, err := c.Deadlines(t.Context(), nil, 7, overdue)
		if err != nil {
			t.Fatal(err)
		}
		want := "5"
		if overdue {
			want = "1"
		}
		if len(report.Items) != 1 || report.Items[0].AssignmentID != want || !report.Partial {
			t.Fatalf("bad deadline classification %+v", report)
		}
	}
}

func TestSnapshotPartialAndConcurrencyBound(t *testing.T) {
	var active, peak atomic.Int32
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		if strings.HasSuffix(r.URL.Path, "/quizzes") {
			w.WriteHeader(403)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/42") {
			io.WriteString(w, `{"id":42,"name":"Biology"}`)
		} else {
			io.WriteString(w, `[]`)
		}
	})
	r, err := c.Snapshot(t.Context(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Partial || len(r.Data) != 6 || r.Warnings["list_quizzes"] == "" || peak.Load() > 4 || peak.Load() < 2 {
		t.Fatalf("bad snapshot %+v peak %d", r, peak.Load())
	}
}

func TestCourseResolutionDoesNotGuess(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{"id":42,"name":"Biology A","course_code":"BIO"},{"id":"43","name":"Biology B","course_code":"BIO"}]`)
	})
	for _, query := range []string{"42", "Biology A"} {
		r, err := c.ResolveCourse(t.Context(), query)
		if err != nil || !strings.Contains(string(r), "Biology A") {
			t.Fatalf("resolution %s: %s %v", query, r, err)
		}
	}
	for _, query := range []string{"BIO", "Biology", "unknown", ""} {
		if _, err := c.ResolveCourse(t.Context(), query); err == nil {
			t.Fatalf("ambiguous or missing query accepted %q", query)
		}
	}
}

func TestSubmissionValidationAndNativePayload(t *testing.T) {
	var posts atomic.Int32
	c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			io.WriteString(w, `{"id":7,"submission_types":["online_upload","online_text_entry","online_url"],"allowed_attempts":2,"submission":{"attempt":1}}`)
			return
		}
		posts.Add(1)
		var body struct {
			Submission struct {
				Type    string   `json:"submission_type"`
				FileIDs []string `json:"file_ids"`
			} `json:"submission"`
			Comment struct {
				Text string `json:"text_comment"`
			} `json:"comment"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Submission.Type != "online_upload" || len(body.Submission.FileIDs) != 1 || body.Submission.FileIDs[0] != "9007199254740993" || body.Comment.Text != "Final version" {
			t.Error("native Canvas payload wrong")
		}
		io.WriteString(w, `{"id":"42","workflow_state":"submitted","attempt":2}`)
	})
	r, err := c.Submit(t.Context(), SubmissionInput{CourseID: "42", AssignmentID: "7", Type: FileUpload, FileIDs: []string{"9007199254740993"}, Comment: "Final version"})
	if err != nil || posts.Load() != 1 || !strings.Contains(string(r.Data), "submitted") {
		t.Fatalf("submit %s %v", r.Data, err)
	}
	for _, input := range []SubmissionInput{{CourseID: "42", AssignmentID: "7", Type: TextEntry}, {CourseID: "42", AssignmentID: "7", Type: FileUpload}, {CourseID: "42", AssignmentID: "7", Type: URLSubmission, URL: "javascript:alert(1)"}, {Type: "external_tool"}} {
		if _, err := c.Submit(t.Context(), input); err == nil {
			t.Fatal("invalid submission accepted")
		}
	}
	if posts.Load() != 1 {
		t.Fatal("invalid submissions wrote remote state")
	}
}

func TestSubmissionLocksTypesAndAttempts(t *testing.T) {
	for _, assignment := range []string{`{"locked_for_user":true,"submission_types":["online_text_entry"]}`, `{"submission_types":["external_tool"]}`, `{"submission_types":["online_text_entry"],"allowed_attempts":1,"submission":{"attempt":1}}`} {
		c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Error("ineligible submission reached mutation")
			}
			io.WriteString(w, assignment)
		})
		if _, err := c.Submit(t.Context(), SubmissionInput{CourseID: "42", AssignmentID: "7", Type: TextEntry, Text: "work"}); err == nil {
			t.Fatal("ineligible submission accepted")
		}
	}
}

func TestConversationReadNeverMarksRead(t *testing.T) {
	c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("auto_mark_as_read") != "false" {
			t.Error("read mutated inbox")
		}
		io.WriteString(w, `{"id":42,"messages":[]}`)
	})
	if _, err := c.Read(t.Context(), "get_conversation", map[string]string{"conversation_id": "42"}, map[string][]string{"auto_mark_as_read": {"true"}}); err != nil {
		t.Fatal(err)
	}
}

func TestNullableGradesAndSettingsUpdateSemantics(t *testing.T) {
	doc := `{"id":42,"assignment_id":7,"score":null,"grade":null,"rubric_assessment":{"criterion_1":{"points":0,"comments":"Review this"}},"submission_comments":[{"id":1,"comment":"feedback"}],"submission_history":[{"attempt":1}]}`
	var submission Submission
	if err := json.Unmarshal([]byte(doc), &submission); err != nil {
		t.Fatal(err)
	}
	if submission.Score != nil || submission.Grade != nil || submission.RubricAssessment["criterion_1"].Points == nil || *submission.RubricAssessment["criterion_1"].Points != 0 || len(submission.History) != 1 {
		t.Fatalf("typed feedback lost: %+v", submission)
	}
	f := false
	raw, err := json.Marshal(UserSettings{ManualMarkAsRead: &f})
	if err != nil || string(raw) != `{"manual_mark_as_read":false}` {
		t.Fatalf("false vs absent lost: %s %v", raw, err)
	}
}
