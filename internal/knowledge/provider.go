package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
)

var contractTools = []string{"knowledge_context", "knowledge_query", "knowledge_item"}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)

	return t.base.RoundTrip(r)
}

// provider is one lazily dialled MCP session to a knowledge provider. A failed
// connect or tool check makes it absent until absentUntil.
type provider struct {
	name        string
	url         string
	token       string
	httpClient  *http.Client
	clk         clock.Clock
	dialTimeout time.Duration
	retryAfter  time.Duration

	mu          sync.Mutex
	session     *mcp.ClientSession
	absentUntil time.Time
	absent      bool
	closed      bool
}

func newProvider(cfg config.KnowledgeProvider, base http.RoundTripper, clk clock.Clock, dialTimeout, retryAfter time.Duration) *provider {
	httpClient := &http.Client{
		Transport: &bearerTransport{token: cfg.Token, base: base},
		// bearerTransport sets the header on every hop, so a followed redirect
		// would hand the token to whatever host the provider names.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	return &provider{
		name:        cfg.Name,
		url:         cfg.URL,
		token:       cfg.Token,
		httpClient:  httpClient,
		clk:         clk,
		dialTimeout: dialTimeout,
		retryAfter:  retryAfter,
	}
}

// redact is applied to every text that can echo a provider's response, so a
// provider repeating the bearer token never puts it in an error or log line.
func (p *provider) redact(s string) string {
	if p.token == "" {
		return s
	}

	return strings.ReplaceAll(s, p.token, "[redacted]")
}

// acquire returns the live session, dialling one when there is none and the
// provider is not inside its absent window.
func (p *provider) acquire(ctx context.Context) (*mcp.ClientSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.acquireLocked(ctx)
}

func (p *provider) acquireLocked(ctx context.Context) (*mcp.ClientSession, error) {
	switch {
	case p.closed:
		return nil, fmt.Errorf("%w: %s: service closed", ErrProviderAbsent, p.name)
	case p.session != nil:
		return p.session, nil
	case p.clk.Now().Before(p.absentUntil):
		return nil, fmt.Errorf("%w: %s", ErrProviderAbsent, p.name)
	}

	sess, missing, err := p.dial(ctx)
	if err != nil {
		// A caller that gave up says nothing about the provider.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrProviderAbsent, p.name, ctx.Err())
		}

		p.markAbsent(err, missing)

		return nil, fmt.Errorf("%w: %s: %s", ErrProviderAbsent, p.name, p.redact(err.Error()))
	}

	if p.absent {
		p.absent = false
		slog.Info("knowledge provider available again", "provider", p.name)
	}

	p.session = sess

	return sess, nil
}

func (p *provider) dial(ctx context.Context) (*mcp.ClientSession, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.dialTimeout)
	defer cancel()

	// No server-to-client handlers are registered, so a standalone SSE stream
	// would carry nothing.
	transport := &mcp.StreamableClientTransport{Endpoint: p.url, HTTPClient: p.httpClient, DisableStandaloneSSE: true}
	client := mcp.NewClient(&mcp.Implementation{Name: "contextmatrix", Version: "0.1.0"}, nil)

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	have := make(map[string]bool)

	for tool, err := range sess.Tools(ctx, nil) {
		if err != nil {
			_ = sess.Close()

			return nil, nil, fmt.Errorf("list tools: %w", err)
		}

		have[tool.Name] = true
	}

	var missing []string

	for _, name := range contractTools {
		if !have[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		_ = sess.Close()

		return nil, missing, fmt.Errorf("missing contract tools: %s", strings.Join(missing, ", "))
	}

	return sess, nil, nil
}

func (p *provider) markAbsent(err error, missing []string) {
	p.absentUntil = p.clk.Now().Add(p.retryAfter)

	if p.absent {
		return
	}

	p.absent = true
	slog.Warn("knowledge provider unavailable",
		"provider", p.name, "error", p.redact(err.Error()), "missing_tools", missing, "retry_after", p.retryAfter)
}

// callTool calls on sess and, when that session was lost, redials once. Only
// the caller whose session is still current redials; the others reuse the
// session it dialled.
func (p *provider) callTool(ctx context.Context, sess *mcp.ClientSession, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	res, err := sess.CallTool(ctx, params)
	if err == nil || (!errors.Is(err, mcp.ErrConnectionClosed) && !errors.Is(err, mcp.ErrSessionMissing)) {
		return res, err
	}

	p.mu.Lock()

	if p.session == sess {
		_ = sess.Close()
		p.session = nil
	}

	fresh, err := p.acquireLocked(ctx)
	p.mu.Unlock()

	if err != nil {
		return nil, err
	}

	return fresh.CallTool(ctx, params)
}

func (p *provider) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	if p.session == nil {
		return nil
	}

	err := p.session.Close()
	p.session = nil

	if err != nil {
		return fmt.Errorf("close knowledge provider %s session: %s", p.name, p.redact(err.Error()))
	}

	return nil
}
