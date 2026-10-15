package d2l

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	"github.com/RobertLMcCrary/D2L-MCP/internal/lms"
)

func le(path string) string { return "/d2l/api/le/" + config.LEVersion + path }

func writeClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	return testClient(t, handler).WithWrites(true)
}

func TestWritesDisabledSendsNothing(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent with writes disabled: %s %s", r.Method, r.URL.Path)
	}))

	err := client.Write(t.Context(), "POST", le("/1/discussions/forums/1/topics/1/posts/"), map[string]any{}, nil)
	if AsError(err).Code != "writes_disabled" {
		t.Fatalf("err = %v, want writes_disabled", err)
	}

	if _, err := client.Submit(t.Context(), Submission{CourseID: 1, FolderID: 1, Files: []string{"a.txt"}}); AsError(err).Code != "writes_disabled" {
		t.Fatalf("submit err = %v, want writes_disabled", err)
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	var calls atomic.Int32
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))

	if err := client.Write(t.Context(), "POST", le("/1/x"), map[string]any{"a": 1}, nil); err == nil {
		t.Fatal("rate-limited write reported success")
	}
	if calls.Load() != 1 {
		t.Fatalf("write sent %d times, want exactly 1", calls.Load())
	}
}

func TestWriteRedirectIsNotFollowed(t *testing.T) {
	var calls atomic.Int32
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, le("/1/elsewhere"), http.StatusTemporaryRedirect)
	}))

	if err := client.Write(t.Context(), "POST", le("/1/x"), map[string]any{}, nil); err == nil {
		t.Fatal("redirected write reported success")
	}
	if calls.Load() != 1 {
		t.Fatalf("redirect was followed: %d requests", calls.Load())
	}
}

func TestWriteErrorKeepsBrightspaceDetail(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`Anonymous posts are not allowed`))
	}))

	err := AsError(client.Write(t.Context(), "POST", le("/1/x"), map[string]any{}, nil))
	if len(err.Details) != 1 || err.Details[0] != "Anonymous posts are not allowed" {
		t.Fatalf("details = %v, want the server's message", err.Details)
	}
}

type capturedPart struct {
	header http.Header
	body   string
}

// readMixed parses a request the way Brightspace does: multipart/mixed with
// an exact Content-Length and the JSON block first.
func readMixed(t *testing.T, r *http.Request) []capturedPart {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("Content-Type = %q, want multipart/mixed", r.Header.Get("Content-Type"))
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.ContentLength != int64(len(raw)) {
		t.Fatalf("Content-Length = %d, body is %d bytes", r.ContentLength, len(raw))
	}

	var parts []capturedPart
	reader := multipart.NewReader(strings.NewReader(string(raw)), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(part)
		parts = append(parts, capturedPart{header: http.Header(part.Header), body: string(body)})
	}
}

func managedFile(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSubmitSendsDocumentedMultipartAndVerifies(t *testing.T) {
	root := managedFile(t, "lab4.sql", "SELECT 1;\n")
	due := FormatTime(time.Now().Add(-time.Hour))
	var posted atomic.Bool

	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == le("/7/dropbox/folders/9"):
			_, _ = w.Write([]byte(`{"Id":9,"Name":"Lab 4","SubmissionType":0,"GroupTypeId":null,"DueDate":"` + due + `"}`))
		case r.Method == "POST" && r.URL.Path == le("/7/dropbox/folders/9/submissions/mysubmissions/"):
			posted.Store(true)
			parts := readMixed(t, r)
			if len(parts) != 2 {
				t.Fatalf("got %d parts, want JSON then file", len(parts))
			}
			if parts[0].header.Get("Content-Type") != "application/json" {
				t.Errorf("first part Content-Type = %q", parts[0].header.Get("Content-Type"))
			}
			var comment map[string]any
			if err := json.Unmarshal([]byte(parts[0].body), &comment); err != nil || comment["Text"] != "see attached" || comment["Html"] != nil {
				t.Errorf("comment block = %s, want RichText {Text, Html:null}", parts[0].body)
			}
			if got := parts[1].header.Get("Content-Disposition"); got != `form-data; name=""; filename="lab4.sql"` {
				t.Errorf("file Content-Disposition = %q", got)
			}
			if parts[1].body != "SELECT 1;\n" {
				t.Errorf("file body = %q", parts[1].body)
			}
		case r.Method == "GET" && r.URL.Path == le("/7/dropbox/folders/9/submissions/mysubmissions/"):
			_, _ = w.Write([]byte(`[{"Status":1,"Submissions":[{"Id":1}]}]`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))

	result, err := client.Submit(t.Context(), Submission{
		CourseID: 7, FolderID: 9, Comment: "see attached", Files: []string{"lab4.sql"}, ManagedRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !posted.Load() || !result.Late || result.Verification == nil || result.Folder != "Lab 4" {
		t.Fatalf("result = %+v; want posted, late, verified", result)
	}
}

func TestSubmitRejectsBeforeUploading(t *testing.T) {
	root := managedFile(t, "a.txt", "x")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		folder string
		in     Submission
	}{
		{"text-only folder", `{"SubmissionType":1}`, Submission{Files: []string{"a.txt"}}},
		{"on-paper folder", `{"SubmissionType":2}`, Submission{Files: []string{"a.txt"}}},
		{"hidden folder", `{"SubmissionType":0,"IsHidden":true}`, Submission{Files: []string{"a.txt"}}},
		{"group folder without group", `{"SubmissionType":0,"GroupTypeId":3}`, Submission{Files: []string{"a.txt"}}},
		{"individual folder with group", `{"SubmissionType":0,"GroupTypeId":null}`, Submission{GroupID: 4, Files: []string{"a.txt"}}},
		{"no files", `{"SubmissionType":0}`, Submission{}},
		{"traversal", `{"SubmissionType":0}`, Submission{Files: []string{"../secret.txt"}}},
		{"absolute path", `{"SubmissionType":0}`, Submission{Files: []string{outside}}},
		{"symlink escape", `{"SubmissionType":0}`, Submission{Files: []string{"link.txt"}}},
		{"directory", `{"SubmissionType":0}`, Submission{Files: []string{"."}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("%s sent a %s", tc.name, r.Method)
					return
				}
				_, _ = w.Write([]byte(tc.folder))
			}))
			in := tc.in
			in.CourseID, in.FolderID, in.ManagedRoot = 7, 9, root
			if _, err := client.Submit(t.Context(), in); err == nil {
				t.Fatal("submission was accepted")
			}
		})
	}
}

func TestGroupSubmitUsesGroupRoute(t *testing.T) {
	root := managedFile(t, "a.txt", "x")
	var posted atomic.Bool
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			if r.URL.Path != le("/7/dropbox/folders/9/submissions/group/4/") {
				t.Errorf("POST %s, want the group route", r.URL.Path)
			}
			posted.Store(true)
		case strings.HasSuffix(r.URL.Path, "/mysubmissions/"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`{"SubmissionType":4,"GroupTypeId":3}`))
		}
	}))

	if _, err := client.Submit(t.Context(), Submission{CourseID: 7, FolderID: 9, GroupID: 4, Files: []string{"a.txt"}, ManagedRoot: root}); err != nil {
		t.Fatal(err)
	}
	if !posted.Load() {
		t.Fatal("group submission not sent")
	}
}

func TestReplyWithoutSubjectReusesParent(t *testing.T) {
	topic := PostTarget{CourseID: 7, ForumID: 2, TopicID: 3}
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"PostId":5,"Subject":"Normalization question"}`))
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("post without files Content-Type = %q, want JSON", r.Header.Get("Content-Type"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		message, _ := body["Message"].(map[string]any)
		if body["Subject"] != "Re: Normalization question" || body["ParentPostId"] != float64(5) || message["Type"] != "Text" || message["Content"] != "3NF here" {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"PostId":6}`))
	}))

	parent := int64(5)
	post, err := client.CreatePost(t.Context(), topic, NewPost{ParentPostID: &parent, Message: "3NF here"})
	if err != nil || post["PostId"] != float64(6) {
		t.Fatalf("post = %v, err = %v", post, err)
	}
}

func TestNewThreadNeedsSubject(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("request sent for an invalid thread")
	}))

	if _, err := client.CreatePost(t.Context(), PostTarget{}, NewPost{Message: "hi"}); err == nil {
		t.Fatal("thread without subject accepted")
	}
}

func TestPostAttachmentsUseNumberedParts(t *testing.T) {
	root := managedFile(t, "er.png", "png")
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := readMixed(t, r)
		if len(parts) != 2 || !strings.Contains(parts[1].header.Get("Content-Disposition"), `name="file 0"`) {
			t.Errorf("parts = %+v", parts)
		}
		if parts[1].header.Get("Content-Type") != "image/png" {
			t.Errorf("attachment Content-Type = %q", parts[1].header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"PostId":1}`))
	}))

	_, err := client.CreatePost(t.Context(), PostTarget{CourseID: 1, ForumID: 1, TopicID: 1},
		NewPost{Subject: "ER diagram", Message: "attached", Files: []string{"er.png"}, ManagedRoot: root})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePostSendsSinglePartMultipart(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != le("/1/discussions/forums/2/topics/3/posts/4") {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if parts := readMixed(t, r); len(parts) != 1 || !strings.Contains(parts[0].body, `"Subject":"Fixed"`) {
			t.Errorf("parts = %+v", parts)
		}
		_, _ = w.Write([]byte(`{"PostId":4}`))
	}))

	if _, err := client.UpdatePost(t.Context(), PostTarget{CourseID: 1, ForumID: 2, TopicID: 3}, 4, "Fixed", "body", false); err != nil {
		t.Fatal(err)
	}
}

func TestSetPostStateReportsPartialProgress(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Rating/MyRating") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"IsRead":true}`))
	}))

	read, rating := true, 4
	applied, err := client.SetPostState(t.Context(), PostTarget{CourseID: 1, ForumID: 2, TopicID: 3}, 4, PostState{Read: &read, Rating: &rating})
	if err != nil {
		t.Fatal(err)
	}
	if applied["read"] == nil || !strings.Contains(applied["error"].(string), "rating failed") {
		t.Fatalf("applied = %v; want read applied and rating failure reported", applied)
	}
}

func TestUpdateProgressPreservesOtherFields(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/d2l/api/le/unstable/7/content/userprogress/") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method == "GET" {
			if r.URL.Query().Get("userId") != "11" {
				t.Errorf("userId = %q", r.URL.Query().Get("userId"))
			}
			_, _ = w.Write([]byte(`{"ObjectId":5,"UserId":11,"TotalTime":300,"NumVisits":2,"IsRead":false,"Completed":false,"CompletedDate":null}`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["TotalTime"] != float64(300) || body["NumVisits"] != float64(2) || body["Completed"] != true || body["CompletedDate"] == nil || body["IsRead"] != false {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"ObjectId":5,"Completed":true}`))
	}))

	completed := true
	if _, err := client.UpdateProgress(t.Context(), 7, 5, 11, nil, &completed); err != nil {
		t.Fatal(err)
	}
}

func TestGenericWriteThroughProvider(t *testing.T) {
	client := writeClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || string(body) != `{"IsFlagged":true}` {
			t.Errorf("%s %s", r.Method, body)
		}
		_, _ = w.Write([]byte(`{"IsFlagged":true}`))
	}))

	resp, err := client.Do(t.Context(), lmsRequest("PUT", le("/1/discussions/forums/2/topics/3/posts/4/Flag"), `{"IsFlagged":true}`))
	if err != nil || string(resp.Data) != `{"IsFlagged":true}` {
		t.Fatalf("resp = %s, err = %v", resp.Data, err)
	}
}

func lmsRequest(method, path, body string) lms.Request {
	return lms.Request{Method: method, Path: path, Body: json.RawMessage(body)}
}
