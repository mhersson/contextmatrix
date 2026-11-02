package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/config"
)

const testToken = "tok-secret-123"

var testTarget = Target{Provider: "intent", Scope: "payments", Project: "alpha", Caller: "blueprint B1 stage intake"}

// newTestService builds a Service over one provider named "intent" and
// returns it with the audit log path.
func newTestService(t *testing.T, url, token string, opts Options) (*Service, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "knowledge.log")
	audit, err := NewAuditLog(path, DefaultAuditMaxBytes, DefaultAuditKeep)
	require.NoError(t, err)

	s := New([]config.KnowledgeProvider{{Name: "intent", URL: url, Token: token}}, audit, opts)

	t.Cleanup(func() { _ = s.Close() })

	return s, path
}

func auditEntries(t *testing.T, path string) []map[string]any {
	t.Helper()

	var out []map[string]any

	for _, line := range readLines(t, path) {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))

		out = append(out, m)
	}

	return out
}

func toolCalls(calls []stubCall, tool string) int {
	n := 0

	for _, c := range calls {
		if c.Tool == tool {
			n++
		}
	}

	return n
}

func fakeClock() *clock.FakeClock {
	return clock.Fake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
}

func TestService_ScopeInjectedAndArgsForwarded(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name     string
		call     func(s *Service, tg Target) (Answer, error)
		tool     string
		wantArgs map[string]any
	}{
		{
			"context", func(s *Service, tg Target) (Answer, error) { return s.Context(ctx, tg, "refund flow", 5) },
			"knowledge_context",
			map[string]any{"scope": "payments", "task": "refund flow", "limit": float64(5)},
		},
		{
			"context omits zero limit", func(s *Service, tg Target) (Answer, error) { return s.Context(ctx, tg, "t", 0) },
			"knowledge_context",
			map[string]any{"scope": "payments", "task": "t"},
		},
		{
			"query with kinds", func(s *Service, tg Target) (Answer, error) { return s.Query(ctx, tg, "refunds", []string{"rule"}, 8) },
			"knowledge_query",
			map[string]any{"scope": "payments", "query": "refunds", "kinds": []any{"rule"}, "limit": float64(8)},
		},
		{
			"query omits empty kinds", func(s *Service, tg Target) (Answer, error) { return s.Query(ctx, tg, "q", nil, 0) },
			"knowledge_query",
			map[string]any{"scope": "payments", "query": "q"},
		},
		{
			"item with related", func(s *Service, tg Target) (Answer, error) { return s.Item(ctx, tg, []string{"R-1"}, true) },
			"knowledge_item",
			map[string]any{"scope": "payments", "ids": []any{"R-1"}, "related": true},
		},
		{
			"item without related", func(s *Service, tg Target) (Answer, error) { return s.Item(ctx, tg, []string{"R-1"}, false) },
			"knowledge_item",
			map[string]any{"scope": "payments", "ids": []any{"R-1"}},
		},
	}

	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, _ := newTestService(t, stub.URL(), testToken, Options{})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := tc.call(s, testTarget)
			require.NoError(t, err)
			assert.NotEmpty(t, a["items"])

			calls := stub.Calls()
			require.NotEmpty(t, calls)

			last := calls[len(calls)-1]
			assert.Equal(t, tc.tool, last.Tool)
			assert.Equal(t, tc.wantArgs, last.Args)
		})
	}
}

func TestService_MissingIdsPassThrough(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, _ := newTestService(t, stub.URL(), testToken, Options{})

	a, err := s.Item(t.Context(), testTarget, []string{"R-1", "R-9"}, false)
	require.NoError(t, err)

	items, ok := a["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)

	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "R-1", item["id"])
	assert.Equal(t, "kept", item["x_extra"])
	assert.Equal(t, []any{"R-9"}, a["missing"])
}

func TestService_UnknownProvider(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	counter := &countingTransport{}
	s, path := newTestService(t, stub.URL(), testToken, Options{Transport: counter})

	tg := testTarget
	tg.Provider = "nope"

	_, err := s.Context(t.Context(), tg, "t", 0)
	require.ErrorIs(t, err, ErrUnknownProvider)

	assert.Zero(t, counter.Count())
	assert.Empty(t, stub.Calls())
	assert.Empty(t, readLines(t, path))
}

func TestService_WrongTokenIsAbsent(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: "right"})
	s, path := newTestService(t, stub.URL(), "wrong", Options{})

	_, err := s.Context(t.Context(), testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)
	assert.Empty(t, stub.Calls())
	assert.Empty(t, readLines(t, path))
}

func TestService_MissingToolAbsentThenRecovers(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken, Omit: []string{"knowledge_item"}})
	clk := fakeClock()
	s, _ := newTestService(t, stub.URL(), testToken, Options{Clock: clk, RetryAfter: 30 * time.Second})

	_, err := s.Context(t.Context(), testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)
	assert.Empty(t, stub.Calls())

	stub.AddTool("knowledge_item")

	_, err = s.Context(t.Context(), testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)
	assert.Empty(t, stub.Calls())

	clk.Advance(31 * time.Second)

	a, err := s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, a["items"])
	assert.Equal(t, 1, toolCalls(stub.Calls(), "knowledge_context"))
}

func TestService_UnreachableFailsFastInsideAbsentWindow(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL + "/mcp"
	dead.Close()

	counter := &countingTransport{}
	clk := fakeClock()
	s, path := newTestService(t, url, testToken, Options{Clock: clk, Transport: counter})

	_, err := s.Context(t.Context(), testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)

	requests := counter.Count()
	assert.Positive(t, requests)

	_, err = s.Context(t.Context(), testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)
	assert.Equal(t, requests, counter.Count())
	assert.Empty(t, readLines(t, path))
}

func TestService_CancelledDialDoesNotMarkAbsent(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, _ := newTestService(t, stub.URL(), testToken, Options{Clock: fakeClock()})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := s.Context(ctx, testTarget, "t", 0)
	require.ErrorIs(t, err, ErrProviderAbsent)
	require.ErrorIs(t, err, context.Canceled)

	_, err = s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
}

func TestService_RedialsOnceOnLostSession(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, _ := newTestService(t, stub.URL(), testToken, Options{})

	_, err := s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
	require.Equal(t, 1, stub.Initializes())

	stub.DropSessions()

	const parallel = 4

	var wg sync.WaitGroup

	errs := make([]error, parallel)

	for i := range parallel {
		wg.Go(func() {
			_, errs[i] = s.Context(t.Context(), testTarget, "t", 0)
		})
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	assert.Equal(t, 2, stub.Initializes())
}

func TestService_Timeout(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken, Delay: map[string]time.Duration{"knowledge_item": 2 * time.Second}})
	s, path := newTestService(t, stub.URL(), testToken, Options{ItemTimeout: 100 * time.Millisecond})

	start := time.Now()
	_, err := s.Item(t.Context(), testTarget, []string{"R-1"}, false)

	require.ErrorIs(t, err, ErrTimeout)
	assert.Less(t, time.Since(start), time.Second)

	entries := auditEntries(t, path)
	require.Len(t, entries, 1)
	assert.NotEmpty(t, entries[0]["error"])
	assert.Nil(t, entries[0]["answer"])
}

func TestService_CallerCancelIsNotTimeout(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken, Delay: map[string]time.Duration{"knowledge_query": 2 * time.Second}})
	s, _ := newTestService(t, stub.URL(), testToken, Options{})

	_, err := s.Context(t.Context(), testTarget, "warm up", 0)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)

	_, err = s.Query(ctx, testTarget, "q", nil, 0)
	require.ErrorIs(t, err, ErrProviderFailed)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrTimeout)

	// The session survives a cancelled call: no redial.
	_, err = s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, stub.Initializes())
}

func TestService_ProviderToolErrorIsFailed(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken, Fail: map[string]string{"knowledge_query": "unknown scope"}})
	s, path := newTestService(t, stub.URL(), testToken, Options{})

	_, err := s.Query(t.Context(), testTarget, "refunds", nil, 0)
	require.ErrorIs(t, err, ErrProviderFailed)

	entries := auditEntries(t, path)
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0]["error"], "unknown scope")
}

func TestService_BadAnswerStillAudited(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		maxBytes   int
		wantAnswer any
	}{
		{"not JSON is kept as a string", "not json", 0, "not json"},
		{"oversized is dropped", `{"items":[{"id":"R-1","text":"` + strings.Repeat("x", 128) + `"}]}`, 64, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := newStubProvider(t, stubOptions{Token: testToken, Text: map[string]string{"knowledge_context": tc.text}})
			s, path := newTestService(t, stub.URL(), testToken, Options{MaxAnswerBytes: tc.maxBytes})

			_, err := s.Context(t.Context(), testTarget, "t", 0)
			require.ErrorIs(t, err, ErrProviderFailed)

			entries := auditEntries(t, path)
			require.Len(t, entries, 1)
			assert.Equal(t, tc.wantAnswer, entries[0]["answer"])
			assert.NotEmpty(t, entries[0]["error"])
		})
	}
}

func TestService_ItemCache(t *testing.T) {
	ctx := t.Context()

	t.Run("hits, keys and expiry", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken})
		clk := fakeClock()
		s, path := newTestService(t, stub.URL(), testToken, Options{Clock: clk})

		first, err := s.Item(ctx, testTarget, []string{"R-1", "D-1"}, false)
		require.NoError(t, err)

		second, err := s.Item(ctx, testTarget, []string{"D-1", "R-1"}, false)
		require.NoError(t, err)
		assert.Equal(t, first, second)
		assert.Equal(t, 1, toolCalls(stub.Calls(), "knowledge_item"))

		entries := auditEntries(t, path)
		require.Len(t, entries, 2)
		assert.NotContains(t, entries[0], "cached")
		assert.Equal(t, true, entries[1]["cached"])
		assert.InDelta(t, 0, entries[1]["duration_ms"], 0)
		assert.Equal(t, entries[0]["answer"], entries[1]["answer"])
		assert.Equal(t, map[string]any{"scope": "payments", "ids": []any{"D-1", "R-1"}}, entries[1]["arguments"])

		_, err = s.Item(ctx, testTarget, []string{"R-1", "D-1"}, true)
		require.NoError(t, err)
		assert.Equal(t, 2, toolCalls(stub.Calls(), "knowledge_item"))

		clk.Advance(61 * time.Second)

		_, err = s.Item(ctx, testTarget, []string{"R-1", "D-1"}, false)
		require.NoError(t, err)
		assert.Equal(t, 3, toolCalls(stub.Calls(), "knowledge_item"))
	})

	t.Run("failures are not cached", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken, Fail: map[string]string{"knowledge_item": "boom"}})
		s, _ := newTestService(t, stub.URL(), testToken, Options{Clock: fakeClock()})

		for range 2 {
			_, err := s.Item(ctx, testTarget, []string{"R-1"}, false)
			require.ErrorIs(t, err, ErrProviderFailed)
		}

		assert.Equal(t, 2, toolCalls(stub.Calls(), "knowledge_item"))
	})

	t.Run("context and query are never cached", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken})
		s, _ := newTestService(t, stub.URL(), testToken, Options{Clock: fakeClock()})

		for range 2 {
			_, err := s.Context(ctx, testTarget, "t", 0)
			require.NoError(t, err)

			_, err = s.Query(ctx, testTarget, "q", nil, 0)
			require.NoError(t, err)
		}

		calls := stub.Calls()
		assert.Equal(t, 2, toolCalls(calls, "knowledge_context"))
		assert.Equal(t, 2, toolCalls(calls, "knowledge_query"))
	})
}

func TestService_AuditEntryCarriesFullAnswer(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, path := newTestService(t, stub.URL(), testToken, Options{})

	_, err := s.Context(t.Context(), testTarget, "refund flow", 0)
	require.NoError(t, err)

	entries := auditEntries(t, path)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "intent", e["provider"])
	assert.Equal(t, "payments", e["scope"])
	assert.Equal(t, "alpha", e["project"])
	assert.Equal(t, "blueprint B1 stage intake", e["caller"])
	assert.Equal(t, "knowledge_context", e["tool"])
	assert.Equal(t, map[string]any{"scope": "payments", "task": "refund flow"}, e["arguments"])
	assert.NotContains(t, e, "error")
	assert.NotContains(t, e, "cached")

	dur, ok := e["duration_ms"].(float64)
	require.True(t, ok)
	assert.GreaterOrEqual(t, dur, float64(0))

	answer, ok := e["answer"].(map[string]any)
	require.True(t, ok)

	items, ok := answer["items"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, items)

	first, ok := items[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, fixtureRuleText, first["text"])
}

func TestService_AuditCallerCutTo256Runes(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, path := newTestService(t, stub.URL(), testToken, Options{})

	tg := testTarget
	tg.Caller = strings.Repeat("é", 300)

	_, err := s.Context(t.Context(), tg, "t", 0)
	require.NoError(t, err)

	entries := auditEntries(t, path)
	require.Len(t, entries, 1)
	assert.Equal(t, strings.Repeat("é", 256), entries[0]["caller"])
}

type failingAudit struct{}

func (failingAudit) Write(Entry) error { return errors.New("disk full") }
func (failingAudit) Close() error      { return nil }

func TestService_AuditWriteFailureDoesNotFailCall(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s, _ := newTestService(t, stub.URL(), testToken, Options{})
	s.audit = failingAudit{}

	a, err := s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, a["items"])
}

func TestService_NilAuditLog(t *testing.T) {
	stub := newStubProvider(t, stubOptions{Token: testToken})
	s := New([]config.KnowledgeProvider{{Name: "intent", URL: stub.URL(), Token: testToken}}, nil, Options{})

	a, err := s.Context(t.Context(), testTarget, "t", 0)
	require.NoError(t, err)
	assert.NotEmpty(t, a["items"])
	require.NoError(t, s.Close())
}

func TestService_TokenNeverLeaks(t *testing.T) {
	ctx := t.Context()

	assertNoToken := func(t *testing.T, err error, path string) {
		t.Helper()

		if err != nil {
			assert.NotContains(t, err.Error(), testToken)
		}

		data, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		assert.NotContains(t, string(data), testToken)
	}

	t.Run("rejected token", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: "another"})
		s, path := newTestService(t, stub.URL(), testToken, Options{})

		_, err := s.Context(ctx, testTarget, "t", 0)
		require.ErrorIs(t, err, ErrProviderAbsent)
		assertNoToken(t, err, path)
	})

	t.Run("tool error echoing the token", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken, Fail: map[string]string{"knowledge_context": "bad bearer " + testToken}})
		s, path := newTestService(t, stub.URL(), testToken, Options{})

		_, err := s.Context(ctx, testTarget, "t", 0)
		require.ErrorIs(t, err, ErrProviderFailed)
		assertNoToken(t, err, path)
		assert.Len(t, readLines(t, path), 1)
	})

	t.Run("successful call", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken})
		s, path := newTestService(t, stub.URL(), testToken, Options{})

		_, err := s.Context(ctx, testTarget, "t", 0)
		require.NoError(t, err)
		assertNoToken(t, nil, path)
		assert.Len(t, readLines(t, path), 1)
	})
}

func TestService_Close(t *testing.T) {
	t.Run("never dialled", func(t *testing.T) {
		s, _ := newTestService(t, "http://127.0.0.1:1/mcp", testToken, Options{})
		require.NoError(t, s.Close())
		require.NoError(t, s.Close())
	})

	t.Run("closes sessions and refuses later calls", func(t *testing.T) {
		stub := newStubProvider(t, stubOptions{Token: testToken})
		counter := &countingTransport{}
		s, path := newTestService(t, stub.URL(), testToken, Options{Transport: counter})

		_, err := s.Context(t.Context(), testTarget, "t", 0)
		require.NoError(t, err)

		_, err = s.Item(t.Context(), testTarget, []string{"R-1"}, false)
		require.NoError(t, err)

		before := counter.Count()

		require.NoError(t, s.Close())
		assert.Greater(t, counter.Count(), before, "closing a session ends it on the provider")

		afterClose := counter.Count()

		_, err = s.Context(t.Context(), testTarget, "t", 0)
		require.ErrorIs(t, err, ErrProviderAbsent)

		// A cached item is not served after Close either.
		_, err = s.Item(t.Context(), testTarget, []string{"R-1"}, false)
		require.ErrorIs(t, err, ErrProviderAbsent)

		assert.Equal(t, afterClose, counter.Count())
		assert.Len(t, readLines(t, path), 2)

		require.NoError(t, s.Close())
	})
}
