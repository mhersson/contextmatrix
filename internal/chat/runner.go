package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/runner"
)

// sseStreamClient is a package-level HTTP client for long-lived SSE connections.
// Timeout 0 prevents the per-request deadline from terminating the stream;
// cancellation is driven by the request context instead.
var sseStreamClient = &http.Client{Timeout: 0}

// ErrOversizedSSELine is returned by StreamLogs when the scanner encounters a
// line that exceeds the 1 MiB buffer cap. The bufio.Scanner state is
// unrecoverable after ErrTooLong, so the connection is closed and this
// sentinel is returned so the consumer in startConsumer can retry with
// exponential backoff rather than treating the disconnect as a clean close.
var ErrOversizedSSELine = errors.New("chat: /logs: oversized SSE line exceeded buffer")

// RunnerClientConfig wires the HMAC-signed webhook client.
type RunnerClientConfig struct {
	BaseURL    string       // e.g. http://contextmatrix-runner:8080
	HMACKey    string       // pre-shared HMAC secret
	MCPAPIKey  string       // forwarded to chat containers as CM_MCP_API_KEY
	HTTPClient *http.Client // optional; defaults to a 30s-timeout client
}

// runnerClient implements RunnerClient by talking HMAC-signed HTTP to the
// runner's /chat/* and /message endpoints.
type runnerClient struct {
	baseURL   string
	key       string
	mcpAPIKey string
	httpc     *http.Client
}

// NewRunnerClient constructs a RunnerClient. If cfg.HTTPClient is nil, a
// 30-second-timeout default client is used.
func NewRunnerClient(cfg RunnerClientConfig) RunnerClient {
	c := cfg.HTTPClient
	if c == nil {
		c = &http.Client{Timeout: 30 * time.Second}
	}

	return &runnerClient{baseURL: cfg.BaseURL, key: cfg.HMACKey, mcpAPIKey: cfg.MCPAPIKey, httpc: c}
}

type chatStartPayload struct {
	SessionID string         `json:"session_id"`
	Project   string         `json:"project,omitempty"`
	RepoURL   string         `json:"repo_url,omitempty"`
	MCPAPIKey string         `json:"mcp_api_key,omitempty"`
	Model     string         `json:"model,omitempty"`
	Resume    *ResumeContext `json:"resume,omitempty"`
	Primer    string         `json:"primer,omitempty"`
}

type chatEndPayload struct {
	SessionID string `json:"session_id"`
}

type messagePayload struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	MessageID string `json:"message_id,omitempty"`
}

func (c *runnerClient) StartChat(ctx context.Context, opts StartChatOpts) (string, error) {
	body, err := json.Marshal(chatStartPayload{
		SessionID: opts.SessionID,
		Project:   opts.Project,
		RepoURL:   opts.RepoURL,
		MCPAPIKey: c.mcpAPIKey,
		Model:     opts.Model,
		Resume:    opts.Resume,
		Primer:    opts.Primer,
	})
	if err != nil {
		return "", fmt.Errorf("chat: runner: marshal StartChat payload: %w", err)
	}

	resp, err := c.post(ctx, "/chat/start", body)
	if err != nil {
		return "", err
	}

	var out struct {
		ContainerID string `json:"container_id"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		return "", fmt.Errorf("chat: decode StartChat resp: %w", err)
	}

	return out.ContainerID, nil
}

func (c *runnerClient) EndChat(ctx context.Context, sessionID string) error {
	body, err := json.Marshal(chatEndPayload{SessionID: sessionID})
	if err != nil {
		return fmt.Errorf("chat: runner: marshal EndChat payload: %w", err)
	}

	_, err = c.post(ctx, "/chat/end", body)

	return err
}

func (c *runnerClient) SendChatMessage(ctx context.Context, sessionID, content, messageID string) error {
	body, err := json.Marshal(messagePayload{SessionID: sessionID, Content: content, MessageID: messageID})
	if err != nil {
		return fmt.Errorf("chat: runner: marshal SendChatMessage payload: %w", err)
	}

	_, err = c.post(ctx, "/message", body)

	return err
}

// runnerLogEntry mirrors the runner's logbroadcast.LogEntry JSON shape.
type runnerLogEntry struct {
	Timestamp time.Time       `json:"ts"`
	SessionID string          `json:"session_id,omitempty"`
	Type      string          `json:"type"`
	Content   string          `json:"content,omitempty"`
	Usage     *runnerLogUsage `json:"usage,omitempty"`
	Model     string          `json:"model,omitempty"`
}

// runnerLogUsage mirrors the runner's logbroadcast.TokenUsage JSON shape.
type runnerLogUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CacheReadTokens   int64 `json:"cache_read_tokens"`
	CacheCreateTokens int64 `json:"cache_creation_tokens"`
}

// StreamLogs subscribes to the runner's HMAC-signed /logs?session_id=<id>
// SSE endpoint and dispatches each parsed entry to onEntry. The HTTP client
// is constructed without a timeout for this call because the SSE connection
// is long-lived; cancellation is via ctx.
func (c *runnerClient) StreamLogs(ctx context.Context, sessionID string, onEntry func(LogEntry)) error {
	fullURL := c.baseURL + "/logs?session_id=" + url.QueryEscape(sessionID)

	parsed, err := url.Parse(fullURL)
	if err != nil {
		return fmt.Errorf("chat: parse logs URL: %w", err)
	}

	uri := parsed.RequestURI()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("chat: build logs request: %w", err)
	}

	sig, ts := runner.SignRequestHeaders(c.key, http.MethodGet, uri, nil)

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	// Use the package-level no-timeout client for the SSE stream; ctx drives cancellation.
	resp, err := sseStreamClient.Do(req)
	if err != nil {
		return fmt.Errorf("chat: /logs request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return fmt.Errorf("chat: /logs: status %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()

		// Accept both "data:" and "data: " (with or without trailing space).
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}

		var entry runnerLogEntry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			// Log at Debug so schema drift is observable without spamming production.
			preview := raw
			if len(preview) > 256 {
				preview = preview[:256]
			}

			slog.Debug("chat: /logs: unparseable SSE frame", "preview", preview, "err", err)

			continue
		}

		out := LogEntry{
			Timestamp: entry.Timestamp,
			Type:      entry.Type,
			Content:   entry.Content,
			Model:     entry.Model,
		}

		if entry.Usage != nil {
			out.Usage = &TokenUsage{
				InputTokens:       entry.Usage.InputTokens,
				OutputTokens:      entry.Usage.OutputTokens,
				CacheReadTokens:   entry.Usage.CacheReadTokens,
				CacheCreateTokens: entry.Usage.CacheCreateTokens,
			}
		}

		onEntry(out)
	}

	if err := scanner.Err(); err != nil {
		// If a single SSE line exceeds the 1 MiB buffer cap the scanner state
		// is unrecoverable — return ErrOversizedSSELine so startConsumer's
		// retry loop reconnects rather than treating it as a clean close.
		if errors.Is(err, bufio.ErrTooLong) {
			slog.Warn("chat: /logs: oversized SSE line; reconnecting", "session_id", sessionID)

			return ErrOversizedSSELine
		}

		return fmt.Errorf("chat: /logs scan: %w", err)
	}

	return nil
}

// post sends an HMAC-signed POST and returns the body on 2xx.
func (c *runnerClient) post(ctx context.Context, path string, body []byte) ([]byte, error) {
	fullURL := c.baseURL + path

	parsed, err := url.Parse(fullURL)
	if err != nil {
		return nil, fmt.Errorf("chat: parse URL: %w", err)
	}

	uri := parsed.RequestURI() // path + "?" + raw query (or just path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("chat: build request: %w", err)
	}

	sig, ts := runner.SignRequestHeaders(c.key, http.MethodPost, uri, body)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("X-Webhook-Timestamp", ts)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat: %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("chat: %s: read response: %w", path, err)
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chat: %s: status %d: %s", path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
