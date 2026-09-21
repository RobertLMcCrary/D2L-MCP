package mcpserver

import (
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/RobertLMcCrary/D2L-MCP/internal/app"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
)

func initializedClient(t *testing.T) *mcpclient.Client {
	t.Helper()

	service := &app.Service{Paths: config.Paths{Dir: t.TempDir()}}

	client, err := mcpclient.NewInProcessClient(New(service))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = client.Close() })

	if err := client.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	request := mcp.InitializeRequest{}
	request.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	request.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1"}

	if _, err := client.Initialize(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	return client
}

func TestToolCatalogAndAnnotations(t *testing.T) {
	client := initializedClient(t)

	result, err := client.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Tools) < 18 {
		t.Fatalf("got %d tools, want at least 18", len(result.Tools))
	}

	for _, tool := range result.Tools {
		if tool.InputSchema.Type != "object" || tool.OutputSchema.Type != "object" {
			t.Fatalf("tool %s lacks typed schemas", tool.Name)
		}

		annotations := tool.Annotations
		missingAnnotation := annotations.ReadOnlyHint == nil ||
			annotations.DestructiveHint == nil ||
			annotations.IdempotentHint == nil ||
			annotations.OpenWorldHint == nil

		if missingAnnotation {
			t.Fatalf("tool %s lacks complete annotations", tool.Name)
		}
	}
}

func TestStatusReturnsStructuredResult(t *testing.T) {
	client := initializedClient(t)

	request := mcp.CallToolRequest{}
	request.Params.Name = "d2l_status"
	request.Params.Arguments = map[string]any{"network": false}

	result, err := client.CallTool(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	if result.StructuredContent == nil || result.IsError {
		t.Fatalf("unexpected status result: %+v", result)
	}
}

func TestGuidanceResourcesAndPrompts(t *testing.T) {
	client := initializedClient(t)

	resources, err := client.ListResources(t.Context(), mcp.ListResourcesRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(resources.Resources) != 4 {
		t.Fatalf("got %d resources, want 4", len(resources.Resources))
	}

	prompts, err := client.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(prompts.Prompts) != 3 {
		t.Fatalf("got %d prompts, want 3", len(prompts.Prompts))
	}
}
