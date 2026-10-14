package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Cali0707/mcpskills/internal/catalog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testEndpointPath = "/mcp"

func TestStreamableHTTPMCPConnection(t *testing.T) {
	source, err := catalog.NewDirectorySource(filepath.Join("..", "catalog", "testdata", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := newMCPServer(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(newHTTPHandler(server, testEndpointPath, nil))
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "mcpskills-test-client",
		Version: "test",
	}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpServer.URL + testEndpointPath,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect to Streamable HTTP server: %v", err)
	}
	defer session.Close()

	initialized := session.InitializeResult()
	if initialized == nil {
		t.Fatal("Connect() returned a session without initialization information")
	}
	if initialized.ServerInfo == nil || initialized.ServerInfo.Name != serverName {
		t.Errorf("server info = %+v, want name %q", initialized.ServerInfo, serverName)
	}
	if initialized.ProtocolVersion != "2026-07-28" {
		t.Errorf("protocol version = %q, want %q", initialized.ProtocolVersion, "2026-07-28")
	}
	if extension, ok := initialized.Capabilities.Extensions[skillsExtension].(map[string]any); !ok || extension["directoryRead"] != true {
		t.Errorf("Skills extension = %v, want directoryRead true", initialized.Capabilities.Extensions)
	}
	if initialized.Capabilities.Resources == nil {
		t.Error("resources capability not advertised")
	}
}
