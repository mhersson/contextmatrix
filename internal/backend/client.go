package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

const (
	// maxRetries is the number of retry attempts for transient failures.
	maxRetries = 3
	// requestTimeout is the per-request timeout.
	requestTimeout = 10 * time.Second
)

// BackoffBase is the base duration for exponential retry backoff
// (waits of BackoffBase, 2*BackoffBase, 4*BackoffBase between attempts).
// Exported so tests can override it (remember to restore via t.Cleanup).
var BackoffBase = time.Second

// Wire DTOs are defined in contextmatrix-protocol; aliased here so existing
// call sites and tests keep compiling unchanged.
type (
	TriggerPayload    = protocol.TriggerPayload
	KillPayload       = protocol.KillPayload
	MessagePayload    = protocol.MessagePayload
	PromotePayload    = protocol.PromotePayload
	EndSessionPayload = protocol.EndSessionPayload
	StopAllPayload    = protocol.StopAllPayload
)

// ContainerInfo is a decoded entry from GET /containers. The backend sources
// these from Docker directly (filtered to the worker containers it manages),
// so a populated slice is the authoritative answer to "what containers are
// actually running right now" — independent of the backend's in-memory tracker
// or of CM's runner_status field. The Docker-authoritative reconcile sweep
// uses this list as its decision input.
//
// Tracked reflects the backend's tracker state at response time: Tracked=false
// combined with State="running" is the tracker/Docker divergence signature
// that the older in-process cleanup paths could not detect.
type ContainerInfo struct {
	ContainerID string
	CardID      string
	SessionID   string
	Project     string
	State       string
	StartedAt   time.Time
	Tracked     bool
}

// Client sends signed webhooks to the task backend.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

// NewClient creates a new backend webhook client.
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

// EndSession sends an end-session webhook so the backend closes the worker
// container's stdin; claude receives EOF and exits, ending the interactive
// session.
func (c *Client) EndSession(ctx context.Context, p EndSessionPayload) error {
	return c.send(ctx, c.baseURL+"/end-session", p)
}

// HealthInfo is the parsed shape of the backend's /health response.
type HealthInfo struct {
	OK                bool
	RunningContainers int
	MaxConcurrent     int
}

// Health queries the backend's /health endpoint. The backend exposes
// max_concurrent (its global capacity cap) and the live container count.
// /health is unauthenticated on the backend side; we still sign so the
// shared transport stays consistent.
func (c *Client) Health(ctx context.Context) (HealthInfo, error) {
	body, err := c.sendGet(ctx, c.baseURL+"/health")
	if err != nil {
		return HealthInfo{}, err
	}

	var parsed protocol.HealthResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return HealthInfo{}, fmt.Errorf("parse /health response: %w", err)
	}

	return HealthInfo(parsed), nil
}

// ListContainers queries the backend's /containers endpoint for every worker
// container it currently manages. The returned slice is CM's ground truth
// for "what containers are actually running right now" — independent of any
// CM-side bookkeeping. An error here is not recoverable by retry at the call
// site: the caller should log and continue rather than risk firing spurious
// kills against a backend that briefly can't answer.
func (c *Client) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	body, err := c.sendGet(ctx, c.baseURL+"/containers")
	if err != nil {
		return nil, err
	}

	var parsed protocol.ListContainersResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse /containers response: %w", err)
	}

	if !parsed.OK {
		return nil, fmt.Errorf("backend /containers returned ok=false")
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
			ContainerID: c.ContainerID,
			CardID:      c.CardID,
			SessionID:   c.SessionID,
			Project:     c.Project,
			State:       c.State,
			StartedAt:   started,
			Tracked:     c.Tracked,
		})
	}

	return out, nil
}

// requestURI extracts the request-target form (path + "?" + raw query) of an
// absolute URL for HMAC signing. The receiver binds the signature to
// r.URL.RequestURI(), so sender and receiver must agree — any URI-rewriting
// proxy between them would break auth. An empty path is normalized to "/"
// to match how net/http reports the default root path.
func requestURI(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url %q: %w", rawURL, err)
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	if u.RawQuery != "" {
		return path + "?" + u.RawQuery, nil
	}

	return path, nil
}

// send marshals payload, signs it, and POSTs to rawURL with retries.
func (c *Client) send(ctx context.Context, rawURL string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	uri, err := requestURI(rawURL)
	if err != nil {
		return err
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signature := protocol.SignPayloadWithTimestamp(c.apiKey, http.MethodPost, uri, body, ts)

	var lastErr error
	for attempt := range maxRetries {
		lastErr = c.doRequest(ctx, rawURL, body, signature, ts)
		if lastErr == nil {
			return nil
		}
		// Only retry on transient errors (not 4xx).
		if isClientError(lastErr) {
			return lastErr
		}
		// Exponential backoff with ±25% jitter to spread concurrent retries.
		// Cap the shift at 30 to avoid int overflow if maxRetries ever grows
		// past ~30 (1<<31 overflows int32 on 32-bit; 1<<63 overflows int64).
		shift := min(attempt, 30)

		base := time.Duration(1<<uint(shift)) * BackoffBase
		// jitter is in [-25%, +25%] of base
		jitter := time.Duration(rand.Int64N(int64(base)/2) - int64(base)/4) //nolint:gosec // non-security jitter
		backoff := base + jitter
		ctxlog.Logger(ctx).Warn("backend webhook transient error, retrying",
			"url", rawURL,
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

	return fmt.Errorf("backend webhook failed after %d attempts: %w", maxRetries, lastErr)
}

// sendGet signs and performs a GET against rawURL, returning the raw response
// body. GET requests sign an empty body under the same HMAC scheme the
// backend accepts on POST endpoints, so the existing replay/skew protections
// apply uniformly. Errors are not retried here: the reconcile sweep calls
// this on a 60s tick and a transient failure is better surfaced than
// silently retried (the next tick retries anyway).
func (c *Client) sendGet(ctx context.Context, rawURL string) ([]byte, error) {
	uri, err := requestURI(rawURL)
	if err != nil {
		return nil, err
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signature := protocol.SignPayloadWithTimestamp(c.apiKey, http.MethodGet, uri, nil, ts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create GET request: %w", err)
	}

	req.Header.Set(protocol.SignatureHeader, "sha256="+signature)
	req.Header.Set(protocol.TimestampHeader, ts)

	result := "failure"

	defer func() { metrics.BackendWebhookTotal.WithLabelValues(result).Inc() }()

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

// doRequest performs a single HTTP request to the backend.
func (c *Client) doRequest(ctx context.Context, url string, body []byte, signature, ts string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.SignatureHeader, "sha256="+signature)
	req.Header.Set(protocol.TimestampHeader, ts)

	result := "failure"

	defer func() { metrics.BackendWebhookTotal.WithLabelValues(result).Inc() }()

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
			body:       rejectionDetail(respBody),
		}
	}

	// Only reached on <400, where the backend contract says
	// protocol.SuccessResponse — so ok:false here is the off-contract /
	// logical-rejection case. Decode as ErrorResponse, a field superset of
	// what we branch on; on-contract non-2xx rejections are decoded in the
	// >=400 branch above.
	var parsed protocol.ErrorResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !parsed.OK {
		// Backend explicitly rejected — do not retry.
		return &webhookError{
			statusCode: resp.StatusCode,
			body:       rejectionDetail(respBody),
			clientErr:  true,
		}
	}

	result = "success"

	return nil
}

// rejectionDetail extracts a human-readable detail from a backend rejection
// body: the stable `code: message` pair from protocol.ErrorResponse when
// present, falling back to the raw body when it doesn't decode or carries
// neither field (e.g. stop-all's 207 StopAllResponse shape).
func rejectionDetail(respBody []byte) string {
	var parsed protocol.ErrorResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return string(respBody)
	}

	switch {
	case parsed.Code != "" && parsed.Message != "":
		return parsed.Code + ": " + parsed.Message
	case parsed.Code != "":
		return parsed.Code
	case parsed.Message != "":
		return parsed.Message
	default:
		return string(respBody)
	}
}

// webhookError represents an HTTP error response from the backend.
type webhookError struct {
	statusCode int
	body       string
	clientErr  bool // true for logical rejections (ok:false) — never retry
}

func (e *webhookError) Error() string {
	return fmt.Sprintf("backend returned HTTP %d: %s", e.statusCode, e.body)
}

// HTTPStatusCode exposes the backend's response status so callers can
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
