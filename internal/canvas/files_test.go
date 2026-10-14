package canvas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadCompletesProtocolWithoutLeakingCredentials(t *testing.T) {
	for _, completion := range []int{201, 301, 302, 303} {
		t.Run(fmt.Sprint(completion), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "essay.txt"), []byte("My essay"), 0o600); err != nil {
				t.Fatal(err)
			}
			var origin string
			stages := []string{}
			c, s := fixture(t, true, func(w http.ResponseWriter, r *http.Request) {
				stages = append(stages, r.URL.Path)
				switch r.URL.Path {
				case "/api/v1/courses/42/assignments/7/submissions/self/files":
					if r.Header.Get("Authorization") == "" {
						t.Error("ticket unauthenticated")
					}
					var params map[string]any
					json.NewDecoder(r.Body).Decode(&params)
					if params["on_duplicate"] != "rename" || params["size"] != float64(8) {
						t.Error("wrong upload metadata")
					}
					json.NewEncoder(w).Encode(map[string]any{"upload_url": origin + "/storage", "upload_params": map[string]any{"key": "opaque", "policy": "signed", "expires": 123}})
				case "/storage":
					if r.Header.Get("Authorization") != "" {
						t.Error("Canvas token leaked to storage")
					}
					if r.ContentLength <= 8 {
						t.Error("streaming body has no known length")
					}
					reader, err := r.MultipartReader()
					if err != nil {
						t.Fatal(err)
					}
					names := []string{}
					for {
						part, err := reader.NextPart()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						names = append(names, part.FormName())
						content, _ := io.ReadAll(part)
						if part.FormName() == "file" && string(content) != "My essay" {
							t.Error("file content lost")
						}
						if part.FormName() == "expires" && string(content) != "123" {
							t.Error("scalar upload param changed")
						}
					}
					if len(names) != 4 || names[len(names)-1] != "file" {
						t.Errorf("file was not last: %v", names)
					}
					w.Header().Set("Location", origin+"/api/v1/files/99/create_success?uuid=opaque")
					w.WriteHeader(completion)
				case "/api/v1/files/99/create_success":
					if r.Method != "GET" || r.Header.Get("Authorization") == "" {
						t.Error("upload finalization wrong")
					}
					io.WriteString(w, `{"id":99,"display_name":"essay.txt","size":8}`)
				default:
					t.Errorf("unexpected upload stage %s", r.URL.Path)
				}
			})
			origin = s.URL
			r, err := c.Upload(t.Context(), root, UploadInput{Path: "essay.txt", CourseID: "42", AssignmentID: "7"})
			if err != nil || !strings.Contains(string(r.Data), "99") || len(stages) != 3 {
				t.Fatalf("upload %s stages %v err %v", r.Data, stages, err)
			}
		})
	}
}

func TestUploadRejectsLocalTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip(err)
	}
	c, _ := fixture(t, true, func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe local upload reached network") })
	for _, path := range []string{"../secret.txt", filepath.Join(outside, "secret.txt"), "escape/secret.txt"} {
		if _, err := c.Upload(t.Context(), root, UploadInput{Path: path}); err == nil {
			t.Fatalf("escape accepted %q", path)
		}
	}
}

func TestDownloadNeverOverwritesAndSanitizesNames(t *testing.T) {
	var origin string
	c, s := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/99" {
			json.NewEncoder(w).Encode(map[string]any{"id": 99, "display_name": "../../notes.txt", "size": 7, "url": origin + "/signed?verifier=opaque"})
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("token leaked to signed download")
		}
		io.WriteString(w, "content")
	})
	origin = s.URL
	root := t.TempDir()
	a, err := c.DownloadFile(t.Context(), root, "99")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.DownloadFile(t.Context(), root, "99")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "notes.txt" || b.Name != "notes-1.txt" || a.Path == b.Path || a.Size != 7 {
		t.Fatalf("bad downloads %+v %+v", a, b)
	}
	content, err := os.ReadFile(a.Path)
	if err != nil || string(content) != "content" {
		t.Fatal("file corrupted")
	}
}

func TestDownloadLockedAndOversizedMetadata(t *testing.T) {
	for _, body := range []string{`{"id":99,"locked_for_user":true}`, `{"id":99,"hidden_for_user":true}`, `{"id":99,"size":268435457}`} {
		c, _ := fixture(t, false, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/files/99" {
				t.Error("restricted file downloaded")
			}
			io.WriteString(w, body)
		})
		if _, err := c.DownloadFile(t.Context(), t.TempDir(), "99"); err == nil {
			t.Fatal("restricted download accepted")
		}
	}
}

func TestExternalDestinationsRejectPrivateRanges(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1", "10.1.2.3", "169.254.169.254", "100.100.100.200", "::ffff:127.0.0.1", "192.0.2.1"} {
		if publicIP(netip.MustParseAddr(address)) {
			t.Fatalf("unsafe IP accepted %s", address)
		}
	}
	for _, raw := range []string{"http://storage.example/", "https://user:pass@storage.example/", "https://storage.example/#x"} {
		if _, err := fileURL(raw); err == nil {
			t.Fatal("unsafe storage URL accepted")
		}
	}
}

func TestPortableFilenamesAndIncompleteUploadResponse(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"../notes.txt", "notes.txt"}, {"CON.txt", "_CON.txt"}, {"LPT1", "_LPT1"}, {"...", ""}, {"a\\b.txt", "b.txt"}} {
		if got := safeName(tc.input); got != tc.want {
			t.Fatalf("%q -> %q expected %q", tc.input, got, tc.want)
		}
	}
	for _, raw := range []string{`null`, `{}`, `{"workflow_state":"pending"}`} {
		if _, err := completedUpload(json.RawMessage(raw)); err == nil {
			t.Fatalf("incomplete upload accepted %s", raw)
		}
	}
}
