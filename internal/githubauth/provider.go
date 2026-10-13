// Package githubauth provides the GitHub authentication primitives of the
// ContextMatrix server. It exposes a TokenGenerator interface and three
// implementations: AppProvider for GitHub App credentials, PATProvider for
// fine-grained personal access tokens, and CachingProvider for amortizing
// App-token minting across calls.
package githubauth

import (
	"context"
	"time"
)

// TokenGenerator returns a usable git/REST credential and the time at
// which that credential expires. Implementations must be safe for
// concurrent use.
type TokenGenerator interface {
	GenerateToken(ctx context.Context) (token string, expiresAt time.Time, err error)
}

// Option configures an AppProvider.
type Option func(*AppProvider)

// CacheOption configures a CachingProvider.
type CacheOption func(*CachingProvider)
