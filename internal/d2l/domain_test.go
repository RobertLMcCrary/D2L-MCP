package d2l

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolverParity(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Items": []any{
				map[string]any{
					"OrgUnit": map[string]any{
						"Id":   10,
						"Code": "CO.CS1302",
						"Name": "Data Structures Section 04",
						"Type": map[string]any{"Name": "Course Offering"},
					},
				},
				map[string]any{
					"OrgUnit": map[string]any{
						"Id":   20,
						"Code": "CO.MATH2202",
						"Name": "Calculus II",
						"Type": map[string]any{"Name": "Course Offering"},
					},
				},
			},
			"PagingInfo": map[string]any{"HasMoreItems": false},
		})
	}))

	resolver := NewResolver(client)

	for _, query := range []string{"10", "CO.CS1302", "data structures", "calculus"} {
		course, err := resolver.Resolve(t.Context(), query)
		if err != nil {
			t.Fatalf("resolve %q: %v", query, err)
		}
		if course.ID == 0 {
			t.Fatalf("resolve %q returned empty course", query)
		}
	}
}

func TestReadableHTMLRemovesExecutableContent(t *testing.T) {
	source := `<html>
		<style>secret css</style>
		<body>
			<h1>Course</h1>
			<p>Hello <b>student</b></p>
			<script>ignore()</script>
		</body>
	</html>`

	text, err := readableHTML(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}

	if text != "Course\nHello student" {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestSyllabusIdentityAndPath(t *testing.T) {
	course := Course{Name: "CS 3305 - Fall Semester 2026", Code: "CO.430.CS3305.10931.20264"}
	term, subject, number := courseIdentity(course)

	wrongIdentity := term != "Fall Semester 2026" ||
		subject != "CS" ||
		number != "3305" ||
		extractCRN(course.Code) != "10931"

	if wrongIdentity {
		t.Fatalf("unexpected identity: %q %q %q", term, subject, number)
	}

	item := map[string]any{
		"syllabus_id": 42,
		"term_name":   term,
		"title":       "CS3305 10931",
		"sub_title":   "Section 04",
	}

	path := documentPath(item)

	if !strings.HasPrefix(path, "42/") {
		t.Fatalf("unexpected path %q", path)
	}
}

func TestFilenameAndDestinationSafety(t *testing.T) {
	if got := safeFilename(`../../secret.txt`, "fallback"); got != "secret.txt" {
		t.Fatalf("got %q", got)
	}

	root := t.TempDir()

	if _, err := secureDestination(root, root, "../escape"); err == nil {
		t.Fatal("expected unsafe destination error")
	}

	if _, err := secureDestination(root, filepath.Join(root, "ok"), "file.pdf"); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadRootRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")

	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(parent, "link")

	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := secureMkdirAll(link, filepath.Join(link, "files")); err == nil {
		t.Fatal("expected symlink root rejection")
	}
}

func TestAssignmentDownloadStreamsWithoutOverwrite(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/dropbox/folders/") {
			assignment := map[string]any{
				"Id":   7,
				"Name": "Starter Files",
				"Attachments": []any{
					map[string]any{
						"FileId":   8,
						"FileName": "../../main.go",
					},
				},
			}

			_ = json.NewEncoder(w).Encode([]any{assignment})

			return
		}

		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("package main\n"))
	}))

	root := t.TempDir()
	downloader := NewDownloader(client, root)
	course := Course{ID: 1, Name: "Data Structures"}

	files, err := downloader.Assignment(t.Context(), course, "starter")
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 || files[0].Name != "main.go" {
		t.Fatalf("unexpected files: %+v", files)
	}

	data, err := os.ReadFile(files[0].Path)
	if err != nil || string(data) != "package main\n" {
		t.Fatalf("unexpected downloaded content %q: %v", data, err)
	}

	if _, err := downloader.Assignment(t.Context(), course, "starter"); err == nil {
		t.Fatal("expected no-overwrite error")
	}
}

func FuzzSafeFilename(f *testing.F) {
	for _, seed := range []string{"notes.pdf", "../../token.json", `..\\token`, "\x00bad", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		got := safeFilename(value, "fallback")
		if got == "" || got != filepath.Base(got) || strings.Contains(got, "/") || strings.Contains(got, "\\") {
			t.Fatalf("unsafe result %q", got)
		}
	})
}
