//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

// apiClient is a cookie-jar HTTP client for CM's browser surface. In auth.mode:
// multi essentially the whole API is session-gated, so every scenario drives it
// through an authenticated admin session (see bootAdminSession). Each persona
// gets its own jar so sessions stay independent; the CSRF header is set on
// writes because the auth/admin routes are not CSRF-exempt.
type apiClient struct {
	baseURL string
	hc      *http.Client
}

func newAPIClient(t *testing.T, baseURL string) *apiClient {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}

	return &apiClient{baseURL: baseURL, hc: &http.Client{Timeout: 10 * time.Second, Jar: jar}}
}

// do issues a request, decoding a JSON body into `into` on 2xx. Returns the
// status and the (truncated) raw body for assertion messages.
func (c *apiClient) do(t *testing.T, method, path string, body, into any) (int, string) {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s %s: %v", method, path, err)
		}
	}

	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		t.Fatalf("req %s %s: %v", method, path, err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		req.Header.Set("X-Requested-With", "contextmatrix")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if into != nil && resp.StatusCode < 400 && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("decode %s %s: %v body=%s", method, path, err, raw)
		}
	}

	return resp.StatusCode, string(raw)
}

type activityEntry struct {
	Timestamp string `json:"ts"`
	Action    string `json:"action"`
	Message   string `json:"message"`
	Agent     string `json:"agent"`
	Skill     string `json:"skill,omitempty"`
}

type tokenUsage struct {
	Model            string  `json:"model,omitempty"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

// cardSnapshot mirrors the subset of CM's card JSON the scenarios assert on.
// WorkerStatus uses the protocol v0.8.0 wire tag (worker_status).
type cardSnapshot struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	State         string          `json:"state"`
	AssignedAgent string          `json:"assigned_agent"`
	WorkerStatus  string          `json:"worker_status"`
	Autonomous    bool            `json:"autonomous"`
	Body          string          `json:"body"`
	TokenUsage    *tokenUsage     `json:"token_usage,omitempty"`
	ActivityLog   []activityEntry `json:"activity_log"`
}

func (c *apiClient) getCard(t *testing.T, project, cardID string) cardSnapshot {
	t.Helper()

	var card cardSnapshot

	status, body := c.do(t, http.MethodGet, fmt.Sprintf("/api/projects/%s/cards/%s", project, cardID), nil, &card)
	if status != http.StatusOK {
		t.Fatalf("getCard %s/%s: HTTP %d body=%s", project, cardID, status, body)
	}

	return card
}

// pollUntil retries fn until it returns true or the deadline expires.
func pollUntil(ctx context.Context, t *testing.T, label string, fn func() bool) {
	t.Helper()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(60 * time.Second)
	}

	for time.Now().Before(deadline) {
		if fn() {
			return
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("pollUntil timed out: %s", label)
}
