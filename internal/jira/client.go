package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/config"
)

var (
	// ErrNotFound is returned when a Jira issue does not exist.
	ErrNotFound = errors.New("jira: issue not found")
	// ErrUnauthorized is returned when Jira rejects the credentials.
	ErrUnauthorized = errors.New("jira: unauthorized")
	// ErrRateLimited is returned when Jira rate-limits the request.
	ErrRateLimited = errors.New("jira: rate limit exceeded")
)

const (
	maxResponseBody = 10 << 20 // 10 MB
	searchPageSize  = 50
	maxSearchPages  = 10 // safety limit: 500 issues per epic
)

// Client is a Jira REST API client.
type Client struct {
	httpClient   *http.Client
	baseURL      string // e.g. https://company.atlassian.net
	email        string // non-empty → Basic Auth (Cloud), empty → Bearer (Server/DC)
	token        string
	sessionToken string // browser session cookie (testing only)
}

// NewClient creates a new Jira API client from the global config.
func NewClient(cfg config.JiraConfig) *Client {
	return &Client{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		email:        cfg.Email,
		token:        cfg.Token,
		sessionToken: cfg.SessionToken,
	}
}

// FetchIssue retrieves a single issue by key (e.g. "PROJ-42").
func (c *Client) FetchIssue(ctx context.Context, key string) (*Issue, error) {
	rawURL := fmt.Sprintf("%s/rest/api/3/issue/%s", c.baseURL, key)

	var issue Issue
	if err := c.get(ctx, rawURL, &issue); err != nil {
		return nil, fmt.Errorf("fetch issue %s: %w", key, err)
	}

	return &issue, nil
}

// FetchEpicChildren retrieves all child issues of an epic via JQL search.
// Uses cursor-based pagination (nextPageToken), capped at maxSearchPages.
func (c *Client) FetchEpicChildren(ctx context.Context, epicKey string) ([]Issue, error) {
	// "Epic Link" is the classic field; "parent" covers next-gen / team-managed projects.
	jql := fmt.Sprintf(`"Epic Link" = "%s" OR parent = "%s"`, epicKey, epicKey)
	fields := "summary,status,issuetype,priority,labels,components,description"

	var all []Issue
	nextPageToken := ""

	for range maxSearchPages {
		if ctx.Err() != nil {
			return all, ctx.Err()
		}

		rawURL := fmt.Sprintf("%s/rest/api/3/search/jql?jql=%s&maxResults=%d&fields=%s",
			c.baseURL, url.QueryEscape(jql), searchPageSize, url.QueryEscape(fields))
		if nextPageToken != "" {
			rawURL += "&nextPageToken=" + url.QueryEscape(nextPageToken)
		}

		var result searchResult
		if err := c.get(ctx, rawURL, &result); err != nil {
			return all, fmt.Errorf("search epic children: %w", err)
		}

		all = append(all, result.Issues...)

		if result.NextPageToken == "" {
			break
		}
		nextPageToken = result.NextPageToken
	}

	return all, nil
}

// PostComment adds a comment to a Jira issue.
func (c *Client) PostComment(ctx context.Context, issueKey, body string) error {
	rawURL := fmt.Sprintf("%s/rest/api/3/issue/%s/comment", c.baseURL, issueKey)

	payload := struct {
		Body string `json:"body"`
	}{Body: body}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal comment: %w", err)
	}

	if err := c.validateURL(rawURL); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "contextmatrix")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return checkResponseStatus(resp)
}

// get performs a GET request, validates the URL, and decodes the JSON response into dest.
func (c *Client) get(ctx context.Context, rawURL string, dest any) error {
	if err := c.validateURL(rawURL); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "contextmatrix")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := checkResponseStatus(resp); err != nil {
		return err
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(dest); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// checkResponseStatus maps Jira HTTP status codes to sentinel errors.
func checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jira api: status %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}

// setAuth sets the appropriate authentication on the request.
// Priority: session cookie → Basic Auth (Cloud) → Bearer token (Server/DC).
func (c *Client) setAuth(req *http.Request) {
	if c.sessionToken != "" {
		req.AddCookie(&http.Cookie{Name: "tenant.session.token", Value: c.sessionToken})
	} else if c.email != "" {
		req.SetBasicAuth(c.email, c.token)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// validateURL ensures the request URL matches the configured Jira base URL.
func (c *Client) validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}

	if u.Host != base.Host {
		return fmt.Errorf("jira: request to unexpected host %q (expected %q)", u.Host, base.Host)
	}

	return nil
}
