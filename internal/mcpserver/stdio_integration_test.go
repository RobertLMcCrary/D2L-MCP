package mcpserver

import (
	"os"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestStdioSubprocess(t *testing.T) {
	binary := os.Getenv("D2L_MCP_TEST_BINARY")
	if binary == "" {
		t.Skip("set D2L_MCP_TEST_BINARY to run subprocess protocol test")
	}

	env := isolatedStdioEnv(t)

	client, err := mcpclient.NewStdioMCPClient(binary, env, "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "stdio-test", Version: "1"}

	result, err := client.Initialize(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if result.ServerInfo.Name != "D2L Brightspace" {
		t.Fatalf("unexpected server %q", result.ServerInfo.Name)
	}

	tools, err := client.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(tools.Tools) < 18 {
		t.Fatalf("got %d tools", len(tools.Tools))
	}
}

func isolatedStdioEnv(t *testing.T) []string {
	t.Helper()
	env := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "LMS_STATE_DIR" || key == "LMS_PROVIDER" || strings.HasPrefix(key, "CANVAS_") || strings.HasPrefix(key, "D2L_") {
			continue
		}
		env = append(env, value)
	}
	return append(env, "LMS_STATE_DIR="+t.TempDir(), "D2L_NO_AUTO_LOGIN=1")
}

func TestCanvasStdioSubprocess(t *testing.T) {
	binary := os.Getenv("D2L_MCP_TEST_BINARY")
	if binary == "" {
		t.Skip("set D2L_MCP_TEST_BINARY to run subprocess protocol test")
	}
	client, err := mcpclient.NewStdioMCPClient(binary, append(isolatedStdioEnv(t), "CANVAS_HOST=https://school.example", "CANVAS_TOKEN=fixture-token"), "serve")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	r := mcp.InitializeRequest{}
	r.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	r.Params.ClientInfo = mcp.Implementation{Name: "canvas-stdio", Version: "1"}
	init, err := client.Initialize(t.Context(), r)
	if err != nil || init.ServerInfo.Name != "Canvas LMS" {
		t.Fatalf("init %+v %v", init, err)
	}
	tools, err := client.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil || len(tools.Tools) < 100 {
		t.Fatalf("tools %d %v", len(tools.Tools), err)
	}
	callCanvas(t, client, "lms_status", map[string]any{"network": false})
	callCanvas(t, client, "canvas_search_operations", map[string]any{"query": "settings"})
	resources, err := client.ListResources(t.Context(), mcp.ListResourcesRequest{})
	if err != nil || len(resources.Resources) != 2 {
		t.Fatal("missing stdio resources")
	}
}
