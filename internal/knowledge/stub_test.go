package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	fixtureRuleText     = "Refunds over 500 EUR need a second approver."
	fixtureDecisionText = "Partial refunds are under discussion."
)

var stubToolNames = []string{"knowledge_context", "knowledge_query", "knowledge_item"}

type stubOptions struct {
	Token string
	Omit  []string
	Delay map[string]time.Duration
	Fail  map[string]string
	Text  map[string]string // tool answers with only this text content
}

type stubCall struct {
	Tool string
	Args map[string]any
}

// stubProvider is a stateful go-sdk MCP server serving the three provider
// contract tools behind a bearer check.
type stubProvider struct {
	opts   stubOptions
	server *mcp.Server
	srv    *httptest.Server

	mu          sync.Mutex
	handler     http.Handler
	calls       []stubCall
	initializes int
}

func newStubProvider(t *testing.T, opts stubOptions) *stubProvider {
	t.Helper()

	p := &stubProvider{
		opts:   opts,
		server: mcp.NewServer(&mcp.Implementation{Name: "stub-provider", Version: "0.0.1"}, nil),
	}

	for _, name := range stubToolNames {
		if !slices.Contains(opts.Omit, name) {
			p.AddTool(name)
		}
	}

	p.handler = p.newHandler()
	p.srv = httptest.NewServer(http.HandlerFunc(p.serveHTTP))
	t.Cleanup(p.Close)

	return p
}

func (p *stubProvider) newHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return p.server }, nil)
}

func (p *stubProvider) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+p.opts.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return
	}

	if r.Method == http.MethodPost && r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)

			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		var msg struct {
			Method string `json:"method"`
		}

		if json.Unmarshal(body, &msg) == nil && msg.Method == "initialize" {
			p.mu.Lock()
			p.initializes++
			p.mu.Unlock()
		}
	}

	p.mu.Lock()
	h := p.handler
	p.mu.Unlock()

	h.ServeHTTP(w, r)
}

// AddTool registers one of the contract tools.
func (p *stubProvider) AddTool(name string) {
	p.server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, p.toolHandler(name))
}

func (p *stubProvider) toolHandler(name string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
		}

		p.mu.Lock()
		p.calls = append(p.calls, stubCall{Tool: name, Args: args})
		p.mu.Unlock()

		if d := p.opts.Delay[name]; d > 0 {
			timer := time.NewTimer(d)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		if text, ok := p.opts.Text[name]; ok {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		}

		if msg, ok := p.opts.Fail[name]; ok {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
		}

		answer := stubAnswer(name, args)

		text, err := json.Marshal(answer)
		if err != nil {
			return nil, err
		}

		return &mcp.CallToolResult{StructuredContent: answer, Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil
	}
}

func stubFixtures() map[string]map[string]any {
	return map[string]map[string]any{
		"R-1": {"id": "R-1", "kind": "rule", "title": "Second approver", "text": fixtureRuleText, "agreed": true, "x_extra": "kept"},
		"D-1": {"id": "D-1", "kind": "decision", "title": "Partial refunds", "text": fixtureDecisionText, "status": "open"},
	}
}

func stubAnswer(tool string, args map[string]any) map[string]any {
	fx := stubFixtures()

	if tool != "knowledge_item" {
		return map[string]any{"items": []any{fx["R-1"], fx["D-1"]}}
	}

	items, missing := []any{}, []any{}

	ids, _ := args["ids"].([]any)
	for _, raw := range ids {
		id, _ := raw.(string)
		if item, ok := fx[id]; ok {
			items = append(items, item)
		} else {
			missing = append(missing, id)
		}
	}

	answer := map[string]any{"items": items}
	if len(missing) > 0 {
		answer["missing"] = missing
	}

	return answer
}

// URL is the MCP endpoint.
func (p *stubProvider) URL() string { return p.srv.URL + "/mcp" }

func (p *stubProvider) Calls() []stubCall {
	p.mu.Lock()
	defer p.mu.Unlock()

	return slices.Clone(p.calls)
}

func (p *stubProvider) Initializes() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.initializes
}

// DropSessions swaps in a fresh handler, so earlier session ids answer 404.
func (p *stubProvider) DropSessions() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handler = p.newHandler()
}

func (p *stubProvider) Close() { p.srv.Close() }

// countingTransport counts the requests it forwards.
type countingTransport struct {
	base http.RoundTripper
	n    atomic.Int64
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.n.Add(1)

	base := c.base
	if base == nil {
		base = http.DefaultTransport
	}

	return base.RoundTrip(req)
}

func (c *countingTransport) Count() int64 { return c.n.Load() }
