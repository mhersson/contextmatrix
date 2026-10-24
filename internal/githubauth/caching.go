package githubauth

import (
	"context"
	"strings"
	"sync"
	"time"
)

// defaultRefreshSkew is how close to expiry the cache considers a
// token "stale" and refreshes proactively.
const defaultRefreshSkew = 5 * time.Minute

// CachingProvider wraps a TokenGenerator and reuses the most recent
// token until it is within RefreshSkew of expiry. Errors are not cached
// - the next call retries the inner provider.
type CachingProvider struct {
	inner       TokenGenerator
	refreshSkew time.Duration

	mu      sync.Mutex
	token   string
	expires time.Time

	// Scoped tokens live apart from the broad one under their own lock so a
	// slow scoped mint never blocks the broad token.
	repoMu     sync.Mutex
	repoTokens map[string]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// Compile-time check.
var _ TokenGenerator = (*CachingProvider)(nil)

// WithRefreshSkew overrides the default 5-minute refresh window.
func WithRefreshSkew(d time.Duration) CacheOption {
	return func(c *CachingProvider) {
		if d > 0 {
			c.refreshSkew = d
		}
	}
}

// NewCachingProvider wraps inner with a cache.
func NewCachingProvider(inner TokenGenerator, opts ...CacheOption) *CachingProvider {
	c := &CachingProvider{
		inner:       inner,
		refreshSkew: defaultRefreshSkew,
		repoTokens:  make(map[string]cachedToken),
	}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// GenerateToken returns the cached token if it has more than refreshSkew
// remaining; otherwise it asks the inner provider for a fresh one.
func (c *CachingProvider) GenerateToken(ctx context.Context) (string, time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Until(c.expires) > c.refreshSkew {
		return c.token, c.expires, nil
	}

	token, exp, err := c.inner.GenerateToken(ctx)
	if err != nil {
		// Don't poison the cache on error - keep whatever was there
		// (which can be stale, but the next call retries the inner
		// provider).
		return "", time.Time{}, err
	}

	c.token = token
	c.expires = exp

	return token, exp, nil
}

// GenerateRepoToken is GenerateToken for a single repository, cached per
// repo name (case-insensitive) apart from the broad token.
func (c *CachingProvider) GenerateRepoToken(ctx context.Context, repo string) (string, time.Time, error) {
	c.repoMu.Lock()
	defer c.repoMu.Unlock()

	key := strings.ToLower(repo)

	if e, ok := c.repoTokens[key]; ok && e.token != "" && time.Until(e.expires) > c.refreshSkew {
		return e.token, e.expires, nil
	}

	token, exp, err := c.inner.GenerateRepoToken(ctx, repo)
	if err != nil {
		return "", time.Time{}, err
	}

	// A non-expiring token (PAT) needs no cache, and caching it would grow the
	// map with every caller-chosen name.
	if exp.Equal(patExpiry) {
		return token, exp, nil
	}

	c.repoTokens[key] = cachedToken{token: token, expires: exp}

	return token, exp, nil
}
