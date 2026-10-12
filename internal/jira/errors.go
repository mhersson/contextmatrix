package jira

import (
	"fmt"
	"time"
)

// AuthError is a 401 response. It wraps ErrUnauthorized.
type AuthError struct {
	captcha bool
}

func (e *AuthError) Error() string {
	if e.captcha {
		return "jira: unauthorized: a login CAPTCHA blocks basic auth for this account; sign in to Jira in a browser once to clear it"
	}

	return ErrUnauthorized.Error()
}

func (e *AuthError) Unwrap() error { return ErrUnauthorized }

// CaptchaRequired reports whether Jira refused basic auth because the account
// must pass a login CAPTCHA first (X-Seraph-LoginReason: AUTHENTICATION_DENIED).
func (e *AuthError) CaptchaRequired() bool { return e.captcha }

// RateLimitError is a 429 the client did not wait out within its budget. It
// wraps ErrRateLimited.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("jira: rate limited, retry after %s", e.RetryAfter)
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// StatusError is any non-2xx response other than 401, 404 and 429. Message is
// Jira's own error text, bounded; it never carries request data.
type StatusError struct {
	StatusCode int
	Message    string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("jira: status %d: %s", e.StatusCode, e.Message)
}
