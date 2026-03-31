// Package mcp provides an MCP server exposing ContextMatrix tools and prompts
// via Streamable HTTP transport on POST /mcp.
package mcp

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/service"
)

// NewServer creates a configured MCP server with all tools and prompts registered.
func NewServer(svc *service.CardService, skillsDir string) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "contextmatrix",
			Version: "0.1.0",
		},
		nil,
	)

	registerTools(server, svc, skillsDir)
	registerPrompts(server, svc, skillsDir)

	return server
}

// NewHandler returns an http.Handler for MCP Streamable HTTP transport.
// Register this on POST /mcp in the router.
func NewHandler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return server },
		nil,
	)
}
