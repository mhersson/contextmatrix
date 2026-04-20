package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrRateLimited is returned when the GitHub API rate limit is exhausted.
var ErrRateLimited = errors.New("github: rate limit exceeded")

const (
	defaultBaseURL  = "https://api.github.com"
	perPage         = 100
	maxPages        = 10       // safety limit to avoid unbounded pagination
	maxResponseBody = 10 << 20 // 10 MB limit on successful response bodies
)

// Issue represents a GitHub issue (subset of fields).
type Issue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Labels      []Label   `json:"labels"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// Label represents a GitHub issue label.
type Label struct {
	Name string `json:"name"`
}

// Client is a GitHub REST API client.
type Client struct {
	httpClient *http.Client
	token      string
	baseURL    string // configurable for testing; defaults to https://api.github.com
}

// NewClient creates a new GitHub API client with the given token.
// It uses the default GitHub API base URL (https://api.github.com).
func NewClient(token string) *Client {
	return NewClientWithBaseURL(token, "")
}

// NewClientWithBaseURL creates a new GitHub API client with the given token and
// base URL. The base URL is trimmed of any trailing slash. If baseURL is empty,
// it defaults to https://api.github.com. Use this constructor when targeting
// GitHub Enterprise Server instances that expose the API at a custom host.
func NewClientWithBaseURL(token, baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
		baseURL:    baseURL,
	}
}

// FetchOpenIssues retrieves all open issues (not PRs) from the given repository.
// If labelFilter is non-empty, only issues matching ALL specified labels are returned.
func (c *Client) FetchOpenIssues(ctx context.Context, owner, repo string, labelFilter []string) ([]Issue, error) {
	u, err := url.Parse(fmt.Sprintf("%s/repos/%s/%s/issues",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo)))
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	q := u.Query()
	q.Set("state", "open")
	q.Set("sort", "created")
	q.Set("direction", "desc")
	q.Set("per_page", strconv.Itoa(perPage))

	if len(labelFilter) > 0 {
		q.Set("labels", strings.Join(labelFilter, ","))
	}

	u.RawQuery = q.Encode()

	var allIssues []Issue

	nextURL := u.String()

	for page := 0; page < maxPages && nextURL != ""; page++ {
		issues, next, rateLimited, err := c.fetchPage(ctx, nextURL)
		if err != nil {
			return allIssues, err
		}

		for _, issue := range issues {
			// GitHub's issues endpoint also returns pull requests; skip them.
			if issue.PullRequest != nil {
				continue
			}

			allIssues = append(allIssues, issue)
		}

		if rateLimited {
			// This page was the last one before the rate limit is hit.
			// Return what we have so far plus the sentinel error.
			return allIssues, ErrRateLimited
		}

		nextURL = next
	}

	return allIssues, nil
}

// branchItem is the minimal subset of a GitHub branch object used by FetchBranches.
type branchItem struct {
	Name string `json:"name"`
}

// FetchBranches retrieves all branch names from the given repository, sorted alphabetically.
func (c *Client) FetchBranches(ctx context.Context, owner, repo string) ([]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/repos/%s/%s/branches",
		c.baseURL, url.PathEscape(owner), url.PathEscape(repo)))
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	q := u.Query()
	q.Set("per_page", strconv.Itoa(perPage))
	u.RawQuery = q.Encode()

	var allNames []string

	nextURL := u.String()

	for page := 0; page < maxPages && nextURL != ""; page++ {
		names, next, rateLimited, err := c.fetchBranchPage(ctx, nextURL)
		if err != nil {
			return allNames, err
		}

		allNames = append(allNames, names...)

		if rateLimited {
			// This page was the last one before the rate limit is hit.
			// Return what we have so far plus the sentinel error.
			sort.Strings(allNames)

			return allNames, ErrRateLimited
		}

		nextURL = next
	}

	sort.Strings(allNames)

	return allNames, nil
}

// fetchBranchPage fetches a single page of branches. Returns the branch names, the next
// page URL (empty if none), whether the rate limit is now exhausted, and any error.
func (c *Client) fetchBranchPage(ctx context.Context, rawURL string) ([]string, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "contextmatrix")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("http request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", false, ErrRateLimited
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return nil, "", false, fmt.Errorf("github api: status %d: %s", resp.StatusCode, string(body))
	}

	var branches []branchItem
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&branches); err != nil {
		return nil, "", false, fmt.Errorf("decode response: %w", err)
	}

	names := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.Name)
	}

	next := c.parseLinkNext(resp.Header.Get("Link"))

	// Check if rate limit is now exhausted. The current page's data is valid;
	// remaining=0 means the *next* request would be rate-limited.
	rateLimited := false

	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, _ := strconv.Atoi(remaining); n == 0 {
			rateLimited = true
		}
	}

	return names, next, rateLimited, nil
}

// fetchPage fetches a single page of issues. Returns the issues, the next page
// URL (empty if none), whether the rate limit is now exhausted, and any error.
func (c *Client) fetchPage(ctx context.Context, rawURL string) ([]Issue, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "contextmatrix")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("http request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", false, ErrRateLimited
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return nil, "", false, fmt.Errorf("github api: status %d: %s", resp.StatusCode, string(body))
	}

	var issues []Issue
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&issues); err != nil {
		return nil, "", false, fmt.Errorf("decode response: %w", err)
	}

	next := c.parseLinkNext(resp.Header.Get("Link"))

	// Check if rate limit is now exhausted. The current page's data is valid;
	// remaining=0 means the *next* request would be rate-limited.
	rateLimited := false

	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, _ := strconv.Atoi(remaining); n == 0 {
			rateLimited = true
		}
	}

	return issues, next, rateLimited, nil
}

// linkNextRe matches rel="next" in a Link header.
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// parseLinkNext extracts the "next" URL from a Link header.
// Returns empty string if absent or if the URL host does not match the host
// of c.baseURL — preventing SSRF via manipulated Link headers.
func (c *Client) parseLinkNext(header string) string {
	if header == "" {
		return ""
	}

	m := linkNextRe.FindStringSubmatch(header)
	if len(m) < 2 {
		return ""
	}

	// Validate that the next URL points to the same host as the configured
	// base URL to prevent SSRF via manipulated Link headers.
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return ""
	}

	next, err := url.Parse(m[1])
	if err != nil || next.Host != base.Host {
		return ""
	}

	return m[1]
}
