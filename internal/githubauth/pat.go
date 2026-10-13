package githubauth

import (
	"context"
	"errors"
	"time"
)

// patExpiry is the sentinel "expiry" returned for PAT-mode tokens. PATs
// don't have a server-managed TTL the way App-installation tokens do;
// the sentinel signals to CachingProvider that the cached value is
// always fresh.
var patExpiry = time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)

// PATProvider returns a static fine-grained personal access token.
type PATProvider struct {
	token string
}

// Compile-time check that PATProvider implements TokenGenerator.
var _ TokenGenerator = (*PATProvider)(nil)

// NewPATProvider constructs a PATProvider. Returns an error if token is
// empty.
func NewPATProvider(token string) (*PATProvider, error) {
	if token == "" {
		return nil, errors.New("github pat token is required")
	}

	return &PATProvider{token: token}, nil
}

// GenerateToken returns the configured PAT and a far-future expiry. The
// context is accepted for interface compatibility but unused.
func (p *PATProvider) GenerateToken(_ context.Context) (string, time.Time, error) {
	return p.token, patExpiry, nil
}
