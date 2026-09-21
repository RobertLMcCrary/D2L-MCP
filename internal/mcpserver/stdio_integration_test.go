package mcpserver

import (
	"os"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestStdioSubprocess(t *testing.T) {
	binary := os.Getenv("D2L_MCP_TEST_BINARY")
	if binary == "" {
		t.Skip("set D2L_MCP_TEST_BINARY to run subprocess protocol test")
	}

	env := append(os.Environ(), "HOME="+t.TempDir(), "D2L_NO_AUTO_LOGIN=1")

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
