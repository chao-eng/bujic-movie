// Package mcp exposes Bujic Movie's media/subtitle capabilities over the MCP
// (Model Context Protocol) Streamable HTTP transport for Agent tool-calling.
// Only active API keys are accepted on this endpoint (BR-18a/BR-19).
package mcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/bujic-movie/bujic-movie/internal/service"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ctxKey identifies the authenticated API key id injected into the request ctx.
type ctxKey struct{}

func apiKeyIDFromContext(ctx context.Context) (uint, bool) {
	v := ctx.Value(ctxKey{})
	if v == nil {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

// Gateway is the MCP Streamable HTTP handler with API-key auth + call audit.
type Gateway struct {
	mcpSrv    *mcpserver.StreamableHTTPServer
	keySvc    service.MCPAPIKeyService
	sem       chan struct{}
	recWriter *recordWriter
}

// NewGateway builds the MCP gateway wiring the five tools to the underlying
// SubtitleAgentService and the API-key/call-Record services.
func NewGateway(
	subtitleSvc service.SubtitleAgentService,
	keySvc service.MCPAPIKeyService,
	callRecordSink func(Record) error,
) *Gateway {
	g := &Gateway{
		keySvc:    keySvc,
		sem:       make(chan struct{}, 8), // BR-21: max 8 concurrent tool calls
		recWriter: newRecordWriter(callRecordSink),
	}

	server := mcpserver.NewMCPServer(
		"bujic-movie",
		"v0.3.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithInstructions("Bujic Movie 媒体库字幕工具。先用 mcp_ping 自测，再调用业务工具。"),
	)

	registerTools(server, subtitleSvc, g.sem, g.recWriter)

	g.mcpSrv = mcpserver.NewStreamableHTTPServer(
		server,
		mcpserver.WithEndpointPath("/api/v1/mcp"),
		// Localhost protection off: this endpoint is meant to be reachable from
		// LAN agents; auth is enforced by the wrapping handler below.
		mcpserver.WithDisableLocalhostProtection(true),
	)

	return g
}

// ServeHTTP authenticates the request and delegates to the MCP streamable server.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := extractAPIKey(r)
	if key == "" {
		writeAuthError(w, -32001, "missing api key")
		return
	}
	k, err := g.keySvc.Validate(key)
	if err != nil {
		writeAuthError(w, -32001, "unauthorized: invalid or disabled api key")
		return
	}
	ctx := context.WithValue(r.Context(), ctxKey{}, k.ID)
	g.mcpSrv.ServeHTTP(w, r.WithContext(ctx))
}

func extractAPIKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return strings.TrimSpace(parts[1])
		}
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// writeAuthError writes a JSON-RPC style error so MCP clients can surface it.
func writeAuthError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":` +
		itoa(code) + `,"message":` + quote(msg) + `},"id":null}`))
}
