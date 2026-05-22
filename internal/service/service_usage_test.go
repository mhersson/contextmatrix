package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPriceTokensHelper verifies the cache-tier multiplier arithmetic for the
// package-level PriceTokens helper.
func TestPriceTokensHelper(t *testing.T) {
	t.Parallel()

	rate := ModelRate{Prompt: 0.000003, Completion: 0.000015}

	tests := []struct {
		name        string
		prompt      int64
		cacheRead   int64
		cacheCreate int64
		completion  int64
		wantApprox  float64
	}{
		{
			name:       "prompt only",
			prompt:     1000,
			wantApprox: 1000 * 0.000003,
		},
		{
			name:       "completion only",
			completion: 500,
			wantApprox: 500 * 0.000015,
		},
		{
			name:      "cache read discount",
			cacheRead: 1000,
			// cache_read is billed at 0.10× the prompt rate
			wantApprox: 1000 * 0.000003 * 0.10,
		},
		{
			name:        "cache creation surcharge",
			cacheCreate: 1000,
			// cache_creation is billed at 1.25× the prompt rate
			wantApprox: 1000 * 0.000003 * 1.25,
		},
		{
			name:        "all tiers combined",
			prompt:      1000,
			cacheRead:   2000,
			cacheCreate: 500,
			completion:  300,
			wantApprox: 1000*0.000003 +
				2000*0.000003*0.10 +
				500*0.000003*1.25 +
				300*0.000015,
		},
		{
			name:       "zero tokens",
			wantApprox: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := PriceTokens(rate, tc.prompt, tc.cacheRead, tc.cacheCreate, tc.completion)
			assert.InDelta(t, tc.wantApprox, got, 1e-12)
		})
	}
}

// TestCardServicePriceTokens verifies that the CardService.PriceTokens method
// returns (cost, true) for a known model and (0, false) for an unknown one.
func TestCardServicePriceTokens(t *testing.T) {
	t.Parallel()

	svc, _, cleanup := setupTest(t)
	defer cleanup()

	// Inject a known model into tokenCosts so we can assert correct delegation.
	svc.tokenCosts = map[string]ModelRate{
		"test-model": {Prompt: 0.000003, Completion: 0.000015},
	}

	t.Run("known model returns cost and true", func(t *testing.T) {
		t.Parallel()

		cost, ok := svc.PriceTokens("test-model", 1000, 0, 0, 500)
		require.True(t, ok)

		want := PriceTokens(ModelRate{Prompt: 0.000003, Completion: 0.000015}, 1000, 0, 0, 500)
		assert.InDelta(t, want, cost, 1e-12)
	})

	t.Run("unknown model returns zero and false", func(t *testing.T) {
		t.Parallel()

		cost, ok := svc.PriceTokens("not-a-real-model", 1000, 0, 0, 500)
		assert.False(t, ok)
		assert.InDelta(t, 0.0, cost, 1e-12)
	})
}
