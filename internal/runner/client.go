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

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

const (
	// maxRetries is the number of retry attempts for transient failures.
	maxRetries = 3
	// requestTimeout is the per-request timeout.
	requestTimeout = 10 * time.Second
	// signatureHeader carries the HMAC-SHA256 signature.
	signatureHeader = "X-Signature-256"
)

// BackoffBase is the base duration for exponential retry backoff
// (waits of BackoffBase, 2*BackoffBase, 4*BackoffBase between attempts).
// Exported so tests can override it (remember to restore via t.Cleanup).
var BackoffBase = time.Second

// TriggerPayload is sent to the runner to start a task.
type TriggerPayload struct {
	CardID      string `json:"card_id"`
	Project     string `json:"project"`
	RepoURL     string `json:"repo_url"`
	MCPAPIKey   string `json:"mcp_api_key,omitempty"`
	RunnerImage string `json:"runner_image,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
	Interactive bool   `json:"interactive,omitempty"`
	Model       string `json:"model,omitempty"`
}

// KillPayload is sent to the runner to stop a specific task.
type KillPayload struct {
	CardID  string `json:"card_id"`
	Project string `json:"project"`
}

// MessagePayload is sent to the runner to deliver a human message to a running task.
type MessagePayload struct {
	CardID    string `json:"card_id"`
	Project   string `json:"project"`
	Content   string `json:"content"`
	MessageID string `json:"message_id"`
}

// PromotePayload is sent to the runner to promote a task from interactive pause to completion.
type PromotePayload struct {
	CardID  string `json:"card_id"`
	Project string `json:"project"`
}

// EndSessionPayload is sent to the runner to close the stdin of an interactive
// container so claude exits on EOF. Used when a released card reaches a
// terminal state.
type EndSessionPayload struct {
	CardID  string `json:"card_id"`
	Project string `json:"project"`
}

// StopAllPayload is sent to the runner to stop all tasks.
type StopAllPayload struct {
	Project string `json:"project,omitempty"`
}

// ContainerInfo is a decoded entry from GET /containers. The runner sources
// these from Docker directly (filtered on label contextmatrix.runner=true),
// so a populated slice is the authoritative answer to "what containers are
// actually running right now" — independent of the runner's in-memory tracker
// or of CM's runner_status field. The Docker-authoritative reconcile sweep
// uses this list as its decision input.
//
// Tracked reflects the runner's tracker state at response time: Tracked=false
// combined with State="running" is the tracker/Docker divergence signature
// that the older in-process cleanup paths could not detect.
type ContainerInfo struct {
	ContainerID   string
	ContainerName string
	CardID        string
	Project       string
	State         string
	StartedAt     time.Time
	Tracked       bool
}

// containerInfoWire is the on-the-wire shape of a /containers entry. Kept
// separate from ContainerInfo so the public type carries a parsed time.Time
// while the HTTP response stays string-valued.
type containerInfoWire struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name,omitempty"`
	CardID        string `json:"card_id"`
	Project       string `json:"project"`
	State         string `json:"state"`
	StartedAt     string `json:"started_at"`
	Tracked       bool   `json:"tracked"`
}

type listContainersResponseWire struct {
	OK         bool                `json:"ok"`
	Containers []containerInfoWire `json:"containers"`
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

// Message sends a human message to a running interactive task.
func (c *Client) Message(ctx context.Context, p MessagePayload) error {
	return c.send(ctx, c.baseURL+"/message", p)
}

// Promote sends a promote webhook to signal that an interactive task may proceed.
func (c *Client) Promote(ctx context.Context, p PromotePayload) error {
	return c.send(ctx, c.baseURL+"/promote", p)
}

// EndSession sends an end-session webhook so the runner closes the container's
// stdin; claude receives EOF and exits, ending the interactive session.
func (c *Client) EndSession(ctx context.Context, p EndSessionPayload) error {
	return c.send(ctx, c.baseURL+"/end-session", p)
}

// ListContainers queries the runner's /containers endpoint for every Docker
// container currently labeled as runner-managed. The returned slice is CM's
// ground truth for "what containers are actually running right now" —
// independent of any CM-side bookkeeping. An error here is not recoverable
// by retry at the call site: the caller should log and continue rather than
// risk firing spurious kills against a runner that briefly can't answer.
func (c *Client) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	body, err := c.sendGet(ctx, c.baseURL+"/containers")
	if err != nil {
		return nil, err
	}

	var parsed listContainersResponseWire
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse /containers response: %w", err)
	}

	if !parsed.OK {
		return nil, fmt.Errorf("runner /containers returned ok=false")
	}

	out := make([]ContainerInfo, 0, len(parsed.Containers))

	for _, c := range parsed.Containers {
		// A missing or malformed timestamp is non-fatal: we still want the
		// sweep to act on the container, just without the age-cap input.
		var started time.Time

		if c.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, c.StartedAt); err == nil {
				started = t
			}
		}

		out = append(out, ContainerInfo{
			ContainerID:   c.ContainerID,
			ContainerName: c.ContainerName,
			CardID:        c.CardID,
			Project:       c.Project,
			State:         c.State,
			StartedAt:     started,
			Tracked:       c.Tracked,
		})
	}

	return out, nil
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
		// Exponential backoff scaled from BackoffBase (default: 1s, 2s, 4s).
		backoff := time.Duration(1<<uint(attempt)) * BackoffBase
		ctxlog.Logger(ctx).Warn("runner webhook transient error, retrying",
			"url", url,
			"attempt", attempt+1,
			"backoff", backoff,
			"error", lastErr,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}

	return fmt.Errorf("runner webhook failed after %d attempts: %w", maxRetries, lastErr)
}

// sendGet signs and performs a GET against url, returning the raw response
// body. GET requests sign an empty body under the same HMAC scheme the
// runner accepts on POST endpoints, so the existing replay/skew protections
// apply uniformly. Errors are not retried here: the reconcile sweep calls
// this on a 60s tick and a transient failure is better surfaced than
// silently retried (the next tick retries anyway).
func (c *Client) sendGet(ctx context.Context, url string) ([]byte, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signPayloadWithTimestamp(c.apiKey, nil, ts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set(signatureHeader, "sha256="+signature)
	req.Header.Set(timestampHeader, ts)

	result := "failure"

	defer func() { metrics.RunnerWebhookTotal.WithLabelValues(result).Inc() }()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send GET request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read GET response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &webhookError{
			statusCode: resp.StatusCode,
			body:       string(respBody),
		}
	}

	result = "success"

	return respBody, nil
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

	result := "failure"

	defer func() { metrics.RunnerWebhookTotal.WithLabelValues(result).Inc() }()

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

	var parsed WebhookResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !parsed.OK {
		// Runner explicitly rejected — do not retry.
		return &webhookError{
			statusCode: resp.StatusCode,
			body:       parsed.Error,
			clientErr:  true,
		}
	}

	result = "success"

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

// HTTPStatusCode exposes the runner's response status so callers can
// classify errors without depending on the concrete type.
func (e *webhookError) HTTPStatusCode() int {
	return e.statusCode
}

// isClientError returns true if err should not be retried (4xx or logical rejection).
func isClientError(err error) bool {
	var we *webhookError
	if errors.As(err, &we) {
		return we.clientErr || (we.statusCode >= 400 && we.statusCode < 500)
	}

	return false
}
