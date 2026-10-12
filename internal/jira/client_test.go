package jira

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "s3cr3t-token"

// newTestClient returns a client against h whose 429 waits are recorded
// instead of slept.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	auth, err := NewAuthorizer("basic", "me@acme.com", testToken)
	require.NoError(t, err)

	// A trailing slash on the base URL must not produce "//rest".
	c := NewClient(srv.URL+"/", auth, srv.Client())

	var (
		mu     sync.Mutex
		sleeps []time.Duration
	)

	c.sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()

		sleeps = append(sleeps, d)

		return nil
	}

	return c, &sleeps
}

func TestClient_SendsHeaders(t *testing.T) {
	var got *http.Request

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		_, _ = io.Copy(io.Discard, r.Body)

		w.WriteHeader(http.StatusNoContent)
	})

	require.NoError(t, c.do(t.Context(), http.MethodPost, "/rest/api/3/x", nil, map[string]string{"a": "b"}, nil))
	assert.Equal(t, "/rest/api/3/x", got.URL.Path)
	assert.Equal(t, "application/json", got.Header.Get("Accept"))
	assert.Equal(t, "application/json", got.Header.Get("Content-Type"))
	assert.Equal(t, "contextmatrix", got.Header.Get("User-Agent"))
	assert.NotEmpty(t, got.Header.Get("Authorization"))

	require.NoError(t, c.do(t.Context(), http.MethodGet, "/rest/api/3/y", nil, nil, nil))
	assert.Empty(t, got.Header.Get("Content-Type"), "GET carries no body")
}

func TestClient_StatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header map[string]string
		body   string
		check  func(t *testing.T, err error)
	}{
		{
			name: "401", status: http.StatusUnauthorized,
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorIs(t, err, ErrUnauthorized)
				authErr, ok := errors.AsType[*AuthError](err)
				require.True(t, ok)
				assert.False(t, authErr.CaptchaRequired())
			},
		},
		{
			name: "401 captcha", status: http.StatusUnauthorized,
			header: map[string]string{"X-Seraph-LoginReason": "AUTHENTICATION_DENIED"},
			check: func(t *testing.T, err error) {
				t.Helper()

				authErr, ok := errors.AsType[*AuthError](err)
				require.True(t, ok)
				assert.True(t, authErr.CaptchaRequired())
			},
		},
		{
			name: "404", status: http.StatusNotFound,
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorIs(t, err, ErrNotFound)
			},
		},
		{
			name: "400 carries jira messages", status: http.StatusBadRequest,
			body: `{"errorMessages":["Transition id '9' is not valid"],"errors":{"resolution":"Field is required"}}`,
			check: func(t *testing.T, err error) {
				t.Helper()

				se, ok := errors.AsType[*StatusError](err)
				require.True(t, ok)
				assert.Equal(t, http.StatusBadRequest, se.StatusCode)
				assert.Contains(t, se.Message, "not valid")
				assert.Contains(t, se.Message, "resolution")
			},
		},
		{
			name: "503 without body", status: http.StatusServiceUnavailable,
			check: func(t *testing.T, err error) {
				t.Helper()

				se, ok := errors.AsType[*StatusError](err)
				require.True(t, ok)
				assert.Equal(t, http.StatusServiceUnavailable, se.StatusCode)
				assert.NotEmpty(t, se.Message)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}

				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})

			err := c.do(t.Context(), http.MethodGet, "/rest/api/3/myself", nil, nil, nil)
			require.Error(t, err)
			tt.check(t, err)
			assert.NotContains(t, err.Error(), testToken)
		})
	}
}

func TestClient_RateLimit(t *testing.T) {
	// Real wall time (truncated so an HTTP-date round-trips exactly): the
	// deadline case builds a real ctx deadline from it, which must lie ahead.
	fixedNow := time.Now().Truncate(time.Second)

	tests := []struct {
		name       string
		retryAfter []string // one per 429 response before a 200; "-" means always 429
		ctxTimeout time.Duration
		wantSleeps []time.Duration
		wantErr    time.Duration // RetryAfter on the returned *RateLimitError; 0 = success
	}{
		{name: "waits seconds then succeeds", retryAfter: []string{"2"}, wantSleeps: []time.Duration{2 * time.Second}},
		{name: "missing header uses default", retryAfter: []string{""}, wantSleeps: []time.Duration{5 * time.Second}},
		{
			name: "http-date header", retryAfter: []string{fixedNow.Add(3 * time.Second).UTC().Format(http.TimeFormat)},
			wantSleeps: []time.Duration{3 * time.Second},
		},
		{
			name: "no deadline caps total waiting at 30s", retryAfter: []string{"-", "20"},
			wantSleeps: []time.Duration{20 * time.Second}, wantErr: 20 * time.Second,
		},
		{
			name: "deadline shorter than wait fails fast", retryAfter: []string{"-", "10"}, ctxTimeout: 5 * time.Second,
			wantErr: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				calls int
			)

			c, sleeps := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				mu.Lock()
				defer mu.Unlock()

				calls++

				always := tt.retryAfter[0] == "-"
				if always || calls <= len(tt.retryAfter) {
					v := tt.retryAfter[len(tt.retryAfter)-1]
					if !always {
						v = tt.retryAfter[calls-1]
					}

					if v != "" {
						w.Header().Set("Retry-After", v)
					}

					w.WriteHeader(http.StatusTooManyRequests)

					return
				}

				_, _ = io.WriteString(w, `{}`)
			})
			c.now = func() time.Time { return fixedNow }

			ctx := t.Context()

			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc

				ctx, cancel = context.WithDeadline(ctx, fixedNow.Add(tt.ctxTimeout))
				defer cancel()
			}

			err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, nil, &struct{}{})

			if tt.wantErr == 0 {
				require.NoError(t, err)
			} else {
				rl, ok := errors.AsType[*RateLimitError](err)
				require.True(t, ok, "want *RateLimitError, got %v", err)
				require.ErrorIs(t, err, ErrRateLimited)
				assert.Equal(t, tt.wantErr, rl.RetryAfter)
			}

			assert.Equal(t, tt.wantSleeps, nilIfEmpty(*sleeps))
		})
	}
}

func nilIfEmpty(d []time.Duration) []time.Duration {
	if len(d) == 0 {
		return nil
	}

	return d
}

func TestClient_RateLimitZeroRetryAfterIsBounded(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)

	c, sleeps := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		calls++

		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	err := c.do(t.Context(), http.MethodGet, "/rest/api/3/myself", nil, nil, nil)

	_, ok := errors.AsType[*RateLimitError](err)
	require.True(t, ok, "want *RateLimitError, got %v", err)
	require.ErrorIs(t, err, ErrRateLimited)

	mu.Lock()
	defer mu.Unlock()

	assert.LessOrEqual(t, calls, 31)
	assert.NotEmpty(t, *sleeps)

	for _, d := range *sleeps {
		assert.GreaterOrEqual(t, d, time.Second)
	}
}

func TestClient_EncodesQuery(t *testing.T) {
	var got url.Values

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()

		w.WriteHeader(http.StatusNoContent)
	})

	q := url.Values{"jql": {"project = A & status = \"Done\""}, "maxResults": {"50"}}
	require.NoError(t, c.do(t.Context(), http.MethodGet, "/rest/api/3/search/jql", q, nil, nil))
	assert.Equal(t, q, got)
}
