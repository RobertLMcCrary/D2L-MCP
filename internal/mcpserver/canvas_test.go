package mcpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RobertLMcCrary/D2L-MCP/internal/app"
	"github.com/RobertLMcCrary/D2L-MCP/internal/canvas"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func canvasMCP(t *testing.T, writes bool, handler http.HandlerFunc, profiles ...string) (*mcpclient.Client, string) {
	t.Helper()
	t.Setenv("CANVAS_TOKEN", "test-secret")
	host := httptest.NewTLSServer(handler)
	t.Cleanup(host.Close)
	root := t.TempDir()
	service := &app.Service{Config: config.Config{Provider: "canvas", LMSHost: host.URL, EnableWrites: writes}, Paths: config.Paths{Dir: t.TempDir(), Downloads: root}, HTTPTransport: host.Client().Transport}
	if len(profiles) > 0 {
		service.Config.ToolProfile = profiles[0]
	}
	client, err := mcpclient.NewInProcessClient(New(service))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := mcp.InitializeRequest{}
	r.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	r.Params.ClientInfo = mcp.Implementation{Name: "canvas-test", Version: "1"}
	result, err := client.Initialize(t.Context(), r)
	if err != nil || result.ServerInfo.Name != "Canvas LMS" {
		t.Fatalf("init %+v %v", result, err)
	}
	return client, root
}

func TestCompactCatalogKeepsEveryOperationReachable(t *testing.T) {
	c, _ := canvasMCP(t, true, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/self/colors" {
			t.Errorf("wrong compact route %s", r.URL.Path)
		}
		io.WriteString(w, `{"custom_colors":{"course_42":"#aabbcc"}}`)
	}, "compact")
	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) > 25 {
		t.Fatalf("compact catalog has %d tools", len(tools.Tools))
	}
	result := callCanvas(t, c, "canvas_search_operations", map[string]any{"query": "color"})
	var search CanvasSearchResult
	if err := json.Unmarshal(result, &search); err != nil {
		t.Fatal(err)
	}
	if search.Total < 2 {
		t.Fatal("read/write discovery incomplete")
	}
	read := callCanvas(t, c, "canvas_read_operation", map[string]any{"operation": "get_colors"})
	if !strings.Contains(string(read), "#aabbcc") {
		t.Fatal("compact invocation lost response")
	}
	for offset, total := 0, len(canvas.Operations)+len(canvas.Mutations); offset < total; offset += 50 {
		r := callCanvas(t, c, "canvas_search_operations", map[string]any{"offset": offset, "limit": 50})
		var page CanvasSearchResult
		json.Unmarshal(r, &page)
		if page.Total != total || len(page.Operations) == 0 {
			t.Fatal("catalog pagination incomplete")
		}
	}
}

func callCanvas(t *testing.T, c *mcpclient.Client, name string, args map[string]any) json.RawMessage {
	t.Helper()
	r := mcp.CallToolRequest{}
	r.Params.Name = name
	r.Params.Arguments = args
	result, err := c.CallTool(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s: %+v", name, result.Content)
	}
	var output struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &output); err != nil || !output.OK {
		t.Fatalf("invalid output %s %v", raw, err)
	}
	return output.Data
}

func TestCanvasToolSchemasAndWriteVisibility(t *testing.T) {
	for _, writes := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[writes], func(t *testing.T) {
			c, _ := canvasMCP(t, writes, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) })
			catalog, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, tool := range catalog.Tools {
				seen[tool.Name] = true
				if strings.HasPrefix(tool.Name, "d2l_") {
					t.Fatal("Canvas advertises Brightspace tools")
				}
				if !toolNamePattern.MatchString(tool.Name) {
					t.Fatal("invalid name")
				}
				assertClientSchema(t, tool.Name+".input", mcp.ToolArgumentsSchema(tool.InputSchema))
				assertClientSchema(t, tool.Name+".output", mcp.ToolArgumentsSchema(tool.OutputSchema))
				if tool.Name == "canvas_get_submission" {
					params := tool.InputSchema.Properties["parameters"].(map[string]any)
					encoded, _ := json.Marshal(params)
					if !strings.Contains(string(encoded), "assignment_id") || !strings.Contains(string(encoded), "required") {
						t.Fatal("missing required endpoint schema")
					}
				}
			}
			if seen["canvas_submit_assignment"] != writes || seen["lms_api_write"] != writes || seen["canvas_update_settings"] != writes {
				t.Fatal("writes exposed contrary to configuration")
			}
			for _, op := range canvas.Operations {
				if !seen["canvas_"+op.Name] {
					t.Fatalf("missing read %s", op.Name)
				}
			}
			if writes {
				for _, op := range canvas.Mutations {
					if !seen["canvas_"+op.Name] {
						t.Fatalf("missing mutation %s", op.Name)
					}
				}
			}
			resources, err := c.ListResources(t.Context(), mcp.ListResourcesRequest{})
			if err != nil || len(resources.Resources) != 2 {
				t.Fatalf("resources %v %v", resources, err)
			}
			for _, resource := range resources.Resources {
				r := mcp.ReadResourceRequest{}
				r.Params.URI = resource.URI
				if _, err := c.ReadResource(t.Context(), r); err != nil {
					t.Fatal(err)
				}
			}
			prompts, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
			if err != nil || len(prompts.Prompts) != 3 {
				t.Fatal("missing Canvas prompts")
			}
		})
	}
}

func TestCanvasMCPUploadSubmitFeedbackSettingsEndToEnd(t *testing.T) {
	var origin string
	var mu sync.Mutex
	submitted := false
	settings := false
	c, root := canvasMCP(t, true, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/users/self/profile":
			io.WriteString(w, `{"id":1,"name":"Student"}`)
		case "/api/v1/courses/42/assignments/7":
			io.WriteString(w, `{"id":7,"name":"Essay","submission_types":["online_upload"],"allowed_attempts":2,"submission":{"attempt":0}}`)
		case "/api/v1/courses/42/assignments/7/submissions/self/files":
			json.NewEncoder(w).Encode(map[string]any{"upload_url": origin + "/storage", "upload_params": map[string]string{"policy": "opaque"}})
		case "/storage":
			if r.Header.Get("Authorization") != "" {
				t.Error("credential leak")
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			defer r.MultipartForm.RemoveAll()
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				content, _ := io.ReadAll(f)
				f.Close()
				if string(content) != "Essay body" {
					t.Error("wrong essay")
				}
			}
			w.Header().Set("Location", origin+"/api/v1/files/99/create_success")
			w.WriteHeader(303)
		case "/api/v1/files/99/create_success":
			io.WriteString(w, `{"id":99,"size":10}`)
		case "/api/v1/courses/42/assignments/7/submissions":
			if r.Method != "POST" {
				t.Error("wrong submit method")
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			ids := body["submission"].(map[string]any)["file_ids"].([]any)
			if len(ids) != 1 || ids[0] != "99" {
				t.Error("wrong file ID")
			}
			submitted = true
			io.WriteString(w, `{"id":1,"workflow_state":"submitted","attempt":1}`)
		case "/api/v1/courses/42/assignments/7/submissions/self":
			if !submitted {
				t.Error("not submitted")
			}
			io.WriteString(w, `{"id":1,"workflow_state":"submitted","attachments":[{"id":99}],"submission_comments":[{"id":2,"comment":"Received"}],"rubric_assessment":{"clarity":{"points":5}}}`)
		case "/api/v1/users/self/settings":
			if r.Method == "PUT" {
				var body canvas.UserSettings
				json.NewDecoder(r.Body).Decode(&body)
				if body.ManualMarkAsRead == nil {
					t.Error("settings body omitted")
				}
				settings = *body.ManualMarkAsRead
			}
			json.NewEncoder(w).Encode(map[string]bool{"manual_mark_as_read": settings})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}) // Origin is exposed by status; no credentials are returned.
	data := callCanvas(t, c, "lms_status", map[string]any{"network": false})
	var status app.ProviderStatus
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	origin = status.Host
	if err := os.WriteFile(filepath.Join(root, "essay.txt"), []byte("Essay body"), 0o600); err != nil {
		t.Fatal(err)
	}
	callCanvas(t, c, "canvas_whoami", nil)
	upload := callCanvas(t, c, "canvas_upload_file", map[string]any{"path": "essay.txt", "course_id": "42", "assignment_id": "7"})
	if !strings.Contains(string(upload), "99") {
		t.Fatal("upload returned no file")
	}
	callCanvas(t, c, "canvas_submit_assignment", map[string]any{"course_id": "42", "assignment_id": "7", "type": "online_upload", "file_ids": []string{"99"}})
	feedback := callCanvas(t, c, "canvas_get_submission", map[string]any{"parameters": map[string]string{"course_id": "42", "assignment_id": "7"}})
	if !strings.Contains(string(feedback), "Received") || !strings.Contains(string(feedback), "rubric_assessment") {
		t.Fatal("feedback lost")
	}
	callCanvas(t, c, "canvas_update_settings", map[string]any{"body": map[string]any{"manual_mark_as_read": true}})
	read := callCanvas(t, c, "canvas_get_settings", nil)
	if !strings.Contains(string(read), "true") {
		t.Fatal("settings write not visible")
	}
}
