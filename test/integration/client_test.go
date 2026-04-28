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

func (c *cmClient) post(t *testing.T, path string, body any, into any) int {
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
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatalf("post do %s: %v", path, err)
	}
	defer resp.Body.Close()
	if into != nil && resp.StatusCode < 400 {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil && err != io.EOF {
			t.Fatalf("post decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
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
}

type cardSnapshot struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	State          string          `json:"state"`
	AssignedAgent  string          `json:"assigned_agent"`
	RunnerStatus   string          `json:"runner_status"`
	Autonomous     bool            `json:"autonomous"`
	ReviewAttempts int             `json:"review_attempts"`
	ActivityLog    []activityEntry `json:"activity_log"`
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

func hasActivityEntry(card cardSnapshot, action, message string) bool {
	for _, e := range card.ActivityLog {
		if e.Action == action && e.Message == message {
			return true
		}
	}
	return false
}

func phaseMarkers(card cardSnapshot) []string {
	var out []string
	for _, e := range card.ActivityLog {
		if e.Action == "phase" {
			out = append(out, e.Message)
		}
	}
	return out
}
