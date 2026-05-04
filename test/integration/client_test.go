//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

type cmClient struct {
	baseURL string
	hc      *http.Client
}

func newCMClient(baseURL string) *cmClient {
	return &cmClient{baseURL: baseURL, hc: &http.Client{Timeout: 10 * time.Second}}
}

// postRaw posts to path and returns (status, body). The response body is
// truncated to 4 KiB so callers can include it in failure messages.
func (c *cmClient) postRaw(t *testing.T, path string, body any, into any) (int, string) {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("post encode %s: %v", path, err)
		}
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		t.Fatalf("post req %s: %v", path, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "human:harness")
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("post do %s: %v", path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if into != nil && resp.StatusCode < 400 && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("post decode %s: %v body=%s", path, err, raw)
		}
	}

	return resp.StatusCode, string(raw)
}

func (c *cmClient) get(t *testing.T, path string, into any) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		t.Fatalf("get req %s: %v", path, err)
	}

	req.Header.Set("X-Agent-ID", "human:harness")

	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("get do %s: %v", path, err)
	}
	defer resp.Body.Close()

	if into != nil && resp.StatusCode < 400 {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil && err != io.EOF {
			t.Fatalf("get decode %s: %v", path, err)
		}
	}

	return resp.StatusCode
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

type cardSnapshot struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	State             string          `json:"state"`
	AssignedAgent     string          `json:"assigned_agent"`
	RunnerStatus      string          `json:"runner_status"`
	Autonomous        bool            `json:"autonomous"`
	DiscoveryComplete bool            `json:"discovery_complete"`
	Body              string          `json:"body"`
	ReviewAttempts    int             `json:"review_attempts"`
	TokenUsage        *tokenUsage     `json:"token_usage,omitempty"`
	ActivityLog       []activityEntry `json:"activity_log"`
}

func (c *cmClient) getCard(t *testing.T, project, cardID string) cardSnapshot {
	t.Helper()

	var card cardSnapshot

	status := c.get(t, fmt.Sprintf("/api/projects/%s/cards/%s", project, cardID), &card)
	if status != http.StatusOK {
		t.Fatalf("getCard %s/%s: HTTP %d", project, cardID, status)
	}

	return card
}

// listCardsResponse mirrors the envelope CM returns from
// GET /api/projects/{project}/cards. Only Items is needed by callers
// here — pagination cursor + total are ignored because the harness
// canary scenarios fit comfortably under defaultCardPageLimit (500).
type listCardsResponse struct {
	Items []cardSnapshot `json:"items"`
}

// listCards fetches cards for a project, optionally filtered by parent ID.
func (c *cmClient) listCards(t *testing.T, project, parent string) []cardSnapshot {
	t.Helper()

	path := fmt.Sprintf("/api/projects/%s/cards", project)
	if parent != "" {
		path += "?parent=" + parent
	}

	var resp listCardsResponse

	status := c.get(t, path, &resp)
	if status != http.StatusOK {
		t.Fatalf("listCards %s parent=%s: HTTP %d", project, parent, status)
	}

	return resp.Items
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
