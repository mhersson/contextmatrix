package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhersson/contextmatrix/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Trigger_Success(t *testing.T) {
	var (
		received    TriggerPayload
		receivedSig string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/trigger", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedSig = r.Header.Get(signatureHeader)

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	err := c.Trigger(context.Background(), TriggerPayload{
		CardID:  "TEST-001",
		Project: "test-project",
		RepoURL: "git@github.com:org/repo.git",
	})

	require.NoError(t, err)
	assert.Equal(t, "TEST-001", received.CardID)
	assert.Equal(t, "test-project", received.Project)
	assert.True(t, strings.HasPrefix(receivedSig, "sha256="))
}

func TestClient_Trigger_VerifiesHMAC(t *testing.T) {
	const apiKey = "shared-secret"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigHeader := r.Header.Get(signatureHeader)
		assert.True(t, strings.HasPrefix(sigHeader, "sha256="))
		sig := strings.TrimPrefix(sigHeader, "sha256=")

		tsHeader := r.Header.Get(timestampHeader)
		assert.NotEmpty(t, tsHeader, "timestamp header should be present")

		body, _ := io.ReadAll(r.Body)
		assert.True(t, VerifySignatureWithTimestamp(apiKey, r.Method, r.URL.Path, sig, tsHeader, body, DefaultMaxClockSkew),
			"HMAC signature with timestamp should be valid")

		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, apiKey)
	err := c.Trigger(context.Background(), TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.NoError(t, err)
}

func TestTriggerPayload_BaseBranch(t *testing.T) {
	var received TriggerPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")

	// With base_branch set: should appear in JSON.
	err := c.Trigger(context.Background(), TriggerPayload{
		CardID:     "TEST-001",
		Project:    "test-project",
		RepoURL:    "git@github.com:org/repo.git",
		BaseBranch: "main",
	})
	require.NoError(t, err)
	assert.Equal(t, "main", received.BaseBranch)

	// With base_branch empty: should be omitted from JSON (omitempty).
	var rawPayload map[string]any

	srvOmit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &rawPayload)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srvOmit.Close()

	c2 := NewClient(srvOmit.URL, "test-key")
	err = c2.Trigger(context.Background(), TriggerPayload{
		CardID:  "TEST-001",
		Project: "test-project",
	})
	require.NoError(t, err)

	_, hasBaseBranch := rawPayload["base_branch"]
	assert.False(t, hasBaseBranch, "base_branch should be omitted when empty")
}

func TestClient_Kill_Success(t *testing.T) {
	var received KillPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/kill", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.Kill(context.Background(), KillPayload{CardID: "TEST-001", Project: "p"})
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", received.CardID)
}

// TestClient_EndSessionAndKill_DistinctSignatures is the regression guard for
// the replay-cache collision bug: /end-session and /kill carry identical JSON
// bodies, so when they fire back-to-back in the same Unix second their HMAC
// signatures MUST differ — otherwise the runner's replay cache (keyed on
// signature) rejects the second call with 409 duplicate and the container
// leaks. Binding method + path into the signature is what guarantees the
// divergence.
func TestClient_EndSessionAndKill_DistinctSignatures(t *testing.T) {
	const apiKey = "shared-secret"

	var capturedSigs []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSigs = append(capturedSigs, r.Header.Get(signatureHeader))
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, apiKey)
	ctx := context.Background()

	require.NoError(t, c.EndSession(ctx, EndSessionPayload{CardID: "TEST-001", Project: "p"}))
	require.NoError(t, c.Kill(ctx, KillPayload{CardID: "TEST-001", Project: "p"}))

	require.Len(t, capturedSigs, 2)
	assert.NotEqual(t, capturedSigs[0], capturedSigs[1],
		"end-session and kill must produce distinct signatures even with identical bodies in the same second")
}

func TestClient_StopAll_Success(t *testing.T) {
	var received StopAllPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/stop-all", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.StopAll(context.Background(), StopAllPayload{Project: "test-project"})
	require.NoError(t, err)
	assert.Equal(t, "test-project", received.Project)
}

func TestClient_RetryOn500(t *testing.T) {
	origBackoff := BackoffBase
	BackoffBase = time.Millisecond

	t.Cleanup(func() { BackoffBase = origBackoff })

	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"ok":false,"error":"temporary"}`))

			return
		}

		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	// Use a long timeout to allow retries with backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := c.Trigger(ctx, TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_NoRetryOn400(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.Trigger(context.Background(), TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry on 4xx")
	assert.Contains(t, err.Error(), "400")
}

func TestClient_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`error`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := NewClient(srv.URL, "key")
	err := c.Trigger(ctx, TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.Error(t, err)
}

func TestClient_RunnerReturnsNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: false, Error: "container limit reached"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.Trigger(context.Background(), TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container limit reached")
}

func TestTriggerPayload_InteractiveOmitempty(t *testing.T) {
	// interactive=false should be omitted from JSON.
	p := TriggerPayload{CardID: "TEST-001", Project: "p"}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "interactive", "interactive should be omitted when false")

	// interactive=true should appear in JSON.
	p.Interactive = true
	raw, err = json.Marshal(p)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"interactive":true`, "interactive should appear when true")
}

func TestClient_Message_Success(t *testing.T) {
	var (
		received                MessagePayload
		receivedSig, receivedTS string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/message", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedSig = r.Header.Get(signatureHeader)
		receivedTS = r.Header.Get(timestampHeader)

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	payload := MessagePayload{
		CardID:    "TEST-001",
		Project:   "test-project",
		Content:   "hello from agent",
		MessageID: "msg-abc-123",
	}
	err := c.Message(context.Background(), payload)

	require.NoError(t, err)
	assert.Equal(t, "TEST-001", received.CardID)
	assert.Equal(t, "test-project", received.Project)
	assert.Equal(t, "hello from agent", received.Content)
	assert.Equal(t, "msg-abc-123", received.MessageID)
	assert.True(t, strings.HasPrefix(receivedSig, "sha256="), "X-Signature-256 should be set")
	assert.NotEmpty(t, receivedTS, "X-Webhook-Timestamp should be set")
}

func TestClient_Promote_Success(t *testing.T) {
	var (
		received                PromotePayload
		receivedSig, receivedTS string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/promote", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		receivedSig = r.Header.Get(signatureHeader)
		receivedTS = r.Header.Get(timestampHeader)

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key")
	payload := PromotePayload{
		CardID:  "TEST-001",
		Project: "test-project",
	}
	err := c.Promote(context.Background(), payload)

	require.NoError(t, err)
	assert.Equal(t, "TEST-001", received.CardID)
	assert.Equal(t, "test-project", received.Project)
	assert.True(t, strings.HasPrefix(receivedSig, "sha256="), "X-Signature-256 should be set")
	assert.NotEmpty(t, receivedTS, "X-Webhook-Timestamp should be set")
}

func TestClient_Message_NoRetryOn404(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.Message(context.Background(), MessagePayload{CardID: "TEST-001", Project: "p", Content: "hi", MessageID: "m1"})
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry on 404")
	assert.Contains(t, err.Error(), "404")
}

func TestClient_Promote_NoRetryOn404(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	err := c.Promote(context.Background(), PromotePayload{CardID: "TEST-001", Project: "p"})
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "should not retry on 404")
	assert.Contains(t, err.Error(), "404")
}

func TestClient_RetryOn503(t *testing.T) {
	origBackoff := BackoffBase
	BackoffBase = time.Millisecond

	t.Cleanup(func() { BackoffBase = origBackoff })

	tests := []struct {
		name string
		fn   func(c *Client, ctx context.Context) error
	}{
		{
			name: "Message",
			fn: func(c *Client, ctx context.Context) error {
				return c.Message(ctx, MessagePayload{CardID: "TEST-001", Project: "p", Content: "hi", MessageID: "m1"})
			},
		},
		{
			name: "Promote",
			fn: func(c *Client, ctx context.Context) error {
				return c.Promote(ctx, PromotePayload{CardID: "TEST-001", Project: "p"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var attempts atomic.Int32

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"ok":false,"error":"temporary"}`))
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "key")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			err := tt.fn(c, ctx)
			require.Error(t, err)
			assert.Equal(t, int32(maxRetries), attempts.Load(), "should retry up to maxRetries on 503")
		})
	}
}

// TestClient_WebhookMetrics_Success verifies that a successful webhook call
// increments runner_webhook_total{result="success"} and not the failure series.
func TestClient_WebhookMetrics_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(WebhookResponse{OK: true})
	}))
	defer srv.Close()

	beforeSuccess := testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("success"))
	beforeFailure := testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("failure"))

	c := NewClient(srv.URL, "key")
	err := c.Trigger(context.Background(), TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("success"))-beforeSuccess, float64(1),
		"success counter should increment on 2xx response")
	assert.InDelta(t, 0, testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("failure"))-beforeFailure, 0.0,
		"failure counter should not increment on success")
}

// TestClient_WebhookMetrics_Failure verifies that a failed webhook call
// increments runner_webhook_total{result="failure"}.
func TestClient_WebhookMetrics_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request`))
	}))
	defer srv.Close()

	beforeSuccess := testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("success"))
	beforeFailure := testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("failure"))

	c := NewClient(srv.URL, "key")
	err := c.Trigger(context.Background(), TriggerPayload{CardID: "TEST-001", Project: "p"})
	require.Error(t, err)

	assert.InDelta(t, 0, testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("success"))-beforeSuccess, 0.0,
		"success counter should not increment on failure")
	assert.GreaterOrEqual(t, testutil.ToFloat64(metrics.RunnerWebhookTotal.WithLabelValues("failure"))-beforeFailure, float64(1),
		"failure counter should increment on non-2xx response")
}

// TestClient_ListContainers_Success round-trips a typical /containers
// response: two entries, one tracked and one not, so the caller can tell a
// divergent orphan from a legitimate in-flight card.
func TestClient_ListContainers_Success(t *testing.T) {
	started := time.Now().Add(-45 * time.Minute).UTC().Truncate(time.Second)

	const apiKey = "shared-secret"

	var receivedSig, receivedTS string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/containers", r.URL.Path)

		receivedSig = r.Header.Get(signatureHeader)
		receivedTS = r.Header.Get(timestampHeader)

		// GET signs an empty body.
		body, _ := io.ReadAll(r.Body)
		assert.Empty(t, body, "GET body must be empty")

		sig := strings.TrimPrefix(receivedSig, "sha256=")
		assert.True(t, VerifySignatureWithTimestamp(apiKey, r.Method, r.URL.Path, sig, receivedTS, nil, DefaultMaxClockSkew),
			"HMAC over empty body + timestamp must verify")

		payload := map[string]any{
			"ok": true,
			"containers": []map[string]any{
				{
					"container_id":   "abc123",
					"container_name": "cmr-contextmatrix-ctxmax-436",
					"card_id":        "ctxmax-436",
					"project":        "contextmatrix",
					"state":          "running",
					"started_at":     started.Format(time.RFC3339),
					"tracked":        false,
				},
				{
					"container_id": "def456",
					"card_id":      "alpha-001",
					"project":      "proj",
					"state":        "exited",
					"started_at":   started.Format(time.RFC3339),
					"tracked":      true,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, apiKey)
	got, err := c.ListContainers(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "ctxmax-436", got[0].CardID)
	assert.Equal(t, "running", got[0].State)
	assert.False(t, got[0].Tracked)
	assert.Equal(t, started.Unix(), got[0].StartedAt.Unix())

	assert.Equal(t, "alpha-001", got[1].CardID)
	assert.True(t, got[1].Tracked)
}

// TestClient_ListContainers_RunnerError surfaces a runner 502 so the sweep
// logs and skips this tick rather than acting on a half-response.
func TestClient_ListContainers_RunnerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"code":"upstream_failure","message":"docker list failed"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListContainers(context.Background())
	require.Error(t, err)
}

// TestClient_ListContainers_RunnerOKFalse rejects a 200 OK body with ok=false
// so the sweep doesn't act on an ambiguous response.
func TestClient_ListContainers_RunnerOKFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"containers":[]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "key")
	_, err := c.ListContainers(context.Background())
	require.Error(t, err)
}
