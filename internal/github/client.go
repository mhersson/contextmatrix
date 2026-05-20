package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	githubauth "github.com/mhersson/contextmatrix-githubauth"
	"github.com/mhersson/contextmatrix/internal/metrics"
)

// ErrRateLimited is returned when the GitHub API rate limit is exhausted.
var ErrRateLimited = errors.New("github: rate limit exceeded")

// ErrPermissionDenied is returned when GitHub responds with 403 for reasons
// other than rate limiting — revoked PAT, SAML SSO not authorised, missing
// repo permissions, IP allowlist denial, etc. The syncer must surface these
// as configuration problems rather than silently retrying on the next cycle
// the way it does for transient rate-limit responses.
var ErrPermissionDenied = errors.New("github: permission denied")

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
	provider   githubauth.TokenGenerator
	baseURL    string // configurable for testing; defaults to https://api.github.com
}

// NewClient creates a new GitHub API client with the given token provider.
// It uses the default GitHub API base URL (https://api.github.com).
func NewClient(provider githubauth.TokenGenerator) *Client {
	return NewClientWithBaseURL(provider, "")
}

// NewClientWithBaseURL creates a new GitHub API client with the given token
// provider and base URL. The base URL is trimmed of any trailing slash. If
// baseURL is empty, it defaults to https://api.github.com. Use this constructor
// when targeting GitHub Enterprise Server instances that expose the API at a
// custom host.
func NewClientWithBaseURL(provider githubauth.TokenGenerator, baseURL string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	transport := &http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   10,
	}

	return &Client{
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
		provider: provider,
		baseURL:  baseURL,
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
		issues, next, rateLimited, err := doPage[Issue](ctx, c, nextURL)
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

	if nextURL != "" {
		slog.Warn("github: FetchOpenIssues hit maxPages cap; some issues may be missing",
			"owner", owner, "repo", repo, "maxPages", maxPages)
		metrics.GitHubPagesTruncatedTotal.WithLabelValues("issues").Inc()
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
		items, next, rateLimited, err := doPage[branchItem](ctx, c, nextURL)
		if err != nil {
			return allNames, err
		}

		for _, b := range items {
			allNames = append(allNames, b.Name)
		}

		if rateLimited {
			// This page was the last one before the rate limit is hit.
			// Return what we have so far plus the sentinel error.
			slices.Sort(allNames)

			return allNames, ErrRateLimited
		}

		nextURL = next
	}

	if nextURL != "" {
		slog.Warn("github: FetchBranches hit maxPages cap; some branches may be missing",
			"owner", owner, "repo", repo, "maxPages", maxPages)
		metrics.GitHubPagesTruncatedTotal.WithLabelValues("branches").Inc()
	}

	slices.Sort(allNames)

	return allNames, nil
}

// pageDecoder is an interface satisfied by any type that can be JSON-decoded
// from a GitHub API list response. The constraint exists only to bound the
// type parameter; in practice any struct works.
type pageDecoder interface {
	Issue | branchItem
}

// doPage fetches a single paginated GitHub API page and decodes the JSON body
// into a slice of T. It returns the decoded items, the next-page URL (empty
// when exhausted), whether the rate limit is now exhausted after this page,
// and any transport/HTTP/decode error.
func doPage[T pageDecoder](ctx context.Context, c *Client, rawURL string) ([]T, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "contextmatrix")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	token, _, err := c.provider.GenerateToken(req.Context())
	if err != nil {
		return nil, "", false, fmt.Errorf("get github token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("http request: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	// 429 is unambiguously rate-limiting. 403 is overloaded — GitHub returns
	// it for genuine rate-limit responses (primary or secondary) and for many
	// non-rate-limit failures (revoked PAT, SAML SSO not authorised, missing
	// repo permissions, IP allowlist denial). The syncer treats ErrRateLimited
	// as transient, so misclassifying a permission failure as rate-limited
	// silently abandons every cycle without surfacing the auth problem.
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", false, ErrRateLimited
	}

	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		if isRateLimitForbidden(resp.Header, body) {
			return nil, "", false, ErrRateLimited
		}

		return nil, "", false, fmt.Errorf("%w: status 403: %s",
			ErrPermissionDenied, sanitizeBody(body))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

		return nil, "", false, fmt.Errorf("github api: status %d: %s", resp.StatusCode, sanitizeBody(body))
	}

	var items []T
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&items); err != nil {
		return nil, "", false, fmt.Errorf("decode response: %w", err)
	}

	next := c.parseLinkNext(resp.Header.Get("Link"))

	// Check if rate limit is now exhausted. The current page's data is valid;
	// remaining=0 means the *next* request would be rate-limited.
	rateLimited := false

	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, err := strconv.Atoi(remaining); err == nil && n <= 0 {
			rateLimited = true
		}
	}

	return items, next, rateLimited, nil
}

// isRateLimitForbidden distinguishes a rate-limit 403 from a permission 403.
// GitHub uses 403 for both primary rate limits (X-RateLimit-Remaining: 0) and
// secondary abuse-detection rate limits (the body contains "secondary rate
// limit"). Everything else — revoked PAT, SAML SSO, missing repo permissions,
// IP allowlist denial — is treated as a permission error so the syncer
// surfaces it instead of looping forever.
func isRateLimitForbidden(header http.Header, body []byte) bool {
	if remaining := header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, err := strconv.Atoi(remaining); err == nil && n <= 0 {
			return true
		}
	}

	// Body inspection is the fallback for secondary rate limits, which do not
	// always advertise themselves via X-RateLimit-Remaining=0. Match GitHub's
	// documented strings case-insensitively.
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "secondary rate limit") || strings.Contains(lower, "rate limit") {
		return true
	}

	return false
}

// sanitizeBody strips non-printable runes from remote-controlled error body
// bytes before they are interpolated into log/error strings.
func sanitizeBody(b []byte) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			return r
		}

		return -1
	}, string(b))
}

// linkNextRe matches rel="next" in a Link header.
var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// parseLinkNext extracts the "next" URL from a Link header.
// Returns empty string if absent, if the URL host does not match the host of
// c.baseURL, or if the URL scheme would downgrade from HTTPS to HTTP —
// preventing SSRF via manipulated Link headers and token leakage over plain HTTP.
func (c *Client) parseLinkNext(header string) string {
	if header == "" {
		return ""
	}

	m := linkNextRe.FindStringSubmatch(header)
	if len(m) < 2 {
		return ""
	}

	// Validate that the next URL points to the same host and scheme as the
	// configured base URL to prevent SSRF and scheme-downgrade attacks.
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return ""
	}

	next, err := url.Parse(m[1])
	if err != nil || next.Host != base.Host {
		return ""
	}

	// Disallow scheme downgrade (e.g. https → http), which would send the
	// bearer token over an unencrypted connection.
	if next.Scheme != base.Scheme {
		return ""
	}

	return m[1]
}
