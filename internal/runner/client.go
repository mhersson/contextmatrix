package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	// maxRetries is the number of retry attempts for transient failures.
	maxRetries = 3
	// requestTimeout is the per-request timeout.
	requestTimeout = 10 * time.Second
	// signatureHeader carries the HMAC-SHA256 signature.
	signatureHeader = "X-Signature-256"
)

// TriggerPayload is sent to the runner to start a task.
type TriggerPayload struct {
	CardID      string `json:"card_id"`
	Project     string `json:"project"`
	RepoURL     string `json:"repo_url"`
	MCPURL      string `json:"mcp_url"`
	MCPAPIKey   string `json:"mcp_api_key,omitempty"`
	RunnerImage string `json:"runner_image,omitempty"`
}

// KillPayload is sent to the runner to stop a specific task.
type KillPayload struct {
	CardID  string `json:"card_id"`
	Project string `json:"project"`
}

// StopAllPayload is sent to the runner to stop all tasks.
type StopAllPayload struct {
	Project string `json:"project,omitempty"`
}

// WebhookResponse is the expected response from the runner.
type WebhookResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Client sends signed webhooks to the contextmatrix-runner.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a new runner webhook client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: requestTimeout},
		baseURL:    baseURL,
		apiKey:     apiKey,
	}
}

// Trigger sends a trigger webhook to start a task.
func (c *Client) Trigger(ctx context.Context, p TriggerPayload) error {
	return c.send(ctx, c.baseURL+"/trigger", p)
}

// Kill sends a kill webhook to stop a specific task.
func (c *Client) Kill(ctx context.Context, p KillPayload) error {
	return c.send(ctx, c.baseURL+"/kill", p)
}

// StopAll sends a stop-all webhook to stop all tasks.
func (c *Client) StopAll(ctx context.Context, p StopAllPayload) error {
	return c.send(ctx, c.baseURL+"/stop-all", p)
}

// send marshals payload, signs it, and POSTs to url with retries.
func (c *Client) send(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signPayloadWithTimestamp(c.apiKey, body, ts)

	var lastErr error
	for attempt := range maxRetries {
		lastErr = c.doRequest(ctx, url, body, signature, ts)
		if lastErr == nil {
			return nil
		}
		// Only retry on transient errors (not 4xx).
		if isClientError(lastErr) {
			return lastErr
		}
		// Exponential backoff: 1s, 2s, 4s
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("runner webhook failed after %d attempts: %w", maxRetries, lastErr)
}

// doRequest performs a single HTTP request to the runner.
func (c *Client) doRequest(ctx context.Context, url string, body []byte, signature, ts string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(signatureHeader, "sha256="+signature)
	req.Header.Set(timestampHeader, ts)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &webhookError{
			statusCode: resp.StatusCode,
			body:       string(respBody),
		}
	}

	var result WebhookResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		// Runner explicitly rejected — do not retry.
		return &webhookError{
			statusCode: resp.StatusCode,
			body:       result.Error,
			clientErr:  true,
		}
	}

	return nil
}

// webhookError represents an HTTP error response from the runner.
type webhookError struct {
	statusCode int
	body       string
	clientErr  bool // true for logical rejections (ok:false) — never retry
}

func (e *webhookError) Error() string {
	return fmt.Sprintf("runner returned HTTP %d: %s", e.statusCode, e.body)
}

// isClientError returns true if err should not be retried (4xx or logical rejection).
func isClientError(err error) bool {
	var we *webhookError
	if errors.As(err, &we) {
		return we.clientErr || (we.statusCode >= 400 && we.statusCode < 500)
	}
	return false
}
