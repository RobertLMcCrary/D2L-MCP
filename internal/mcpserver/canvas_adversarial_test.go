package mcpserver

import (
	"encoding/json"
	"github.com/mark3labs/mcp-go/mcp"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRawWireMutationNumbersAreForwardedExactly(t *testing.T) {
	c, _ := canvasMCP(t, true, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !strings.Contains(string(body), "9007199254740993") || !strings.Contains(string(body), "0.12345678901234567890123456789") || strings.Contains(string(body), "9007199254740992") {
			t.Errorf("numeric payload changed: %s", body)
		}
		var payload struct {
			Attempt   int             `json:"attempt"`
			Token     string          `json:"validation_token"`
			Extension json.RawMessage `json:"extension"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Attempt != 1 || payload.Token != "fixture-only" || string(payload.Extension) != `{"nested":[null,false,9007199254740993]}` {
			t.Errorf("typed or extensible body changed: %s", body)
		}
		io.WriteString(w, `{}`)
	})
	body := `{"attempt":1,"validation_token":"fixture-only","extension":{"nested":[null,false,9007199254740993]},"quiz_questions":[{"id":"1","answer":9007199254740993},{"id":"2","answer":0.12345678901234567890123456789}]}`
	for _, wire := range []string{
		`{"name":"lms_api_write","arguments":{"method":"POST","path":"/api/v1/quiz_submissions/7/questions","body":` + body + `}}`,
		`{"name":"canvas_write_operation","arguments":{"operation":"answer_quiz_questions","parameters":{"submission_id":"7"},"body":` + body + `}}`,
		`{"name":"canvas_answer_quiz_questions","arguments":{"parameters":{"submission_id":"7"},"body":` + body + `}}`,
	} {
		var request mcp.CallToolRequest
		if err := json.Unmarshal([]byte(`{"params":`+wire+`}`), &request); err != nil {
			t.Fatal(err)
		}
		result, err := c.CallTool(t.Context(), request)
		if err != nil || result.IsError {
			t.Fatalf("raw wire tool failed: %+v %v", result, err)
		}
	}
}

func TestCanvasDownloadsExposeAnMCPFileArtifact(t *testing.T) {
	var host string
	c, _ := canvasMCP(t, false, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/99" {
			json.NewEncoder(w).Encode(map[string]any{"id": 99, "display_name": "notes.txt", "size": 5, "content-type": "text/plain", "url": host + "/signed?verifier=opaque"})
			return
		}
		io.WriteString(w, "notes")
	})
	status := callCanvas(t, c, "lms_status", map[string]any{"network": false})
	var value struct {
		Host string `json:"host"`
	}
	json.Unmarshal(status, &value)
	host = value.Host
	r := mcp.CallToolRequest{}
	r.Params.Name = "canvas_download_file"
	r.Params.Arguments = map[string]any{"file_id": "99"}
	result, err := c.CallTool(t.Context(), r)
	if err != nil || result.IsError {
		t.Fatalf("download %v %+v", err, result)
	}
	links := 0
	for _, content := range result.Content {
		if link, ok := content.(mcp.ResourceLink); ok {
			links++
			if !strings.HasPrefix(link.URI, "file://") || link.MIMEType != "text/plain" || link.Name != "notes.txt" {
				t.Fatalf("invalid artifact %+v", link)
			}
		}
	}
	if links != 1 {
		t.Fatalf("download has %d artifact links", links)
	}
}
