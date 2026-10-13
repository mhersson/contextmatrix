package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	maxResponseBytes = 8 << 20
	maxErrorMessage  = 300
	// defaultRetryAfter applies when a 429 carries no usable Retry-After.
	defaultRetryAfter = 5 * time.Second
	// minRetryAfter floors every wait so a zero or past Retry-After cannot
	// turn the retry loop into a busy spin.
	minRetryAfter = time.Second
	// noDeadlineWaitBudget caps total 429 waiting for a ctx without a deadline.
	noDeadlineWaitBudget = 30 * time.Second
	userAgent            = "contextmatrix"
)

// Client calls the Jira Cloud REST v3 API.
type Client struct {
	baseURL string
	auth    Authorizer
	hc      *http.Client
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
}

// NewClient returns a client for apiBaseURL: the site URL, or the
// api.atlassian.com gateway URL for service-account scoped tokens. A nil hc
// gets a client with conservative timeouts that honours proxy env vars.
func NewClient(apiBaseURL string, auth Authorizer, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 15 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				MaxIdleConnsPerHost:   4,
			},
		}
	}

	return &Client{
		baseURL: strings.TrimRight(apiBaseURL, "/"),
		auth:    auth,
		hc:      hc,
		now:     time.Now,
		sleep:   sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do sends one API call and decodes a 2xx JSON body into out (nil discards
// it). A 429 is waited out and retried while the wait fits the budget.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}

		payload = b
	}

	var waited time.Duration

	for {
		resp, err := c.send(ctx, method, path, query, payload)
		if err != nil {
			return err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return decodeResponse(resp, out)
		}

		wait := max(parseRetryAfter(resp.Header.Get("Retry-After"), c.now()), minRetryAfter)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		if !c.canWait(ctx, waited, wait) {
			return &RateLimitError{RetryAfter: wait}
		}

		if err := c.sleep(ctx, wait); err != nil {
			return fmt.Errorf("wait out rate limit: %w", err)
		}

		waited += wait
	}
}

// canWait reports whether another wait fits: before the ctx deadline when
// there is one, else within noDeadlineWaitBudget in total.
func (c *Client) canWait(ctx context.Context, waited, wait time.Duration) bool {
	if deadline, ok := ctx.Deadline(); ok {
		return c.now().Add(wait).Before(deadline)
	}

	return waited+wait <= noDeadlineWaitBudget
}

func (c *Client) send(ctx context.Context, method, path string, query url.Values, payload []byte) (*http.Response, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if err := c.auth.Apply(req); err != nil {
		return nil, fmt.Errorf("authorize request: %w", err)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}

	return resp, nil
}

func decodeResponse(resp *http.Response, out any) error {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if len(body) > maxResponseBytes {
		return fmt.Errorf("response exceeds %d MiB limit", maxResponseBytes>>20)
	}

	captcha := strings.Contains(resp.Header.Get("X-Seraph-LoginReason"), "AUTHENTICATION_DENIED")

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &AuthError{captcha: captcha}
	case resp.StatusCode == http.StatusForbidden && captcha:
		return &AuthError{captcha: true}
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return &StatusError{StatusCode: resp.StatusCode, Message: errorMessage(body, resp.StatusCode)}
	}

	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// errorMessage extracts Jira's errorMessages and field errors, bounded and
// with control characters flattened, falling back to the HTTP status text.
func errorMessage(body []byte, status int) string {
	var parsed struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}

	var parts []string

	if json.Unmarshal(body, &parsed) == nil {
		parts = append(parts, parsed.ErrorMessages...)

		for _, field := range slices.Sorted(maps.Keys(parsed.Errors)) {
			parts = append(parts, field+": "+parsed.Errors[field])
		}
	}

	msg := strings.Join(parts, "; ")
	if msg == "" {
		msg = http.StatusText(status)
	}

	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}

		return r
	}, msg)

	if len(msg) > maxErrorMessage {
		msg = strings.ToValidUTF8(msg[:maxErrorMessage], "")
	}

	return msg
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)

	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}

	if at, err := http.ParseTime(v); err == nil {
		return max(at.Sub(now), 0)
	}

	return defaultRetryAfter
}
