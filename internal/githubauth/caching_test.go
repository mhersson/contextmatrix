package githubauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider counts calls and returns a configured (token, expiresAt, err).
type fakeProvider struct {
	mu      sync.Mutex
	calls   atomic.Int32
	tokens  []string  // returned in order; last value reused if exhausted
	expiry  time.Time // returned for every call
	failOn  int       // call number (1-indexed) on which to return the error
	failErr error

	repoCalls   atomic.Int32
	lastRepo    string
	repoFailErr error // returned by GenerateRepoToken when non-nil
}

func (f *fakeProvider) GenerateRepoToken(_ context.Context, repo string) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := f.repoCalls.Add(1)
	f.lastRepo = repo

	if f.repoFailErr != nil {
		err := f.repoFailErr
		f.repoFailErr = nil

		return "", time.Time{}, err
	}

	return fmt.Sprintf("scoped-%s-%d", repo, n), f.expiry, nil
}

func (f *fakeProvider) GenerateToken(_ context.Context) (string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls.Add(1)
	n := int(f.calls.Load())

	if f.failOn != 0 && n == f.failOn {
		return "", time.Time{}, f.failErr
	}

	idx := n - 1
	if idx >= len(f.tokens) {
		idx = len(f.tokens) - 1
	}

	return f.tokens[idx], f.expiry, nil
}

func TestCachingProvider_FreshCacheReused(t *testing.T) {
	inner := &fakeProvider{
		tokens: []string{"first", "second"},
		expiry: time.Now().Add(45 * time.Minute),
	}
	cp := NewCachingProvider(inner)

	// First call: cache miss; mints "first".
	token, _, err := cp.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", token)

	// Second call: still inside validity → cache hit; returns "first" again.
	token, _, err = cp.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", token)

	assert.Equal(t, int32(1), inner.calls.Load(), "inner provider should be called once")
}

func TestCachingProvider_NearExpiryRefreshes(t *testing.T) {
	inner := &fakeProvider{
		tokens: []string{"first", "second"},
		expiry: time.Now().Add(2 * time.Minute), // < 5m default skew
	}
	cp := NewCachingProvider(inner)

	token, _, err := cp.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", token)

	// First call cached "first" with expiry 2m out - that's already
	// within skew, so the next call should refresh and return "second".
	token, _, err = cp.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "second", token)
	assert.Equal(t, int32(2), inner.calls.Load())
}

func TestCachingProvider_CustomSkew(t *testing.T) {
	inner := &fakeProvider{
		tokens: []string{"first", "second"},
		expiry: time.Now().Add(10 * time.Minute),
	}
	// With a 30-minute skew, a 10-minute-out token is already considered
	// stale and triggers a refresh on every call.
	cp := NewCachingProvider(inner)
	cp.refreshSkew = 30 * time.Minute

	_, _, _ = cp.GenerateToken(context.Background())
	_, _, _ = cp.GenerateToken(context.Background())

	assert.Equal(t, int32(2), inner.calls.Load())
}

func TestCachingProvider_PropagatesError(t *testing.T) {
	inner := &fakeProvider{
		tokens:  []string{"x"},
		expiry:  time.Now().Add(10 * time.Minute),
		failOn:  1,
		failErr: errSentinel("boom"),
	}
	cp := NewCachingProvider(inner)

	_, _, err := cp.GenerateToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestCachingProvider_RetriesAfterError(t *testing.T) {
	inner := &fakeProvider{
		tokens:  []string{"first", "second"},
		expiry:  time.Now().Add(45 * time.Minute),
		failOn:  1, // first call fails
		failErr: errSentinel("boom"),
	}
	cp := NewCachingProvider(inner)

	// First call: fails; cache empty.
	_, _, err := cp.GenerateToken(context.Background())
	require.Error(t, err)

	// Second call: inner succeeds on call n=2, returning tokens[1]="second".
	// The key property: errors are not cached - the retry reaches the inner
	// provider rather than returning the previous error.
	token, _, err := cp.GenerateToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "second", token)
}

func TestCachingProvider_Concurrent(t *testing.T) {
	inner := &fakeProvider{
		tokens: []string{"once"},
		expiry: time.Now().Add(45 * time.Minute),
	}
	cp := NewCachingProvider(inner)

	const goroutines = 50

	tokens := make(chan string, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			tok, _, err := cp.GenerateToken(context.Background())
			assert.NoError(t, err)

			tokens <- tok
		}()
	}

	wg.Wait()
	close(tokens)

	// All goroutines should see the same token.
	for tok := range tokens {
		assert.Equal(t, "once", tok)
	}
	// Inner provider should have been called at most a small number of
	// times. With a single-mutex serialization, exactly once.
	assert.Equal(t, int32(1), inner.calls.Load())
}

// errSentinel is a tiny helper for constructing error values in tests.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func TestCachingProvider_RepoTokens(t *testing.T) {
	ctx := context.Background()

	t.Run("one mint per repo, case-insensitive", func(t *testing.T) {
		inner := &fakeProvider{tokens: []string{"broad"}, expiry: time.Now().Add(45 * time.Minute)}
		cp := NewCachingProvider(inner)

		first, _, err := cp.GenerateRepoToken(ctx, "Widgets")
		require.NoError(t, err)
		second, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		assert.Equal(t, first, second)
		assert.Equal(t, int32(1), inner.repoCalls.Load())

		other, _, err := cp.GenerateRepoToken(ctx, "gadgets")
		require.NoError(t, err)
		assert.NotEqual(t, first, other)
		assert.Equal(t, int32(2), inner.repoCalls.Load())
	})

	t.Run("scoped and broad slots are separate", func(t *testing.T) {
		inner := &fakeProvider{tokens: []string{"broad"}, expiry: time.Now().Add(45 * time.Minute)}
		cp := NewCachingProvider(inner)

		scoped, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		assert.NotEqual(t, "broad", scoped)
		assert.Zero(t, inner.calls.Load())

		broad, _, err := cp.GenerateToken(ctx)
		require.NoError(t, err)
		assert.Equal(t, "broad", broad)
		assert.Equal(t, int32(1), inner.calls.Load())

		again, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		assert.Equal(t, scoped, again)
	})

	t.Run("inner error is returned and not cached", func(t *testing.T) {
		inner := &fakeProvider{tokens: []string{"broad"}, expiry: time.Now().Add(45 * time.Minute), repoFailErr: errors.New("boom")}
		cp := NewCachingProvider(inner)

		token, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.Error(t, err)
		assert.Empty(t, token)

		token, _, err = cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		assert.NotEmpty(t, token)
		assert.Equal(t, int32(2), inner.repoCalls.Load())
	})

	t.Run("expiry within skew re-mints", func(t *testing.T) {
		inner := &fakeProvider{tokens: []string{"broad"}, expiry: time.Now().Add(2 * time.Minute)}
		cp := NewCachingProvider(inner)

		first, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		second, _, err := cp.GenerateRepoToken(ctx, "widgets")
		require.NoError(t, err)
		assert.NotEqual(t, first, second)
		assert.Equal(t, int32(2), inner.repoCalls.Load())
	})
}

func TestCachingProvider_GenerateRepoToken_DoesNotCacheNonExpiringToken(t *testing.T) {
	pat, err := NewPATProvider("pat-token")
	require.NoError(t, err)

	c := NewCachingProvider(pat)

	for i := range 5 {
		tok, exp, err := c.GenerateRepoToken(context.Background(), fmt.Sprintf("repo-%d", i))
		require.NoError(t, err)
		assert.Equal(t, "pat-token", tok)
		assert.True(t, exp.Equal(patExpiry))
	}

	c.repoMu.Lock()
	defer c.repoMu.Unlock()

	assert.Empty(t, c.repoTokens)
}
