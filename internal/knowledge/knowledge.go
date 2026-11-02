package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
)

const (
	defaultItemTimeout    = 5 * time.Second
	defaultSearchTimeout  = 45 * time.Second
	defaultDialTimeout    = 10 * time.Second
	defaultRetryAfter     = 30 * time.Second
	defaultCacheTTL       = 60 * time.Second
	defaultCacheSize      = 256
	defaultMaxAnswerBytes = 1 << 20
	maxCallerRunes        = 256
)

// Target names the provider and scope a call goes to, plus the audit-only
// project and caller.
type Target struct {
	Provider string // project's knowledge_provider
	Scope    string // project's knowledge_scope; the service adds it to the provider arguments
	Project  string // audit only
	Caller   string // audit only, never sent to the provider
}

// Options tunes a Service; zero fields take the defaults.
type Options struct {
	ItemTimeout, SearchTimeout, DialTimeout, RetryAfter, CacheTTL time.Duration
	CacheSize, MaxAnswerBytes                                     int
	Clock                                                         clock.Clock
	Transport                                                     http.RoundTripper // base under the bearer transport; nil = http.DefaultTransport
}

func (o Options) withDefaults() Options {
	if o.ItemTimeout <= 0 {
		o.ItemTimeout = defaultItemTimeout
	}

	if o.SearchTimeout <= 0 {
		o.SearchTimeout = defaultSearchTimeout
	}

	if o.DialTimeout <= 0 {
		o.DialTimeout = defaultDialTimeout
	}

	if o.RetryAfter <= 0 {
		o.RetryAfter = defaultRetryAfter
	}

	if o.CacheTTL <= 0 {
		o.CacheTTL = defaultCacheTTL
	}

	if o.CacheSize <= 0 {
		o.CacheSize = defaultCacheSize
	}

	if o.MaxAnswerBytes <= 0 {
		o.MaxAnswerBytes = defaultMaxAnswerBytes
	}

	if o.Clock == nil {
		o.Clock = clock.Real()
	}

	if o.Transport == nil {
		o.Transport = http.DefaultTransport
	}

	return o
}

type auditWriter interface {
	Write(e Entry) error
	Close() error
}

// Service calls the configured knowledge providers. Every error it returns
// wraps ErrUnknownProvider, ErrProviderAbsent, ErrTimeout or ErrProviderFailed.
type Service struct {
	providers      map[string]*provider
	audit          auditWriter
	cache          *itemCache
	clk            clock.Clock
	itemTimeout    time.Duration
	searchTimeout  time.Duration
	maxAnswerBytes int

	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

// New builds a Service. Sessions are dialled on first use; audit may be nil.
func New(providers []config.KnowledgeProvider, audit *AuditLog, opts Options) *Service {
	opts = opts.withDefaults()

	s := &Service{
		providers:      make(map[string]*provider, len(providers)),
		cache:          newItemCache(opts.Clock, opts.CacheTTL, opts.CacheSize),
		clk:            opts.Clock,
		itemTimeout:    opts.ItemTimeout,
		searchTimeout:  opts.SearchTimeout,
		maxAnswerBytes: opts.MaxAnswerBytes,
	}

	// Assigned only when non-nil: a nil *AuditLog in the interface is not nil.
	if audit != nil {
		s.audit = audit
	}

	for _, cfg := range providers {
		s.providers[cfg.Name] = newProvider(cfg, opts.Transport, opts.Clock, opts.DialTimeout, opts.RetryAfter)
	}

	return s
}

// Context asks the provider for the knowledge relevant to task.
func (s *Service) Context(ctx context.Context, t Target, task string, limit int) (Answer, error) {
	args := map[string]any{"task": task}
	if limit > 0 {
		args["limit"] = limit
	}

	a, _, err := s.call(ctx, t, "knowledge_context", args, s.searchTimeout)

	return a, err
}

// Query searches the provider's knowledge.
func (s *Service) Query(ctx context.Context, t Target, query string, kinds []string, limit int) (Answer, error) {
	args := map[string]any{"query": query}
	if len(kinds) > 0 {
		args["kinds"] = kinds
	}

	if limit > 0 {
		args["limit"] = limit
	}

	a, _, err := s.call(ctx, t, "knowledge_query", args, s.searchTimeout)

	return a, err
}

// Item fetches items by id. Successful answers are cached; a cache hit is
// audited without reaching the provider.
func (s *Service) Item(ctx context.Context, t Target, ids []string, related bool) (Answer, error) {
	args := map[string]any{"ids": ids}
	if related {
		args["related"] = true
	}

	if _, err := s.lookup(t); err != nil {
		return nil, err
	}

	key := itemKey(t.Provider, t.Scope, ids, related)

	if a, raw, ok := s.cache.get(key); ok {
		args["scope"] = t.Scope

		e := s.entry(t, "knowledge_item", args, s.clk.Now())
		e.Answer = raw
		e.Cached = true
		s.writeAudit(e)

		return a, nil
	}

	a, raw, err := s.call(ctx, t, "knowledge_item", args, s.itemTimeout)
	if err != nil {
		return nil, err
	}

	s.cache.put(key, a, raw)

	return a, nil
}

func (s *Service) lookup(t Target) (*provider, error) {
	p, ok := s.providers[t.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, t.Provider)
	}

	if s.closed.Load() {
		return nil, fmt.Errorf("%w: %s: service closed", ErrProviderAbsent, p.name)
	}

	return p, nil
}

// call sends one tool call and audits it. Nothing is audited when no
// tools/call was sent: an unknown provider or an absent one.
func (s *Service) call(ctx context.Context, t Target, tool string, args map[string]any, timeout time.Duration) (Answer, json.RawMessage, error) {
	p, err := s.lookup(t)
	if err != nil {
		return nil, nil, err
	}

	args["scope"] = t.Scope

	sess, err := p.acquire(ctx)
	if err != nil {
		return nil, nil, err
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := s.clk.Now()
	res, err := p.callTool(callCtx, sess, &mcp.CallToolParams{Name: tool, Arguments: args})
	elapsed := s.clk.Now().Sub(start)

	var (
		answer Answer
		raw    json.RawMessage
	)

	switch {
	case err != nil:
		err = classify(ctx, callCtx, p, tool, err, timeout)
	case res.IsError:
		err = fmt.Errorf("%w: %s: %s: %s", ErrProviderFailed, p.name, tool, p.redact(toolText(res)))
	default:
		answer, raw, err = normalizeAnswer(res, s.maxAnswerBytes)
	}

	e := s.entry(t, tool, args, start)
	e.Answer = s.auditAnswer(raw)
	e.DurationMS = elapsed.Milliseconds()

	if err != nil {
		e.Error = err.Error()
	}

	s.writeAudit(e)

	if err != nil {
		return nil, nil, err
	}

	return answer, raw, nil
}

// classify maps a failed call to one sentinel. ErrTimeout is CM's own call
// timeout; a caller whose context ended first gets ErrProviderFailed wrapping
// that context error, since nobody waits for the answer.
func classify(ctx, callCtx context.Context, p *provider, tool string, err error, timeout time.Duration) error {
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("%w: %s: %s: caller gave up: %w", ErrProviderFailed, p.name, tool, ctx.Err())
	case callCtx.Err() != nil:
		return fmt.Errorf("%w: %s: %s: no answer within %s", ErrTimeout, p.name, tool, timeout)
	case errors.Is(err, ErrProviderAbsent):
		return err
	default:
		return fmt.Errorf("%w: %s: %s: %s", ErrProviderFailed, p.name, tool, p.redact(err.Error()))
	}
}

func toolText(res *mcp.CallToolResult) string {
	var parts []string

	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}

	return strings.Join(parts, "\n")
}

func (s *Service) entry(t Target, tool string, args map[string]any, at time.Time) Entry {
	return Entry{
		Time:      at,
		Provider:  t.Provider,
		Scope:     t.Scope,
		Project:   t.Project,
		Caller:    cutRunes(t.Caller, maxCallerRunes),
		Tool:      tool,
		Arguments: args,
	}
}

// auditAnswer keeps a malformed or oversized answer from failing or bloating
// the audit line: invalid JSON is kept as a string, oversized as null.
func (s *Service) auditAnswer(raw json.RawMessage) json.RawMessage {
	switch {
	case len(raw) == 0, len(raw) > s.maxAnswerBytes:
		return nil
	case json.Valid(raw):
		return raw
	}

	b, err := json.Marshal(string(raw))
	if err != nil {
		return nil
	}

	return b
}

func (s *Service) writeAudit(e Entry) {
	if s.audit == nil {
		return
	}

	if err := s.audit.Write(e); err != nil {
		slog.Error("write knowledge audit log", "provider", e.Provider, "tool", e.Tool, "error", err)
	}
}

// Close closes every provider session, then the audit log. Later calls fail
// with ErrProviderAbsent; closing again returns the first result.
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)

		var errs []error

		for _, p := range s.providers {
			if err := p.close(); err != nil {
				errs = append(errs, err)
			}
		}

		if s.audit != nil {
			if err := s.audit.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close knowledge audit log: %w", err))
			}
		}

		s.closeErr = errors.Join(errs...)
	})

	return s.closeErr
}

func cutRunes(s string, n int) string {
	i := 0

	for pos := range s {
		if i == n {
			return s[:pos]
		}

		i++
	}

	return s
}
