package mcpserver

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/RobertLMcCrary/D2L-MCP/internal/app"
	"github.com/RobertLMcCrary/D2L-MCP/internal/config"
)

// toolNamePattern is the tool-name subset clients keep unchanged.
// Cursor rewrites other characters, and some clients reject them.
var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

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

	seen := map[string]bool{}

	for _, tool := range result.Tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %s", tool.Name)
		}
		seen[tool.Name] = true

		if !toolNamePattern.MatchString(tool.Name) {
			t.Errorf("tool name %q is outside the portable name set", tool.Name)
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %s has an empty description", tool.Name)
		}

		annotations := tool.Annotations
		missingAnnotation := annotations.ReadOnlyHint == nil ||
			annotations.DestructiveHint == nil ||
			annotations.IdempotentHint == nil ||
			annotations.OpenWorldHint == nil

		if missingAnnotation {
			t.Errorf("tool %s lacks complete annotations", tool.Name)
		}

		assertClientSchema(t, tool.Name+".input", mcp.ToolArgumentsSchema(tool.InputSchema))
		assertClientSchema(t, tool.Name+".output", mcp.ToolArgumentsSchema(tool.OutputSchema))
		assertOutputOmitsOptionalPayload(t, tool.Name, tool.OutputSchema)
	}

	if !seen["d2l_get_updates"] {
		t.Fatal("d2l_get_updates missing from catalog")
	}
}

func TestSchemaNormalizerRewritesBooleanSchemas(t *testing.T) {
	schema := mcp.ToolArgumentsSchema{
		Type: "object",
		Properties: map[string]any{
			"data": true,
			"items": map[string]any{
				"additionalProperties": true,
				"properties": map[string]any{
					"access": true,
				},
			},
		},
		AdditionalProperties: true,
	}

	normalizeToolSchema(&schema)

	if _, ok := schema.AdditionalProperties.(map[string]any); !ok {
		t.Fatalf("additionalProperties = %#v, want object schema", schema.AdditionalProperties)
	}
	if _, ok := schema.Properties["data"].(map[string]any); !ok {
		t.Fatalf("data schema = %#v, want object", schema.Properties["data"])
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "true") {
		t.Fatalf("boolean true remains in %s", raw)
	}
}

func assertClientSchema(t *testing.T, name string, schema mcp.ToolArgumentsSchema) {
	t.Helper()

	// @modelcontextprotocol/sdk 1.25.1, used by Cursor, requires the root
	// type to be the string "object" and every direct property schema to be
	// a JSON object. A boolean schema fails that parse and drops every tool.
	if schema.Type != "object" {
		t.Errorf("%s type = %q, want object", name, schema.Type)
	}

	for key, property := range schema.Properties {
		if key == "" {
			t.Errorf("%s has an empty property name", name)
		}
		if _, ok := property.(map[string]any); !ok {
			t.Errorf("%s property %s = %#v, want object schema", name, key, property)
		}
	}

	for _, key := range schema.Required {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("%s required %q is not a property", name, key)
		}
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	rejectNonObjectSchemaNodes(t, name, "schema", decoded)
}

func assertOutputOmitsOptionalPayload(t *testing.T, tool string, schema mcp.ToolOutputSchema) {
	t.Helper()

	for _, key := range schema.Required {
		if key == "data" || key == "error" {
			t.Errorf("%s output requires %q, so an error or success result fails client validation", tool, key)
		}
	}
}

func rejectNonObjectSchemaNodes(t *testing.T, tool, path string, value any) {
	t.Helper()

	switch node := value.(type) {
	case bool:
		if node {
			t.Errorf("%s %s is boolean true; strict MCP clients reject that schema node", tool, path)
		}
	case nil:
		t.Errorf("%s %s is null", tool, path)
	case map[string]any:
		for key, child := range node {
			if key == "additionalProperties" && child == false {
				continue
			}
			if key == "description" {
				text, _ := child.(string)
				if strings.HasPrefix(strings.TrimSpace(text), "required,") {
					t.Errorf("%s %s description still contains the jsonschema required tag: %q", tool, path, text)
				}
			}
			rejectNonObjectSchemaNodes(t, tool, path+"."+key, child)
		}
	case []any:
		for i, child := range node {
			rejectNonObjectSchemaNodes(t, tool, fmt.Sprintf("%s[%d]", path, i), child)
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
